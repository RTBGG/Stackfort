// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestHOTPMatchesRFC6238SHA1VectorAtSixDigits(t *testing.T) {
	t.Parallel()

	// RFC 6238's SHA-1 vector at 59 seconds is 94287082 for eight digits;
	// the corresponding six-digit dynamic truncation is 287082.
	secret := []byte("12345678901234567890")
	if got := hotp(secret, 59/30); got != "287082" {
		t.Fatalf("hotp vector = %q, want 287082", got)
	}
}

func TestTOTPEnrollmentEncryptsSecretAndRequiresVerifiedActivation(t *testing.T) {
	repository, state, _ := newKeyedTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }

	login, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.10",
	))
	if err != nil {
		t.Fatalf("PasswordLogin: %v", err)
	}
	authenticated := authenticateForTest(t, repository, login.SessionToken)
	enrollment, err := repository.BeginTOTPEnrollment(context.Background(), BeginTOTPEnrollmentParams{
		Subject: authenticated.AuthorizationSubject(), SourceAddress: "192.0.2.10",
	})
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil || len(secret) != totpSecretBytes {
		t.Fatalf("decode enrollment secret: length=%d error=%v", len(secret), err)
	}
	if enrollment.ProvisioningURI == "" || !enrollment.ExpiresAt.Equal(current.Add(totpSetupTTL)) {
		t.Fatalf("unexpected enrollment: %#v", enrollment)
	}
	var ciphertext []byte
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), `
			SELECT secret_ciphertext FROM totp_setup_challenges WHERE id = ?`,
			string(enrollment.ChallengeID)).Scan(&ciphertext)
	}); err != nil {
		t.Fatalf("read setup ciphertext: %v", err)
	}
	if bytes.Contains(ciphertext, secret) || bytes.Contains(ciphertext, []byte(enrollment.Secret)) {
		t.Fatal("setup persisted plaintext TOTP material")
	}

	_, err = repository.ConfirmTOTPEnrollment(context.Background(), ConfirmTOTPEnrollmentParams{
		Subject: authenticated.AuthorizationSubject(), ChallengeID: enrollment.ChallengeID,
		Code: "000000", SourceAddress: "192.0.2.10",
	})
	if !errors.Is(err, ErrMFAChallengeInvalid) {
		t.Fatalf("wrong activation code error = %v", err)
	}
	activation, err := repository.ConfirmTOTPEnrollment(context.Background(), ConfirmTOTPEnrollmentParams{
		Subject: authenticated.AuthorizationSubject(), ChallengeID: enrollment.ChallengeID,
		Code: hotp(secret, uint64(current.Unix()/totpPeriodSeconds)), SourceAddress: "192.0.2.10",
	})
	if err != nil {
		t.Fatalf("ConfirmTOTPEnrollment: %v", err)
	}
	if len(activation.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("recovery code count = %d, want %d", len(activation.RecoveryCodes), recoveryCodeCount)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: login.SessionToken,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("pre-factor session replay error = %v", err)
	}
	var factorCiphertext, storedRecoveryHash []byte
	var auditDetails string
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		if err := reader.QueryRowContext(context.Background(), `
			SELECT secret_ciphertext FROM totp_factors WHERE id = ?`,
			string(activation.FactorID)).Scan(&factorCiphertext); err != nil {
			return err
		}
		if err := reader.QueryRowContext(context.Background(), `
			SELECT code_hash FROM recovery_codes WHERE factor_id = ? ORDER BY id LIMIT 1`,
			string(activation.FactorID)).Scan(&storedRecoveryHash); err != nil {
			return err
		}
		return reader.QueryRowContext(context.Background(), `
			SELECT COALESCE(group_concat(details_json), '') FROM audit_events`).Scan(&auditDetails)
	}); err != nil {
		t.Fatalf("read factor storage: %v", err)
	}
	if bytes.Contains(factorCiphertext, secret) || len(storedRecoveryHash) != sha256.Size {
		t.Fatal("factor or recovery storage is not encrypted/hash-only")
	}
	for _, raw := range activation.RecoveryCodes {
		if bytes.Equal(storedRecoveryHash, []byte(raw)) {
			t.Fatal("recovery code was stored in plaintext")
		}
		if strings.Contains(auditDetails, raw) {
			t.Fatal("audit details contained a recovery code")
		}
	}
	if strings.Contains(auditDetails, enrollment.Secret) ||
		strings.Contains(auditDetails, enrollment.ProvisioningURI) {
		t.Fatal("audit details contained TOTP provisioning material")
	}
}

func TestMFALoginWithTOTPAndSingleUseRecoveryCode(t *testing.T) {
	repository, state, _ := newKeyedTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	secret, recoveryCodes := activateTOTPForTest(t, repository)

	passwordResult, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.20",
	))
	if err != nil {
		t.Fatalf("MFA PasswordLogin: %v", err)
	}
	if !passwordResult.MFARequired || passwordResult.SessionToken != "" || passwordResult.CSRFToken != "" {
		t.Fatalf("password phase created browser secrets: %#v", passwordResult)
	}
	var activeSessions int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), `
			SELECT COUNT(*) FROM sessions WHERE revoked_at IS NULL`).Scan(&activeSessions)
	}); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if activeSessions != 0 {
		t.Fatalf("active sessions after password phase = %d, want 0", activeSessions)
	}

	if _, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: passwordResult.MFAChallengeToken, Code: "000000",
	}); !errors.Is(err, ErrMFAChallengeInvalid) {
		t.Fatalf("wrong MFA code error = %v", err)
	}
	current = current.Add(totpPeriodSeconds * time.Second)
	totpResult, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: passwordResult.MFAChallengeToken,
		Code:           hotp(secret, uint64(current.Unix()/totpPeriodSeconds)),
	})
	if err != nil {
		t.Fatalf("CompleteMFALogin TOTP: %v", err)
	}
	if totpResult.Session.AuthenticationLevel != SessionAuthenticationTOTP ||
		totpResult.Session.MFAAuthenticatedAt == nil {
		t.Fatalf("unexpected TOTP session: %#v", totpResult.Session)
	}
	if _, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: passwordResult.MFAChallengeToken,
		Code:           hotp(secret, uint64(current.Unix()/totpPeriodSeconds)),
	}); !errors.Is(err, ErrMFAChallengeInvalid) {
		t.Fatalf("challenge replay error = %v", err)
	}

	next, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.21",
	))
	if err != nil {
		t.Fatalf("recovery PasswordLogin: %v", err)
	}
	recoveryResult, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: next.MFAChallengeToken, Code: recoveryCodes[0],
	})
	if err != nil {
		t.Fatalf("CompleteMFALogin recovery: %v", err)
	}
	if recoveryResult.Session.AuthenticationLevel != SessionAuthenticationRecovery {
		t.Fatalf("recovery session level = %q", recoveryResult.Session.AuthenticationLevel)
	}
	replay, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.22",
	))
	if err != nil {
		t.Fatalf("replay PasswordLogin: %v", err)
	}
	if _, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: replay.MFAChallengeToken, Code: recoveryCodes[0],
	}); !errors.Is(err, ErrMFAChallengeInvalid) {
		t.Fatalf("recovery replay error = %v", err)
	}
}

func TestTOTPReplacementRequiresCurrentFactorAndRemovalRevokesSessions(t *testing.T) {
	repository, state, _ := newKeyedTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	_, originalRecoveryCodes := activateTOTPForTest(t, repository)

	login, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.30",
	))
	if err != nil {
		t.Fatalf("replacement PasswordLogin: %v", err)
	}
	completed, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: login.MFAChallengeToken, Code: originalRecoveryCodes[0],
	})
	if err != nil {
		t.Fatalf("replacement CompleteMFALogin: %v", err)
	}
	subject := authenticateForTest(t, repository, completed.SessionToken).AuthorizationSubject()
	if _, err := repository.BeginTOTPEnrollment(context.Background(), BeginTOTPEnrollmentParams{
		Subject: subject, CurrentFactor: "000000", SourceAddress: "192.0.2.30",
	}); !errors.Is(err, ErrMFAChallengeInvalid) {
		t.Fatalf("replacement without current factor error = %v", err)
	}
	enrollment, err := repository.BeginTOTPEnrollment(context.Background(), BeginTOTPEnrollmentParams{
		Subject: subject, CurrentFactor: originalRecoveryCodes[1], SourceAddress: "192.0.2.30",
	})
	if err != nil {
		t.Fatalf("Begin replacement: %v", err)
	}
	replacementSecret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatalf("decode replacement secret: %v", err)
	}
	replacement, err := repository.ConfirmTOTPEnrollment(context.Background(), ConfirmTOTPEnrollmentParams{
		Subject: subject, ChallengeID: enrollment.ChallengeID,
		Code:          hotp(replacementSecret, uint64(current.Unix()/totpPeriodSeconds)),
		SourceAddress: "192.0.2.30",
	})
	if err != nil {
		t.Fatalf("Confirm replacement: %v", err)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: completed.SessionToken,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("replacement session replay error = %v", err)
	}
	var activeFactors, replacedFactors int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		if err := reader.QueryRowContext(context.Background(), `
			SELECT COUNT(*) FROM totp_factors WHERE status = 'active'`).Scan(&activeFactors); err != nil {
			return err
		}
		return reader.QueryRowContext(context.Background(), `
			SELECT COUNT(*) FROM totp_factors WHERE status = 'replaced'`).Scan(&replacedFactors)
	}); err != nil {
		t.Fatalf("inspect replacement: %v", err)
	}
	if activeFactors != 1 || replacedFactors != 1 {
		t.Fatalf("active factors=%d replaced factors=%d", activeFactors, replacedFactors)
	}

	next, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.31",
	))
	if err != nil {
		t.Fatalf("post-replacement PasswordLogin: %v", err)
	}
	if _, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: next.MFAChallengeToken, Code: originalRecoveryCodes[2],
	}); !errors.Is(err, ErrMFAChallengeInvalid) {
		t.Fatalf("replaced factor recovery error = %v", err)
	}
	postReplacement, err := repository.CompleteMFALogin(context.Background(), CompleteMFALoginParams{
		ChallengeToken: next.MFAChallengeToken, Code: replacement.RecoveryCodes[0],
	})
	if err != nil {
		t.Fatalf("replacement recovery login: %v", err)
	}
	postReplacementSubject := authenticateForTest(t, repository, postReplacement.SessionToken).AuthorizationSubject()
	if err := repository.DisableTOTP(context.Background(), DisableTOTPParams{
		Subject: postReplacementSubject, CurrentFactor: replacement.RecoveryCodes[1],
		SourceAddress: "192.0.2.31",
	}); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: postReplacement.SessionToken,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("factor-removal session replay error = %v", err)
	}
	passwordOnly, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.32",
	))
	if err != nil || passwordOnly.MFARequired || passwordOnly.SessionToken == "" {
		t.Fatalf("password-only login after removal = %#v, %v", passwordOnly, err)
	}
}

func TestSecretEnvelopeBindsContextAndSurvivesSameKeyRestart(t *testing.T) {
	repository, state, key := newKeyedTestRepository(t)
	recordID, _ := repository.newID()
	identityID, _ := repository.newID()
	secret := []byte("retrievable test secret")
	envelope, err := repository.encryptSecret("test-kind", recordID, identityID, secret)
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	decrypted, err := repository.decryptSecret("test-kind", recordID, identityID, envelope)
	if err != nil || !bytes.Equal(decrypted, secret) {
		t.Fatalf("decryptSecret: value=%q error=%v", decrypted, err)
	}
	clear(decrypted)

	restarted, err := NewRepositoryWithMasterKey(state, key)
	if err != nil {
		t.Fatalf("restart repository: %v", err)
	}
	decrypted, err = restarted.decryptSecret("test-kind", recordID, identityID, envelope)
	if err != nil || !bytes.Equal(decrypted, secret) {
		t.Fatalf("same-key restart decrypt: value=%q error=%v", decrypted, err)
	}
	clear(decrypted)
	if _, err := restarted.decryptSecret("other-kind", recordID, identityID, envelope); err == nil {
		t.Fatal("envelope accepted the wrong purpose")
	}
	tampered := envelope
	tampered.Ciphertext = append([]byte(nil), envelope.Ciphertext...)
	tampered.Ciphertext[0] ^= 0x80
	if _, err := restarted.decryptSecret("test-kind", recordID, identityID, tampered); err == nil {
		t.Fatal("envelope accepted tampered ciphertext")
	}
	wrongKey := bytes.Repeat([]byte{0x7f}, 32)
	wrongRepository, err := NewRepositoryWithMasterKey(state, wrongKey)
	if err != nil {
		t.Fatalf("wrong-key repository: %v", err)
	}
	if _, err := wrongRepository.decryptSecret("test-kind", recordID, identityID, envelope); err == nil {
		t.Fatal("envelope accepted a different host key")
	}
	unkeyed, err := NewRepository(state)
	if err != nil {
		t.Fatalf("unkeyed repository: %v", err)
	}
	if _, err := unkeyed.decryptSecret("test-kind", recordID, identityID, envelope); !errors.Is(err, ErrSecretStorageUnavailable) {
		t.Fatalf("unkeyed decrypt error = %v", err)
	}
}

func activateTOTPForTest(t *testing.T, repository *Repository) ([]byte, []string) {
	t.Helper()
	login, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.1",
	))
	if err != nil {
		t.Fatalf("initial PasswordLogin: %v", err)
	}
	authenticated := authenticateForTest(t, repository, login.SessionToken)
	enrollment, err := repository.BeginTOTPEnrollment(context.Background(), BeginTOTPEnrollmentParams{
		Subject: authenticated.AuthorizationSubject(), SourceAddress: "192.0.2.1",
	})
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatalf("decode setup secret: %v", err)
	}
	activation, err := repository.ConfirmTOTPEnrollment(context.Background(), ConfirmTOTPEnrollmentParams{
		Subject: authenticated.AuthorizationSubject(), ChallengeID: enrollment.ChallengeID,
		Code:          hotp(secret, uint64(repository.timestamp().Unix()/totpPeriodSeconds)),
		SourceAddress: "192.0.2.1",
	})
	if err != nil {
		t.Fatalf("ConfirmTOTPEnrollment: %v", err)
	}
	return secret, activation.RecoveryCodes
}

func authenticateForTest(t *testing.T, repository *Repository, token string) AuthenticatedSession {
	t.Helper()
	authenticated, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: token,
	})
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}
	return authenticated
}

func newKeyedTestRepository(t *testing.T) (*Repository, *store.Store, []byte) {
	t.Helper()
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	key := bytes.Repeat([]byte{0x51}, 32)
	repository, err := NewRepositoryWithMasterKey(state, key)
	if err != nil {
		t.Fatalf("NewRepositoryWithMasterKey: %v", err)
	}
	return repository, state, key
}
