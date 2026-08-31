// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/store"
)

const (
	sessionTokenPrefix = "sfs_"
	csrfTokenPrefix    = "sfc_"
	mfaTokenPrefix     = "sfm_"
	authTokenBytes     = 32

	passwordSessionAbsoluteTTL = 12 * time.Hour
	passwordSessionIdleTTL     = 30 * time.Minute
	passwordSessionTouch       = time.Minute
	mfaLoginChallengeTTL       = 5 * time.Minute

	loginGlobalWindow   = time.Minute
	loginGlobalLimit    = 120
	loginGlobalBlock    = time.Minute
	loginSourceWindow   = time.Minute
	loginSourceLimit    = 10
	loginSourceBlock    = 5 * time.Minute
	loginIdentityWindow = 10 * time.Minute
	loginIdentityLimit  = 5
	loginIdentityBlock  = 15 * time.Minute

	maximumVerifiedArgonMemory  = 256 * 1024
	maximumVerifiedArgonTime    = 10
	maximumVerifiedArgonThreads = 16
)

var (
	dummyPasswordSalt = []byte("StackfortDummyV1")
	dummyPasswordHash = sha256.Sum256([]byte("Stackfort authentication timing equalizer v1"))
)

type passwordCandidate struct {
	exists      bool
	identity    Identity
	hash        []byte
	salt        []byte
	memory      uint32
	iterations  uint32
	parallelism uint8
	version     int64
}

type authenticationRatePolicy struct {
	scope  string
	key    string
	window time.Duration
	limit  int64
	block  time.Duration
}

// PasswordLogin verifies a credential without a quick-exit path. It creates a
// session only when no active second factor exists; otherwise it returns a
// short-lived, hash-only-persisted MFA challenge.
func (r *Repository) PasswordLogin(ctx context.Context, params PasswordLoginParams) (PasswordLoginResult, error) {
	sourceAddress, err := normalizeSourceAddress(params.SourceAddress)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	userAgent, err := validateOptionalText(params.UserAgent, "userAgent", 512)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	if err := validateLoginPasswordInput(params.Password); err != nil {
		return PasswordLoginResult{}, err
	}
	if len(params.PreviousSessionToken) > 128 {
		return PasswordLoginResult{}, fmt.Errorf("%w: previous session token is too long", ErrInvalidInput)
	}

	_, normalizedEmail, emailErr := normalizeEmail(params.Email)
	identityKey := authenticationIdentityKey(normalizedEmail)
	if emailErr != nil {
		identityKey = authenticationIdentityKey(strings.ToLower(strings.TrimSpace(params.Email)))
	}
	if err := r.authorizePasswordAttempt(ctx, sourceAddress, identityKey); err != nil {
		return PasswordLoginResult{}, err
	}

	candidate, err := r.loadPasswordCandidate(ctx, normalizedEmail, emailErr == nil)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	select {
	case r.passwordDerivationSlots <- struct{}{}:
		defer func() { <-r.passwordDerivationSlots }()
	case <-ctx.Done():
		return PasswordLoginResult{}, ctx.Err()
	}
	derived := r.derivePassword(
		[]byte(params.Password), candidate.salt, candidate.iterations,
		candidate.memory, candidate.parallelism, bootstrapArgonKeyBytes,
	)
	passwordMatches := len(derived) == len(candidate.hash) && subtle.ConstantTimeCompare(derived, candidate.hash) == 1
	validPassword := passwordMatches && candidate.exists && candidate.identity.Status == IdentityActive
	if !validPassword {
		if err := r.recordIdentityLoginFailure(ctx, identityKey); err != nil {
			return PasswordLoginResult{}, err
		}
		return PasswordLoginResult{}, ErrAuthenticationDenied
	}

	sessionToken, sessionTokenHash, err := r.newAuthenticationToken(sessionTokenPrefix)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	csrfToken, csrfTokenHash, err := r.newAuthenticationToken(csrfTokenPrefix)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	sessionID, err := r.newID()
	if err != nil {
		return PasswordLoginResult{}, err
	}
	mfaChallengeToken, mfaChallengeTokenHash, err := r.newAuthenticationToken(mfaTokenPrefix)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	mfaChallengeID, err := r.newID()
	if err != nil {
		return PasswordLoginResult{}, err
	}
	now := r.timestamp()
	session := Session{
		ID: sessionID, IdentityID: candidate.identity.ID,
		CreatedAt: now, AuthenticatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(passwordSessionAbsoluteTTL), SourceAddress: sourceAddress,
		UserAgent: userAgent, AuthenticationLevel: SessionAuthenticationPassword,
	}
	mfaExpiresAt := now.Add(mfaLoginChallengeTTL)
	var previousSessionHash [sha256.Size]byte
	if params.PreviousSessionToken != "" {
		previousSessionHash = sha256.Sum256([]byte(params.PreviousSessionToken))
	}

	var mfaRequired bool
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if err := revalidatePasswordCandidateTx(ctx, executor, candidate); err != nil {
			return err
		}
		var factorID ID
		factorErr := executor.QueryRowContext(ctx, `
			SELECT id FROM totp_factors
			WHERE identity_id = ? AND status = 'active'`, string(candidate.identity.ID)).Scan(&factorID)
		if factorErr != nil && !errors.Is(factorErr, sql.ErrNoRows) {
			return factorErr
		}
		if factorErr == nil {
			mfaRequired = true
			var previousSessionID any
			if params.PreviousSessionToken != "" {
				resolvedID, resolveErr := resolvePreviousSessionIDTx(
					ctx, executor, previousSessionHash, candidate.identity.ID,
				)
				if resolveErr != nil {
					return resolveErr
				}
				if resolvedID != "" {
					previousSessionID = string(resolvedID)
				}
			}
			if _, err := executor.ExecContext(ctx, `
				UPDATE mfa_login_challenges SET consumed_at = ?
				WHERE identity_id = ? AND consumed_at IS NULL`,
				formatTime(now), string(candidate.identity.ID)); err != nil {
				return err
			}
			if _, err := executor.ExecContext(ctx, `
				INSERT INTO mfa_login_challenges (
					id, identity_id, token_hash, previous_session_id,
					created_at, expires_at, source_address, user_agent
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				string(mfaChallengeID), string(candidate.identity.ID), mfaChallengeTokenHash[:],
				previousSessionID, formatTime(now), formatTime(mfaExpiresAt),
				nullableString(sourceAddress), nullableString(userAgent)); err != nil {
				return err
			}
			if _, err := executor.ExecContext(ctx, `
				DELETE FROM authentication_rate_limits
				WHERE scope = 'identity' AND rate_key = ?`, identityKey); err != nil {
				return err
			}
			return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
				ActorID: &candidate.identity.ID, SourceAddress: sourceAddress,
				Action: "authentication.mfa_required", TargetType: "mfa_challenge",
				TargetID: string(mfaChallengeID), RequestID: requestID, Result: AuditSuccess,
				Details: map[string]any{"expiresAt": formatTime(mfaExpiresAt)},
			}, now)
		}
		if params.PreviousSessionToken != "" {
			if err := r.rotatePreviousSessionTx(ctx, executor, previousSessionHash, now); err != nil {
				return err
			}
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO sessions (
				id, identity_id, token_hash, csrf_secret_hash, created_at, authenticated_at,
				last_seen_at, expires_at, source_address, user_agent
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(session.ID), string(session.IdentityID), sessionTokenHash[:], csrfTokenHash[:],
			formatTime(now), formatTime(now), formatTime(now), formatTime(session.ExpiresAt),
			nullableString(sourceAddress), nullableString(userAgent)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			DELETE FROM authentication_rate_limits
			WHERE scope = 'identity' AND rate_key = ?`, identityKey); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:       &candidate.identity.ID,
			SessionID:     &session.ID,
			SourceAddress: sourceAddress,
			Action:        "authentication.login_succeeded",
			TargetType:    "session",
			TargetID:      string(session.ID),
			RequestID:     requestID,
			Result:        AuditSuccess,
			Details: map[string]any{
				"expiresAt": formatTime(session.ExpiresAt),
			},
		}, now)
	})
	if err != nil {
		return PasswordLoginResult{}, classifyDatabaseError(err)
	}
	if mfaRequired {
		return PasswordLoginResult{
			Identity: candidate.identity, MFARequired: true,
			MFAChallengeToken: mfaChallengeToken, MFAChallengeExpiresAt: mfaExpiresAt,
		}, nil
	}
	return PasswordLoginResult{
		Identity: candidate.identity, Session: session,
		SessionToken: sessionToken, CSRFToken: csrfToken,
	}, nil
}

// CompleteMFALogin consumes one TOTP or recovery code and creates the browser
// session that PasswordLogin deliberately withheld.
func (r *Repository) CompleteMFALogin(ctx context.Context, params CompleteMFALoginParams) (PasswordLoginResult, error) {
	if params.ChallengeToken == "" || len(params.ChallengeToken) > 128 ||
		!strings.HasPrefix(params.ChallengeToken, mfaTokenPrefix) || len(params.Code) > 128 {
		return PasswordLoginResult{}, ErrMFAChallengeInvalid
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	challengeHash := sha256.Sum256([]byte(params.ChallengeToken))
	sessionToken, sessionTokenHash, err := r.newAuthenticationToken(sessionTokenPrefix)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	csrfToken, csrfTokenHash, err := r.newAuthenticationToken(csrfTokenPrefix)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	sessionID, err := r.newID()
	if err != nil {
		return PasswordLoginResult{}, err
	}
	now := r.timestamp()
	var identity Identity
	var session Session
	var challengeFailure bool
	var retryAfter time.Duration
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var challengeID, identityID ID
		var previousSessionID sql.NullString
		var expiresAt, identityCreatedAt, identityUpdatedAt string
		var sourceAddress, userAgent, locale, identityStatus string
		var attemptCount int64
		err := executor.QueryRowContext(ctx, `
			SELECT c.id, c.identity_id, c.previous_session_id, c.expires_at, c.attempt_count,
			       COALESCE(c.source_address, ''), COALESCE(c.user_agent, ''),
			       i.email, i.normalized_email, i.display_name, i.locale, i.status,
			       i.created_at, i.updated_at
			FROM mfa_login_challenges c
			JOIN identities i ON i.id = c.identity_id
			WHERE c.token_hash = ? AND c.consumed_at IS NULL`, challengeHash[:]).Scan(
			&challengeID, &identityID, &previousSessionID, &expiresAt, &attemptCount,
			&sourceAddress, &userAgent, &identity.Email, &identity.NormalizedEmail,
			&identity.DisplayName, &locale, &identityStatus, &identityCreatedAt, &identityUpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			challengeFailure = true
			return nil
		}
		if err != nil {
			return err
		}
		identity.ID = identityID
		identity.Locale = Locale(locale)
		identity.Status = IdentityStatus(identityStatus)
		identity.CreatedAt, err = parseTime(identityCreatedAt)
		if err != nil {
			return err
		}
		identity.UpdatedAt, err = parseTime(identityUpdatedAt)
		if err != nil {
			return err
		}
		challengeExpiresAt, err := parseTime(expiresAt)
		if err != nil {
			return err
		}
		retryAfter, err = mfaAttemptRetryTx(ctx, executor, identityID, now)
		if err != nil || retryAfter > 0 {
			return err
		}
		if identity.Status != IdentityActive || !challengeExpiresAt.After(now) || attemptCount >= totpMaximumAttempts {
			if _, err := executor.ExecContext(ctx, `
				UPDATE mfa_login_challenges SET consumed_at = ?
				WHERE id = ? AND consumed_at IS NULL`, formatTime(now), string(challengeID)); err != nil {
				return err
			}
			challengeFailure = true
			return nil
		}
		factor, err := r.loadActiveTOTPFactorTx(ctx, executor, identityID)
		if errors.Is(err, sql.ErrNoRows) {
			if _, updateErr := executor.ExecContext(ctx, `
				UPDATE mfa_login_challenges SET consumed_at = ?
				WHERE id = ? AND consumed_at IS NULL`, formatTime(now), string(challengeID)); updateErr != nil {
				return updateErr
			}
			challengeFailure = true
			return nil
		}
		if err != nil {
			return err
		}
		verified, authenticationLevel, err := r.verifyCurrentFactorTx(ctx, executor, factor, params.Code, now)
		if err != nil {
			return err
		}
		if !verified {
			nextAttempts := attemptCount + 1
			if nextAttempts >= totpMaximumAttempts {
				_, err = executor.ExecContext(ctx, `
					UPDATE mfa_login_challenges SET attempt_count = ?, consumed_at = ?
					WHERE id = ? AND consumed_at IS NULL`, nextAttempts, formatTime(now), string(challengeID))
			} else {
				_, err = executor.ExecContext(ctx, `
					UPDATE mfa_login_challenges SET attempt_count = ?
					WHERE id = ? AND consumed_at IS NULL`, nextAttempts, string(challengeID))
			}
			if err != nil {
				return err
			}
			challengeFailure = true
			return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
				ActorID: &identityID, SourceAddress: sourceAddress,
				Action: "authentication.mfa_failed", TargetType: "mfa_challenge",
				TargetID: string(challengeID), RequestID: requestID, Result: AuditFailure,
				Details: map[string]any{"attemptCount": nextAttempts},
			}, now)
		}
		consumed, err := executor.ExecContext(ctx, `
			UPDATE mfa_login_challenges SET consumed_at = ?
			WHERE id = ? AND consumed_at IS NULL`, formatTime(now), string(challengeID))
		if err != nil {
			return err
		}
		rows, err := consumed.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			challengeFailure = true
			return nil
		}
		if previousSessionID.Valid {
			if _, err := executor.ExecContext(ctx, `
				UPDATE sessions SET revoked_at = ?, revocation_reason = 'login_rotation'
				WHERE id = ? AND identity_id = ? AND revoked_at IS NULL`,
				formatTime(now), previousSessionID.String, string(identityID)); err != nil {
				return err
			}
		}
		session = Session{
			ID: sessionID, IdentityID: identityID, CreatedAt: now, AuthenticatedAt: now,
			LastSeenAt: now, ExpiresAt: now.Add(passwordSessionAbsoluteTTL),
			SourceAddress: sourceAddress, UserAgent: userAgent,
			AuthenticationLevel: authenticationLevel, MFAAuthenticatedAt: &now,
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO sessions (
				id, identity_id, token_hash, csrf_secret_hash, created_at, authenticated_at,
				last_seen_at, expires_at, source_address, user_agent,
				authentication_level, mfa_authenticated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(session.ID), string(identityID), sessionTokenHash[:], csrfTokenHash[:],
			formatTime(now), formatTime(now), formatTime(now), formatTime(session.ExpiresAt),
			nullableString(sourceAddress), nullableString(userAgent), string(authenticationLevel), formatTime(now)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `DELETE FROM mfa_attempt_limits WHERE identity_id = ?`,
			string(identityID)); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &identityID, SessionID: &session.ID, SourceAddress: sourceAddress,
			Action: "authentication.login_succeeded", TargetType: "session",
			TargetID: string(session.ID), RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{
				"authenticationLevel": authenticationLevel, "expiresAt": formatTime(session.ExpiresAt),
			},
		}, now)
	})
	if err != nil {
		return PasswordLoginResult{}, classifyDatabaseError(err)
	}
	if retryAfter > 0 {
		return PasswordLoginResult{}, &AuthenticationRateLimitError{RetryAfter: retryAfter}
	}
	if challengeFailure {
		if identity.ID != "" {
			if err := r.recordMFAFailure(ctx, identity.ID); err != nil {
				return PasswordLoginResult{}, err
			}
		}
		return PasswordLoginResult{}, ErrMFAChallengeInvalid
	}
	return PasswordLoginResult{
		Identity: identity, Session: session, SessionToken: sessionToken, CSRFToken: csrfToken,
	}, nil
}

func resolvePreviousSessionIDTx(
	ctx context.Context,
	executor store.Executor,
	tokenHash [sha256.Size]byte,
	identityID ID,
) (ID, error) {
	var sessionID ID
	err := executor.QueryRowContext(ctx, `
		SELECT id FROM sessions
		WHERE token_hash = ? AND identity_id = ? AND revoked_at IS NULL`,
		tokenHash[:], string(identityID)).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return sessionID, err
}

// AuthenticateSession resolves only cookie-shaped bearer material and, for
// unsafe requests, verifies a same-session CSRF header and cookie.
func (r *Repository) AuthenticateSession(ctx context.Context, params AuthenticateSessionParams) (AuthenticatedSession, error) {
	if params.SessionToken == "" || len(params.SessionToken) > 128 {
		return AuthenticatedSession{}, ErrSessionInvalid
	}
	tokenHash := sha256.Sum256([]byte(params.SessionToken))
	now := r.timestamp()
	var authenticated AuthenticatedSession
	var csrfHash []byte
	var createdAt, authenticatedAt, lastSeenAt, expiresAt string
	var authenticationLevel string
	var mfaAuthenticatedAt sql.NullString
	var identityCreatedAt, identityUpdatedAt string
	var locale, identityStatus string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT s.id, s.identity_id, s.created_at, s.authenticated_at, s.last_seen_at,
			       s.expires_at, COALESCE(s.source_address, ''), COALESCE(s.user_agent, ''),
			       s.csrf_secret_hash, s.authentication_level, s.mfa_authenticated_at,
			       i.email, i.normalized_email, i.display_name, i.locale, i.status,
			       i.created_at, i.updated_at
			FROM sessions s
			JOIN identities i ON i.id = s.identity_id
			WHERE s.token_hash = ? AND s.revoked_at IS NULL`, tokenHash[:]).Scan(
			&authenticated.Session.ID,
			&authenticated.Session.IdentityID,
			&createdAt,
			&authenticatedAt,
			&lastSeenAt,
			&expiresAt,
			&authenticated.Session.SourceAddress,
			&authenticated.Session.UserAgent,
			&csrfHash,
			&authenticationLevel,
			&mfaAuthenticatedAt,
			&authenticated.Identity.Email,
			&authenticated.Identity.NormalizedEmail,
			&authenticated.Identity.DisplayName,
			&locale,
			&identityStatus,
			&identityCreatedAt,
			&identityUpdatedAt,
		)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthenticatedSession{}, ErrSessionInvalid
		}
		return AuthenticatedSession{}, err
	}
	authenticated.Identity.ID = authenticated.Session.IdentityID
	authenticated.Identity.Locale = Locale(locale)
	authenticated.Identity.Status = IdentityStatus(identityStatus)
	authenticated.Identity.CreatedAt, err = parseTime(identityCreatedAt)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	authenticated.Identity.UpdatedAt, err = parseTime(identityUpdatedAt)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	authenticated.Session.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	authenticated.Session.AuthenticatedAt, err = parseTime(authenticatedAt)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	authenticated.Session.LastSeenAt, err = parseTime(lastSeenAt)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	authenticated.Session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil {
		return AuthenticatedSession{}, err
	}
	authenticated.Session.AuthenticationLevel = SessionAuthenticationLevel(authenticationLevel)
	if mfaAuthenticatedAt.Valid {
		parsed, err := parseTime(mfaAuthenticatedAt.String)
		if err != nil {
			return AuthenticatedSession{}, err
		}
		authenticated.Session.MFAAuthenticatedAt = &parsed
	}
	if authenticated.Identity.Status != IdentityActive {
		return AuthenticatedSession{}, ErrSessionInvalid
	}
	expiryReason := ""
	if !authenticated.Session.ExpiresAt.After(now) {
		expiryReason = "absolute_expiry"
	} else if !authenticated.Session.LastSeenAt.Add(passwordSessionIdleTTL).After(now) {
		expiryReason = "idle_expiry"
	}
	if expiryReason != "" {
		if err := r.expireSession(ctx, authenticated, expiryReason, now); err != nil {
			return AuthenticatedSession{}, err
		}
		return AuthenticatedSession{}, ErrSessionInvalid
	}
	if params.RequireCSRF && !validSessionCSRF(csrfHash, params.CSRFHeaderToken, params.CSRFCookieToken) {
		return AuthenticatedSession{}, ErrCSRFInvalid
	}
	if !authenticated.Session.LastSeenAt.Add(passwordSessionTouch).After(now) {
		err := r.state.Write(ctx, func(executor store.Executor) error {
			result, err := executor.ExecContext(ctx, `
				UPDATE sessions SET last_seen_at = ?
				WHERE id = ? AND identity_id = ? AND revoked_at IS NULL`,
				formatTime(now), string(authenticated.Session.ID), string(authenticated.Identity.ID))
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrSessionInvalid
			}
			return nil
		})
		if err != nil {
			return AuthenticatedSession{}, classifyDatabaseError(err)
		}
		authenticated.Session.LastSeenAt = now
	}
	authenticated.authorizationProof = r.authorizationSubjectProof(
		authenticated.Identity.ID,
		authenticated.Session.ID,
	)
	return authenticated, nil
}

// RevokeSession invalidates one already-authenticated session server-side.
func (r *Repository) RevokeSession(ctx context.Context, params RevokeSessionParams) error {
	if err := validateID(params.IdentityID, "identityId"); err != nil {
		return err
	}
	if err := validateID(params.SessionID, "sessionId"); err != nil {
		return err
	}
	reason, err := validateAction(params.Reason, "reason", 80)
	if err != nil {
		return err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	sourceAddress := ""
	if strings.TrimSpace(params.SourceAddress) != "" {
		sourceAddress, err = normalizeSourceAddress(params.SourceAddress)
		if err != nil {
			return err
		}
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, err := executor.ExecContext(ctx, `
			UPDATE sessions
			SET revoked_at = ?, revocation_reason = ?
			WHERE id = ? AND identity_id = ? AND revoked_at IS NULL`,
			formatTime(now), reason, string(params.SessionID), string(params.IdentityID))
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrSessionInvalid
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:       &params.IdentityID,
			SessionID:     &params.SessionID,
			SourceAddress: sourceAddress,
			Action:        "session.revoked",
			TargetType:    "session",
			TargetID:      string(params.SessionID),
			RequestID:     requestID,
			Result:        AuditSuccess,
			Details: map[string]any{
				"reason": reason,
			},
		}, now)
	})
	return classifyDatabaseError(err)
}

func (r *Repository) loadPasswordCandidate(ctx context.Context, normalizedEmail string, validEmail bool) (passwordCandidate, error) {
	candidate := passwordCandidate{
		salt: append([]byte(nil), dummyPasswordSalt...), hash: append([]byte(nil), dummyPasswordHash[:]...),
		memory: bootstrapArgonMemory, iterations: bootstrapArgonTime,
		parallelism: bootstrapArgonThreads, version: 19,
	}
	if !validEmail {
		return candidate, nil
	}
	var memory, iterations uint32
	var parallelism uint8
	var locale, status, createdAt, updatedAt string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT i.id, i.email, i.normalized_email, i.display_name, i.locale, i.status,
			       i.created_at, i.updated_at,
			       p.password_hash, p.salt, p.memory_kib, p.iterations, p.parallelism, p.version
			FROM identities i
			JOIN password_credentials p ON p.identity_id = i.id
			WHERE i.normalized_email = ?`, normalizedEmail).Scan(
			&candidate.identity.ID, &candidate.identity.Email, &candidate.identity.NormalizedEmail,
			&candidate.identity.DisplayName, &locale, &status, &createdAt, &updatedAt,
			&candidate.hash, &candidate.salt, &memory, &iterations, &parallelism, &candidate.version,
		)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return candidate, nil
	}
	if err != nil {
		return passwordCandidate{}, err
	}
	if err := validateCredentialVerificationParameters(candidate.hash, candidate.salt, memory, iterations, parallelism, candidate.version); err != nil {
		return passwordCandidate{}, err
	}
	candidate.exists = true
	candidate.memory = memory
	candidate.iterations = iterations
	candidate.parallelism = parallelism
	candidate.identity.Locale = Locale(locale)
	candidate.identity.Status = IdentityStatus(status)
	candidate.identity.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return passwordCandidate{}, err
	}
	candidate.identity.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return passwordCandidate{}, err
	}
	return candidate, nil
}

func validateCredentialVerificationParameters(hash, salt []byte, memory, iterations uint32, parallelism uint8, version int64) error {
	if len(hash) != bootstrapArgonKeyBytes || len(salt) < 16 || len(salt) > 64 ||
		memory < 8192 || memory > maximumVerifiedArgonMemory ||
		iterations < 1 || iterations > maximumVerifiedArgonTime ||
		parallelism < 1 || parallelism > maximumVerifiedArgonThreads || version != 19 {
		return errors.New("stored password credential uses unsupported verification parameters")
	}
	return nil
}

func revalidatePasswordCandidateTx(ctx context.Context, executor store.Executor, candidate passwordCandidate) error {
	var status string
	var hash, salt []byte
	var memory, iterations uint32
	var parallelism uint8
	var version int64
	err := executor.QueryRowContext(ctx, `
		SELECT i.status, p.password_hash, p.salt, p.memory_kib, p.iterations, p.parallelism, p.version
		FROM identities i
		JOIN password_credentials p ON p.identity_id = i.id
		WHERE i.id = ?`, string(candidate.identity.ID)).Scan(
		&status, &hash, &salt, &memory, &iterations, &parallelism, &version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAuthenticationDenied
	}
	if err != nil {
		return err
	}
	if status != string(IdentityActive) || subtle.ConstantTimeCompare(hash, candidate.hash) != 1 ||
		subtle.ConstantTimeCompare(salt, candidate.salt) != 1 ||
		memory != candidate.memory || iterations != candidate.iterations ||
		parallelism != candidate.parallelism || version != candidate.version {
		return ErrAuthenticationDenied
	}
	return nil
}

func (r *Repository) newAuthenticationToken(prefix string) (string, [sha256.Size]byte, error) {
	raw := make([]byte, authTokenBytes)
	if _, err := io.ReadFull(r.random, raw); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("generate authentication token: %w", err)
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}

func (r *Repository) rotatePreviousSessionTx(ctx context.Context, executor store.Executor, tokenHash [sha256.Size]byte, now time.Time) error {
	var sessionID, identityID ID
	err := executor.QueryRowContext(ctx, `
		SELECT id, identity_id FROM sessions
		WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash[:]).Scan(&sessionID, &identityID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := executor.ExecContext(ctx, `
		UPDATE sessions
		SET revoked_at = ?, revocation_reason = 'login_rotation'
		WHERE id = ? AND revoked_at IS NULL`, formatTime(now), string(sessionID)); err != nil {
		return err
	}
	return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
		ActorID:    &identityID,
		SessionID:  &sessionID,
		Action:     "session.rotated",
		TargetType: "session",
		TargetID:   string(sessionID),
		Result:     AuditSuccess,
		Details: map[string]any{
			"reason": "login_rotation",
		},
	}, now)
}

func (r *Repository) authorizePasswordAttempt(ctx context.Context, sourceAddress, identityKey string) error {
	now := r.timestamp()
	var retryAfter time.Duration
	err := r.state.Write(ctx, func(executor store.Executor) error {
		policies := []authenticationRatePolicy{{
			scope: "global", key: "*", window: loginGlobalWindow,
			limit: loginGlobalLimit, block: loginGlobalBlock,
		}, {
			scope: "source", key: sourceAddress, window: loginSourceWindow,
			limit: loginSourceLimit, block: loginSourceBlock,
		}}
		for _, policy := range policies {
			limitedFor, err := applyAuthenticationAttemptLimitTx(ctx, executor, policy, now)
			if err != nil {
				return err
			}
			if limitedFor > 0 {
				retryAfter = limitedFor
				return nil
			}
		}
		limitedFor, err := authenticationIdentityBlockedTx(ctx, executor, identityKey, now)
		if err != nil {
			return err
		}
		if limitedFor > 0 {
			retryAfter = limitedFor
			return nil
		}
		_, err = executor.ExecContext(ctx, `
			DELETE FROM authentication_rate_limits
			WHERE scope != 'global' AND updated_at < ?`, formatTime(now.Add(-24*time.Hour)))
		return err
	})
	if err != nil {
		return classifyDatabaseError(err)
	}
	if retryAfter > 0 {
		return &AuthenticationRateLimitError{RetryAfter: retryAfter}
	}
	return nil
}

func applyAuthenticationAttemptLimitTx(ctx context.Context, executor store.Executor, policy authenticationRatePolicy, now time.Time) (time.Duration, error) {
	var windowStartedAt string
	var attemptCount int64
	var blockedUntil sql.NullString
	err := executor.QueryRowContext(ctx, `
		SELECT window_started_at, attempt_count, blocked_until
		FROM authentication_rate_limits
		WHERE scope = ? AND rate_key = ?`, policy.scope, policy.key).Scan(&windowStartedAt, &attemptCount, &blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = executor.ExecContext(ctx, `
			INSERT INTO authentication_rate_limits (
				scope, rate_key, window_started_at, attempt_count, updated_at
			) VALUES (?, ?, ?, 1, ?)`, policy.scope, policy.key, formatTime(now), formatTime(now))
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	if blockedUntil.Valid {
		blocked, err := parseTime(blockedUntil.String)
		if err != nil {
			return 0, err
		}
		if blocked.After(now) {
			return blocked.Sub(now), nil
		}
	}
	windowStart, err := parseTime(windowStartedAt)
	if err != nil {
		return 0, err
	}
	if !now.Before(windowStart.Add(policy.window)) {
		_, err = executor.ExecContext(ctx, `
			UPDATE authentication_rate_limits
			SET window_started_at = ?, attempt_count = 1, blocked_until = NULL, updated_at = ?
			WHERE scope = ? AND rate_key = ?`,
			formatTime(now), formatTime(now), policy.scope, policy.key)
		return 0, err
	}
	attemptCount++
	if attemptCount > policy.limit {
		blockUntil := now.Add(policy.block)
		_, err = executor.ExecContext(ctx, `
			UPDATE authentication_rate_limits
			SET attempt_count = ?, blocked_until = ?, updated_at = ?
			WHERE scope = ? AND rate_key = ?`,
			attemptCount, formatTime(blockUntil), formatTime(now), policy.scope, policy.key)
		return policy.block, err
	}
	_, err = executor.ExecContext(ctx, `
		UPDATE authentication_rate_limits
		SET attempt_count = ?, blocked_until = NULL, updated_at = ?
		WHERE scope = ? AND rate_key = ?`,
		attemptCount, formatTime(now), policy.scope, policy.key)
	return 0, err
}

func authenticationIdentityBlockedTx(ctx context.Context, executor store.Executor, identityKey string, now time.Time) (time.Duration, error) {
	var blockedUntil sql.NullString
	err := executor.QueryRowContext(ctx, `
		SELECT blocked_until FROM authentication_rate_limits
		WHERE scope = 'identity' AND rate_key = ?`, identityKey).Scan(&blockedUntil)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !blockedUntil.Valid) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	blocked, err := parseTime(blockedUntil.String)
	if err != nil {
		return 0, err
	}
	if blocked.After(now) {
		return blocked.Sub(now), nil
	}
	return 0, nil
}

func (r *Repository) recordIdentityLoginFailure(ctx context.Context, identityKey string) error {
	now := r.timestamp()
	return r.state.Write(ctx, func(executor store.Executor) error {
		var windowStartedAt string
		var attemptCount int64
		err := executor.QueryRowContext(ctx, `
			SELECT window_started_at, attempt_count
			FROM authentication_rate_limits
			WHERE scope = 'identity' AND rate_key = ?`, identityKey).Scan(&windowStartedAt, &attemptCount)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = executor.ExecContext(ctx, `
				INSERT INTO authentication_rate_limits (
					scope, rate_key, window_started_at, attempt_count, updated_at
				) VALUES ('identity', ?, ?, 1, ?)`, identityKey, formatTime(now), formatTime(now))
			return err
		}
		if err != nil {
			return err
		}
		windowStart, err := parseTime(windowStartedAt)
		if err != nil {
			return err
		}
		if !now.Before(windowStart.Add(loginIdentityWindow)) {
			_, err = executor.ExecContext(ctx, `
				UPDATE authentication_rate_limits
				SET window_started_at = ?, attempt_count = 1, blocked_until = NULL, updated_at = ?
				WHERE scope = 'identity' AND rate_key = ?`,
				formatTime(now), formatTime(now), identityKey)
			return err
		}
		attemptCount++
		var blockedUntil any
		if attemptCount >= loginIdentityLimit {
			blockedUntil = formatTime(now.Add(loginIdentityBlock))
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE authentication_rate_limits
			SET attempt_count = ?, blocked_until = ?, updated_at = ?
			WHERE scope = 'identity' AND rate_key = ?`,
			attemptCount, blockedUntil, formatTime(now), identityKey)
		return err
	})
}

func (r *Repository) expireSession(ctx context.Context, authenticated AuthenticatedSession, reason string, now time.Time) error {
	return r.state.Write(ctx, func(executor store.Executor) error {
		result, err := executor.ExecContext(ctx, `
			UPDATE sessions SET revoked_at = ?, revocation_reason = ?
			WHERE id = ? AND identity_id = ? AND revoked_at IS NULL`,
			formatTime(now), reason, string(authenticated.Session.ID), string(authenticated.Identity.ID))
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    &authenticated.Identity.ID,
			SessionID:  &authenticated.Session.ID,
			Action:     "session.expired",
			TargetType: "session",
			TargetID:   string(authenticated.Session.ID),
			Result:     AuditSuccess,
			Details: map[string]any{
				"reason": reason,
			},
		}, now)
	})
}

func validSessionCSRF(storedHash []byte, headerToken, cookieToken string) bool {
	if len(storedHash) != sha256.Size || headerToken == "" || cookieToken == "" ||
		len(headerToken) > 128 || len(cookieToken) > 128 {
		return false
	}
	headerHash := sha256.Sum256([]byte(headerToken))
	cookieHash := sha256.Sum256([]byte(cookieToken))
	return subtle.ConstantTimeCompare(headerHash[:], storedHash) == 1 &&
		subtle.ConstantTimeCompare(cookieHash[:], storedHash) == 1 &&
		subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookieToken)) == 1
}

func validateLoginPasswordInput(password string) error {
	if !utf8.ValidString(password) || len(password) > bootstrapMaximumBytes || utf8.RuneCountInString(password) > bootstrapMaximumRunes {
		return fmt.Errorf("%w: password input exceeds supported bounds", ErrInvalidInput)
	}
	return nil
}

func authenticationIdentityKey(normalizedEmail string) string {
	digest := sha256.Sum256([]byte(normalizedEmail))
	return hex.EncodeToString(digest[:])
}
