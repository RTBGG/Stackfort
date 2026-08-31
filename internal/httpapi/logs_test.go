// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"github.com/RTBGG/stackfort/internal/logworkspace"
)

type logWorkspaceStub struct {
	params    logworkspace.ReadParams
	wafParams logworkspace.WAFReadParams
	calls     int
}

func (stub *logWorkspaceStub) ReadWAFEvents(
	_ context.Context, params logworkspace.WAFReadParams,
) (agentprotocol.WAFEventReadResponse, error) {
	stub.calls++
	stub.wafParams = params
	domain, _ := core.NormalizeDomainName("example.test")
	return agentprotocol.WAFEventReadResponse{
		Domain: domain, Events: []agentprotocol.WAFEvent{}, RetentionDays: hostinglogs.RetentionDays,
		MaximumActiveBytes: hostinglogs.MaximumActiveBytes, NativeDataWithheld: true,
	}, nil
}

func (stub *logWorkspaceStub) Read(_ context.Context, params logworkspace.ReadParams) (agentprotocol.HostingLogReadResponse, error) {
	stub.calls++
	stub.params = params
	domain, _ := core.NormalizeDomainName("example.test")
	return agentprotocol.HostingLogReadResponse{
		Domain: domain, Kind: params.Kind, Records: []agentprotocol.HostingLogRecord{},
		RetentionDays: hostinglogs.RetentionDays, MaximumActiveBytes: hostinglogs.MaximumActiveBytes,
		SensitiveRedaction: true,
	}, nil
}

func TestLogRouteIsAuthenticatedAndStrictlyAccountScoped(t *testing.T) {
	t.Parallel()
	service := &logWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, LogWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/logs?domainId=0198b935-b600-7000-8000-000000000412&kind=error&cursor=42%3A128", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.calls != 1 ||
		service.params.AccountID != "0198b935-b600-7000-8000-000000000411" ||
		service.params.DomainID != "0198b935-b600-7000-8000-000000000412" ||
		service.params.Kind != agentprotocol.HostingLogError || service.params.Cursor != "42:128" ||
		!strings.Contains(recorder.Body.String(), `"queryStringsStored":false`) {
		t.Fatalf("status=%d calls=%d params=%#v body=%s", recorder.Code, service.calls, service.params, recorder.Body.String())
	}
}

func TestLogRouteRejectsUnknownOrRepeatedQueryFields(t *testing.T) {
	t.Parallel()
	for _, query := range []string{
		"domainId=0198b935-b600-7000-8000-000000000412&kind=access&limit=100",
		"domainId=0198b935-b600-7000-8000-000000000412&domainId=0198b935-b600-7000-8000-000000000412&kind=access",
	} {
		service := &logWorkspaceStub{}
		handler := NewWithServices(discardHostTestLogger(), nil, Services{
			Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, LogWorkspace: service,
		})
		request := httptest.NewRequest(http.MethodGet,
			"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/logs?"+query, nil)
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest || service.calls != 0 || !strings.Contains(recorder.Body.String(), "invalid_log_request") {
			t.Fatalf("query=%q status=%d calls=%d body=%s", query, recorder.Code, service.calls, recorder.Body.String())
		}
	}
}

func TestWAFEventRouteIsAuthenticatedAndStrictlyAccountScoped(t *testing.T) {
	t.Parallel()
	service := &logWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, LogWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/waf-events?domainId=0198b935-b600-7000-8000-000000000412&cursor=42%3A128", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.calls != 1 ||
		service.wafParams.AccountID != "0198b935-b600-7000-8000-000000000411" ||
		service.wafParams.DomainID != "0198b935-b600-7000-8000-000000000412" ||
		service.wafParams.Cursor != "42:128" ||
		!strings.Contains(recorder.Body.String(), `"nativeDataWithheld":true`) {
		t.Fatalf("status=%d calls=%d params=%#v body=%s", recorder.Code, service.calls, service.wafParams, recorder.Body.String())
	}
}
