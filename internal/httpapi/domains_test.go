// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/domainlifecycle"
	"github.com/RTBGG/stackfort/internal/operations"
)

func TestDomainCreateEndpointRequiresCSRFAndQueuesTypedIdempotentOperation(t *testing.T) {
	t.Parallel()

	accountID := core.ID("0198b935-b600-7000-8000-000000000101")
	operationID := core.ID("0198b935-b600-7000-8000-000000000102")
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000103"},
	}}
	domains := &domainServiceStub{operation: core.Operation{ID: operationID, Status: core.OperationPending}}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, Domains: domains,
	})
	body := []byte(`{"name":"static.example.test","target":{"type":"static"},"wafMode":"detection_only"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+string(accountID)+"/domains", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-static-1")
	request.Header.Set("X-Request-ID", "domain-http-1")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status/body = %d / %s", recorder.Code, recorder.Body.String())
	}
	if !authentication.authParams.RequireCSRF || authentication.authParams.CSRFHeaderToken != "csrf-bound" ||
		authentication.authParams.CSRFCookieToken != "csrf-bound" {
		t.Fatalf("authentication params = %#v", authentication.authParams)
	}
	if domains.queueCalls != 1 || domains.command.AccountID != accountID ||
		domains.command.IdempotencyKey != "create-static-1" || domains.command.RequestID != "domain-http-1" ||
		domains.command.Payload.Action != operations.DomainLifecycleCreate ||
		domains.command.Payload.Target == nil || domains.command.Payload.Target.Type != core.DomainTargetStatic ||
		domains.command.Payload.WAFMode == nil || *domains.command.Payload.WAFMode != core.WAFModeDetectionOnly {
		t.Fatalf("queued command = %#v", domains.command)
	}
	var response domainOperationResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil ||
		response.OperationID != operationID || response.DomainID != operationID {
		t.Fatalf("response = %#v / %v", response, err)
	}
}

func TestDomainEndpointRejectsUnknownJSONBeforeQueue(t *testing.T) {
	t.Parallel()

	accountID := "0198b935-b600-7000-8000-000000000111"
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000112"},
	}}
	domains := &domainServiceStub{}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, Domains: domains,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID+"/domains",
		bytes.NewBufferString(`{"name":"static.example.test","target":{"type":"static"},"configurationText":"include /etc/shadow;"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || domains.queueCalls != 0 {
		t.Fatalf("status/calls/body = %d / %d / %s", recorder.Code, domains.queueCalls, recorder.Body.String())
	}
}

func TestDomainRoutingPreviewEndpointReturnsExactHostAndDestinationExamples(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000121")
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000122"},
	}}
	domains := &domainServiceStub{}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, Domains: domains,
	})
	body := []byte(`{
		"name":"WWW.Redirect.Example",
		"target":{"type":"redirect","redirect":{
			"statusCode":302,
			"targetUrl":"https://destination.example/base?fixed=1",
			"hostMode":"www_only",
			"preservePath":true,
			"preserveQuery":true
		}}
	}`)
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/"+string(accountID)+"/domains/preview", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	responseBody := recorder.Body.String()
	if recorder.Code != http.StatusOK || domains.previewCalls != 1 ||
		domains.previewParams.AccountID != accountID || domains.previewParams.Name != "WWW.Redirect.Example" ||
		authentication.authParams.RequireCSRF ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"sourcePattern":"redirect.example","sourceUrl":"https://redirect.example/example/path?source=preview","action":"inactive"`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"statusCode":302`)) ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"destinationUrl":"https://destination.example/base/example/path?fixed=1\u0026source=preview"`)) {
		t.Fatalf("status/calls/params/body = %d/%d/%#v/%s",
			recorder.Code, domains.previewCalls, domains.previewParams, responseBody)
	}
}

func TestAccountOperationEndpointReturnsOnlyScopedBrowserSafeStatus(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000131")
	operationID := core.ID("0198b935-b600-7000-8000-000000000132")
	now := time.Date(2026, time.August, 25, 10, 0, 0, 0, time.UTC)
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000133"},
	}}
	domains := &domainServiceStub{operation: core.Operation{
		ID: operationID, AccountID: &accountID, Kind: operations.DomainLifecycleKind,
		Status: core.OperationRunning, Stage: "rendering", ProgressPercent: 45,
		RequestID: "must-not-leak", IdempotencyKey: "must-not-leak",
		Payload: map[string]any{"secret": "must-not-leak"}, Result: map[string]any{"secret": "must-not-leak"},
		AttemptCount: 1, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, Domains: domains,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/"+string(accountID)+"/operations/"+string(operationID), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || domains.statusCalls != 1 ||
		domains.statusParams.AccountID != accountID || domains.statusParams.OperationID != operationID ||
		authentication.authParams.RequireCSRF {
		t.Fatalf("status/calls/params/auth/body = %d/%d/%#v/%#v/%s",
			recorder.Code, domains.statusCalls, domains.statusParams, authentication.authParams, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"progressPercent":45`)) ||
		bytes.Contains(recorder.Body.Bytes(), []byte("must-not-leak")) ||
		strings.Contains(body, "payload") || strings.Contains(body, "result") ||
		strings.Contains(body, "requestId") || strings.Contains(body, "idempotencyKey") {
		t.Fatalf("unsafe or incomplete response = %s", body)
	}
}

func TestAccountOperationEndpointMakesDeniedAccountOpaque(t *testing.T) {
	t.Parallel()
	accountID := "0198b935-b600-7000-8000-000000000141"
	operationID := "0198b935-b600-7000-8000-000000000142"
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000143"},
	}}
	domains := &domainServiceStub{statusErr: core.ErrAuthorizationDenied}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, Domains: domains,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/"+accountID+"/operations/"+operationID, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"resource_not_found"`) {
		t.Fatalf("status/body = %d / %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdministratorWAFExceptionEndpointQueuesOnlyStructuredScope(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000151")
	domainID := core.ID("0198b935-b600-7000-8000-000000000152")
	operationID := core.ID("0198b935-b600-7000-8000-000000000153")
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000154"},
	}}
	domains := &domainServiceStub{operation: core.Operation{ID: operationID, Status: core.OperationPending}}
	handler := NewWithServices(discardHostTestLogger(), healthChecker{}, Services{
		Authentication: authentication, Domains: domains,
	})
	body := `{"ruleId":941100,"requestPath":"/search","parameter":"q","expiresAt":"2026-09-01T10:00:00Z"}`
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/accounts/"+string(accountID)+"/domains/"+string(domainID)+"/waf-exceptions",
		strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "waf-exception-create-1")
	request.Header.Set("X-Request-ID", "waf-exception-http-1")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted || domains.wafCreateCalls != 1 ||
		domains.wafCreate.AccountID != accountID || domains.wafCreate.DomainID != domainID ||
		domains.wafCreate.RuleID != 941100 || domains.wafCreate.RequestPath != "/search" ||
		domains.wafCreate.Parameter != "q" || !authentication.authParams.RequireCSRF {
		t.Fatalf("status/command/auth/body = %d / %#v / %#v / %s",
			recorder.Code, domains.wafCreate, authentication.authParams, recorder.Body.String())
	}
}

type domainServiceStub struct {
	operation      core.Operation
	queueErr       error
	command        domainlifecycle.Command
	queueCalls     int
	domains        []core.Domain
	listErr        error
	preview        core.DomainRoutingPreview
	previewParams  domainlifecycle.PreviewParams
	previewCalls   int
	previewErr     error
	statusParams   domainlifecycle.OperationStatusParams
	statusCalls    int
	statusErr      error
	wafCreate      domainlifecycle.WAFExceptionCommand
	wafCreateCalls int
	wafRemove      domainlifecycle.RemoveWAFExceptionCommand
	wafExceptions  []core.DomainWAFException
}

func (stub *domainServiceStub) Queue(_ context.Context, command domainlifecycle.Command) (core.Operation, error) {
	stub.queueCalls++
	stub.command = command
	return stub.operation, stub.queueErr
}

func (stub *domainServiceStub) List(context.Context, domainlifecycle.ListParams) ([]core.Domain, error) {
	return stub.domains, stub.listErr
}

func (stub *domainServiceStub) OperationStatus(
	_ context.Context,
	params domainlifecycle.OperationStatusParams,
) (core.Operation, error) {
	stub.statusCalls++
	stub.statusParams = params
	return stub.operation, stub.statusErr
}

func (stub *domainServiceStub) Preview(
	_ context.Context,
	params domainlifecycle.PreviewParams,
) (core.DomainRoutingPreview, error) {
	stub.previewCalls++
	stub.previewParams = params
	if stub.previewErr != nil {
		return core.DomainRoutingPreview{}, stub.previewErr
	}
	if stub.preview.Routes != nil {
		return stub.preview, nil
	}
	return core.PreviewDomainRouting(core.DomainRoutingPreviewParams{
		Name: params.Name, CanonicalMode: params.CanonicalMode, Target: params.Target,
	})
}

func (stub *domainServiceStub) QueueWAFException(
	_ context.Context,
	command domainlifecycle.WAFExceptionCommand,
) (core.Operation, error) {
	stub.wafCreateCalls++
	stub.wafCreate = command
	return stub.operation, stub.queueErr
}

func (stub *domainServiceStub) QueueWAFExceptionRemoval(
	_ context.Context,
	command domainlifecycle.RemoveWAFExceptionCommand,
) (core.Operation, error) {
	stub.wafRemove = command
	return stub.operation, stub.queueErr
}

func (stub *domainServiceStub) ListWAFExceptions(
	context.Context,
	domainlifecycle.ListWAFExceptionsParams,
) ([]core.DomainWAFException, error) {
	return stub.wafExceptions, stub.listErr
}
