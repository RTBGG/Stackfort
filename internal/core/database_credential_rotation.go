// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

// PrepareDatabaseCredentialRotation creates a fresh candidate credential and
// a replay-safe host operation. The currently authoritative user envelope is
// deliberately not changed in this transaction.
func (r *Repository) PrepareDatabaseCredentialRotation(
	ctx context.Context,
	params PrepareDatabaseCredentialRotationParams,
) (ManagedDatabaseCredentialRotation, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	if err := validateID(params.DatabaseUserID, "databaseUserId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil || r.validateAuthorizationSubject(params.Subject) != nil {
		return ManagedDatabaseCredentialRotation{}, ErrInvalidInput
	}
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject, Action: AuthorizationAccountCredentialsManage,
		AccountID: &params.AccountID,
	}); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}

	actorID, accountID := params.Subject.identityID, params.AccountID
	operation, payloadJSON, err := r.prepareOperation(CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: managedDatabaseOperationKind,
		RetryClass: RetrySafe, RequestID: requestID, IdempotencyKey: params.IdempotencyKey,
		Payload: map[string]any{
			"action": "rotate_user", "databaseUserId": string(params.DatabaseUserID),
		},
		MaxAttempts: 3,
	})
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	password, err := r.generateManagedDatabasePassword()
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	defer clear(password)
	envelope, err := r.encryptSecret(
		managedDatabaseEnvelopeDomain, params.DatabaseUserID, params.AccountID, password,
	)
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}

	now := operation.CreatedAt
	rotation := ManagedDatabaseCredentialRotation{Operation: operation}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		createdOperation, replayed, createErr := r.createOperationTx(
			ctx, executor, operation, payloadJSON, now,
		)
		if createErr != nil {
			return createErr
		}
		rotation.Operation = createdOperation
		if replayed {
			var loadErr error
			rotation, loadErr = loadManagedDatabaseCredentialRotationTx(
				ctx, executor, params.AccountID, createdOperation.ID,
			)
			return loadErr
		}

		facts, factsErr := loadPHPMyAdminAuthorizationFacts(
			ctx, executor, params.Subject.identityID, params.Subject.sessionID, params.AccountID,
		)
		if factsErr != nil {
			return factsErr
		}
		if _, factsErr = evaluateAuthorization(
			AuthorizationAccountCredentialsManage, &params.AccountID,
			authorizationPolicies[AuthorizationAccountCredentialsManage], facts, now,
		); factsErr != nil {
			return factsErr
		}
		rotation.DatabaseUser, factsErr = findManagedDatabaseUserTx(
			ctx, executor, params.AccountID, params.DatabaseUserID, false,
		)
		if factsErr != nil {
			return factsErr
		}
		if rotation.DatabaseUser.Status != ManagedDatabaseActive {
			return fmt.Errorf("%w: database user is not active", ErrConflict)
		}
		if _, insertErr := executor.ExecContext(ctx, `
			INSERT INTO managed_database_credential_rotations (
				operation_id, account_id, database_user_id,
				password_ciphertext, password_nonce, password_wrapped_key,
				password_wrap_nonce, password_key_version, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(operation.ID), string(params.AccountID), string(params.DatabaseUserID),
			envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey,
			envelope.WrapNonce, envelope.KeyVersion, formatTime(now)); insertErr != nil {
			return insertErr
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &actorID, SessionID: &params.Subject.sessionID,
			Action:     "database.credential_rotation_prepared",
			TargetType: "managed_database_user", TargetID: string(params.DatabaseUserID),
			AccountID: &params.AccountID, RequestID: requestID,
			OperationID: &operation.ID, Result: AuditSuccess,
			Details: map[string]any{"activeGenerationChanged": false},
		}, now)
	})
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, classifyDatabaseError(err)
	}
	return rotation, nil
}

// LoadDatabaseCredentialRotation is the internal worker boundary. It returns
// the candidate plaintext only while the rotation remains unapplied.
func (r *Repository) LoadDatabaseCredentialRotation(
	ctx context.Context,
	accountID, operationID ID,
) (ManagedDatabaseCredentialRotation, DatabaseCredential, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, DatabaseCredential{}, err
	}
	if err := validateID(operationID, "operationId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, DatabaseCredential{}, err
	}
	var rotation ManagedDatabaseCredentialRotation
	var envelope encryptedSecretEnvelope
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var loadErr error
		rotation, loadErr = loadManagedDatabaseCredentialRotationTx(ctx, reader, accountID, operationID)
		if loadErr != nil || rotation.AppliedAt != nil {
			return loadErr
		}
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext, password_nonce, password_wrapped_key,
			       password_wrap_nonce, password_key_version
			FROM managed_database_credential_rotations
			WHERE account_id = ? AND operation_id = ? AND applied_at IS NULL`,
			string(accountID), string(operationID)).Scan(
			&envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey,
			&envelope.WrapNonce, &envelope.KeyVersion,
		)
	})
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, DatabaseCredential{}, classifyDatabaseError(err)
	}
	credential := DatabaseCredential{
		AccountID: accountID, UserID: rotation.DatabaseUser.ID,
		Username: rotation.DatabaseUser.PhysicalName, Host: rotation.DatabaseUser.Host,
	}
	if rotation.AppliedAt != nil {
		return rotation, credential, nil
	}
	credential.Password, err = r.decryptSecret(
		managedDatabaseEnvelopeDomain, rotation.DatabaseUser.ID, accountID, envelope,
	)
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, DatabaseCredential{}, err
	}
	return rotation, credential, nil
}

// CompleteDatabaseCredentialRotation atomically promotes the candidate after
// the host mutation succeeds, resets the one-time reveal, and revokes every
// still-outstanding phpMyAdmin handoff for the principal. Existing phpMyAdmin
// sessions hold the old MariaDB password and are therefore rejected by the
// database from this point onward.
func (r *Repository) CompleteDatabaseCredentialRotation(
	ctx context.Context,
	params CompleteDatabaseCredentialRotationParams,
) (ManagedDatabaseCredentialRotation, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	now := r.timestamp()
	var rotation ManagedDatabaseCredentialRotation
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var loadErr error
		rotation, loadErr = loadManagedDatabaseCredentialRotationTx(
			ctx, executor, params.AccountID, params.OperationID,
		)
		if loadErr != nil || rotation.AppliedAt != nil {
			return loadErr
		}
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE managed_database_users
			SET password_ciphertext = r.password_ciphertext,
			    password_nonce = r.password_nonce,
			    password_wrapped_key = r.password_wrapped_key,
			    password_wrap_nonce = r.password_wrap_nonce,
			    password_key_version = r.password_key_version,
			    password_revealed_at = NULL,
			    credential_generation = credential_generation + 1,
			    updated_at = ?
			FROM managed_database_credential_rotations r
			WHERE managed_database_users.account_id = r.account_id
			  AND managed_database_users.id = r.database_user_id
			  AND r.account_id = ? AND r.operation_id = ? AND r.applied_at IS NULL
			  AND managed_database_users.status = 'active'
			  AND managed_database_users.removed_at IS NULL`,
			formatTime(now), string(params.AccountID), string(params.OperationID))
		if updateErr != nil {
			return updateErr
		}
		if affectedErr := expectAffected(result); affectedErr != nil {
			return fmt.Errorf("%w: database credential rotation target changed", ErrConflict)
		}
		revokedResult, revokeErr := executor.ExecContext(ctx, `
			UPDATE phpmyadmin_handoffs
			SET state = 'revoked', revoked_at = ?
			WHERE account_id = ? AND database_user_id = ? AND state = 'issued'`,
			formatTime(now), string(params.AccountID), string(rotation.DatabaseUser.ID))
		if revokeErr != nil {
			return revokeErr
		}
		revoked, _ := revokedResult.RowsAffected()
		if _, updateErr = executor.ExecContext(ctx, `
			UPDATE managed_database_credential_rotations SET applied_at = ?
			WHERE account_id = ? AND operation_id = ? AND applied_at IS NULL`,
			formatTime(now), string(params.AccountID), string(params.OperationID)); updateErr != nil {
			return updateErr
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "database.credential_rotated",
			TargetType: "managed_database_user", TargetID: string(rotation.DatabaseUser.ID),
			AccountID: &params.AccountID, RequestID: requestID,
			OperationID: &params.OperationID, Result: AuditSuccess,
			Details: map[string]any{
				"generationAdvanced":         true,
				"oneTimeRevealReset":         true,
				"outstandingHandoffsRevoked": revoked,
			},
		}, now)
	})
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, classifyDatabaseError(err)
	}
	return loadManagedDatabaseCredentialRotation(r, ctx, params.AccountID, params.OperationID)
}

func loadManagedDatabaseCredentialRotation(
	r *Repository,
	ctx context.Context,
	accountID, operationID ID,
) (ManagedDatabaseCredentialRotation, error) {
	var rotation ManagedDatabaseCredentialRotation
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var loadErr error
		rotation, loadErr = loadManagedDatabaseCredentialRotationTx(ctx, reader, accountID, operationID)
		return loadErr
	})
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, classifyDatabaseError(err)
	}
	return rotation, nil
}

func loadManagedDatabaseCredentialRotationTx(
	ctx context.Context,
	reader store.Reader,
	accountID, operationID ID,
) (ManagedDatabaseCredentialRotation, error) {
	var databaseUserID ID
	var appliedAt sql.NullString
	if err := reader.QueryRowContext(ctx, `
		SELECT database_user_id, applied_at
		FROM managed_database_credential_rotations
		WHERE account_id = ? AND operation_id = ?`,
		string(accountID), string(operationID)).Scan(&databaseUserID, &appliedAt); err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	operation, err := loadScopedOperation(ctx, reader, OperationScope{
		AccountID: &accountID, OperationID: operationID,
	})
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	if operation.Kind != managedDatabaseOperationKind {
		return ManagedDatabaseCredentialRotation{}, errors.New("stored database credential rotation operation is invalid")
	}
	user, err := findManagedDatabaseUserTx(ctx, reader, accountID, databaseUserID, true)
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	parsedAppliedAt, err := parseOptionalTime(appliedAt)
	if err != nil {
		return ManagedDatabaseCredentialRotation{}, err
	}
	return ManagedDatabaseCredentialRotation{
		Operation: operation, DatabaseUser: user, AppliedAt: parsedAppliedAt,
	}, nil
}
