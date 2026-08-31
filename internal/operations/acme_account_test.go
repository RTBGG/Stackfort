// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestACMEAccountRegistrationUsesApprovedStagingAuthorityAndEncryptedRepositorySigner(t *testing.T) {
	t.Parallel()
	accountID := testID(t)
	operationID := testID(t)
	actorID := testID(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeACMEAccountRepository{account: core.ACMEAccount{
		ID: accountID, Environment: core.ACMELetsEncryptStaging,
		DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
		ContactEmail: "admin@example.test", Status: core.ACMEAccountPending,
	}, signer: key}
	registrar := &fakeACMERegistrar{result: ACMERegistrationResult{
		AccountURI: "https://acme-staging.example/acct/1",
		OrdersURL:  "https://acme-staging.example/acct/1/orders",
		TermsURL:   "https://acme-staging.example/terms",
		Status:     core.ACMEAccountValid,
	}}
	handler, err := NewACMEAccountRegistrationHandler(repository, registrar)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := NewACMEAccountRegistrationPayload(ACMEAccountRegistrationPayload{
		Environment: core.ACMELetsEncryptStaging, ContactEmail: "admin@example.test", TermsAccepted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeNGINXReporter{}
	result, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: core.Operation{
		ID: operationID, ActorID: &actorID, Kind: ACMEAccountRegistrationKind,
		RequestID: "acme-request", Payload: payload,
	}}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if repository.ensure == nil || repository.ensure.Environment != core.ACMELetsEncryptStaging ||
		repository.ensure.OperationID == nil || *repository.ensure.OperationID != operationID {
		t.Fatalf("ensure params = %#v", repository.ensure)
	}
	if registrar.calls != 1 || registrar.request.DirectoryURL != repository.account.DirectoryURL ||
		registrar.request.Signer != key || !registrar.request.TermsAccepted {
		t.Fatalf("registrar request = %#v", registrar.request)
	}
	if repository.complete == nil || repository.complete.AccountURI != registrar.result.AccountURI ||
		repository.complete.Status != core.ACMEAccountValid {
		t.Fatalf("complete params = %#v", repository.complete)
	}
	if result["acmeAccountId"] != string(accountID) || result["status"] != core.ACMEAccountValid {
		t.Fatalf("result = %#v", result)
	}
	if len(reporter.stages) != 3 || reporter.stages[0] != "securing-account-key" ||
		reporter.stages[1] != "registering" || reporter.stages[2] != "recording" {
		t.Fatalf("stages = %#v", reporter.stages)
	}
}

func TestACMEAccountRegistrationReplayAndStrictPayload(t *testing.T) {
	t.Parallel()
	accountID, operationID, actorID := testID(t), testID(t), testID(t)
	repository := &fakeACMEAccountRepository{account: core.ACMEAccount{
		ID: accountID, Environment: core.ACMELetsEncryptStaging,
		DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
		ContactEmail: "admin@example.test", Status: core.ACMEAccountValid,
	}}
	registrar := &fakeACMERegistrar{}
	handler, _ := NewACMEAccountRegistrationHandler(repository, registrar)
	payload, _ := NewACMEAccountRegistrationPayload(ACMEAccountRegistrationPayload{
		Environment: core.ACMELetsEncryptStaging, ContactEmail: "admin@example.test", TermsAccepted: true,
	})
	if _, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: core.Operation{
		ID: operationID, ActorID: &actorID, Kind: ACMEAccountRegistrationKind,
		RequestID: "acme-replay", Payload: payload,
	}}, &fakeNGINXReporter{}); err != nil {
		t.Fatal(err)
	}
	if registrar.calls != 0 || repository.complete != nil {
		t.Fatal("completed ACME account replay contacted the authority or rewrote registration")
	}
	payload["directoryUrl"] = "https://attacker.example/directory"
	_, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: core.Operation{
		ID: operationID, ActorID: &actorID, Kind: ACMEAccountRegistrationKind,
		RequestID: "acme-invalid", Payload: payload,
	}}, &fakeNGINXReporter{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "acme.registration_payload_invalid" {
		t.Fatalf("raw directory payload error = %v", err)
	}
}

type fakeACMEAccountRepository struct {
	account  core.ACMEAccount
	signer   crypto.Signer
	ensure   *core.EnsureACMEAccountParams
	complete *core.CompleteACMERegistrationParams
}

func (repository *fakeACMEAccountRepository) EnsureACMEAccount(
	_ context.Context,
	params core.EnsureACMEAccountParams,
) (core.ACMEAccount, error) {
	repository.ensure = &params
	return repository.account, nil
}

func (repository *fakeACMEAccountRepository) LoadACMEAccountSigner(context.Context, core.ID) (crypto.Signer, error) {
	return repository.signer, nil
}

func (repository *fakeACMEAccountRepository) CompleteACMERegistration(
	_ context.Context,
	params core.CompleteACMERegistrationParams,
) (core.ACMEAccount, error) {
	repository.complete = &params
	repository.account.Status = params.Status
	repository.account.AccountURI = params.AccountURI
	repository.account.OrdersURL = params.OrdersURL
	repository.account.TermsURL = params.TermsURL
	return repository.account, nil
}

type fakeACMERegistrar struct {
	calls   int
	request ACMERegistrationRequest
	result  ACMERegistrationResult
	err     error
}

func (registrar *fakeACMERegistrar) Register(
	_ context.Context,
	request ACMERegistrationRequest,
) (ACMERegistrationResult, error) {
	registrar.calls++
	registrar.request = request
	return registrar.result, registrar.err
}
