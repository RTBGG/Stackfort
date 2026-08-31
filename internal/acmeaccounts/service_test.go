// SPDX-License-Identifier: AGPL-3.0-or-later

package acmeaccounts

import (
	"context"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
)

func TestServiceRequiresPlatformPolicyAndQueuesOnlyTypedRegistration(t *testing.T) {
	t.Parallel()
	actorID := core.ID("019c1234-5678-7abc-8def-0123456789ab")
	sessionID := core.ID("019c1234-5678-7abc-8def-0123456789ac")
	subject := (core.AuthenticatedSession{
		Identity: core.Identity{ID: actorID}, Session: core.Session{ID: sessionID},
	}).AuthorizationSubject()
	repository := &fakeRepository{operation: core.Operation{
		ID: "019c1234-5678-7abc-8def-0123456789ad", Status: core.OperationPending,
	}}
	service, _ := New(repository)
	operation, err := service.QueueRegistration(context.Background(), RegisterCommand{
		Subject: subject, Environment: core.ACMELetsEncryptStaging,
		ContactEmail: "admin@example.test", TermsAccepted: true,
		RequestID: "acme-service-request", IdempotencyKey: "acme-service-key",
	})
	if err != nil || operation.ID != repository.operation.ID {
		t.Fatalf("queued operation = %#v, %v", operation, err)
	}
	if repository.authorize.Action != core.AuthorizationPlatformManage || repository.create == nil ||
		repository.create.AccountID != nil || repository.create.ActorID == nil ||
		*repository.create.ActorID != actorID || repository.create.Kind != operations.ACMEAccountRegistrationKind ||
		repository.create.RetryClass != core.RetrySafe || repository.create.MaxAttempts != 3 {
		t.Fatalf("authorization/create = %#v / %#v", repository.authorize, repository.create)
	}
	if repository.create.Payload["environment"] != string(core.ACMELetsEncryptStaging) ||
		repository.create.Payload["termsAccepted"] != true {
		t.Fatalf("typed operation payload = %#v", repository.create.Payload)
	}

	repository.authorizeErr = core.ErrAuthorizationDenied
	repository.create = nil
	if _, err := service.QueueRegistration(context.Background(), RegisterCommand{
		Subject: subject, Environment: core.ACMELetsEncryptStaging,
		ContactEmail: "admin@example.test", TermsAccepted: true,
		RequestID: "denied", IdempotencyKey: "denied",
	}); !errors.Is(err, core.ErrAuthorizationDenied) || repository.create != nil {
		t.Fatalf("denied queue = %#v / %v", repository.create, err)
	}
}

type fakeRepository struct {
	authorize    core.AuthorizeParams
	authorizeErr error
	create       *core.CreateOperationParams
	operation    core.Operation
	accounts     []core.ACMEAccount
}

func (repository *fakeRepository) Authorize(
	_ context.Context,
	params core.AuthorizeParams,
) (core.AuthorizationDecision, error) {
	repository.authorize = params
	return core.AuthorizationDecision{}, repository.authorizeErr
}

func (repository *fakeRepository) CreateOperation(
	_ context.Context,
	params core.CreateOperationParams,
) (core.Operation, error) {
	repository.create = &params
	return repository.operation, nil
}

func (repository *fakeRepository) ListACMEAccounts(context.Context) ([]core.ACMEAccount, error) {
	return repository.accounts, nil
}
