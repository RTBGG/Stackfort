// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestTLSCertificateLifecycleRetainsValidPredecessorOnRenewalFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x51}, 32))
	if err != nil {
		t.Fatal(err)
	}
	actor := createTestIdentity(t, repository, "tls-owner@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "TLS package", Slug: "tls-package", Limits: testLimits(5), ActorID: &actor.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createTestAccount(t, repository, actor.ID, packageRecord.ID, "TLS", "tls")
	domain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID, Name: "example.test", CanonicalMode: CanonicalPreferApex,
		Target:    DomainTargetSpec{Type: DomainTargetStatic, RootMode: DocumentRootDefault},
		RequestID: "tls-domain-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, updateErr := executor.ExecContext(ctx, `UPDATE domains SET status = 'active' WHERE id = ?`, string(domain.ID))
		return updateErr
	}); err != nil {
		t.Fatal(err)
	}
	registrationOperation, err := repository.CreateOperation(ctx, CreateOperationParams{
		ActorID: &actor.ID, Kind: "acme.account.register", RetryClass: RetrySafe,
		RequestID: "tls-acme-register", IdempotencyKey: "tls-acme-register", Payload: map[string]any{}, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	acmeAccount, err := repository.EnsureACMEAccount(ctx, EnsureACMEAccountParams{
		Environment: ACMELetsEncryptProduction, ContactEmail: "tls-owner@example.test", TermsAccepted: true,
		ActorID: &actor.ID, OperationID: &registrationOperation.ID, RequestID: "tls-acme-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CompleteACMERegistration(ctx, CompleteACMERegistrationParams{
		AccountID: acmeAccount.ID, AccountURI: "https://acme.example/acct/1",
		OrdersURL: "https://acme.example/acct/1/orders", TermsURL: "https://acme.example/terms",
		Status: ACMEAccountValid, ActorID: &actor.ID, OperationID: &registrationOperation.ID,
		RequestID: "tls-acme-complete",
	})
	if err != nil {
		t.Fatal(err)
	}
	issueOperation := createTLSCoreTestOperation(t, repository, account.ID, actor.ID, "tls-issue")
	order, err := repository.PrepareTLSCertificateOrder(ctx, PrepareTLSCertificateOrderParams{
		AccountID: account.ID, DomainID: domain.ID, OperationID: issueOperation.ID,
		Environment: ACMELetsEncryptProduction, ActorID: &actor.ID, RequestID: "tls-prepare",
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.Purpose != TLSCertificateIssue || order.Certificate.Status != TLSCertificateOrdering ||
		!slices.Equal(order.Certificate.Names, []string{"example.test", "www.example.test"}) {
		t.Fatalf("prepared order = %#v", order)
	}
	certificateSigner, err := repository.LoadTLSCertificateSigner(ctx, order.CertificateID)
	if err != nil {
		t.Fatalf("load encrypted certificate signer: %v", err)
	}
	encodedCertificateKey, err := x509.MarshalPKCS8PrivateKey(certificateSigner)
	if err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `SELECT key_ciphertext FROM tls_certificates WHERE id = ?`,
			string(order.CertificateID)).Scan(&ciphertext)
	}); err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, encodedCertificateKey) || bytes.Equal(ciphertext, encodedCertificateKey) {
		t.Fatal("certificate private key was not stored as an opaque envelope ciphertext")
	}
	if err := repository.RecordTLSCertificateOrderURL(ctx, issueOperation.ID, "https://acme.example/order/1"); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordTLSCertificateOrderURL(ctx, issueOperation.ID, "https://acme.example/order/different"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting order URL error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	certificate, err := repository.StageTLSCertificate(ctx, StageTLSCertificateParams{
		OperationID: issueOperation.ID, CertificateID: order.CertificateID,
		FullChainPEM: "public-chain", CertificateURL: "https://acme.example/cert/1",
		FingerprintSHA256: strings.Repeat("a", 64), Issuer: "Test CA", SerialHex: "01",
		NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(90 * 24 * time.Hour),
		NextRenewalAt: now.Add(55 * 24 * time.Hour), ActorID: &actor.ID, RequestID: "tls-stage",
	})
	if err != nil || certificate.Status != TLSCertificateStaged {
		t.Fatalf("staged certificate = %#v, %v", certificate, err)
	}
	revision, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: account.ID, Document: map[string]any{"tls": "fixture"}, Reason: "tls.test",
		OperationID: &issueOperation.ID, ActorID: &actor.ID, RequestID: "tls-revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.RecordAppliedStateRevision(ctx, RecordAppliedStateRevisionParams{
		AccountID: account.ID, DesiredStateRevisionID: revision.ID, OperationID: &issueOperation.ID,
		ConfigDigest: bytes.Repeat([]byte{0x31}, 32), ActorID: &actor.ID, RequestID: "tls-applied",
	})
	if err != nil {
		t.Fatal(err)
	}
	certificate, err = repository.ActivateTLSCertificate(ctx, ActivateTLSCertificateParams{
		AccountID: account.ID, DomainID: domain.ID, CertificateID: certificate.ID,
		DesiredStateRevisionID: revision.ID, OperationID: issueOperation.ID,
		ActorID: &actor.ID, RequestID: "tls-activate",
	})
	if err != nil || certificate.Status != TLSCertificateActive {
		t.Fatalf("active certificate = %#v, %v", certificate, err)
	}

	renewOperation := createTLSCoreTestOperation(t, repository, account.ID, actor.ID, "tls-renew")
	renewOrder, err := repository.PrepareTLSCertificateOrder(ctx, PrepareTLSCertificateOrderParams{
		AccountID: account.ID, DomainID: domain.ID, OperationID: renewOperation.ID,
		Environment: ACMELetsEncryptProduction, ReplacesCertificateID: &certificate.ID,
		ActorID: &actor.ID, RequestID: "tls-renew-prepare",
	})
	if err != nil || renewOrder.Purpose != TLSCertificateRenew {
		t.Fatalf("renewal order = %#v, %v", renewOrder, err)
	}
	retryAt := now.Add(6 * time.Hour)
	if err := repository.FailTLSCertificateOrder(ctx, FailTLSCertificateOrderParams{
		OperationID: renewOperation.ID, ErrorCode: "tls.acme_authority_unavailable",
		Final: true, RetryAt: &retryAt, ActorID: &actor.ID, RequestID: "tls-renew-failed",
	}); err != nil {
		t.Fatal(err)
	}
	current, err := repository.GetDomain(ctx, account.ID, domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.TLS.ActiveCertificateRef != string(certificate.ID) || current.TLS.IssuanceStatus != TLSFailed ||
		current.TLS.ExpiresAt == nil || !current.TLS.ExpiresAt.Equal(*certificate.ExpiresAt) ||
		current.TLS.NextRenewalAt == nil || !current.TLS.NextRenewalAt.Equal(retryAt) {
		t.Fatalf("TLS state after failed renewal = %#v", current.TLS)
	}
	if err := repository.RemoveDomain(ctx, RemoveDomainParams{
		AccountID: account.ID, DomainID: domain.ID, ActorID: &actor.ID, RequestID: "tls-domain-remove",
	}); err != nil {
		t.Fatal(err)
	}
	retired, err := repository.GetTLSCertificate(ctx, certificate.ID)
	if err != nil || retired.Status != TLSCertificateRetired || retired.RetiredAt == nil {
		t.Fatalf("retired certificate = %#v, %v", retired, err)
	}
	removed, err := repository.GetDomain(ctx, account.ID, domain.ID)
	if err != nil || removed.TLS.Enabled || removed.TLS.IssuanceStatus != TLSDisabled ||
		removed.TLS.ActiveCertificateRef != "" || removed.TLS.NextRenewalAt != nil {
		t.Fatalf("removed domain TLS state = %#v, %v", removed.TLS, err)
	}
}

func createTLSCoreTestOperation(
	t *testing.T,
	repository *Repository,
	accountID ID,
	actorID ID,
	key string,
) Operation {
	t.Helper()
	operation, err := repository.CreateOperation(context.Background(), CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: "tls.certificate.lifecycle",
		RetryClass: RetrySafe, RequestID: key, IdempotencyKey: key,
		Payload: map[string]any{"domainId": string(accountID)}, MaxAttempts: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}
