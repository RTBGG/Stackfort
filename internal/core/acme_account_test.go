// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/store"
	"golang.org/x/crypto/acme"
)

func TestACMEAccountCredentialIsEncryptedReplaySafeAndRetained(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	masterKey := bytes.Repeat([]byte{0x42}, 32)
	repository, err := NewRepositoryWithMasterKey(state, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	actor := createTestIdentity(t, repository, "acme-admin@example.test")
	operation, err := repository.CreateOperation(ctx, CreateOperationParams{
		ActorID: &actor.ID, Kind: "acme.account.register", RetryClass: RetrySafe,
		RequestID: "acme-operation", IdempotencyKey: "acme-operation", Payload: map[string]any{}, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	operationID := operation.ID
	created, err := repository.EnsureACMEAccount(ctx, EnsureACMEAccountParams{
		Environment: ACMELetsEncryptStaging, ContactEmail: "ACME-Admin@Example.Test",
		TermsAccepted: true, ActorID: &actor.ID, OperationID: &operationID, RequestID: "acme-create-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != ACMEAccountPending || created.ContactEmail != "acme-admin@example.test" ||
		created.DirectoryURL != letsEncryptStagingDirectory || len(created.PublicKeyThumbprint) != 43 {
		t.Fatalf("created ACME account = %#v", created)
	}
	replayed, err := repository.EnsureACMEAccount(ctx, EnsureACMEAccountParams{
		Environment: ACMELetsEncryptStaging, ContactEmail: "acme-admin@example.test",
		TermsAccepted: true, ActorID: &actor.ID, OperationID: &operationID, RequestID: "acme-create-1",
	})
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("replayed ACME account = %#v, %v", replayed, err)
	}
	if _, err := repository.EnsureACMEAccount(ctx, EnsureACMEAccountParams{
		Environment: ACMELetsEncryptStaging, ContactEmail: "different@example.test",
		TermsAccepted: true, ActorID: &actor.ID, OperationID: &operationID, RequestID: "acme-create-2",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting contact error = %v", err)
	}

	var ciphertext, nonce, wrappedKey, wrapNonce []byte
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT key_ciphertext, key_nonce, key_wrapped_key, key_wrap_nonce
			FROM acme_accounts WHERE id = ?`, string(created.ID)).Scan(
			&ciphertext, &nonce, &wrappedKey, &wrapNonce,
		)
	}); err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) == 0 || len(nonce) != 12 || len(wrappedKey) != 48 || len(wrapNonce) != 12 {
		t.Fatalf("encrypted ACME key envelope lengths = %d/%d/%d/%d",
			len(ciphertext), len(nonce), len(wrappedKey), len(wrapNonce))
	}
	signer, err := repository.LoadACMEAccountSigner(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	thumbprint, err := acme.JWKThumbprint(signer.Public())
	if err != nil || thumbprint != created.PublicKeyThumbprint {
		t.Fatalf("loaded signer thumbprint = %q, %v", thumbprint, err)
	}

	registered, err := repository.CompleteACMERegistration(ctx, CompleteACMERegistrationParams{
		AccountID: created.ID, AccountURI: "https://acme-staging.example/acct/1",
		OrdersURL: "https://acme-staging.example/acct/1/orders",
		TermsURL:  "https://acme-staging.example/terms", Status: ACMEAccountValid,
		ActorID: &actor.ID, OperationID: &operationID, RequestID: "acme-complete-1",
	})
	if err != nil || registered.Status != ACMEAccountValid || registered.RegisteredAt == nil {
		t.Fatalf("registered ACME account = %#v, %v", registered, err)
	}
	replayedRegistration, err := repository.CompleteACMERegistration(ctx, CompleteACMERegistrationParams{
		AccountID: created.ID, AccountURI: "https://acme-staging.example/acct/1",
		OrdersURL: "https://acme-staging.example/acct/1/orders",
		TermsURL:  "https://acme-staging.example/terms", Status: ACMEAccountValid,
		ActorID: &actor.ID, OperationID: &operationID, RequestID: "acme-complete-1",
	})
	if err != nil || replayedRegistration.RegisteredAt == nil ||
		!replayedRegistration.RegisteredAt.Equal(*registered.RegisteredAt) {
		t.Fatalf("replayed registration = %#v, %v", replayedRegistration, err)
	}
	accounts, err := repository.ListACMEAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != created.ID {
		t.Fatalf("ACME account list = %#v, %v", accounts, err)
	}

	wrongKeyRepository, err := NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongKeyRepository.LoadACMEAccountSigner(ctx, created.ID); err == nil {
		t.Fatal("ACME credential decrypted with a different host master key")
	}
	err = state.Write(ctx, func(executor store.Executor) error {
		_, deleteErr := executor.ExecContext(ctx, "DELETE FROM acme_accounts WHERE id = ?", string(created.ID))
		return deleteErr
	})
	if err == nil {
		t.Fatal("retained ACME account was deleted")
	}
}

func TestACMEAccountRequiresSecretStorageAndExplicitTerms(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	_, err := repository.EnsureACMEAccount(context.Background(), EnsureACMEAccountParams{
		Environment: ACMELetsEncryptStaging, ContactEmail: "admin@example.test",
		TermsAccepted: true, RequestID: "no-secret-storage",
	})
	if !errors.Is(err, ErrSecretStorageUnavailable) {
		t.Fatalf("unkeyed repository error = %v", err)
	}
	_, err = repository.EnsureACMEAccount(context.Background(), EnsureACMEAccountParams{
		Environment: ACMELetsEncryptStaging, ContactEmail: "admin@example.test",
		TermsAccepted: false, RequestID: "terms-not-accepted",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("terms rejection error = %v", err)
	}
}
