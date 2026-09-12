// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func bootstrapDigestFixture(seed byte) (string, BootstrapCapabilityDigest) {
	token := bootstrapTokenPrefix + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{seed}, bootstrapTokenBytes))
	return token, BootstrapCapabilityDigest(sha256.Sum256([]byte(token)))
}

func TestBootstrapDigestCanonicalParserAndInputBounds(t *testing.T) {
	t.Parallel()
	_, digest := bootstrapDigestFixture(1)
	encoded := hex.EncodeToString(digest[:])
	parsed, err := ParseBootstrapCapabilityDigest(encoded)
	if err != nil || parsed != digest {
		t.Fatalf("parse canonical digest: %v", err)
	}
	for _, value := range []string{"", encoded[:63], encoded + "0", strings.ToUpper(encoded), " " + encoded[1:], encoded[:63] + "g", strings.Repeat("0", 64), encoded + "\n", strings.Repeat("é", 32)} {
		if result, err := ParseBootstrapCapabilityDigest(value); !errors.Is(err, ErrInvalidInput) || result != (BootstrapCapabilityDigest{}) {
			t.Fatalf("noncanonical input accepted: length=%d error=%v", len(value), err)
		}
	}
	repository, state := newTestRepository(t)
	for _, params := range []RegisterBootstrapCapabilityDigestParams{
		{}, {Digest: digest, TTL: -time.Minute}, {Digest: digest, TTL: time.Minute - 1},
		{Digest: digest, TTL: time.Hour + 1}, {Digest: digest, RequestID: strings.Repeat("x", 129)},
	} {
		if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), params); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid import error=%v", err)
		}
	}
	if _, err := repository.RegisterBootstrapCapabilityDigest(nil, RegisterBootstrapCapabilityDigestParams{Digest: digest}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil context: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.RegisterBootstrapCapabilityDigest(ctx, RegisterBootstrapCapabilityDigestParams{Digest: digest}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
	if countAuditEvents(t, state) != 0 {
		t.Fatal("invalid imports changed state")
	}
}

func TestBootstrapDigestImportUsesExistingRedemptionAndNoSecretsInMetadata(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	token, digest := bootstrapDigestFixture(2)
	registered, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest})
	if err != nil || registered.AlreadyRegistered || registered.ExpiresAt.Sub(registered.CreatedAt) != 15*time.Minute {
		t.Fatalf("register: %+v, %v", registered, err)
	}
	var stored []byte
	var audit string
	if err := state.Read(t.Context(), func(reader store.Reader) error {
		if err := reader.QueryRowContext(t.Context(), `SELECT token_hash FROM bootstrap_capabilities WHERE id = ?`, string(registered.ID)).Scan(&stored); err != nil {
			return err
		}
		return reader.QueryRowContext(t.Context(), `SELECT details_json FROM audit_events WHERE target_id = ?`, string(registered.ID)).Scan(&audit)
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, digest[:]) {
		t.Fatal("stored value is not SHA-256 of the full displayed token")
	}
	metadata, err := json.Marshal(registered)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{audit, string(metadata)} {
		if strings.Contains(value, token) || strings.Contains(value, hex.EncodeToString(digest[:])) {
			t.Fatal("capability material leaked into metadata/audit")
		}
	}
	status, err := repository.AdministratorBootstrapStatus(t.Context())
	if err != nil || !status.Required || !status.CapabilityActive || status.ExpiresAt == nil || !status.ExpiresAt.Equal(registered.ExpiresAt) {
		t.Fatalf("imported status: %+v %v", status, err)
	}
	if _, err := repository.BootstrapAdministrator(t.Context(), validBootstrapParams(token, "192.0.2.31")); err != nil {
		t.Fatalf("redeem imported token: %v", err)
	}
	_, other := bootstrapDigestFixture(3)
	for _, candidate := range []BootstrapCapabilityDigest{digest, other} {
		if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: candidate}); !errors.Is(err, ErrBootstrapDisabled) {
			t.Fatalf("post-admin import allowed: %v", err)
		}
	}
	if err := repository.VerifyAuditChain(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapDigestReplayNeverExtendsOrReplaces(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }
	_, digest := bootstrapDigestFixture(4)
	first, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	retry, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest, TTL: time.Hour})
	if err != nil || !retry.AlreadyRegistered || retry.ID != first.ID || !retry.CreatedAt.Equal(first.CreatedAt) || !retry.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("retry changed capability: %+v %v", retry, err)
	}
	_, other := bootstrapDigestFixture(5)
	if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: other}); !errors.Is(err, ErrConflict) {
		t.Fatalf("different active import: %v", err)
	}
	if countAuditEvents(t, state) != 1 {
		t.Fatal("retry or conflict created an audit event")
	}
	now = first.ExpiresAt // Equality is already expired, never a renewal window.
	if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired same-active import: %v", err)
	}
	second, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: other, TTL: time.Minute})
	if err != nil {
		t.Fatalf("fresh digest after expiry: %v", err)
	}
	now = second.ExpiresAt
	// Replaying terminal history first encounters a different expired active
	// row. Its invalidation must roll back when global digest uniqueness fails.
	if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest}); !errors.Is(err, ErrConflict) {
		t.Fatalf("historical import: %v", err)
	}
	var activeID string
	var count int
	if err := state.Read(t.Context(), func(reader store.Reader) error {
		if err := reader.QueryRowContext(t.Context(), `SELECT id FROM bootstrap_capabilities WHERE consumed_at IS NULL AND invalidated_at IS NULL`).Scan(&activeID); err != nil {
			return err
		}
		return reader.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM bootstrap_capabilities`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if activeID != string(second.ID) || count != 2 || countAuditEvents(t, state) != 2 {
		t.Fatal("historical replay mutated current capability or terminal history")
	}
}

func TestBootstrapDigestImportCannotReactivateExplicitlyReplacedCapability(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	_, digest := bootstrapDigestFixture(6)
	if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateBootstrapCapability(t.Context(), CreateBootstrapCapabilityParams{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("normal create replaced import without permission: %v", err)
	}
	capability, err := repository.CreateBootstrapCapability(t.Context(), CreateBootstrapCapabilityParams{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	createdDigest := BootstrapCapabilityDigest(sha256.Sum256([]byte(capability.Token)))
	retry, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: createdDigest})
	if err != nil || !retry.AlreadyRegistered || retry.ID != capability.ID {
		t.Fatalf("same-active legacy capability: %+v %v", retry, err)
	}
	repository.now = func() time.Time { return capability.ExpiresAt }
	if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest}); !errors.Is(err, ErrConflict) {
		t.Fatalf("replaced capability reactivated: %v", err)
	}
}

func TestConcurrentBootstrapDigestImportsHaveOneIdentityAndLifetime(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	_, digest := bootstrapDigestFixture(7)
	type outcome struct {
		result RegisteredBootstrapCapability
		err    error
	}
	const workers = 8
	results := make(chan outcome, workers)
	for index := 0; index < workers; index++ {
		go func(index int) {
			result, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest, TTL: time.Duration(index+1) * time.Minute})
			results <- outcome{result, err}
		}(index)
	}
	var first RegisteredBootstrapCapability
	created := 0
	for index := 0; index < workers; index++ {
		item := <-results
		if item.err != nil {
			t.Fatalf("concurrent import: %v", item.err)
		}
		if index == 0 {
			first = item.result
		}
		if item.result.ID != first.ID || !item.result.CreatedAt.Equal(first.CreatedAt) || !item.result.ExpiresAt.Equal(first.ExpiresAt) {
			t.Fatal("concurrent import changed identity/lifetime")
		}
		if !item.result.AlreadyRegistered {
			created++
		}
	}
	if created != 1 || countAuditEvents(t, state) != 1 {
		t.Fatal("concurrent import registered more than once")
	}
}

func TestBootstrapDigestLifetimeStartsInsideRegistrationTransaction(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	initial := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(initial.UnixNano())
	repository.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	locked, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		holderDone <- state.Write(t.Context(), func(store.Executor) error { close(locked); <-release; return nil })
	}()
	<-locked
	_, digest := bootstrapDigestFixture(8)
	done := make(chan RegisteredBootstrapCapability, 1)
	failed := make(chan error, 1)
	go func() {
		result, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest, TTL: time.Hour})
		if err != nil {
			failed <- err
			return
		}
		done <- result
	}()
	// The transaction stays occupied while the importer has time to start.
	time.AfterFunc(50*time.Millisecond, func() { clock.Store(initial.Add(2 * time.Hour).UnixNano()); close(release) })
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failed:
		t.Fatal(err)
	case result := <-done:
		if !result.CreatedAt.Equal(initial.Add(2*time.Hour)) || result.ExpiresAt.Sub(result.CreatedAt) != time.Hour {
			t.Fatalf("lifetime sampled before registration: %+v", result)
		}
	}
}

func TestImportedBootstrapCapabilityPreservesPersistentRateLimits(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	token, digest := bootstrapDigestFixture(9)
	if _, err := repository.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < bootstrapSourceLimit; attempt++ {
		if _, err := repository.BootstrapAdministrator(t.Context(), validBootstrapParams("sfb_wrong", "192.0.2.39")); !errors.Is(err, ErrBootstrapDenied) {
			t.Fatalf("invalid attempt: %v", err)
		}
	}
	restarted, err := NewRepository(state)
	if err != nil {
		t.Fatal(err)
	}
	var derivations atomic.Int64
	restarted.derivePassword = func(_, _ []byte, _, _ uint32, _ uint8, length uint32) []byte {
		derivations.Add(1)
		return make([]byte, length)
	}
	if _, err := restarted.RegisterBootstrapCapabilityDigest(t.Context(), RegisterBootstrapCapabilityDigestParams{Digest: digest}); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.BootstrapAdministrator(t.Context(), validBootstrapParams(token, "192.0.2.39")); !errors.Is(err, ErrBootstrapRateLimited) || derivations.Load() != 0 {
		t.Fatalf("import retry reset persisted rate limit: %v, derivations=%d", err, derivations.Load())
	}
}
