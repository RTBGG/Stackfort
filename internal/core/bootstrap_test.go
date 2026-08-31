// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/store"
	"golang.org/x/crypto/argon2"
)

func TestBootstrapCapabilityStoresOnlyDigestAndCanBeReplaced(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	capability, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	if len(capability.Token) <= len(bootstrapTokenPrefix) || capability.Token[:len(bootstrapTokenPrefix)] != bootstrapTokenPrefix {
		t.Fatalf("token = %q, want prefixed capability", capability.Token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(capability.Token[len(bootstrapTokenPrefix):])
	if err != nil || len(raw) != bootstrapTokenBytes {
		t.Fatalf("decode token: length=%d error=%v", len(raw), err)
	}

	wantHash := sha256.Sum256([]byte(capability.Token))
	var storedHash []byte
	var storedTextMatches int
	if err := state.Read(ctx, func(reader store.Reader) error {
		if err := reader.QueryRowContext(ctx, `
			SELECT token_hash FROM bootstrap_capabilities WHERE id = ?`,
			string(capability.ID)).Scan(&storedHash); err != nil {
			return err
		}
		return reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM bootstrap_capabilities
			WHERE CAST(token_hash AS TEXT) = ?`, capability.Token).Scan(&storedTextMatches)
	}); err != nil {
		t.Fatalf("read capability: %v", err)
	}
	if !bytes.Equal(storedHash, wantHash[:]) || storedTextMatches != 0 {
		t.Fatalf("stored capability is not exactly its digest")
	}

	status, err := repository.AdministratorBootstrapStatus(ctx)
	if err != nil {
		t.Fatalf("AdministratorBootstrapStatus: %v", err)
	}
	if !status.Required || !status.CapabilityActive || status.ExpiresAt == nil {
		t.Fatalf("unexpected bootstrap status: %#v", status)
	}
	if _, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second active capability error = %v, want ErrConflict", err)
	}
	replacement, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{Replace: true})
	if err != nil {
		t.Fatalf("replace capability: %v", err)
	}
	if replacement.Token == capability.Token {
		t.Fatal("replacement reused the raw token")
	}
	var invalidationReason string
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT invalidation_reason FROM bootstrap_capabilities WHERE id = ?`,
			string(capability.ID)).Scan(&invalidationReason)
	}); err != nil {
		t.Fatalf("read invalidated capability: %v", err)
	}
	if invalidationReason != "replaced" {
		t.Fatalf("invalidation reason = %q, want replaced", invalidationReason)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `DELETE FROM bootstrap_capabilities WHERE id = ?`, string(capability.ID))
		return err
	}); err == nil {
		t.Fatal("terminal bootstrap capability was deletable")
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			UPDATE bootstrap_capabilities SET invalidation_reason = 'expired' WHERE id = ?`,
			string(capability.ID))
		return err
	}); err == nil {
		t.Fatal("terminal bootstrap capability was mutable")
	}
}

func TestBootstrapAdministratorIsAtomicAndConsumesCapability(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	capability, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	password := "correct horse battery staple"
	identity, err := repository.BootstrapAdministrator(ctx, BootstrapAdministratorParams{
		Token:         capability.Token,
		Email:         "Admin@Example.com",
		DisplayName:   "Platform Administrator",
		Password:      password,
		Locale:        LocaleGerman,
		SourceAddress: "2001:0db8::1",
		RequestID:     "bootstrap-test",
	})
	if err != nil {
		t.Fatalf("BootstrapAdministrator: %v", err)
	}
	if identity.NormalizedEmail != "admin@example.com" || identity.Locale != LocaleGerman {
		t.Fatalf("unexpected identity: %#v", identity)
	}

	var hash, salt []byte
	var memory, iterations, parallelism, version int64
	var role, consumedBy, sourceAddress string
	if err := state.Read(ctx, func(reader store.Reader) error {
		if err := reader.QueryRowContext(ctx, `
			SELECT password_hash, salt, memory_kib, iterations, parallelism, version
			FROM password_credentials WHERE identity_id = ?`, string(identity.ID)).Scan(
			&hash, &salt, &memory, &iterations, &parallelism, &version,
		); err != nil {
			return err
		}
		if err := reader.QueryRowContext(ctx, `
			SELECT role FROM platform_role_assignments WHERE identity_id = ?`,
			string(identity.ID)).Scan(&role); err != nil {
			return err
		}
		if err := reader.QueryRowContext(ctx, `
			SELECT consumed_by_identity_id FROM bootstrap_capabilities WHERE id = ?`,
			string(capability.ID)).Scan(&consumedBy); err != nil {
			return err
		}
		return reader.QueryRowContext(ctx, `
			SELECT source_address FROM audit_events
			WHERE action = 'bootstrap.administrator_created'`).Scan(&sourceAddress)
	}); err != nil {
		t.Fatalf("read bootstrapped records: %v", err)
	}
	if memory != bootstrapArgonMemory || iterations != bootstrapArgonTime || parallelism != bootstrapArgonThreads || version != argon2.Version {
		t.Fatalf("stored Argon2id parameters = m=%d t=%d p=%d v=%d", memory, iterations, parallelism, version)
	}
	wantHash := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(hash)))
	if !bytes.Equal(hash, wantHash) {
		t.Fatal("stored credential does not verify with its explicit Argon2id parameters")
	}
	if role != string(PlatformAdministrator) || consumedBy != string(identity.ID) || sourceAddress != "2001:db8::1" {
		t.Fatalf("role=%q consumedBy=%q source=%q", role, consumedBy, sourceAddress)
	}
	status, err := repository.AdministratorBootstrapStatus(ctx)
	if err != nil {
		t.Fatalf("AdministratorBootstrapStatus: %v", err)
	}
	if status.Required || status.CapabilityActive || status.ExpiresAt != nil {
		t.Fatalf("bootstrap remained enabled: %#v", status)
	}
	if _, err := repository.BootstrapAdministrator(ctx, BootstrapAdministratorParams{
		Token: capability.Token, Email: "other@example.com", DisplayName: "Other Admin",
		Password: password, Locale: LocaleEnglish, SourceAddress: "192.0.2.1",
	}); !errors.Is(err, ErrBootstrapDisabled) {
		t.Fatalf("reused capability error = %v, want ErrBootstrapDisabled", err)
	}
	if _, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{}); !errors.Is(err, ErrBootstrapDisabled) {
		t.Fatalf("new capability after bootstrap error = %v, want ErrBootstrapDisabled", err)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestInvalidBootstrapAttemptsArePersistentlyRateLimitedBeforeHashing(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixedNow }
	if _, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{}); err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	var derivations atomic.Int64
	repository.derivePassword = func(_, _ []byte, _, _ uint32, _ uint8, length uint32) []byte {
		derivations.Add(1)
		return make([]byte, length)
	}

	params := validBootstrapParams("sfb_invalid", "192.0.2.15")
	for attempt := 1; attempt <= bootstrapSourceLimit; attempt++ {
		if _, err := repository.BootstrapAdministrator(ctx, params); !errors.Is(err, ErrBootstrapDenied) {
			t.Fatalf("attempt %d error = %v, want ErrBootstrapDenied", attempt, err)
		}
	}
	if derivations.Load() != 0 {
		t.Fatalf("invalid attempts invoked password derivation %d times", derivations.Load())
	}

	restarted, err := NewRepository(state)
	if err != nil {
		t.Fatalf("NewRepository after simulated restart: %v", err)
	}
	restarted.now = func() time.Time { return fixedNow }
	restarted.derivePassword = repository.derivePassword
	_, err = restarted.BootstrapAdministrator(ctx, params)
	var rateLimit *BootstrapRateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.RetryAfter != bootstrapSourceBlock {
		t.Fatalf("persisted rate-limit error = %#v, want %s", err, bootstrapSourceBlock)
	}
	if derivations.Load() != 0 {
		t.Fatalf("rate-limited attempt invoked password derivation %d times", derivations.Load())
	}
}

func TestBootstrapGlobalPressureLimitUsesDistinctSources(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return fixedNow }
	if _, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{}); err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	for attempt := 1; attempt <= bootstrapGlobalLimit; attempt++ {
		params := validBootstrapParams("wrong", fmt.Sprintf("198.51.100.%d", attempt))
		if _, err := repository.BootstrapAdministrator(ctx, params); !errors.Is(err, ErrBootstrapDenied) {
			t.Fatalf("global attempt %d error = %v, want ErrBootstrapDenied", attempt, err)
		}
	}
	_, err := repository.BootstrapAdministrator(ctx, validBootstrapParams("wrong", "203.0.113.1"))
	if !errors.Is(err, ErrBootstrapRateLimited) {
		t.Fatalf("global pressure error = %v, want ErrBootstrapRateLimited", err)
	}
}

func TestExpiredBootstrapCapabilityIsInvalidatedBeforeHashing(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	current := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	capability, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{TTL: time.Minute})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	current = current.Add(2 * time.Minute)
	var derivations atomic.Int64
	repository.derivePassword = func(_, _ []byte, _, _ uint32, _ uint8, length uint32) []byte {
		derivations.Add(1)
		return make([]byte, length)
	}
	_, err = repository.BootstrapAdministrator(ctx, validBootstrapParams(capability.Token, "192.0.2.44"))
	if !errors.Is(err, ErrBootstrapDenied) || derivations.Load() != 0 {
		t.Fatalf("expired capability error=%v derivations=%d", err, derivations.Load())
	}
	var reason string
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT invalidation_reason FROM bootstrap_capabilities WHERE id = ?`,
			string(capability.ID)).Scan(&reason)
	}); err != nil {
		t.Fatalf("read invalidation: %v", err)
	}
	if reason != "expired" {
		t.Fatalf("invalidation reason = %q, want expired", reason)
	}
}

func TestBootstrapConstraintFailureRollsBackWithoutConsumingCapability(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	if _, err := repository.CreateIdentity(ctx, CreateIdentityParams{
		Email: "admin@example.com", DisplayName: "Existing Identity", Locale: LocaleEnglish,
	}); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	capability, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	repository.derivePassword = func(password, salt []byte, _, _ uint32, _ uint8, length uint32) []byte {
		digest := sha256.Sum256(append(append([]byte(nil), password...), salt...))
		return append([]byte(nil), digest[:length]...)
	}

	_, err = repository.BootstrapAdministrator(ctx, validBootstrapParams(capability.Token, "192.0.2.10"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting bootstrap error = %v, want ErrConflict", err)
	}
	var activeCapabilities, administrators int
	if err := state.Read(ctx, func(reader store.Reader) error {
		if err := reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM bootstrap_capabilities
			WHERE consumed_at IS NULL AND invalidated_at IS NULL`).Scan(&activeCapabilities); err != nil {
			return err
		}
		return reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM platform_role_assignments WHERE role = 'platform_admin'`).Scan(&administrators)
	}); err != nil {
		t.Fatalf("read rollback state: %v", err)
	}
	if activeCapabilities != 1 || administrators != 0 {
		t.Fatalf("active capabilities=%d administrators=%d", activeCapabilities, administrators)
	}

	params := validBootstrapParams(capability.Token, "192.0.2.10")
	params.Email = "first-admin@example.com"
	identity, err := repository.BootstrapAdministrator(ctx, params)
	if err != nil {
		t.Fatalf("retry bootstrap after conflict: %v", err)
	}
	if identity.NormalizedEmail != "first-admin@example.com" {
		t.Fatalf("unexpected administrator: %#v", identity)
	}
}

func TestConcurrentBootstrapCreatesExactlyOneAdministrator(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	capability, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	repository.derivePassword = func(password, salt []byte, _, _ uint32, _ uint8, length uint32) []byte {
		digest := sha256.Sum256(append(append([]byte(nil), password...), salt...))
		return append([]byte(nil), digest[:length]...)
	}

	const workers = 8
	var successes atomic.Int64
	var unexpectedMu sync.Mutex
	var unexpected []error
	start := make(chan struct{})
	var group sync.WaitGroup
	for worker := 1; worker <= workers; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			<-start
			params := validBootstrapParams(capability.Token, fmt.Sprintf("203.0.113.%d", worker))
			_, err := repository.BootstrapAdministrator(ctx, params)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrBootstrapDisabled), errors.Is(err, ErrBootstrapDenied), errors.Is(err, ErrConflict):
			default:
				unexpectedMu.Lock()
				unexpected = append(unexpected, err)
				unexpectedMu.Unlock()
			}
		}(worker)
	}
	close(start)
	group.Wait()
	if successes.Load() != 1 || len(unexpected) != 0 {
		t.Fatalf("successes=%d unexpected=%v", successes.Load(), unexpected)
	}
	var administrators int
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM platform_role_assignments WHERE role = 'platform_admin'`).Scan(&administrators)
	}); err != nil {
		t.Fatalf("count administrators: %v", err)
	}
	if administrators != 1 {
		t.Fatalf("administrator count = %d, want 1", administrators)
	}
}

func TestBootstrapBoundsConcurrentPasswordDerivation(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	ctx := context.Background()
	capability, err := repository.CreateBootstrapCapability(ctx, CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	entered := make(chan struct{}, 3)
	release := make(chan struct{}, 2)
	var active, maximum, derivations atomic.Int64
	repository.derivePassword = func(password, salt []byte, _, _ uint32, _ uint8, length uint32) []byte {
		derivations.Add(1)
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		digest := sha256.Sum256(append(append([]byte(nil), password...), salt...))
		return append([]byte(nil), digest[:length]...)
	}

	results := make(chan error, 3)
	for worker := 1; worker <= 3; worker++ {
		go func(worker int) {
			_, err := repository.BootstrapAdministrator(ctx, validBootstrapParams(
				capability.Token, fmt.Sprintf("198.51.100.%d", worker),
			))
			results <- err
		}(worker)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("two derivations did not enter")
		}
	}
	select {
	case <-entered:
		t.Fatal("a third derivation bypassed the concurrency bound")
	case <-time.After(100 * time.Millisecond):
	}
	release <- struct{}{}
	release <- struct{}{}

	successes := 0
	for range 3 {
		select {
		case err := <-results:
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrBootstrapDisabled) && !errors.Is(err, ErrBootstrapDenied) {
				t.Fatalf("unexpected bootstrap result: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("bootstrap worker did not complete")
		}
	}
	if successes != 1 || maximum.Load() != 2 || derivations.Load() != 2 {
		t.Fatalf("successes=%d maximum=%d derivations=%d", successes, maximum.Load(), derivations.Load())
	}
}

func TestBootstrapPasswordPolicyDoesNotNormalizeOrRequireComposition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "minimum whitespace", password: "               "},
		{name: "unicode", password: "pässwort mit 雪 und raum"},
		{name: "too short", password: "fourteen-chars", wantErr: true},
		{name: "too many runes", password: string(bytes.Repeat([]byte{'a'}, bootstrapMaximumRunes+1)), wantErr: true},
		{name: "invalid UTF-8", password: string([]byte{0xff, 0xfe}), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateBootstrapPassword(test.password)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateBootstrapPassword(%q) error = %v, wantErr=%v (runes=%d)", test.password, err, test.wantErr, utf8.RuneCountInString(test.password))
			}
		})
	}
}

func validBootstrapParams(token, sourceAddress string) BootstrapAdministratorParams {
	return BootstrapAdministratorParams{
		Token:         token,
		Email:         "admin@example.com",
		DisplayName:   "Platform Administrator",
		Password:      "correct horse battery staple",
		Locale:        LocaleEnglish,
		SourceAddress: sourceAddress,
	}
}
