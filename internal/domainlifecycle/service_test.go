// SPDX-License-Identifier: AGPL-3.0-or-later

package domainlifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
)

func TestAdministratorWAFExceptionUsesPlatformRecentAuthorizationAndClosedPayload(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000301")
	domainID := core.ID("0198b935-b600-7000-8000-000000000302")
	repository := &serviceRepositoryStub{operation: core.Operation{ID: "0198b935-b600-7000-8000-000000000303"}}
	service, _ := New(repository)
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	operation, err := service.QueueWAFException(context.Background(), WAFExceptionCommand{
		AccountID: accountID, DomainID: domainID, RuleID: 941100,
		RequestPath: "/search", Parameter: "q", ExpiresAt: expiresAt,
		RequestID: "waf-exception-request", IdempotencyKey: "waf-exception-create",
	})
	if err != nil || operation.ID != repository.operation.ID {
		t.Fatalf("QueueWAFException = %#v / %v", operation, err)
	}
	if repository.authorization.Action != core.AuthorizationPlatformManage ||
		repository.authorization.AccountID != nil || repository.created.Kind != operations.DomainLifecycleKind {
		t.Fatalf("authorization/operation = %#v / %#v", repository.authorization, repository.created)
	}
	payload := repository.created.Payload
	if payload["action"] != string(operations.DomainLifecycleCreateWAFException) ||
		payload["domainId"] != string(domainID) {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestAccountDomainQueueCannotBypassAdministratorWAFExceptionBoundary(t *testing.T) {
	t.Parallel()
	repository := &serviceRepositoryStub{}
	service, _ := New(repository)
	_, err := service.Queue(context.Background(), Command{
		AccountID: "0198b935-b600-7000-8000-000000000311",
		RequestID: "request", IdempotencyKey: "idempotency",
		Payload: operations.DomainLifecyclePayload{
			Action:       operations.DomainLifecycleCreateWAFException,
			DomainID:     "0198b935-b600-7000-8000-000000000312",
			WAFException: &operations.WAFExceptionIntent{RuleID: 941100, RequestPath: "/"},
		},
	})
	if !errors.Is(err, core.ErrInvalidInput) || repository.authorizeCalls != 0 {
		t.Fatalf("Queue error/authorization calls = %v / %d", err, repository.authorizeCalls)
	}
}

func TestQueueCouplesAuthorizationAndSafeIdempotentOperation(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000201")
	repository := &serviceRepositoryStub{operation: core.Operation{
		ID: "0198b935-b600-7000-8000-000000000202", Status: core.OperationPending,
	}}
	service, _ := New(repository)
	operation, err := service.Queue(context.Background(), Command{
		AccountID: accountID, RequestID: "service-request-1", IdempotencyKey: "service-create-1",
		Payload: operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleCreate, Name: "service.example.test",
			Target: &core.DomainTargetSpec{Type: core.DomainTargetStatic},
		},
	})
	if err != nil || operation.ID != repository.operation.ID {
		t.Fatalf("Queue = %#v / %v", operation, err)
	}
	if repository.authorization.Action != core.AuthorizationAccountResourcesManage ||
		repository.authorization.AccountID == nil || *repository.authorization.AccountID != accountID {
		t.Fatalf("authorization = %#v", repository.authorization)
	}
	if repository.created.Kind != operations.DomainLifecycleKind ||
		repository.created.RetryClass != core.RetrySafe || repository.created.MaxAttempts != 3 ||
		repository.created.IdempotencyKey != "service-create-1" {
		t.Fatalf("created operation = %#v", repository.created)
	}
}

func TestQueueRemovalRequiresDestructiveAuthorization(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000211")
	domainID := "0198b935-b600-7000-8000-000000000212"
	repository := &serviceRepositoryStub{}
	service, _ := New(repository)
	_, err := service.Queue(context.Background(), Command{
		AccountID: accountID, RequestID: "service-request-2", IdempotencyKey: "service-remove-1",
		Payload: operations.DomainLifecyclePayload{Action: operations.DomainLifecycleRemove, DomainID: domainID},
	})
	if err != nil || repository.authorization.Action != core.AuthorizationAccountDestructive {
		t.Fatalf("remove authorization/error = %#v / %v", repository.authorization, err)
	}
}

func TestQueueRejectsMissingIdempotencyBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := &serviceRepositoryStub{}
	service, _ := New(repository)
	_, err := service.Queue(context.Background(), Command{
		AccountID: "0198b935-b600-7000-8000-000000000221", RequestID: "request",
		Payload: operations.DomainLifecyclePayload{Action: operations.DomainLifecycleSuspend,
			DomainID: "0198b935-b600-7000-8000-000000000222"},
	})
	if !errors.Is(err, core.ErrInvalidInput) || repository.authorizeCalls != 0 {
		t.Fatalf("Queue error/authorization calls = %v / %d", err, repository.authorizeCalls)
	}
}

func TestQueueRejectsAccountUntilHostProvisioningCompletes(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000225")
	repository := &serviceRepositoryStub{hostNotReady: true}
	service, _ := New(repository)
	_, err := service.Queue(context.Background(), Command{
		AccountID: accountID, RequestID: "service-request-host-pending", IdempotencyKey: "service-host-pending",
		Payload: operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleCreate, Name: "pending.example.test",
			Target: &core.DomainTargetSpec{Type: core.DomainTargetStatic},
		},
	})
	if !errors.Is(err, core.ErrConflict) || repository.created.Kind != "" {
		t.Fatalf("Queue error/operation = %v / %#v", err, repository.created)
	}
}

func TestPreviewAuthorizesManageAndReturnsNormalizedRoutingExamples(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000231")
	repository := &serviceRepositoryStub{}
	service, _ := New(repository)
	preview, err := service.Preview(context.Background(), PreviewParams{
		AccountID: accountID, Name: "WWW.Redirect.Example",
		Target: core.DomainTargetSpec{Type: core.DomainTargetRedirect, Redirect: &core.RedirectSpec{
			StatusCode: core.RedirectPermanent, TargetURL: "https://destination.example/base",
			HostMode: core.RedirectHostWWWOnly, PreservePath: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.authorization.Action != core.AuthorizationAccountResourcesManage ||
		repository.authorization.AccountID == nil || *repository.authorization.AccountID != accountID ||
		preview.Name.ASCII != "redirect.example" || len(preview.Routes) != 2 ||
		preview.Routes[0].Action != core.DomainRouteInactive ||
		preview.Routes[1].DestinationURL != "https://destination.example/base/example/path" {
		t.Fatalf("authorization/preview = %#v / %#v", repository.authorization, preview)
	}
}

func TestOperationStatusAuthorizesViewAndUsesAccountScope(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000241")
	operationID := core.ID("0198b935-b600-7000-8000-000000000242")
	repository := &serviceRepositoryStub{operation: core.Operation{
		ID: operationID, AccountID: &accountID, Status: core.OperationRunning,
	}}
	service, _ := New(repository)

	operation, err := service.OperationStatus(context.Background(), OperationStatusParams{
		AccountID: accountID, OperationID: operationID,
	})
	if err != nil || operation.ID != operationID {
		t.Fatalf("OperationStatus = %#v / %v", operation, err)
	}
	if repository.authorization.Action != core.AuthorizationAccountResourcesView ||
		repository.authorization.AccountID == nil || *repository.authorization.AccountID != accountID ||
		repository.operationScope.AccountID == nil || *repository.operationScope.AccountID != accountID ||
		repository.operationScope.OperationID != operationID {
		t.Fatalf("authorization/scope = %#v / %#v", repository.authorization, repository.operationScope)
	}
}

func TestOperationStatusStopsBeforeLookupWhenAccountViewIsDenied(t *testing.T) {
	t.Parallel()
	repository := &serviceRepositoryStub{authorizeErr: core.ErrAuthorizationDenied}
	service, _ := New(repository)

	_, err := service.OperationStatus(context.Background(), OperationStatusParams{
		AccountID:   "0198b935-b600-7000-8000-000000000251",
		OperationID: "0198b935-b600-7000-8000-000000000252",
	})
	if !errors.Is(err, core.ErrAuthorizationDenied) || repository.getOperationCalls != 0 {
		t.Fatalf("OperationStatus error/lookups = %v / %d", err, repository.getOperationCalls)
	}
}

type serviceRepositoryStub struct {
	authorization     core.AuthorizeParams
	authorizeCalls    int
	authorizeErr      error
	created           core.CreateOperationParams
	operation         core.Operation
	hostNotReady      bool
	operationScope    core.OperationScope
	getOperationCalls int
}

func (stub *serviceRepositoryStub) GetDomain(_ context.Context, accountID, domainID core.ID) (core.Domain, error) {
	return core.Domain{ID: domainID, AccountID: accountID, WAF: core.DomainWAFPolicy{Mode: core.WAFModeBlockingPL1}}, nil
}

func (stub *serviceRepositoryStub) CurrentPackageAssignment(_ context.Context, accountID core.ID) (core.PackageAssignment, error) {
	return core.PackageAssignment{AccountID: accountID, EffectiveLimits: core.PackageLimits{
		Features: core.PackageFeatures{WAFExceptions: true},
	}}, nil
}

func (stub *serviceRepositoryStub) ListDomainWAFExceptions(context.Context, core.ID, core.ID) ([]core.DomainWAFException, error) {
	return nil, nil
}

func (stub *serviceRepositoryStub) HostingAccountHostReady(context.Context, core.ID) (bool, error) {
	return !stub.hostNotReady, nil
}

func (stub *serviceRepositoryStub) Authorize(
	_ context.Context,
	params core.AuthorizeParams,
) (core.AuthorizationDecision, error) {
	stub.authorizeCalls++
	stub.authorization = params
	return core.AuthorizationDecision{}, stub.authorizeErr
}

func (stub *serviceRepositoryStub) CreateOperation(
	_ context.Context,
	params core.CreateOperationParams,
) (core.Operation, error) {
	stub.created = params
	return stub.operation, nil
}

func (stub *serviceRepositoryStub) GetOperation(
	_ context.Context,
	scope core.OperationScope,
) (core.Operation, error) {
	stub.getOperationCalls++
	stub.operationScope = scope
	return stub.operation, nil
}

func (stub *serviceRepositoryStub) ListDomains(context.Context, core.ID, bool) ([]core.Domain, error) {
	return nil, nil
}
