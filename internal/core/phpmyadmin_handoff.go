// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

const (
	PHPMyAdminHandoffAudience = "stackfort-phpmyadmin-v1"
	phpMyAdminHandoffTTL      = 30 * time.Second
	phpMyAdminHandoffBytes    = 32
)

// IssuePHPMyAdminHandoff creates a short-lived bearer whose digest is the only
// persistent representation. A replacement revokes any still-issued bearer
// for the same browser session and database principal.
func (r *Repository) IssuePHPMyAdminHandoff(
	ctx context.Context,
	params IssuePHPMyAdminHandoffParams,
) (PHPMyAdminHandoff, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return PHPMyAdminHandoff{}, ErrInvalidInput
	}
	if err := validateID(params.DatabaseUserID, "databaseUserId"); err != nil {
		return PHPMyAdminHandoff{}, ErrInvalidInput
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil || r.validateAuthorizationSubject(params.Subject) != nil {
		return PHPMyAdminHandoff{}, ErrInvalidInput
	}
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject, Action: AuthorizationAccountCredentialsManage,
		AccountID: &params.AccountID,
	}); err != nil {
		return PHPMyAdminHandoff{}, err
	}

	raw := make([]byte, phpMyAdminHandoffBytes)
	if _, err := io.ReadFull(r.random, raw); err != nil {
		return PHPMyAdminHandoff{}, fmt.Errorf("generate phpMyAdmin handoff: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	clear(raw)
	tokenHash := sha256.Sum256([]byte(token))
	handoffID, err := r.newID()
	if err != nil {
		return PHPMyAdminHandoff{}, err
	}
	now := r.timestamp()
	expiresAt := now.Add(phpMyAdminHandoffTTL)

	err = r.state.Write(ctx, func(executor store.Executor) error {
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
		var eligible bool
		if queryErr := executor.QueryRowContext(ctx, `
			SELECT EXISTS (
			  SELECT 1
			  FROM managed_database_users u
			  WHERE u.account_id = ? AND u.id = ?
			    AND u.status = 'active' AND u.removed_at IS NULL
			    AND EXISTS (
			      SELECT 1 FROM managed_database_grants g
			      WHERE g.account_id = u.account_id AND g.database_user_id = u.id
			        AND g.status = 'active' AND g.revoked_at IS NULL
			    )
			)`, string(params.AccountID), string(params.DatabaseUserID)).Scan(&eligible); queryErr != nil {
			return queryErr
		}
		if !eligible {
			return ErrNotFound
		}
		if _, updateErr := executor.ExecContext(ctx, `
			UPDATE phpmyadmin_handoffs
			SET state = 'revoked', revoked_at = ?
			WHERE session_id = ? AND database_user_id = ? AND state = 'issued'`,
			formatTime(now), string(params.Subject.sessionID), string(params.DatabaseUserID)); updateErr != nil {
			return updateErr
		}
		if _, insertErr := executor.ExecContext(ctx, `
			INSERT INTO phpmyadmin_handoffs (
			  id, token_hash, account_id, database_user_id, identity_id, session_id,
			  audience, state, expires_at, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'issued', ?, ?)`,
			string(handoffID), tokenHash[:], string(params.AccountID), string(params.DatabaseUserID),
			string(params.Subject.identityID), string(params.Subject.sessionID), PHPMyAdminHandoffAudience,
			formatTime(expiresAt), formatTime(now)); insertErr != nil {
			return insertErr
		}
		actorID, sessionID := params.Subject.identityID, params.Subject.sessionID
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &actorID, SessionID: &sessionID, Action: "phpmyadmin.handoff_issued",
			TargetType: "managed_database_user", TargetID: string(params.DatabaseUserID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{
				"audience": PHPMyAdminHandoffAudience, "expiresAt": formatTime(expiresAt),
			},
		}, now)
	})
	if err != nil {
		return PHPMyAdminHandoff{}, classifyDatabaseError(err)
	}
	return PHPMyAdminHandoff{Token: token, ExpiresAt: expiresAt}, nil
}

// RedeemPHPMyAdminHandoff atomically consumes a valid bearer and decrypts the
// selected account principal. Callers must separately authenticate the fixed
// phpMyAdmin broker audience before invoking this method.
func (r *Repository) RedeemPHPMyAdminHandoff(
	ctx context.Context,
	params RedeemPHPMyAdminHandoffParams,
) (PHPMyAdminCredential, error) {
	if params.Audience != PHPMyAdminHandoffAudience {
		return PHPMyAdminCredential{}, ErrAuthorizationDenied
	}
	raw, err := base64.RawURLEncoding.DecodeString(params.Token)
	if err != nil || len(raw) != phpMyAdminHandoffBytes ||
		base64.RawURLEncoding.EncodeToString(raw) != params.Token {
		clear(raw)
		return PHPMyAdminCredential{}, ErrNotFound
	}
	clear(raw)
	tokenHash := sha256.Sum256([]byte(params.Token))
	now := r.timestamp()
	var credential PHPMyAdminCredential
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var handoffID, accountID, databaseUserID, identityID, sessionID ID
		var audience, state, expiresAtText string
		var envelope encryptedSecretEnvelope
		if queryErr := executor.QueryRowContext(ctx, `
			SELECT h.id, h.account_id, h.database_user_id, h.identity_id, h.session_id,
			       h.audience, h.state, h.expires_at,
			       u.physical_name, u.host, u.password_ciphertext, u.password_nonce,
			       u.password_wrapped_key, u.password_wrap_nonce, u.password_key_version
			FROM phpmyadmin_handoffs h
			JOIN managed_database_users u
			  ON u.account_id = h.account_id AND u.id = h.database_user_id
			WHERE h.token_hash = ? AND u.status = 'active' AND u.removed_at IS NULL
			  AND EXISTS (
			    SELECT 1 FROM managed_database_grants g
			    WHERE g.account_id = u.account_id AND g.database_user_id = u.id
			      AND g.status = 'active' AND g.revoked_at IS NULL
			  )`, tokenHash[:]).Scan(
			&handoffID, &accountID, &databaseUserID, &identityID, &sessionID,
			&audience, &state, &expiresAtText, &credential.Username, &credential.Host,
			&envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey,
			&envelope.WrapNonce, &envelope.KeyVersion,
		); queryErr != nil {
			if errors.Is(queryErr, sql.ErrNoRows) {
				return ErrNotFound
			}
			return queryErr
		}
		expiresAt, parseErr := parseTime(expiresAtText)
		if parseErr != nil {
			return parseErr
		}
		if audience != PHPMyAdminHandoffAudience || state != "issued" || !expiresAt.After(now) {
			return ErrNotFound
		}
		facts, factsErr := loadPHPMyAdminAuthorizationFacts(ctx, executor, identityID, sessionID, accountID)
		if factsErr != nil {
			return factsErr
		}
		if _, factsErr = evaluateAuthorization(
			AuthorizationAccountCredentialsManage, &accountID,
			authorizationPolicies[AuthorizationAccountCredentialsManage], facts, now,
		); factsErr != nil {
			return factsErr
		}
		plaintext, decryptErr := r.decryptSecret(
			managedDatabaseEnvelopeDomain, databaseUserID, accountID, envelope,
		)
		if decryptErr != nil {
			return decryptErr
		}
		credential.Password = plaintext
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE phpmyadmin_handoffs
			SET state = 'consumed', consumed_at = ?
			WHERE id = ? AND state = 'issued'`, formatTime(now), string(handoffID))
		if updateErr != nil {
			clear(credential.Password)
			credential.Password = nil
			return updateErr
		}
		if affectedErr := expectAffected(result); affectedErr != nil {
			clear(credential.Password)
			credential.Password = nil
			return ErrNotFound
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &identityID, SessionID: &sessionID, Action: "phpmyadmin.handoff_consumed",
			TargetType: "managed_database_user", TargetID: string(databaseUserID), AccountID: &accountID,
			RequestID: "phpmyadmin-handoff:" + string(handoffID), Result: AuditSuccess,
			Details: map[string]any{"audience": PHPMyAdminHandoffAudience},
		}, now)
	})
	if err != nil {
		clear(credential.Password)
		return PHPMyAdminCredential{}, classifyDatabaseError(err)
	}
	return credential, nil
}

func loadPHPMyAdminAuthorizationFacts(
	ctx context.Context,
	reader store.Reader,
	identityID, sessionID, accountID ID,
) (authorizationFacts, error) {
	var facts authorizationFacts
	var identityStatus, authenticatedAt, lastSeenAt, expiresAt, accountStatus, membershipRole string
	err := reader.QueryRowContext(ctx, `
		SELECT i.status, s.authenticated_at, s.last_seen_at, s.expires_at,
		       EXISTS (
		         SELECT 1 FROM platform_role_assignments p
		         WHERE p.identity_id = s.identity_id AND p.role = 'platform_admin'
		       ), h.status,
		       COALESCE((
		         SELECT m.role FROM account_memberships m
		         WHERE m.account_id = h.id AND m.identity_id = s.identity_id
		           AND m.revoked_at IS NULL
		       ), '')
		FROM sessions s
		JOIN identities i ON i.id = s.identity_id
		JOIN hosting_accounts h ON h.id = ?
		WHERE s.id = ? AND s.identity_id = ? AND s.revoked_at IS NULL`,
		string(accountID), string(sessionID), string(identityID)).Scan(
		&identityStatus, &authenticatedAt, &lastSeenAt, &expiresAt,
		&facts.platformAdministrator, &accountStatus, &membershipRole,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authorizationFacts{}, ErrSessionInvalid
		}
		return authorizationFacts{}, err
	}
	if err := populateAuthorizationFacts(
		&facts, identityStatus, authenticatedAt, lastSeenAt, expiresAt, accountStatus, membershipRole,
	); err != nil {
		return authorizationFacts{}, err
	}
	facts.accountExists = true
	return facts, nil
}
