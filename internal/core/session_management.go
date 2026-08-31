// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

// ListManagedSessions returns the identity's active sessions without bearer or
// CSRF material. The caller's current session is marked explicitly.
func (r *Repository) ListManagedSessions(ctx context.Context, params ListManagedSessionsParams) ([]ManagedSession, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject,
		Action:  AuthorizationIdentitySessionsView,
	}); err != nil {
		return nil, err
	}

	var sessions []ManagedSession
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT id, identity_id, created_at, authenticated_at, last_seen_at,
			       expires_at, COALESCE(source_address, ''), COALESCE(user_agent, ''),
			       authentication_level, mfa_authenticated_at
			FROM sessions
			WHERE identity_id = ? AND revoked_at IS NULL
			ORDER BY last_seen_at DESC, id DESC
			LIMIT 100`, string(params.Subject.identityID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var managed ManagedSession
			var createdAt, authenticatedAt, lastSeenAt, expiresAt string
			var authenticationLevel string
			var mfaAuthenticatedAt sql.NullString
			if err := rows.Scan(
				&managed.ID, &managed.IdentityID, &createdAt, &authenticatedAt,
				&lastSeenAt, &expiresAt, &managed.SourceAddress, &managed.UserAgent,
				&authenticationLevel, &mfaAuthenticatedAt,
			); err != nil {
				return err
			}
			managed.AuthenticationLevel = SessionAuthenticationLevel(authenticationLevel)
			managed.Current = managed.ID == params.Subject.sessionID
			var parseErr error
			managed.CreatedAt, parseErr = parseTime(createdAt)
			if parseErr != nil {
				return parseErr
			}
			managed.AuthenticatedAt, parseErr = parseTime(authenticatedAt)
			if parseErr != nil {
				return parseErr
			}
			managed.LastSeenAt, parseErr = parseTime(lastSeenAt)
			if parseErr != nil {
				return parseErr
			}
			managed.ExpiresAt, parseErr = parseTime(expiresAt)
			if parseErr != nil {
				return parseErr
			}
			if mfaAuthenticatedAt.Valid {
				parsed, parseErr := parseTime(mfaAuthenticatedAt.String)
				if parseErr != nil {
					return parseErr
				}
				managed.MFAAuthenticatedAt = &parsed
			}
			sessions = append(sessions, managed)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return sessions, nil
}

// RevokeManagedSession revokes one active session owned by the current
// identity. A foreign identifier is denied without disclosing its existence.
func (r *Repository) RevokeManagedSession(ctx context.Context, params RevokeManagedSessionParams) error {
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject,
		Action:  AuthorizationIdentitySessionsManage,
	}); err != nil {
		return err
	}
	if err := validateID(params.TargetSessionID, "sessionId"); err != nil {
		return ErrAuthorizationDenied
	}
	requestID, sourceAddress, err := validateSessionManagementMetadata(params.RequestID, params.SourceAddress)
	if err != nil {
		return err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE sessions
			SET revoked_at = ?, revocation_reason = 'user_revoked'
			WHERE id = ? AND identity_id = ? AND revoked_at IS NULL`,
			formatTime(now), string(params.TargetSessionID), string(params.Subject.identityID))
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrAuthorizationDenied
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "session.user_revoked",
			TargetType: "session", TargetID: string(params.TargetSessionID),
			RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"current": params.TargetSessionID == params.Subject.sessionID},
		}, now)
	})
	return classifyDatabaseError(err)
}

// RevokeAllManagedSessions revokes every active session for the identity, or
// every session except the caller when KeepCurrent is set.
func (r *Repository) RevokeAllManagedSessions(
	ctx context.Context,
	params RevokeAllManagedSessionsParams,
) (RevokeAllManagedSessionsResult, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject,
		Action:  AuthorizationIdentitySessionsManage,
	}); err != nil {
		return RevokeAllManagedSessionsResult{}, err
	}
	requestID, sourceAddress, err := validateSessionManagementMetadata(params.RequestID, params.SourceAddress)
	if err != nil {
		return RevokeAllManagedSessionsResult{}, err
	}
	now := r.timestamp()
	result := RevokeAllManagedSessionsResult{CurrentRevoked: !params.KeepCurrent}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		var keepSessionID *ID
		if params.KeepCurrent {
			keepSessionID = &params.Subject.sessionID
		}
		revoked, err := revokeIdentitySessionsTx(
			ctx, executor, params.Subject.identityID, keepSessionID, "user_revoked_all", now,
		)
		if err != nil {
			return err
		}
		result.Revoked = revoked
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "session.user_revoked_all",
			TargetType: "identity", TargetID: string(params.Subject.identityID),
			RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"keepCurrent": params.KeepCurrent, "revokedSessions": revoked},
		}, now)
	})
	if err != nil {
		return RevokeAllManagedSessionsResult{}, classifyDatabaseError(err)
	}
	return result, nil
}

func revokeIdentitySessionsTx(
	ctx context.Context,
	executor store.Executor,
	identityID ID,
	keepSessionID *ID,
	reason string,
	now time.Time,
) (int64, error) {
	var result sql.Result
	var err error
	if keepSessionID == nil {
		result, err = executor.ExecContext(ctx, `
			UPDATE sessions SET revoked_at = ?, revocation_reason = ?
			WHERE identity_id = ? AND revoked_at IS NULL`,
			formatTime(now), reason, string(identityID))
	} else {
		result, err = executor.ExecContext(ctx, `
			UPDATE sessions SET revoked_at = ?, revocation_reason = ?
			WHERE identity_id = ? AND id <> ? AND revoked_at IS NULL`,
			formatTime(now), reason, string(identityID), string(*keepSessionID))
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func validateSessionManagementMetadata(requestIDValue, sourceAddressValue string) (string, string, error) {
	requestID, err := validateOptionalText(requestIDValue, "requestId", 128)
	if err != nil {
		return "", "", err
	}
	sourceAddress := ""
	if strings.TrimSpace(sourceAddressValue) != "" {
		sourceAddress, err = normalizeSourceAddress(sourceAddressValue)
		if err != nil {
			return "", "", err
		}
	}
	return requestID, sourceAddress, nil
}
