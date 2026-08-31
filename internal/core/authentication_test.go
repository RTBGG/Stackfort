// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestPasswordLoginCreatesHashOnlySessionAndBoundCSRF(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	identity := createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	ctx := context.Background()
	result, err := repository.PasswordLogin(ctx, validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.10"))
	if err != nil {
		t.Fatalf("PasswordLogin: %v", err)
	}
	if result.Identity.ID != identity.ID || result.Session.IdentityID != identity.ID ||
		!bytes.HasPrefix([]byte(result.SessionToken), []byte(sessionTokenPrefix)) ||
		!bytes.HasPrefix([]byte(result.CSRFToken), []byte(csrfTokenPrefix)) {
		t.Fatalf("unexpected login result: %#v", result)
	}
	wantSessionHash := sha256.Sum256([]byte(result.SessionToken))
	wantCSRFHash := sha256.Sum256([]byte(result.CSRFToken))
	var storedSessionHash, storedCSRFHash []byte
	var rawMatches int
	var auditDetails string
	if err := state.Read(ctx, func(reader store.Reader) error {
		if err := reader.QueryRowContext(ctx, `
			SELECT token_hash, csrf_secret_hash FROM sessions WHERE id = ?`,
			string(result.Session.ID)).Scan(&storedSessionHash, &storedCSRFHash); err != nil {
			return err
		}
		if err := reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sessions
			WHERE CAST(token_hash AS TEXT) = ? OR CAST(csrf_secret_hash AS TEXT) = ?`,
			result.SessionToken, result.CSRFToken).Scan(&rawMatches); err != nil {
			return err
		}
		return reader.QueryRowContext(ctx, `
			SELECT COALESCE(group_concat(details_json), '') FROM audit_events`).Scan(&auditDetails)
	}); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !bytes.Equal(storedSessionHash, wantSessionHash[:]) || !bytes.Equal(storedCSRFHash, wantCSRFHash[:]) || rawMatches != 0 {
		t.Fatal("session storage did not contain exactly the secret digests")
	}
	if strings.Contains(auditDetails, result.SessionToken) || strings.Contains(auditDetails, result.CSRFToken) {
		t.Fatal("audit details contained raw session material")
	}

	authenticated, err := repository.AuthenticateSession(ctx, AuthenticateSessionParams{SessionToken: result.SessionToken})
	if err != nil || authenticated.Identity.ID != identity.ID {
		t.Fatalf("AuthenticateSession: result=%#v error=%v", authenticated, err)
	}
	if _, err := repository.AuthenticateSession(ctx, AuthenticateSessionParams{
		SessionToken: result.SessionToken, RequireCSRF: true,
		CSRFHeaderToken: result.CSRFToken, CSRFCookieToken: result.CSRFToken,
	}); err != nil {
		t.Fatalf("AuthenticateSession with CSRF: %v", err)
	}
	if _, err := repository.AuthenticateSession(ctx, AuthenticateSessionParams{
		SessionToken: result.SessionToken, RequireCSRF: true,
		CSRFHeaderToken: "sfc_wrong", CSRFCookieToken: result.CSRFToken,
	}); !errors.Is(err, ErrCSRFInvalid) {
		t.Fatalf("wrong CSRF error = %v, want ErrCSRFInvalid", err)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestPasswordLoginUnknownAndWrongPasswordUseSameDenialAndDerivation(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	createPasswordIdentity(t, repository, "known@example.com", "correct horse battery staple")
	var derivations atomic.Int64
	baseDeriver := repository.derivePassword
	repository.derivePassword = func(password, salt []byte, iterations, memory uint32, parallelism uint8, length uint32) []byte {
		derivations.Add(1)
		return baseDeriver(password, salt, iterations, memory, parallelism, length)
	}

	_, wrongErr := repository.PasswordLogin(context.Background(), validPasswordLogin("known@example.com", "incorrect password value", "192.0.2.1"))
	_, unknownErr := repository.PasswordLogin(context.Background(), validPasswordLogin("unknown@example.com", "incorrect password value", "192.0.2.2"))
	if !errors.Is(wrongErr, ErrAuthenticationDenied) || !errors.Is(unknownErr, ErrAuthenticationDenied) {
		t.Fatalf("wrong error=%v unknown error=%v", wrongErr, unknownErr)
	}
	if derivations.Load() != 2 {
		t.Fatalf("derivations = %d, want 2", derivations.Load())
	}
	var sessions, leakedEmailKeys int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		if err := reader.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sessions").Scan(&sessions); err != nil {
			return err
		}
		return reader.QueryRowContext(context.Background(), `
			SELECT COUNT(*) FROM authentication_rate_limits
			WHERE scope = 'identity' AND rate_key LIKE '%@%'`).Scan(&leakedEmailKeys)
	}); err != nil {
		t.Fatalf("inspect failed login state: %v", err)
	}
	if sessions != 0 || leakedEmailKeys != 0 {
		t.Fatalf("sessions=%d leaked email keys=%d", sessions, leakedEmailKeys)
	}
}

func TestSuspendedIdentityUsesGenericPasswordDenial(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	identity := createPasswordIdentity(t, repository, "suspended@example.com", "correct horse battery staple")
	if err := state.Write(context.Background(), func(executor store.Executor) error {
		_, err := executor.ExecContext(context.Background(), `
			UPDATE identities SET status = 'suspended' WHERE id = ?`, string(identity.ID))
		return err
	}); err != nil {
		t.Fatalf("suspend identity: %v", err)
	}
	var derivations atomic.Int64
	base := repository.derivePassword
	repository.derivePassword = countingDeriver(&derivations, base)
	_, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"suspended@example.com", "correct horse battery staple", "192.0.2.3",
	))
	if !errors.Is(err, ErrAuthenticationDenied) || derivations.Load() != 1 {
		t.Fatalf("suspended login error=%v derivations=%d", err, derivations.Load())
	}
}

func TestOversizedLoginPasswordIsRejectedBeforeDerivation(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	var derivations atomic.Int64
	base := repository.derivePassword
	repository.derivePassword = countingDeriver(&derivations, base)
	_, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", strings.Repeat("x", bootstrapMaximumBytes+1), "192.0.2.4",
	))
	if !errors.Is(err, ErrInvalidInput) || derivations.Load() != 0 {
		t.Fatalf("oversized login error=%v derivations=%d", err, derivations.Load())
	}
}

func TestIdentityLoginLimitSurvivesRepositoryRestartBeforeHashing(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	fixedNow := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixedNow }
	var derivations atomic.Int64
	baseDeriver := repository.derivePassword
	repository.derivePassword = countingDeriver(&derivations, baseDeriver)
	params := validPasswordLogin("admin@example.com", "incorrect password value", "192.0.2.40")
	for attempt := 1; attempt <= loginIdentityLimit; attempt++ {
		if _, err := repository.PasswordLogin(context.Background(), params); !errors.Is(err, ErrAuthenticationDenied) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
	}

	restarted, err := NewRepository(state)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	restarted.now = func() time.Time { return fixedNow }
	restarted.derivePassword = countingDeriver(&derivations, baseDeriver)
	_, err = restarted.PasswordLogin(context.Background(), params)
	var limited *AuthenticationRateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter != loginIdentityBlock {
		t.Fatalf("restart rate error = %#v", err)
	}
	if derivations.Load() != loginIdentityLimit {
		t.Fatalf("rate-limited request reached hashing: derivations=%d", derivations.Load())
	}
}

func TestSourceAndGlobalLoginPressureLimitsRunBeforeHashing(t *testing.T) {
	t.Parallel()

	t.Run("source", func(t *testing.T) {
		repository, _ := newTestRepository(t)
		installFastDeriver(repository)
		var derivations atomic.Int64
		base := repository.derivePassword
		repository.derivePassword = countingDeriver(&derivations, base)
		for attempt := 1; attempt <= loginSourceLimit; attempt++ {
			params := validPasswordLogin(fmt.Sprintf("missing-%d@example.com", attempt), "incorrect password value", "192.0.2.50")
			if _, err := repository.PasswordLogin(context.Background(), params); !errors.Is(err, ErrAuthenticationDenied) {
				t.Fatalf("attempt %d error = %v", attempt, err)
			}
		}
		_, err := repository.PasswordLogin(context.Background(), validPasswordLogin("another@example.com", "incorrect password value", "192.0.2.50"))
		if !errors.Is(err, ErrAuthenticationRateLimited) || derivations.Load() != loginSourceLimit {
			t.Fatalf("source limit error=%v derivations=%d", err, derivations.Load())
		}
	})

	t.Run("global", func(t *testing.T) {
		repository, _ := newTestRepository(t)
		installFastDeriver(repository)
		var derivations atomic.Int64
		base := repository.derivePassword
		repository.derivePassword = countingDeriver(&derivations, base)
		for attempt := 1; attempt <= loginGlobalLimit; attempt++ {
			source := fmt.Sprintf("198.51.%d.%d", (attempt-1)/250, (attempt-1)%250+1)
			params := validPasswordLogin(fmt.Sprintf("missing-global-%d@example.com", attempt), "incorrect password value", source)
			if _, err := repository.PasswordLogin(context.Background(), params); !errors.Is(err, ErrAuthenticationDenied) {
				t.Fatalf("attempt %d error = %v", attempt, err)
			}
		}
		_, err := repository.PasswordLogin(context.Background(), validPasswordLogin("final@example.com", "incorrect password value", "203.0.113.1"))
		if !errors.Is(err, ErrAuthenticationRateLimited) || derivations.Load() != loginGlobalLimit {
			t.Fatalf("global limit error=%v derivations=%d", err, derivations.Load())
		}
	})
}

func TestSuccessfulLoginClearsIdentityFailureCounter(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	for range 4 {
		_, _ = repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "incorrect password value", "192.0.2.60"))
	}
	if _, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.61")); err != nil {
		t.Fatalf("successful PasswordLogin: %v", err)
	}
	if _, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "incorrect password value", "192.0.2.62")); !errors.Is(err, ErrAuthenticationDenied) {
		t.Fatalf("failure after success: %v", err)
	}
	var attempts int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), `
			SELECT attempt_count FROM authentication_rate_limits
			WHERE scope = 'identity' AND rate_key = ?`, authenticationIdentityKey("admin@example.com")).Scan(&attempts)
	}); err != nil {
		t.Fatalf("read identity counter: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("identity attempt count = %d, want 1", attempts)
	}
}

func TestLoginRotatesPresentedSessionAndPreventsReplay(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	first, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.70"))
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	params := validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.70")
	params.PreviousSessionToken = first.SessionToken
	second, err := repository.PasswordLogin(context.Background(), params)
	if err != nil {
		t.Fatalf("rotating login: %v", err)
	}
	if first.SessionToken == second.SessionToken {
		t.Fatal("login rotation reused the session token")
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{SessionToken: first.SessionToken}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("old session replay error = %v", err)
	}
	if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{SessionToken: second.SessionToken}); err != nil {
		t.Fatalf("new session: %v", err)
	}
	var reason string
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), `
			SELECT revocation_reason FROM sessions WHERE id = ?`, string(first.Session.ID)).Scan(&reason)
	}); err != nil {
		t.Fatalf("read rotation: %v", err)
	}
	if reason != "login_rotation" {
		t.Fatalf("rotation reason = %q", reason)
	}
}

func TestCSRFTokenCannotBeReplayedAcrossSessions(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	first, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.80"))
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.81"))
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	_, err = repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
		SessionToken: second.SessionToken, RequireCSRF: true,
		CSRFHeaderToken: first.CSRFToken, CSRFCookieToken: first.CSRFToken,
	})
	if !errors.Is(err, ErrCSRFInvalid) {
		t.Fatalf("cross-session CSRF error = %v", err)
	}
}

func TestRevokeSessionAndServerSideExpiryPreventReplay(t *testing.T) {
	t.Parallel()

	t.Run("logout", func(t *testing.T) {
		repository, _ := newTestRepository(t)
		identity := createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
		result, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.90"))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if err := repository.RevokeSession(context.Background(), RevokeSessionParams{
			IdentityID: identity.ID, SessionID: result.Session.ID, Reason: "logout", SourceAddress: "192.0.2.90",
		}); err != nil {
			t.Fatalf("RevokeSession: %v", err)
		}
		if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{SessionToken: result.SessionToken}); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("revoked replay error = %v", err)
		}
	})

	t.Run("idle", func(t *testing.T) {
		repository, state := newTestRepository(t)
		createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
		current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
		repository.now = func() time.Time { return current }
		result, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.91"))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		current = current.Add(passwordSessionIdleTTL)
		if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{SessionToken: result.SessionToken}); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("idle expiry error = %v", err)
		}
		assertSessionRevocationReason(t, state, result.Session.ID, "idle_expiry")
	})

	t.Run("absolute", func(t *testing.T) {
		repository, state := newTestRepository(t)
		createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
		current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
		repository.now = func() time.Time { return current }
		result, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.92"))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		current = current.Add(passwordSessionAbsoluteTTL)
		if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{SessionToken: result.SessionToken}); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("absolute expiry error = %v", err)
		}
		assertSessionRevocationReason(t, state, result.Session.ID, "absolute_expiry")
	})
}

func TestSessionLastSeenTouchIsPersisted(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	result, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.100"))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	current = current.Add(2 * passwordSessionTouch)
	authenticated, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{SessionToken: result.SessionToken})
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}
	if !authenticated.Session.LastSeenAt.Equal(current) {
		t.Fatalf("returned last seen = %s, want %s", authenticated.Session.LastSeenAt, current)
	}
	var stored string
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), "SELECT last_seen_at FROM sessions WHERE id = ?", string(result.Session.ID)).Scan(&stored)
	}); err != nil {
		t.Fatalf("read last seen: %v", err)
	}
	if stored != formatTime(current) {
		t.Fatalf("stored last seen = %q, want %q", stored, formatTime(current))
	}
}

func TestCredentialChangeDuringLoginPreventsSessionCreation(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	identity := createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	entered := make(chan struct{})
	release := make(chan struct{})
	base := repository.derivePassword
	repository.derivePassword = func(password, salt []byte, iterations, memory uint32, parallelism uint8, length uint32) []byte {
		close(entered)
		<-release
		return base(password, salt, iterations, memory, parallelism, length)
	}
	result := make(chan error, 1)
	go func() {
		_, err := repository.PasswordLogin(context.Background(), validPasswordLogin("admin@example.com", "correct horse battery staple", "192.0.2.110"))
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("password derivation did not start")
	}
	newSalt := bytes.Repeat([]byte{0x55}, 16)
	newHash := fastPasswordHash("a different secure password", newSalt)
	if err := repository.SetPasswordCredential(context.Background(), SetPasswordCredentialParams{
		IdentityID: identity.ID, Hash: newHash, Salt: newSalt,
		MemoryKiB: bootstrapArgonMemory, Iterations: bootstrapArgonTime,
		Parallelism: bootstrapArgonThreads, Version: 19,
	}); err != nil {
		t.Fatalf("SetPasswordCredential: %v", err)
	}
	close(release)
	if err := <-result; !errors.Is(err, ErrAuthenticationDenied) {
		t.Fatalf("racing login error = %v, want ErrAuthenticationDenied", err)
	}
	var sessions int
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sessions").Scan(&sessions)
	}); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("session count = %d, want 0", sessions)
	}
}

func TestPasswordCredentialChangeRevokesAllExistingSessions(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	identity := createPasswordIdentity(t, repository, "admin@example.com", "correct horse battery staple")
	first, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.120",
	))
	if err != nil {
		t.Fatalf("first PasswordLogin: %v", err)
	}
	second, err := repository.PasswordLogin(context.Background(), validPasswordLogin(
		"admin@example.com", "correct horse battery staple", "192.0.2.121",
	))
	if err != nil {
		t.Fatalf("second PasswordLogin: %v", err)
	}
	salt := bytes.Repeat([]byte{0x73}, 16)
	if err := repository.SetPasswordCredential(context.Background(), SetPasswordCredentialParams{
		IdentityID: identity.ID, Hash: fastPasswordHash("a newly changed secure password", salt), Salt: salt,
		MemoryKiB: bootstrapArgonMemory, Iterations: bootstrapArgonTime,
		Parallelism: bootstrapArgonThreads, Version: 19,
	}); err != nil {
		t.Fatalf("SetPasswordCredential: %v", err)
	}
	for _, token := range []string{first.SessionToken, second.SessionToken} {
		if _, err := repository.AuthenticateSession(context.Background(), AuthenticateSessionParams{
			SessionToken: token,
		}); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("credential-change replay error = %v", err)
		}
	}
}

func createPasswordIdentity(t *testing.T, repository *Repository, email, password string) Identity {
	t.Helper()
	installFastDeriver(repository)
	identity := createTestIdentity(t, repository, email)
	salt := bytes.Repeat([]byte{0x42}, 16)
	if err := repository.SetPasswordCredential(context.Background(), SetPasswordCredentialParams{
		IdentityID: identity.ID, Hash: fastPasswordHash(password, salt), Salt: salt,
		MemoryKiB: bootstrapArgonMemory, Iterations: bootstrapArgonTime,
		Parallelism: bootstrapArgonThreads, Version: 19,
	}); err != nil {
		t.Fatalf("SetPasswordCredential: %v", err)
	}
	return identity
}

func installFastDeriver(repository *Repository) {
	repository.derivePassword = func(password, salt []byte, _, _ uint32, _ uint8, length uint32) []byte {
		digest := sha256.Sum256(append(append([]byte(nil), password...), salt...))
		return append([]byte(nil), digest[:length]...)
	}
}

func fastPasswordHash(password string, salt []byte) []byte {
	digest := sha256.Sum256(append(append([]byte(nil), []byte(password)...), salt...))
	return digest[:]
}

func countingDeriver(counter *atomic.Int64, next passwordDeriver) passwordDeriver {
	return func(password, salt []byte, iterations, memory uint32, parallelism uint8, length uint32) []byte {
		counter.Add(1)
		return next(password, salt, iterations, memory, parallelism, length)
	}
}

func validPasswordLogin(email, password, source string) PasswordLoginParams {
	return PasswordLoginParams{
		Email: email, Password: password, SourceAddress: source,
		UserAgent: "Stackfort test browser", RequestID: "login-test",
	}
}

func assertSessionRevocationReason(t *testing.T, state *store.Store, sessionID ID, want string) {
	t.Helper()
	var reason string
	if err := state.Read(context.Background(), func(reader store.Reader) error {
		return reader.QueryRowContext(context.Background(), `
			SELECT revocation_reason FROM sessions WHERE id = ?`, string(sessionID)).Scan(&reason)
	}); err != nil {
		t.Fatalf("read revocation reason: %v", err)
	}
	if reason != want {
		t.Fatalf("revocation reason = %q, want %q", reason, want)
	}
}
