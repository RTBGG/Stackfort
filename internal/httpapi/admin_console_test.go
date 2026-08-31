// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/accountprovisioning"
	"github.com/RTBGG/stackfort/internal/core"
)

type adminConsoleServiceStub struct {
	packages           []core.Package
	accounts           []core.HostingAccountSummary
	operations         []core.Operation
	auditEvents        []core.AuditEvent
	createdPackage     core.CreatePackageParams
	createdAccount     core.CreateHostingAccountParams
	listPackagesCalls  int
	listOperationLimit int
}

func (stub *adminConsoleServiceStub) ListPackages(context.Context) ([]core.Package, error) {
	stub.listPackagesCalls++
	return stub.packages, nil
}
func (stub *adminConsoleServiceStub) CreatePackage(_ context.Context, params core.CreatePackageParams) (core.Package, error) {
	stub.createdPackage = params
	return core.Package{ID: "0198b935-b600-7000-8000-000000000201", Name: params.Name, Slug: params.Slug, Limits: params.Limits}, nil
}
func (stub *adminConsoleServiceStub) ListHostingAccountSummaries(context.Context) ([]core.HostingAccountSummary, error) {
	return stub.accounts, nil
}
func (stub *adminConsoleServiceStub) CreateHostingAccount(_ context.Context, params core.CreateHostingAccountParams) (core.HostingAccount, error) {
	stub.createdAccount = params
	return core.HostingAccount{ID: "0198b935-b600-7000-8000-000000000202", Name: params.Name, Slug: params.Slug}, nil
}
func (stub *adminConsoleServiceStub) Create(
	_ context.Context,
	command accountprovisioning.CreateCommand,
) (accountprovisioning.CreateResult, error) {
	actorID := command.Subject.IdentityID()
	stub.createdAccount = core.CreateHostingAccountParams{
		Name: command.Name, Slug: command.Slug, OwnerIdentityID: command.OwnerIdentityID,
		PackageID: command.PackageID, ActorID: &actorID, RequestID: command.RequestID,
	}
	return accountprovisioning.CreateResult{
		Account: core.HostingAccount{
			ID: "0198b935-b600-7000-8000-000000000202", Name: command.Name, Slug: command.Slug,
		},
		Operation: core.Operation{ID: "0198b935-b600-7000-8000-000000000203", Status: core.OperationPending},
	}, nil
}
func (stub *adminConsoleServiceStub) ListRecentOperations(_ context.Context, limit int) ([]core.Operation, error) {
	stub.listOperationLimit = limit
	return stub.operations, nil
}
func (stub *adminConsoleServiceStub) ListRecentAuditEvents(context.Context, int) ([]core.AuditEvent, error) {
	return stub.auditEvents, nil
}

func TestAdminConsolePackageListRequiresPackageViewAuthorization(t *testing.T) {
	t.Parallel()
	service := &adminConsoleServiceStub{packages: []core.Package{{
		ID: "0198b935-b600-7000-8000-000000000210", Name: "Starter", Slug: "starter",
		Limits: core.PackageLimits{AllowedPHPVersions: []string{}},
	}}}
	authorization := &platformAuthorizationStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication:        &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PlatformAuthorization: authorization, AdminConsole: service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/packages", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.listPackagesCalls != 1 ||
		authorization.params.Action != core.AuthorizationPackagesView ||
		!strings.Contains(recorder.Body.String(), `"name":"Starter"`) {
		t.Fatalf("status/calls/auth/body = %d/%d/%#v/%s", recorder.Code, service.listPackagesCalls, authorization.params, recorder.Body.String())
	}
}

func TestAdminConsoleAccountCreationUsesCSRFRecentActionAndCurrentOwner(t *testing.T) {
	t.Parallel()
	authenticated := authenticatedHostTestSession()
	authentication := &authenticationServiceStub{authenticated: authenticated}
	authorization := &platformAuthorizationStub{}
	service := &adminConsoleServiceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, PlatformAuthorization: authorization,
		AdminConsole: service, AccountProvisioning: service,
	})
	body := bytes.NewBufferString(`{"name":"Primary","slug":"primary","packageId":"0198b935-b600-7000-8000-000000000220"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "admin-create-account")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || !authentication.authParams.RequireCSRF ||
		authorization.params.Action != core.AuthorizationAccountsCreate ||
		service.createdAccount.OwnerIdentityID != authenticated.Identity.ID ||
		service.createdAccount.ActorID == nil || *service.createdAccount.ActorID != authenticated.Identity.ID ||
		service.createdAccount.RequestID != "admin-create-account" ||
		!strings.Contains(recorder.Body.String(), `"provisioningStatus":"pending"`) ||
		!strings.Contains(recorder.Body.String(), `"hostReady":false`) {
		t.Fatalf("status/auth/authorization/account/body = %d/%#v/%#v/%#v/%s", recorder.Code, authentication.authParams, authorization.params, service.createdAccount, recorder.Body.String())
	}
}

func TestAdminConsoleRejectsInvalidOperationLimitBeforeRepositoryCall(t *testing.T) {
	t.Parallel()
	service := &adminConsoleServiceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication:        &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PlatformAuthorization: &platformAuthorizationStub{}, AdminConsole: service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/operations?limit=201", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.listOperationLimit != 0 {
		t.Fatalf("status/limit/body = %d/%d/%s", recorder.Code, service.listOperationLimit, recorder.Body.String())
	}
}
