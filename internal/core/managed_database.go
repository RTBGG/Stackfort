// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/RTBGG/stackfort/internal/databaseidentity"
	"github.com/RTBGG/stackfort/internal/store"
)

const (
	managedDatabaseEnvelopeDomain    = "mariadb.principal.v1"
	managedDatabaseOperationKind     = "database.lifecycle"
	managedDatabaseSecretRandomBytes = 24
)

// PrepareDatabaseWizard atomically creates the safe control records, encrypted
// credential, mutation correlation, and durable operation. A replay returns the
// original records and never creates another MariaDB principal.
func (r *Repository) PrepareDatabaseWizard(
	ctx context.Context,
	params PrepareDatabaseWizardParams,
) (ManagedDatabaseProvisioning, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	if err := validateID(params.ActorID, "actorId"); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	if err := databaseidentity.ValidateAlias(params.DatabaseAlias); err != nil {
		return ManagedDatabaseProvisioning{}, fmt.Errorf("%w: databaseAlias is invalid", ErrInvalidInput)
	}
	if params.Preset != DatabaseGrantReadOnly && params.Preset != DatabaseGrantReadWrite {
		return ManagedDatabaseProvisioning{}, fmt.Errorf("%w: database grant preset is invalid", ErrInvalidInput)
	}
	if params.ExistingUserID == nil {
		if err := databaseidentity.ValidateAlias(params.NewUserAlias); err != nil {
			return ManagedDatabaseProvisioning{}, fmt.Errorf("%w: newUserAlias is invalid", ErrInvalidInput)
		}
	} else {
		if err := validateID(*params.ExistingUserID, "existingUserId"); err != nil {
			return ManagedDatabaseProvisioning{}, err
		}
		if params.NewUserAlias != "" {
			return ManagedDatabaseProvisioning{}, fmt.Errorf("%w: select an existing user or supply a new alias", ErrInvalidInput)
		}
	}

	databaseID, err := r.newID()
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	userID := ID("")
	if params.ExistingUserID != nil {
		userID = *params.ExistingUserID
	} else if userID, err = r.newID(); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	grantID, err := r.newID()
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	payload := map[string]any{
		"action": "provision", "databaseAlias": params.DatabaseAlias,
		"preset": string(params.Preset),
	}
	if params.ExistingUserID == nil {
		payload["newUserAlias"] = params.NewUserAlias
	} else {
		payload["existingUserId"] = string(*params.ExistingUserID)
	}
	actorID, accountID := params.ActorID, params.AccountID
	operation, payloadJSON, err := r.prepareOperation(CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: managedDatabaseOperationKind,
		RetryClass: RetrySafe, RequestID: params.RequestID, IdempotencyKey: params.IdempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}

	physicalDatabase, err := databaseidentity.Derive(string(params.AccountID), params.DatabaseAlias)
	if err != nil {
		return ManagedDatabaseProvisioning{}, fmt.Errorf("%w: database name derivation failed", ErrInvalidInput)
	}
	var password []byte
	var envelope encryptedSecretEnvelope
	if params.ExistingUserID == nil {
		physicalUser, deriveErr := databaseidentity.Derive(string(params.AccountID), params.NewUserAlias)
		if deriveErr != nil {
			return ManagedDatabaseProvisioning{}, fmt.Errorf("%w: database user name derivation failed", ErrInvalidInput)
		}
		_ = physicalUser
		password, err = r.generateManagedDatabasePassword()
		if err != nil {
			return ManagedDatabaseProvisioning{}, err
		}
		defer clear(password)
		envelope, err = r.encryptSecret(managedDatabaseEnvelopeDomain, userID, params.AccountID, password)
		if err != nil {
			return ManagedDatabaseProvisioning{}, err
		}
	}

	now := operation.CreatedAt
	provisioning := ManagedDatabaseProvisioning{Operation: operation}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		createdOperation, replayed, createErr := r.createOperationTx(ctx, executor, operation, payloadJSON, now)
		if createErr != nil {
			return createErr
		}
		provisioning.Operation = createdOperation
		if replayed {
			var loadErr error
			provisioning, loadErr = loadManagedDatabaseProvisioningTx(
				ctx, executor, params.AccountID, createdOperation.ID,
			)
			return loadErr
		}

		limits, limitsErr := currentPackageLimitsTx(ctx, executor, params.AccountID)
		if limitsErr != nil {
			return limitsErr
		}
		var databaseCount, userCount int64
		if err := executor.QueryRowContext(ctx, `
			SELECT
			  (SELECT COUNT(*) FROM managed_databases WHERE account_id = ? AND removed_at IS NULL),
			  (SELECT COUNT(*) FROM managed_database_users WHERE account_id = ? AND removed_at IS NULL)`,
			string(params.AccountID), string(params.AccountID)).Scan(&databaseCount, &userCount); err != nil {
			return err
		}
		if limits.MaxDatabases == 0 || databaseCount >= limits.MaxDatabases {
			return fmt.Errorf("%w: package database limit reached", ErrConflict)
		}
		if params.ExistingUserID == nil && (limits.MaxDatabaseUsers == 0 || userCount >= limits.MaxDatabaseUsers) {
			return fmt.Errorf("%w: package database user limit reached", ErrConflict)
		}

		provisioning.Database = ManagedDatabase{
			ID: databaseID, AccountID: params.AccountID, Alias: params.DatabaseAlias,
			PhysicalName: physicalDatabase, Status: ManagedDatabasePending,
			CreatedAt: now, UpdatedAt: now,
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO managed_databases (
				id, account_id, alias, physical_name, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
			string(databaseID), string(params.AccountID), params.DatabaseAlias, physicalDatabase,
			formatTime(now), formatTime(now)); err != nil {
			return err
		}

		if params.ExistingUserID == nil {
			physicalUser, _ := databaseidentity.Derive(string(params.AccountID), params.NewUserAlias)
			provisioning.DatabaseUser = ManagedDatabaseUser{
				ID: userID, AccountID: params.AccountID, Alias: params.NewUserAlias,
				PhysicalName: physicalUser, Host: databaseidentity.LocalHost,
				Status: ManagedDatabasePending, CreatedAt: now, UpdatedAt: now,
			}
			if _, err := executor.ExecContext(ctx, `
				INSERT INTO managed_database_users (
					id, account_id, alias, physical_name, host, status,
					password_ciphertext, password_nonce, password_wrapped_key,
					password_wrap_nonce, password_key_version, created_at, updated_at
				) VALUES (?, ?, ?, ?, 'localhost', 'pending', ?, ?, ?, ?, ?, ?, ?)`,
				string(userID), string(params.AccountID), params.NewUserAlias, physicalUser,
				envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey, envelope.WrapNonce,
				envelope.KeyVersion, formatTime(now), formatTime(now)); err != nil {
				return err
			}
		} else {
			var loadErr error
			provisioning.DatabaseUser, loadErr = findManagedDatabaseUserTx(
				ctx, executor, params.AccountID, userID, false,
			)
			if loadErr != nil {
				return loadErr
			}
			if provisioning.DatabaseUser.Status != ManagedDatabaseActive {
				return fmt.Errorf("%w: selected database user is not active", ErrConflict)
			}
		}

		provisioning.Grant = ManagedDatabaseGrant{
			ID: grantID, AccountID: params.AccountID, DatabaseID: databaseID,
			DatabaseUserID: userID, Preset: params.Preset, Status: "pending",
			CreatedAt: now, UpdatedAt: now,
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO managed_database_grants (
				id, account_id, database_id, database_user_id, preset, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`,
			string(grantID), string(params.AccountID), string(databaseID), string(userID),
			string(params.Preset), formatTime(now), formatTime(now)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO managed_database_mutations (
				operation_id, account_id, action, database_id, database_user_id, grant_id, created_at
			) VALUES (?, ?, 'provision', ?, ?, ?, ?)`,
			string(operation.ID), string(params.AccountID), string(databaseID), string(userID),
			string(grantID), formatTime(now)); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "database.wizard_prepared",
			TargetType: "managed_database", TargetID: string(databaseID),
			AccountID: &params.AccountID, RequestID: operation.RequestID,
			OperationID: &operation.ID, Result: AuditSuccess,
			Details: map[string]any{"alias": params.DatabaseAlias, "preset": params.Preset},
		}, now)
	})
	if err != nil {
		return ManagedDatabaseProvisioning{}, classifyDatabaseError(err)
	}
	return provisioning, nil
}

func (r *Repository) generateManagedDatabasePassword() ([]byte, error) {
	random := make([]byte, managedDatabaseSecretRandomBytes)
	if _, err := io.ReadFull(r.random, random); err != nil {
		return nil, fmt.Errorf("generate managed database password: %w", err)
	}
	defer clear(random)
	encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(random)))
	base64.RawURLEncoding.Encode(encoded, random)
	return encoded, nil
}

func currentPackageLimitsTx(ctx context.Context, reader store.Reader, accountID ID) (PackageLimits, error) {
	var limitsJSON string
	if err := reader.QueryRowContext(ctx, `
		SELECT a.effective_limits_json
		FROM hosting_accounts h
		JOIN account_package_assignments a
		  ON a.account_id = h.id AND a.id = h.current_package_assignment_id
		WHERE h.id = ? AND h.status = 'active' AND a.superseded_at IS NULL`,
		string(accountID)).Scan(&limitsJSON); err != nil {
		return PackageLimits{}, err
	}
	return decodeLimits(limitsJSON)
}

// LoadDatabaseProvisioning is an internal worker boundary. It is not exposed
// by the HTTP API and returns the decrypted password only in caller-owned
// memory.
func (r *Repository) LoadDatabaseProvisioning(
	ctx context.Context,
	accountID, operationID ID,
) (ManagedDatabaseProvisioning, DatabaseCredential, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return ManagedDatabaseProvisioning{}, DatabaseCredential{}, err
	}
	if err := validateID(operationID, "operationId"); err != nil {
		return ManagedDatabaseProvisioning{}, DatabaseCredential{}, err
	}
	var provisioning ManagedDatabaseProvisioning
	var envelope encryptedSecretEnvelope
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var loadErr error
		provisioning, loadErr = loadManagedDatabaseProvisioningTx(ctx, reader, accountID, operationID)
		if loadErr != nil {
			return loadErr
		}
		if provisioning.DatabaseUser.Status != ManagedDatabasePending {
			return nil
		}
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext, password_nonce, password_wrapped_key,
			       password_wrap_nonce, password_key_version
			FROM managed_database_users
			WHERE account_id = ? AND id = ? AND removed_at IS NULL`,
			string(accountID), string(provisioning.DatabaseUser.ID)).Scan(
			&envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey,
			&envelope.WrapNonce, &envelope.KeyVersion,
		)
	})
	if err != nil {
		return ManagedDatabaseProvisioning{}, DatabaseCredential{}, classifyDatabaseError(err)
	}
	if provisioning.DatabaseUser.Status != ManagedDatabasePending {
		return provisioning, DatabaseCredential{
			AccountID: accountID, UserID: provisioning.DatabaseUser.ID,
			Username: provisioning.DatabaseUser.PhysicalName, Host: provisioning.DatabaseUser.Host,
		}, nil
	}
	password, err := r.decryptSecret(
		managedDatabaseEnvelopeDomain, provisioning.DatabaseUser.ID, accountID, envelope,
	)
	if err != nil {
		return ManagedDatabaseProvisioning{}, DatabaseCredential{}, err
	}
	return provisioning, DatabaseCredential{
		AccountID: accountID, UserID: provisioning.DatabaseUser.ID,
		Username: provisioning.DatabaseUser.PhysicalName,
		Host:     provisioning.DatabaseUser.Host, Password: password,
	}, nil
}

func (r *Repository) CompleteDatabaseProvisioning(
	ctx context.Context,
	params CompleteDatabaseProvisioningParams,
) (ManagedDatabaseProvisioning, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	now := r.timestamp()
	var provisioning ManagedDatabaseProvisioning
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var loadErr error
		provisioning, loadErr = loadManagedDatabaseProvisioningTx(
			ctx, executor, params.AccountID, params.OperationID,
		)
		if loadErr != nil {
			return loadErr
		}
		var appliedAt sql.NullString
		if err := executor.QueryRowContext(ctx, `
			SELECT applied_at FROM managed_database_mutations
			WHERE account_id = ? AND operation_id = ? AND action = 'provision'`,
			string(params.AccountID), string(params.OperationID)).Scan(&appliedAt); err != nil {
			return err
		}
		if appliedAt.Valid {
			return nil
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE managed_databases SET status = 'active', updated_at = ?
			WHERE account_id = ? AND id = ? AND status = 'pending'`,
			formatTime(now), string(params.AccountID), string(provisioning.Database.ID)); err != nil {
			return err
		}
		if provisioning.DatabaseUser.Status == ManagedDatabasePending {
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_database_users SET status = 'active', updated_at = ?
				WHERE account_id = ? AND id = ? AND status = 'pending'`,
				formatTime(now), string(params.AccountID), string(provisioning.DatabaseUser.ID)); err != nil {
				return err
			}
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE managed_database_grants SET status = 'active', updated_at = ?
			WHERE account_id = ? AND id = ? AND status = 'pending'`,
			formatTime(now), string(params.AccountID), string(provisioning.Grant.ID)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE managed_database_mutations SET applied_at = ?
			WHERE account_id = ? AND operation_id = ? AND applied_at IS NULL`,
			formatTime(now), string(params.AccountID), string(params.OperationID)); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "database.provisioned",
			TargetType: "managed_database", TargetID: string(provisioning.Database.ID),
			AccountID: &params.AccountID, RequestID: requestID,
			OperationID: &params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"alias": provisioning.Database.Alias, "preset": provisioning.Grant.Preset},
		}, now)
	})
	if err != nil {
		return ManagedDatabaseProvisioning{}, classifyDatabaseError(err)
	}
	return loadManagedDatabaseProvisioning(r, ctx, params.AccountID, params.OperationID)
}

// PrepareDatabaseDeletion requires the exact visible alias and creates one
// fenced, replay-safe drop operation. A database deletion explicitly revokes
// its live grants; a user deletion is rejected while any live grant exists.
func (r *Repository) PrepareDatabaseDeletion(
	ctx context.Context,
	params PrepareDatabaseDeletionParams,
) (ManagedDatabaseDeletion, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	if err := validateID(params.TargetID, "targetId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	if err := validateID(params.ActorID, "actorId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	if params.TargetKind != DatabaseDeletionDatabase && params.TargetKind != DatabaseDeletionUser {
		return ManagedDatabaseDeletion{}, fmt.Errorf("%w: deletion target is invalid", ErrInvalidInput)
	}
	confirmation, err := validateText(params.Confirmation, "confirmation", 1, databaseidentity.AliasMaximumBytes)
	if err != nil || confirmation != params.Confirmation {
		return ManagedDatabaseDeletion{}, fmt.Errorf("%w: confirmation is invalid", ErrInvalidInput)
	}

	// Validate the typed confirmation before touching the idempotency table, so
	// even a caller that knows a prior key cannot bypass the confirmation gate.
	err = r.state.Read(ctx, func(reader store.Reader) error {
		var alias string
		var loadErr error
		if params.TargetKind == DatabaseDeletionDatabase {
			var target ManagedDatabase
			target, loadErr = findManagedDatabaseTx(ctx, reader, params.AccountID, params.TargetID, false)
			alias = target.Alias
		} else {
			var target ManagedDatabaseUser
			target, loadErr = findManagedDatabaseUserTx(ctx, reader, params.AccountID, params.TargetID, false)
			alias = target.Alias
		}
		if loadErr != nil {
			return loadErr
		}
		if alias != confirmation {
			return fmt.Errorf("%w: typed confirmation does not match", ErrConflict)
		}
		return nil
	})
	if err != nil {
		return ManagedDatabaseDeletion{}, classifyDatabaseError(err)
	}

	action := "drop_database"
	payload := map[string]any{"action": action, "databaseId": string(params.TargetID)}
	if params.TargetKind == DatabaseDeletionUser {
		action = "drop_user"
		payload = map[string]any{"action": action, "databaseUserId": string(params.TargetID)}
	}
	actorID, accountID := params.ActorID, params.AccountID
	operation, payloadJSON, err := r.prepareOperation(CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: managedDatabaseOperationKind,
		RetryClass: RetrySafe, RequestID: params.RequestID, IdempotencyKey: params.IdempotencyKey,
		Payload: payload, MaxAttempts: 3,
	})
	if err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	now := operation.CreatedAt
	deletion := ManagedDatabaseDeletion{Operation: operation, Kind: params.TargetKind}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		createdOperation, replayed, createErr := r.createOperationTx(ctx, executor, operation, payloadJSON, now)
		if createErr != nil {
			return createErr
		}
		deletion.Operation = createdOperation
		if replayed {
			var loadErr error
			deletion, loadErr = loadManagedDatabaseDeletionTx(ctx, executor, params.AccountID, createdOperation.ID)
			return loadErr
		}

		if params.TargetKind == DatabaseDeletionDatabase {
			target, loadErr := findManagedDatabaseTx(ctx, executor, params.AccountID, params.TargetID, false)
			if loadErr != nil {
				return loadErr
			}
			if target.Alias != confirmation || target.Status != ManagedDatabaseActive {
				return fmt.Errorf("%w: database is not deletable", ErrConflict)
			}
			deletion.Database = &target
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_databases SET status = 'deleting', updated_at = ?
				WHERE account_id = ? AND id = ? AND status = 'active'`,
				formatTime(now), string(params.AccountID), string(params.TargetID)); err != nil {
				return err
			}
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_database_grants SET status = 'revoking', updated_at = ?
				WHERE account_id = ? AND database_id = ? AND revoked_at IS NULL AND status = 'active'`,
				formatTime(now), string(params.AccountID), string(params.TargetID)); err != nil {
				return err
			}
			if _, err := executor.ExecContext(ctx, `
				INSERT INTO managed_database_mutations (
					operation_id, account_id, action, database_id, created_at
				) VALUES (?, ?, 'drop_database', ?, ?)`,
				string(operation.ID), string(params.AccountID), string(params.TargetID), formatTime(now)); err != nil {
				return err
			}
		} else {
			target, loadErr := findManagedDatabaseUserTx(ctx, executor, params.AccountID, params.TargetID, false)
			if loadErr != nil {
				return loadErr
			}
			if target.Alias != confirmation || target.Status != ManagedDatabaseActive {
				return fmt.Errorf("%w: database user is not deletable", ErrConflict)
			}
			var liveGrants, pendingRotations int64
			if err := executor.QueryRowContext(ctx, `
				SELECT
				  (SELECT COUNT(*) FROM managed_database_grants
				   WHERE account_id = ? AND database_user_id = ? AND revoked_at IS NULL),
				  (SELECT COUNT(*) FROM managed_database_credential_rotations
				   WHERE account_id = ? AND database_user_id = ? AND applied_at IS NULL)`,
				string(params.AccountID), string(params.TargetID),
				string(params.AccountID), string(params.TargetID)).Scan(&liveGrants, &pendingRotations); err != nil {
				return err
			}
			if liveGrants != 0 {
				return fmt.Errorf("%w: revoke database grants before deleting the user", ErrConflict)
			}
			if pendingRotations != 0 {
				return fmt.Errorf("%w: complete the database credential rotation before deleting the user", ErrConflict)
			}
			deletion.User = &target
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_database_users SET status = 'deleting', updated_at = ?
				WHERE account_id = ? AND id = ? AND status = 'active'`,
				formatTime(now), string(params.AccountID), string(params.TargetID)); err != nil {
				return err
			}
			if _, err := executor.ExecContext(ctx, `
				INSERT INTO managed_database_mutations (
					operation_id, account_id, action, database_user_id, created_at
				) VALUES (?, ?, 'drop_user', ?, ?)`,
				string(operation.ID), string(params.AccountID), string(params.TargetID), formatTime(now)); err != nil {
				return err
			}
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "database.deletion_prepared",
			TargetType: "managed_database_" + string(params.TargetKind), TargetID: string(params.TargetID),
			AccountID: &params.AccountID, RequestID: operation.RequestID,
			OperationID: &operation.ID, Result: AuditSuccess,
			Details: map[string]any{"confirmationMatched": true, "backupAvailable": false},
		}, now)
	})
	if err != nil {
		return ManagedDatabaseDeletion{}, classifyDatabaseError(err)
	}
	return loadManagedDatabaseDeletion(r, ctx, params.AccountID, deletion.Operation.ID)
}

func (r *Repository) LoadDatabaseDeletion(
	ctx context.Context,
	accountID, operationID ID,
) (ManagedDatabaseDeletion, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	if err := validateID(operationID, "operationId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	return loadManagedDatabaseDeletion(r, ctx, accountID, operationID)
}

func (r *Repository) CompleteDatabaseDeletion(
	ctx context.Context,
	params CompleteDatabaseDeletionParams,
) (ManagedDatabaseDeletion, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	now := r.timestamp()
	var deletion ManagedDatabaseDeletion
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var loadErr error
		deletion, loadErr = loadManagedDatabaseDeletionTx(ctx, executor, params.AccountID, params.OperationID)
		if loadErr != nil {
			return loadErr
		}
		var appliedAt sql.NullString
		if err := executor.QueryRowContext(ctx, `
			SELECT applied_at FROM managed_database_mutations
			WHERE account_id = ? AND operation_id = ? AND action IN ('drop_database', 'drop_user')`,
			string(params.AccountID), string(params.OperationID)).Scan(&appliedAt); err != nil {
			return err
		}
		if appliedAt.Valid {
			return nil
		}
		if deletion.Kind == DatabaseDeletionDatabase {
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_database_grants
				SET status = 'revoked', updated_at = ?, revoked_at = ?
				WHERE account_id = ? AND database_id = ? AND revoked_at IS NULL`,
				formatTime(now), formatTime(now), string(params.AccountID), string(deletion.Database.ID)); err != nil {
				return err
			}
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_databases
				SET status = 'deleted', updated_at = ?, removed_at = ?
				WHERE account_id = ? AND id = ? AND status = 'deleting'`,
				formatTime(now), formatTime(now), string(params.AccountID), string(deletion.Database.ID)); err != nil {
				return err
			}
		} else {
			// Destroy the only usable wrapped credential material while retaining
			// a non-secret audit tombstone for the deleted principal.
			if _, err := executor.ExecContext(ctx, `
				UPDATE managed_database_users
				SET status = 'deleted', updated_at = ?, removed_at = ?,
				    password_ciphertext = X'', password_nonce = zeroblob(12),
				    password_wrapped_key = zeroblob(48), password_wrap_nonce = zeroblob(12)
				WHERE account_id = ? AND id = ? AND status = 'deleting'`,
				formatTime(now), formatTime(now), string(params.AccountID), string(deletion.User.ID)); err != nil {
				return err
			}
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE managed_database_mutations SET applied_at = ?
			WHERE account_id = ? AND operation_id = ? AND applied_at IS NULL`,
			formatTime(now), string(params.AccountID), string(params.OperationID)); err != nil {
			return err
		}
		var targetID ID
		if deletion.Database != nil {
			targetID = deletion.Database.ID
		} else {
			targetID = deletion.User.ID
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "database.deleted",
			TargetType: "managed_database_" + string(deletion.Kind), TargetID: string(targetID),
			AccountID: &params.AccountID, RequestID: requestID,
			OperationID: &params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"tombstoneRetained": true, "targetKind": deletion.Kind},
		}, now)
	})
	if err != nil {
		return ManagedDatabaseDeletion{}, classifyDatabaseError(err)
	}
	return loadManagedDatabaseDeletion(r, ctx, params.AccountID, params.OperationID)
}

func (r *Repository) ListDatabaseWorkspace(ctx context.Context, accountID ID) (DatabaseWorkspace, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return DatabaseWorkspace{}, err
	}
	workspace := DatabaseWorkspace{
		Databases: []ManagedDatabase{}, Users: []ManagedDatabaseUser{}, Grants: []ManagedDatabaseGrant{},
	}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		databaseRows, err := reader.QueryContext(ctx, `
			SELECT id, account_id, alias, physical_name, status, created_at, updated_at, removed_at
			FROM managed_databases WHERE account_id = ? AND removed_at IS NULL
			ORDER BY alias, id`, string(accountID))
		if err != nil {
			return err
		}
		defer databaseRows.Close()
		for databaseRows.Next() {
			database, scanErr := scanManagedDatabase(databaseRows)
			if scanErr != nil {
				return scanErr
			}
			workspace.Databases = append(workspace.Databases, database)
		}
		if err := databaseRows.Err(); err != nil {
			return err
		}

		userRows, err := reader.QueryContext(ctx, `
			SELECT id, account_id, alias, physical_name, host, status,
			       (password_revealed_at IS NOT NULL), created_at, updated_at, removed_at
			FROM managed_database_users WHERE account_id = ? AND removed_at IS NULL
			ORDER BY alias, id`, string(accountID))
		if err != nil {
			return err
		}
		defer userRows.Close()
		for userRows.Next() {
			user, scanErr := scanManagedDatabaseUser(userRows)
			if scanErr != nil {
				return scanErr
			}
			workspace.Users = append(workspace.Users, user)
		}
		if err := userRows.Err(); err != nil {
			return err
		}

		grantRows, err := reader.QueryContext(ctx, `
			SELECT id, account_id, database_id, database_user_id, preset,
			       status, created_at, updated_at, revoked_at
			FROM managed_database_grants WHERE account_id = ? AND revoked_at IS NULL
			ORDER BY database_id, database_user_id`, string(accountID))
		if err != nil {
			return err
		}
		defer grantRows.Close()
		for grantRows.Next() {
			grant, scanErr := scanManagedDatabaseGrant(grantRows)
			if scanErr != nil {
				return scanErr
			}
			workspace.Grants = append(workspace.Grants, grant)
		}
		return grantRows.Err()
	})
	if err != nil {
		return DatabaseWorkspace{}, classifyDatabaseError(err)
	}
	return workspace, nil
}

// RevealDatabaseCredential consumes the encrypted password exactly once. It
// requires a fresh authenticated account credential-management decision and is
// deliberately separate from every GET/read model.
func (r *Repository) RevealDatabaseCredential(
	ctx context.Context,
	params RevealDatabaseCredentialParams,
) (RevealedDatabaseCredential, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return RevealedDatabaseCredential{}, err
	}
	if err := validateID(params.UserID, "databaseUserId"); err != nil {
		return RevealedDatabaseCredential{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return RevealedDatabaseCredential{}, err
	}
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject, Action: AuthorizationAccountCredentialsManage,
		AccountID: &params.AccountID,
	}); err != nil {
		return RevealedDatabaseCredential{}, err
	}
	now := r.timestamp()
	var username, host string
	var envelope encryptedSecretEnvelope
	var plaintext []byte
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		var platformAdministrator bool
		var accountStatus, membershipRole string
		if err := executor.QueryRowContext(ctx, `
			SELECT h.status,
			       EXISTS (
			         SELECT 1 FROM platform_role_assignments p
			         WHERE p.identity_id = ? AND p.role = 'platform_admin'
			       ),
			       COALESCE((
			         SELECT m.role FROM account_memberships m
			         WHERE m.account_id = h.id AND m.identity_id = ? AND m.revoked_at IS NULL
			       ), '')
			FROM hosting_accounts h WHERE h.id = ?`,
			string(params.Subject.identityID), string(params.Subject.identityID),
			string(params.AccountID)).Scan(&accountStatus, &platformAdministrator, &membershipRole); err != nil {
			return err
		}
		if AccountStatus(accountStatus) != AccountActive ||
			(!platformAdministrator && membershipRole != string(MembershipOwner) && membershipRole != string(MembershipMember)) {
			return ErrAuthorizationDenied
		}
		var revealedAt sql.NullString
		var provisioningSucceeded bool
		if err := executor.QueryRowContext(ctx, `
			SELECT u.physical_name, u.host, u.password_revealed_at,
			       u.password_ciphertext, u.password_nonce, u.password_wrapped_key,
			       u.password_wrap_nonce, u.password_key_version,
			       EXISTS (
			         SELECT 1
			         FROM managed_database_mutations m
			         JOIN operations o ON o.id = m.operation_id AND o.account_id = m.account_id
			         WHERE m.account_id = u.account_id
			           AND m.database_user_id = u.id
			           AND m.action = 'provision' AND m.applied_at IS NOT NULL
			           AND o.status = 'succeeded'
			       )
			FROM managed_database_users u
			WHERE u.account_id = ? AND u.id = ? AND u.status = 'active' AND u.removed_at IS NULL`,
			string(params.AccountID), string(params.UserID)).Scan(
			&username, &host, &revealedAt,
			&envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey,
			&envelope.WrapNonce, &envelope.KeyVersion, &provisioningSucceeded,
		); err != nil {
			return err
		}
		if revealedAt.Valid || !provisioningSucceeded {
			return fmt.Errorf("%w: database credential cannot be revealed", ErrConflict)
		}
		var decryptErr error
		plaintext, decryptErr = r.decryptSecret(
			managedDatabaseEnvelopeDomain, params.UserID, params.AccountID, envelope,
		)
		if decryptErr != nil {
			return decryptErr
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE managed_database_users
			SET password_revealed_at = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND password_revealed_at IS NULL`,
			formatTime(now), formatTime(now), string(params.AccountID), string(params.UserID))
		if err != nil {
			clear(plaintext)
			plaintext = nil
			return err
		}
		if err := expectAffected(result); err != nil {
			clear(plaintext)
			plaintext = nil
			return fmt.Errorf("%w: database credential was already consumed", ErrConflict)
		}
		actorID := params.Subject.identityID
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &actorID, SessionID: &params.Subject.sessionID,
			Action: "database.credential_revealed", TargetType: "managed_database_user",
			TargetID: string(params.UserID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"singleUse": true},
		}, now)
	})
	if err != nil {
		clear(plaintext)
		return RevealedDatabaseCredential{}, classifyDatabaseError(err)
	}
	return RevealedDatabaseCredential{Username: username, Host: host, Password: plaintext}, nil
}

func loadManagedDatabaseProvisioning(
	r *Repository,
	ctx context.Context,
	accountID, operationID ID,
) (ManagedDatabaseProvisioning, error) {
	var provisioning ManagedDatabaseProvisioning
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		provisioning, err = loadManagedDatabaseProvisioningTx(ctx, reader, accountID, operationID)
		return err
	})
	if err != nil {
		return ManagedDatabaseProvisioning{}, classifyDatabaseError(err)
	}
	return provisioning, nil
}

func loadManagedDatabaseDeletion(
	r *Repository,
	ctx context.Context,
	accountID, operationID ID,
) (ManagedDatabaseDeletion, error) {
	var deletion ManagedDatabaseDeletion
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		deletion, err = loadManagedDatabaseDeletionTx(ctx, reader, accountID, operationID)
		return err
	})
	if err != nil {
		return ManagedDatabaseDeletion{}, classifyDatabaseError(err)
	}
	return deletion, nil
}

func loadManagedDatabaseDeletionTx(
	ctx context.Context,
	reader store.Reader,
	accountID, operationID ID,
) (ManagedDatabaseDeletion, error) {
	var action string
	var databaseID, userID sql.NullString
	if err := reader.QueryRowContext(ctx, `
		SELECT action, database_id, database_user_id
		FROM managed_database_mutations
		WHERE account_id = ? AND operation_id = ? AND action IN ('drop_database', 'drop_user')`,
		string(accountID), string(operationID)).Scan(&action, &databaseID, &userID); err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	operation, err := loadScopedOperation(ctx, reader, OperationScope{
		AccountID: &accountID, OperationID: operationID,
	})
	if err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	deletion := ManagedDatabaseDeletion{
		Operation: operation, Grants: []ManagedDatabaseGrant{}, GrantUsers: []ManagedDatabaseUser{},
	}
	if action == "drop_user" {
		deletion.Kind = DatabaseDeletionUser
		user, loadErr := findManagedDatabaseUserTx(ctx, reader, accountID, ID(userID.String), true)
		if loadErr != nil {
			return ManagedDatabaseDeletion{}, loadErr
		}
		deletion.User = &user
		return deletion, nil
	}
	if action != "drop_database" {
		return ManagedDatabaseDeletion{}, errors.New("stored managed database deletion action is invalid")
	}
	deletion.Kind = DatabaseDeletionDatabase
	database, err := findManagedDatabaseTx(ctx, reader, accountID, ID(databaseID.String), true)
	if err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	deletion.Database = &database
	rows, err := reader.QueryContext(ctx, `
		SELECT g.id, g.account_id, g.database_id, g.database_user_id, g.preset,
		       g.status, g.created_at, g.updated_at, g.revoked_at,
		       u.id, u.account_id, u.alias, u.physical_name, u.host, u.status,
		       (u.password_revealed_at IS NOT NULL), u.created_at, u.updated_at, u.removed_at
		FROM managed_database_grants g
		JOIN managed_database_users u
		  ON u.account_id = g.account_id AND u.id = g.database_user_id
		WHERE g.account_id = ? AND g.database_id = ?
		ORDER BY g.database_user_id, g.id`, string(accountID), databaseID.String)
	if err != nil {
		return ManagedDatabaseDeletion{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var grant ManagedDatabaseGrant
		var user ManagedDatabaseUser
		var preset, grantCreated, grantUpdated, userStatus, userCreated, userUpdated string
		var revokedAt, removedAt sql.NullString
		if err := rows.Scan(
			&grant.ID, &grant.AccountID, &grant.DatabaseID, &grant.DatabaseUserID, &preset,
			&grant.Status, &grantCreated, &grantUpdated, &revokedAt,
			&user.ID, &user.AccountID, &user.Alias, &user.PhysicalName, &user.Host, &userStatus,
			&user.Revealed, &userCreated, &userUpdated, &removedAt,
		); err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		grant.Preset = DatabaseGrantPreset(preset)
		grant.CreatedAt, err = parseTime(grantCreated)
		if err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		grant.UpdatedAt, err = parseTime(grantUpdated)
		if err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		grant.RevokedAt, err = parseOptionalTime(revokedAt)
		if err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		user.Status = ManagedDatabaseStatus(userStatus)
		user.CreatedAt, err = parseTime(userCreated)
		if err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		user.UpdatedAt, err = parseTime(userUpdated)
		if err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		user.RemovedAt, err = parseOptionalTime(removedAt)
		if err != nil {
			return ManagedDatabaseDeletion{}, err
		}
		if databaseidentity.ValidateDerived(string(user.AccountID), user.Alias, user.PhysicalName) != nil ||
			user.Host != databaseidentity.LocalHost {
			return ManagedDatabaseDeletion{}, errors.New("stored managed database grant user is invalid")
		}
		deletion.Grants = append(deletion.Grants, grant)
		deletion.GrantUsers = append(deletion.GrantUsers, user)
	}
	return deletion, rows.Err()
}

func loadManagedDatabaseProvisioningTx(
	ctx context.Context,
	reader store.Reader,
	accountID, operationID ID,
) (ManagedDatabaseProvisioning, error) {
	var databaseID, userID, grantID ID
	if err := reader.QueryRowContext(ctx, `
		SELECT database_id, database_user_id, grant_id
		FROM managed_database_mutations
		WHERE account_id = ? AND operation_id = ? AND action = 'provision'`,
		string(accountID), string(operationID)).Scan(&databaseID, &userID, &grantID); err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	operation, err := loadScopedOperation(ctx, reader, OperationScope{
		AccountID: &accountID, OperationID: operationID,
	})
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	database, err := findManagedDatabaseTx(ctx, reader, accountID, databaseID, true)
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	user, err := findManagedDatabaseUserTx(ctx, reader, accountID, userID, true)
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	grant, err := findManagedDatabaseGrantTx(ctx, reader, accountID, grantID)
	if err != nil {
		return ManagedDatabaseProvisioning{}, err
	}
	return ManagedDatabaseProvisioning{
		Operation: operation, Database: database, DatabaseUser: user, Grant: grant,
	}, nil
}

func findManagedDatabaseTx(
	ctx context.Context, reader store.Reader, accountID, databaseID ID, includeRemoved bool,
) (ManagedDatabase, error) {
	query := `SELECT id, account_id, alias, physical_name, status, created_at, updated_at, removed_at
		FROM managed_databases WHERE account_id = ? AND id = ?`
	if !includeRemoved {
		query += " AND removed_at IS NULL"
	}
	return scanManagedDatabase(reader.QueryRowContext(ctx, query, string(accountID), string(databaseID)))
}

func findManagedDatabaseUserTx(
	ctx context.Context, reader store.Reader, accountID, userID ID, includeRemoved bool,
) (ManagedDatabaseUser, error) {
	query := `SELECT id, account_id, alias, physical_name, host, status,
		(password_revealed_at IS NOT NULL), created_at, updated_at, removed_at
		FROM managed_database_users WHERE account_id = ? AND id = ?`
	if !includeRemoved {
		query += " AND removed_at IS NULL"
	}
	return scanManagedDatabaseUser(reader.QueryRowContext(ctx, query, string(accountID), string(userID)))
}

func findManagedDatabaseGrantTx(
	ctx context.Context, reader store.Reader, accountID, grantID ID,
) (ManagedDatabaseGrant, error) {
	return scanManagedDatabaseGrant(reader.QueryRowContext(ctx, `
		SELECT id, account_id, database_id, database_user_id, preset,
		       status, created_at, updated_at, revoked_at
		FROM managed_database_grants WHERE account_id = ? AND id = ?`,
		string(accountID), string(grantID)))
}

func scanManagedDatabase(scanner rowScanner) (ManagedDatabase, error) {
	var database ManagedDatabase
	var status, createdAt, updatedAt string
	var removedAt sql.NullString
	if err := scanner.Scan(
		&database.ID, &database.AccountID, &database.Alias, &database.PhysicalName,
		&status, &createdAt, &updatedAt, &removedAt,
	); err != nil {
		return ManagedDatabase{}, err
	}
	database.Status = ManagedDatabaseStatus(status)
	if err := databaseidentity.ValidateDerived(string(database.AccountID), database.Alias, database.PhysicalName); err != nil {
		return ManagedDatabase{}, errors.New("stored managed database identity is invalid")
	}
	var err error
	if database.CreatedAt, err = parseTime(createdAt); err != nil {
		return ManagedDatabase{}, err
	}
	if database.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ManagedDatabase{}, err
	}
	if database.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return ManagedDatabase{}, err
	}
	return database, nil
}

func scanManagedDatabaseUser(scanner rowScanner) (ManagedDatabaseUser, error) {
	var user ManagedDatabaseUser
	var status, createdAt, updatedAt string
	var removedAt sql.NullString
	if err := scanner.Scan(
		&user.ID, &user.AccountID, &user.Alias, &user.PhysicalName, &user.Host,
		&status, &user.Revealed, &createdAt, &updatedAt, &removedAt,
	); err != nil {
		return ManagedDatabaseUser{}, err
	}
	user.Status = ManagedDatabaseStatus(status)
	if user.Host != databaseidentity.LocalHost ||
		databaseidentity.ValidateDerived(string(user.AccountID), user.Alias, user.PhysicalName) != nil {
		return ManagedDatabaseUser{}, errors.New("stored managed database user identity is invalid")
	}
	var err error
	if user.CreatedAt, err = parseTime(createdAt); err != nil {
		return ManagedDatabaseUser{}, err
	}
	if user.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ManagedDatabaseUser{}, err
	}
	if user.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return ManagedDatabaseUser{}, err
	}
	return user, nil
}

func scanManagedDatabaseGrant(scanner rowScanner) (ManagedDatabaseGrant, error) {
	var grant ManagedDatabaseGrant
	var preset, createdAt, updatedAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(
		&grant.ID, &grant.AccountID, &grant.DatabaseID, &grant.DatabaseUserID,
		&preset, &grant.Status, &createdAt, &updatedAt, &revokedAt,
	); err != nil {
		return ManagedDatabaseGrant{}, err
	}
	grant.Preset = DatabaseGrantPreset(preset)
	if grant.Preset != DatabaseGrantReadOnly && grant.Preset != DatabaseGrantReadWrite {
		return ManagedDatabaseGrant{}, errors.New("stored managed database grant preset is invalid")
	}
	var err error
	if grant.CreatedAt, err = parseTime(createdAt); err != nil {
		return ManagedDatabaseGrant{}, err
	}
	if grant.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ManagedDatabaseGrant{}, err
	}
	if grant.RevokedAt, err = parseOptionalTime(revokedAt); err != nil {
		return ManagedDatabaseGrant{}, err
	}
	return grant, nil
}
