// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 4226/6238 interoperability requires HMAC-SHA-1; this is not collision-signature use.
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

const (
	totpSecretBytes         = 20
	totpDigits              = 6
	totpPeriodSeconds       = 30
	totpSetupTTL            = 10 * time.Minute
	totpMaximumAttempts     = 5
	recoveryCodeCount       = 10
	recoveryCodeRandomBytes = 16
	mfaAttemptWindow        = 10 * time.Minute
	mfaAttemptLimit         = 5
	mfaAttemptBlock         = 15 * time.Minute
	totpSecretEnvelopeKind  = "totp-factor"
	totpSetupEnvelopeKind   = "totp-setup"
)

type storedTOTPFactor struct {
	id              ID
	identityID      ID
	envelope        encryptedSecretEnvelope
	lastUsedCounter *int64
	activatedAt     time.Time
}

type storedTOTPSetup struct {
	id               ID
	identityID       ID
	sessionID        ID
	replacesFactorID *ID
	envelope         encryptedSecretEnvelope
	expiresAt        time.Time
	attemptCount     int64
}

type generatedRecoveryCode struct {
	id   ID
	raw  string
	hash [sha256.Size]byte
}

type subjectSessionFacts struct {
	email           string
	authenticatedAt time.Time
	lastSeenAt      time.Time
	expiresAt       time.Time
}

// GetTOTPStatus returns only non-secret factor metadata for the current
// identity and the number of still-unused recovery codes.
func (r *Repository) GetTOTPStatus(ctx context.Context, subject AuthorizationSubject) (TOTPStatus, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{Subject: subject, Action: AuthorizationIdentityFactorsView}); err != nil {
		return TOTPStatus{}, err
	}
	var factorID, activatedAt sql.NullString
	var remaining int64
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT f.id, f.activated_at,
			       (SELECT COUNT(*) FROM recovery_codes r
			        WHERE r.factor_id = f.id AND r.identity_id = f.identity_id AND r.used_at IS NULL)
			FROM identities i
			LEFT JOIN totp_factors f ON f.identity_id = i.id AND f.status = 'active'
			WHERE i.id = ?`, string(subject.identityID)).Scan(&factorID, &activatedAt, &remaining)
	})
	if err != nil {
		return TOTPStatus{}, classifyDatabaseError(err)
	}
	if !factorID.Valid {
		return TOTPStatus{}, nil
	}
	parsedActivatedAt, err := parseTime(activatedAt.String)
	if err != nil {
		return TOTPStatus{}, err
	}
	id := ID(factorID.String)
	return TOTPStatus{
		Enabled: true, FactorID: &id, ActivatedAt: &parsedActivatedAt,
		RecoveryCodesRemaining: remaining,
	}, nil
}

// BeginTOTPEnrollment creates a short-lived encrypted setup secret. Existing
// factors must be proven before a replacement challenge can be issued.
func (r *Repository) BeginTOTPEnrollment(ctx context.Context, params BeginTOTPEnrollmentParams) (TOTPEnrollment, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{Subject: params.Subject, Action: AuthorizationIdentityFactorsManage}); err != nil {
		return TOTPEnrollment{}, err
	}
	sourceAddress, err := normalizeSourceAddress(params.SourceAddress)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	if len(params.CurrentFactor) > 128 {
		return TOTPEnrollment{}, fmt.Errorf("%w: current factor is too long", ErrInvalidInput)
	}
	if err := r.checkMFAAttemptAllowed(ctx, params.Subject.identityID); err != nil {
		return TOTPEnrollment{}, err
	}

	challengeID, err := r.newID()
	if err != nil {
		return TOTPEnrollment{}, err
	}
	secret := make([]byte, totpSecretBytes)
	if _, err := io.ReadFull(r.random, secret); err != nil {
		return TOTPEnrollment{}, fmt.Errorf("generate TOTP secret: %w", err)
	}
	defer clear(secret)
	envelope, err := r.encryptSecret(totpSetupEnvelopeKind, challengeID, params.Subject.identityID, secret)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	now := r.timestamp()
	expiresAt := now.Add(totpSetupTTL)
	var email string
	var factorFailure bool
	err = r.state.Write(ctx, func(executor store.Executor) error {
		facts, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now)
		if err != nil {
			return err
		}
		email = facts.email
		activeFactor, err := r.loadActiveTOTPFactorTx(ctx, executor, params.Subject.identityID)
		switch {
		case err == nil:
			verified, _, verifyErr := r.verifyCurrentFactorTx(ctx, executor, activeFactor, params.CurrentFactor, now)
			if verifyErr != nil {
				return verifyErr
			}
			if !verified {
				factorFailure = true
				return nil
			}
		case errors.Is(err, sql.ErrNoRows):
			if strings.TrimSpace(params.CurrentFactor) != "" {
				factorFailure = true
				return nil
			}
			activeFactor = storedTOTPFactor{}
		default:
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE totp_setup_challenges
			SET consumed_at = ?, completion_reason = 'replaced'
			WHERE identity_id = ? AND consumed_at IS NULL`, formatTime(now), string(params.Subject.identityID)); err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO totp_setup_challenges (
				id, identity_id, session_id, replaces_factor_id,
				secret_ciphertext, secret_nonce, wrapped_dek, wrap_nonce, key_version,
				created_at, expires_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(challengeID), string(params.Subject.identityID), string(params.Subject.sessionID),
			nullableIDValue(activeFactor.id), envelope.Ciphertext, envelope.Nonce,
			envelope.WrappedKey, envelope.WrapNonce, envelope.KeyVersion,
			formatTime(now), formatTime(expiresAt))
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "totp.setup_started",
			TargetType: "totp_setup", TargetID: string(challengeID), RequestID: requestID,
			Result: AuditSuccess, Details: map[string]any{"expiresAt": formatTime(expiresAt)},
		}, now)
	})
	if err != nil {
		return TOTPEnrollment{}, classifyDatabaseError(err)
	}
	if factorFailure {
		if err := r.recordMFAFailure(ctx, params.Subject.identityID); err != nil {
			return TOTPEnrollment{}, err
		}
		return TOTPEnrollment{}, ErrMFAChallengeInvalid
	}
	if err := r.clearMFAFailures(ctx, params.Subject.identityID); err != nil {
		return TOTPEnrollment{}, err
	}
	encodedSecret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return TOTPEnrollment{
		ChallengeID: challengeID, Secret: encodedSecret,
		ProvisioningURI: totpProvisioningURI(email, encodedSecret), ExpiresAt: expiresAt,
	}, nil
}

// ConfirmTOTPEnrollment verifies the new authenticator before activating it,
// generates hash-only recovery codes, and revokes all existing sessions.
func (r *Repository) ConfirmTOTPEnrollment(ctx context.Context, params ConfirmTOTPEnrollmentParams) (TOTPActivation, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{Subject: params.Subject, Action: AuthorizationIdentityFactorsManage}); err != nil {
		return TOTPActivation{}, err
	}
	if err := validateID(params.ChallengeID, "challengeId"); err != nil {
		return TOTPActivation{}, ErrMFAChallengeInvalid
	}
	code, err := normalizeTOTPCode(params.Code)
	if err != nil {
		return TOTPActivation{}, ErrMFAChallengeInvalid
	}
	sourceAddress, err := normalizeSourceAddress(params.SourceAddress)
	if err != nil {
		return TOTPActivation{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return TOTPActivation{}, err
	}
	if err := r.checkMFAAttemptAllowed(ctx, params.Subject.identityID); err != nil {
		return TOTPActivation{}, err
	}
	factorID, err := r.newID()
	if err != nil {
		return TOTPActivation{}, err
	}
	recoveryCodes, err := r.newRecoveryCodes()
	if err != nil {
		return TOTPActivation{}, err
	}
	now := r.timestamp()
	var challengeFailure bool
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		setup, err := loadTOTPSetupTx(ctx, executor, params.Subject, params.ChallengeID)
		if errors.Is(err, sql.ErrNoRows) {
			challengeFailure = true
			return nil
		}
		if err != nil {
			return err
		}
		if !setup.expiresAt.After(now) {
			if _, err := executor.ExecContext(ctx, `
				UPDATE totp_setup_challenges SET consumed_at = ?, completion_reason = 'expired'
				WHERE id = ? AND consumed_at IS NULL`, formatTime(now), string(setup.id)); err != nil {
				return err
			}
			challengeFailure = true
			return nil
		}
		secret, err := r.decryptSecret(totpSetupEnvelopeKind, setup.id, setup.identityID, setup.envelope)
		if err != nil {
			return err
		}
		counter, matches := matchTOTP(secret, code, now, nil)
		if !matches {
			clear(secret)
			nextAttempts := setup.attemptCount + 1
			if nextAttempts >= totpMaximumAttempts {
				_, err = executor.ExecContext(ctx, `
					UPDATE totp_setup_challenges
					SET attempt_count = ?, consumed_at = ?, completion_reason = 'attempts_exhausted'
					WHERE id = ? AND consumed_at IS NULL`, nextAttempts, formatTime(now), string(setup.id))
			} else {
				_, err = executor.ExecContext(ctx, `
					UPDATE totp_setup_challenges SET attempt_count = ?
					WHERE id = ? AND consumed_at IS NULL`, nextAttempts, string(setup.id))
			}
			if err != nil {
				return err
			}
			challengeFailure = true
			return nil
		}
		activeFactor, activeErr := r.loadActiveTOTPFactorTx(ctx, executor, params.Subject.identityID)
		if errors.Is(activeErr, sql.ErrNoRows) {
			if setup.replacesFactorID != nil {
				clear(secret)
				challengeFailure = true
				return nil
			}
			activeFactor = storedTOTPFactor{}
		} else if activeErr != nil {
			clear(secret)
			return activeErr
		} else if setup.replacesFactorID == nil || activeFactor.id != *setup.replacesFactorID {
			clear(secret)
			challengeFailure = true
			return nil
		}
		factorEnvelope, err := r.encryptSecret(totpSecretEnvelopeKind, factorID, params.Subject.identityID, secret)
		clear(secret)
		if err != nil {
			return err
		}
		if activeFactor.id != "" {
			if _, err := executor.ExecContext(ctx, `
				UPDATE totp_factors SET status = 'replaced', deactivated_at = ?
				WHERE id = ? AND identity_id = ? AND status = 'active'`,
				formatTime(now), string(activeFactor.id), string(params.Subject.identityID)); err != nil {
				return err
			}
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO totp_factors (
				id, identity_id, status, algorithm, digits, period_seconds,
				secret_ciphertext, secret_nonce, wrapped_dek, wrap_nonce, key_version,
				last_used_counter, created_at, activated_at
			) VALUES (?, ?, 'active', 'SHA1', 6, 30, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(factorID), string(params.Subject.identityID), factorEnvelope.Ciphertext,
			factorEnvelope.Nonce, factorEnvelope.WrappedKey, factorEnvelope.WrapNonce,
			factorEnvelope.KeyVersion, counter, formatTime(now), formatTime(now)); err != nil {
			return err
		}
		for _, recovery := range recoveryCodes {
			if _, err := executor.ExecContext(ctx, `
				INSERT INTO recovery_codes (id, factor_id, identity_id, code_hash, created_at)
				VALUES (?, ?, ?, ?, ?)`, string(recovery.id), string(factorID),
				string(params.Subject.identityID), recovery.hash[:], formatTime(now)); err != nil {
				return err
			}
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE totp_setup_challenges
			SET consumed_at = ?, completion_reason = 'activated'
			WHERE id = ? AND consumed_at IS NULL`, formatTime(now), string(setup.id)); err != nil {
			return err
		}
		revoked, err := revokeIdentitySessionsTx(ctx, executor, params.Subject.identityID, nil, "factor_changed", now)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "totp.activated", TargetType: "totp_factor",
			TargetID: string(factorID), RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"recoveryCodeCount": recoveryCodeCount, "revokedSessions": revoked},
		}, now)
	})
	if err != nil {
		return TOTPActivation{}, classifyDatabaseError(err)
	}
	if challengeFailure {
		if err := r.recordMFAFailure(ctx, params.Subject.identityID); err != nil {
			return TOTPActivation{}, err
		}
		return TOTPActivation{}, ErrMFAChallengeInvalid
	}
	if err := r.clearMFAFailures(ctx, params.Subject.identityID); err != nil {
		return TOTPActivation{}, err
	}
	rawCodes := make([]string, len(recoveryCodes))
	for index, recovery := range recoveryCodes {
		rawCodes[index] = recovery.raw
	}
	return TOTPActivation{FactorID: factorID, ActivatedAt: now, RecoveryCodes: rawCodes}, nil
}

// DisableTOTP requires the currently active factor (or one recovery code), then
// removes the factor and revokes every session for the identity.
func (r *Repository) DisableTOTP(ctx context.Context, params DisableTOTPParams) error {
	if _, err := r.Authorize(ctx, AuthorizeParams{Subject: params.Subject, Action: AuthorizationIdentityFactorsManage}); err != nil {
		return err
	}
	if len(params.CurrentFactor) > 128 {
		return fmt.Errorf("%w: current factor is too long", ErrInvalidInput)
	}
	sourceAddress, err := normalizeSourceAddress(params.SourceAddress)
	if err != nil {
		return err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	if err := r.checkMFAAttemptAllowed(ctx, params.Subject.identityID); err != nil {
		return err
	}
	now := r.timestamp()
	var factorFailure bool
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		factor, err := r.loadActiveTOTPFactorTx(ctx, executor, params.Subject.identityID)
		if errors.Is(err, sql.ErrNoRows) {
			factorFailure = true
			return nil
		}
		if err != nil {
			return err
		}
		verified, _, err := r.verifyCurrentFactorTx(ctx, executor, factor, params.CurrentFactor, now)
		if err != nil {
			return err
		}
		if !verified {
			factorFailure = true
			return nil
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE totp_factors SET status = 'removed', deactivated_at = ?
			WHERE id = ? AND identity_id = ? AND status = 'active'`,
			formatTime(now), string(factor.id), string(params.Subject.identityID)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE totp_setup_challenges SET consumed_at = ?, completion_reason = 'replaced'
			WHERE identity_id = ? AND consumed_at IS NULL`, formatTime(now), string(params.Subject.identityID)); err != nil {
			return err
		}
		revoked, err := revokeIdentitySessionsTx(ctx, executor, params.Subject.identityID, nil, "factor_changed", now)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "totp.removed", TargetType: "totp_factor",
			TargetID: string(factor.id), RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"revokedSessions": revoked},
		}, now)
	})
	if err != nil {
		return classifyDatabaseError(err)
	}
	if factorFailure {
		if err := r.recordMFAFailure(ctx, params.Subject.identityID); err != nil {
			return err
		}
		return ErrMFAChallengeInvalid
	}
	return r.clearMFAFailures(ctx, params.Subject.identityID)
}

func (r *Repository) requireSubjectSessionTx(
	ctx context.Context,
	executor store.Executor,
	subject AuthorizationSubject,
	recent bool,
	now time.Time,
) (subjectSessionFacts, error) {
	if err := r.validateAuthorizationSubject(subject); err != nil {
		return subjectSessionFacts{}, err
	}
	var facts subjectSessionFacts
	var identityStatus, authenticatedAt, lastSeenAt, expiresAt string
	if err := executor.QueryRowContext(ctx, `
		SELECT i.email, i.status, s.authenticated_at, s.last_seen_at, s.expires_at
		FROM sessions s JOIN identities i ON i.id = s.identity_id
		WHERE s.id = ? AND s.identity_id = ? AND s.revoked_at IS NULL`,
		string(subject.sessionID), string(subject.identityID)).Scan(
		&facts.email, &identityStatus, &authenticatedAt, &lastSeenAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return subjectSessionFacts{}, ErrSessionInvalid
		}
		return subjectSessionFacts{}, err
	}
	var err error
	facts.authenticatedAt, err = parseTime(authenticatedAt)
	if err != nil {
		return subjectSessionFacts{}, err
	}
	facts.lastSeenAt, err = parseTime(lastSeenAt)
	if err != nil {
		return subjectSessionFacts{}, err
	}
	facts.expiresAt, err = parseTime(expiresAt)
	if err != nil {
		return subjectSessionFacts{}, err
	}
	if IdentityStatus(identityStatus) != IdentityActive || facts.authenticatedAt.After(now) ||
		!facts.expiresAt.After(now) || !facts.lastSeenAt.Add(passwordSessionIdleTTL).After(now) {
		return subjectSessionFacts{}, ErrSessionInvalid
	}
	if recent && facts.authenticatedAt.Before(now.Add(-recentAuthenticationTTL)) {
		return subjectSessionFacts{}, ErrRecentAuthenticationRequired
	}
	return facts, nil
}

func (r *Repository) loadActiveTOTPFactorTx(ctx context.Context, executor store.Executor, identityID ID) (storedTOTPFactor, error) {
	var factor storedTOTPFactor
	var lastUsed sql.NullInt64
	var activatedAt string
	err := executor.QueryRowContext(ctx, `
		SELECT id, identity_id, secret_ciphertext, secret_nonce, wrapped_dek, wrap_nonce,
		       key_version, last_used_counter, activated_at
		FROM totp_factors WHERE identity_id = ? AND status = 'active'`, string(identityID)).Scan(
		&factor.id, &factor.identityID, &factor.envelope.Ciphertext, &factor.envelope.Nonce,
		&factor.envelope.WrappedKey, &factor.envelope.WrapNonce, &factor.envelope.KeyVersion,
		&lastUsed, &activatedAt)
	if err != nil {
		return storedTOTPFactor{}, err
	}
	if lastUsed.Valid {
		value := lastUsed.Int64
		factor.lastUsedCounter = &value
	}
	factor.activatedAt, err = parseTime(activatedAt)
	return factor, err
}

func loadTOTPSetupTx(
	ctx context.Context,
	executor store.Executor,
	subject AuthorizationSubject,
	challengeID ID,
) (storedTOTPSetup, error) {
	var setup storedTOTPSetup
	var replacesFactorID sql.NullString
	var expiresAt string
	err := executor.QueryRowContext(ctx, `
		SELECT id, identity_id, session_id, replaces_factor_id,
		       secret_ciphertext, secret_nonce, wrapped_dek, wrap_nonce, key_version,
		       expires_at, attempt_count
		FROM totp_setup_challenges
		WHERE id = ? AND identity_id = ? AND session_id = ? AND consumed_at IS NULL`,
		string(challengeID), string(subject.identityID), string(subject.sessionID)).Scan(
		&setup.id, &setup.identityID, &setup.sessionID, &replacesFactorID,
		&setup.envelope.Ciphertext, &setup.envelope.Nonce, &setup.envelope.WrappedKey,
		&setup.envelope.WrapNonce, &setup.envelope.KeyVersion, &expiresAt, &setup.attemptCount)
	if err != nil {
		return storedTOTPSetup{}, err
	}
	if replacesFactorID.Valid {
		value := ID(replacesFactorID.String)
		setup.replacesFactorID = &value
	}
	setup.expiresAt, err = parseTime(expiresAt)
	return setup, err
}

func (r *Repository) verifyCurrentFactorTx(
	ctx context.Context,
	executor store.Executor,
	factor storedTOTPFactor,
	code string,
	now time.Time,
) (bool, SessionAuthenticationLevel, error) {
	if totpCode, err := normalizeTOTPCode(code); err == nil {
		secret, err := r.decryptSecret(totpSecretEnvelopeKind, factor.id, factor.identityID, factor.envelope)
		if err != nil {
			return false, "", err
		}
		counter, matches := matchTOTP(secret, totpCode, now, factor.lastUsedCounter)
		clear(secret)
		if matches {
			result, err := executor.ExecContext(ctx, `
				UPDATE totp_factors SET last_used_counter = ?
				WHERE id = ? AND identity_id = ? AND status = 'active'
				  AND (last_used_counter IS NULL OR last_used_counter < ?)`,
				counter, string(factor.id), string(factor.identityID), counter)
			if err != nil {
				return false, "", err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return false, "", err
			}
			return rows == 1, SessionAuthenticationTOTP, nil
		}
	}
	canonicalRecovery, err := normalizeRecoveryCode(code)
	if err != nil {
		return false, "", nil
	}
	hash := sha256.Sum256([]byte(canonicalRecovery))
	var recoveryID ID
	if err := executor.QueryRowContext(ctx, `
		SELECT id FROM recovery_codes
		WHERE factor_id = ? AND identity_id = ? AND code_hash = ? AND used_at IS NULL`,
		string(factor.id), string(factor.identityID), hash[:]).Scan(&recoveryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", nil
		}
		return false, "", err
	}
	result, err := executor.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ? WHERE id = ? AND used_at IS NULL`,
		formatTime(now), string(recoveryID))
	if err != nil {
		return false, "", err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, "", err
	}
	return rows == 1, SessionAuthenticationRecovery, nil
}

func matchTOTP(secret []byte, code string, now time.Time, lastUsedCounter *int64) (int64, bool) {
	currentCounter := now.Unix() / totpPeriodSeconds
	for _, offset := range []int64{0, -1, 1} {
		counter := currentCounter + offset
		if counter < 0 || (lastUsedCounter != nil && counter <= *lastUsedCounter) {
			continue
		}
		expected := hotp(secret, uint64(counter))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return counter, true
		}
	}
	return 0, false
}

func hotp(secret []byte, counter uint64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	binaryCode := (uint32(digest[offset])&0x7f)<<24 |
		uint32(digest[offset+1])<<16 |
		uint32(digest[offset+2])<<8 |
		uint32(digest[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, binaryCode%1_000_000)
}

func normalizeTOTPCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != totpDigits {
		return "", ErrMFAChallengeInvalid
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", ErrMFAChallengeInvalid
		}
	}
	return value, nil
}

func (r *Repository) newRecoveryCodes() ([]generatedRecoveryCode, error) {
	codes := make([]generatedRecoveryCode, recoveryCodeCount)
	for index := range codes {
		id, err := r.newID()
		if err != nil {
			return nil, err
		}
		random := make([]byte, recoveryCodeRandomBytes)
		if _, err := io.ReadFull(r.random, random); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		hexValue := hex.EncodeToString(random)
		clear(random)
		groups := make([]string, 0, 8)
		for offset := 0; offset < len(hexValue); offset += 4 {
			groups = append(groups, hexValue[offset:offset+4])
		}
		raw := "sfrc_" + strings.Join(groups, "-")
		codes[index] = generatedRecoveryCode{id: id, raw: raw, hash: sha256.Sum256([]byte(raw))}
	}
	return codes, nil
}

func normalizeRecoveryCode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "sfrc_") {
		return "", ErrMFAChallengeInvalid
	}
	hexValue := strings.ReplaceAll(strings.TrimPrefix(value, "sfrc_"), "-", "")
	if len(hexValue) != recoveryCodeRandomBytes*2 {
		return "", ErrMFAChallengeInvalid
	}
	if _, err := hex.DecodeString(hexValue); err != nil {
		return "", ErrMFAChallengeInvalid
	}
	groups := make([]string, 0, 8)
	for offset := 0; offset < len(hexValue); offset += 4 {
		groups = append(groups, hexValue[offset:offset+4])
	}
	return "sfrc_" + strings.Join(groups, "-"), nil
}

func totpProvisioningURI(email, secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", "Stackfort")
	query.Set("algorithm", "SHA1")
	query.Set("digits", strconv.Itoa(totpDigits))
	query.Set("period", strconv.Itoa(totpPeriodSeconds))
	return (&url.URL{Scheme: "otpauth", Host: "totp", Path: "/Stackfort:" + email, RawQuery: query.Encode()}).String()
}

func (r *Repository) checkMFAAttemptAllowed(ctx context.Context, identityID ID) error {
	now := r.timestamp()
	var retryAfter time.Duration
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		retryAfter, err = mfaAttemptRetryTx(ctx, reader, identityID, now)
		return err
	})
	if err != nil {
		return err
	}
	if retryAfter > 0 {
		return &AuthenticationRateLimitError{RetryAfter: retryAfter}
	}
	return nil
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func mfaAttemptRetryTx(
	ctx context.Context,
	querier rowQuerier,
	identityID ID,
	now time.Time,
) (time.Duration, error) {
	var blockedUntil sql.NullString
	err := querier.QueryRowContext(ctx, `
		SELECT blocked_until FROM mfa_attempt_limits WHERE identity_id = ?`,
		string(identityID)).Scan(&blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !blockedUntil.Valid {
		return 0, nil
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

func (r *Repository) recordMFAFailure(ctx context.Context, identityID ID) error {
	now := r.timestamp()
	return r.state.Write(ctx, func(executor store.Executor) error {
		var windowStarted string
		var failureCount int64
		var blockedUntil sql.NullString
		err := executor.QueryRowContext(ctx, `
			SELECT window_started_at, failure_count, blocked_until
			FROM mfa_attempt_limits WHERE identity_id = ?`, string(identityID)).Scan(
			&windowStarted, &failureCount, &blockedUntil)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = executor.ExecContext(ctx, `
				INSERT INTO mfa_attempt_limits (
					identity_id, window_started_at, failure_count, blocked_until, updated_at
				) VALUES (?, ?, 1, NULL, ?)`, string(identityID), formatTime(now), formatTime(now))
			return err
		}
		if err != nil {
			return err
		}
		window, err := parseTime(windowStarted)
		if err != nil {
			return err
		}
		if !window.Add(mfaAttemptWindow).After(now) {
			window = now
			failureCount = 0
		}
		failureCount++
		var nextBlocked any
		if failureCount >= mfaAttemptLimit {
			nextBlocked = formatTime(now.Add(mfaAttemptBlock))
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE mfa_attempt_limits
			SET window_started_at = ?, failure_count = ?, blocked_until = ?, updated_at = ?
			WHERE identity_id = ?`, formatTime(window), failureCount, nextBlocked,
			formatTime(now), string(identityID))
		return err
	})
}

func (r *Repository) clearMFAFailures(ctx context.Context, identityID ID) error {
	return r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `DELETE FROM mfa_attempt_limits WHERE identity_id = ?`, string(identityID))
		return err
	})
}

func nullableIDValue(value ID) any {
	if value == "" {
		return nil
	}
	return string(value)
}
