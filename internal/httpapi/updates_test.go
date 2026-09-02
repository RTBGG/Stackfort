// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/updatecheck"
)

type updateCheckServiceStub struct {
	status       updatecheck.Status
	statusErr    error
	policyErr    error
	checkErr     error
	policyParams core.UpdatePolicyParams
	statusCalls  int
	policyCalls  int
	checkCalls   int
}

func (stub *updateCheckServiceStub) Status(context.Context) (updatecheck.Status, error) {
	stub.statusCalls++
	return stub.status, stub.statusErr
}

func (stub *updateCheckServiceStub) UpdatePolicy(
	_ context.Context, params core.UpdatePolicyParams,
) (updatecheck.Status, error) {
	stub.policyCalls++
	stub.policyParams = params
	return stub.status, stub.policyErr
}

func (stub *updateCheckServiceStub) CheckNow(context.Context) (updatecheck.Status, error) {
	stub.checkCalls++
	return stub.status, stub.checkErr
}

func TestUpdateRoutesArePlatformAuthorizedAndCSRFSafe(t *testing.T) {
	t.Parallel()
	authenticated := authenticatedHostTestSession()
	authentication := &authenticationServiceStub{authenticated: authenticated}
	authorization := &platformAuthorizationStub{}
	service := &updateCheckServiceStub{status: updatecheck.Status{
		CurrentVersion: "1.0.0", CurrentVersionValid: true,
		Channel: core.UpdateChannelStable, AutomaticChecks: true,
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, PlatformAuthorization: authorization, UpdateChecks: service,
	})

	get := httptest.NewRequest(http.MethodGet, "/api/v1/admin/updates", nil)
	get.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusOK || service.statusCalls != 1 ||
		authorization.params.Action != core.AuthorizationPlatformView || authentication.authParams.RequireCSRF ||
		!strings.Contains(getRecorder.Body.String(), `"automaticChecks":true`) {
		t.Fatalf("GET status/calls/auth/body = %d/%d/%#v/%s", getRecorder.Code, service.statusCalls, authorization.params, getRecorder.Body.String())
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/updates/policy",
		bytes.NewBufferString(`{"channel":"beta","automaticChecks":false}`))
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("X-Request-ID", "update-policy-http")
	patch.Header.Set(csrfHeaderName, "csrf-bound")
	patch.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	patch.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	patchRecorder := httptest.NewRecorder()
	handler.ServeHTTP(patchRecorder, patch)
	if patchRecorder.Code != http.StatusOK || service.policyCalls != 1 ||
		authorization.params.Action != core.AuthorizationPlatformManage || !authentication.authParams.RequireCSRF ||
		service.policyParams.Subject.IdentityID() != authenticated.Identity.ID ||
		service.policyParams.Channel != core.UpdateChannelBeta || service.policyParams.AutomaticChecks ||
		service.policyParams.RequestID != "update-policy-http" || service.policyParams.SourceAddress != "192.0.2.1" {
		t.Fatalf("PATCH status/auth/policy = %d/%#v/%#v", patchRecorder.Code, authorization.params, service.policyParams)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/v1/admin/updates/check", nil)
	post.Header.Set(csrfHeaderName, "csrf-bound")
	post.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	post.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusOK || service.checkCalls != 1 ||
		authorization.params.Action != core.AuthorizationPlatformView || !authentication.authParams.RequireCSRF {
		t.Fatalf("POST status/calls/auth = %d/%d/%#v", postRecorder.Code, service.checkCalls, authorization.params)
	}
}

func TestUpdateRoutesFailClosedAndMapDiscoveryErrors(t *testing.T) {
	t.Parallel()
	authorization := &platformAuthorizationStub{err: core.ErrAuthorizationDenied}
	service := &updateCheckServiceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication:        &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PlatformAuthorization: authorization, UpdateChecks: service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/updates", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || service.statusCalls != 0 ||
		!strings.Contains(recorder.Body.String(), "permission_denied") {
		t.Fatalf("denied status/calls/body = %d/%d/%s", recorder.Code, service.statusCalls, recorder.Body.String())
	}

	authorization.err = nil
	service.checkErr = updatecheck.ErrRateLimited
	post := httptest.NewRequest(http.MethodPost, "/api/v1/admin/updates/check", nil)
	post.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	postRecorder := httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusTooManyRequests ||
		!strings.Contains(postRecorder.Body.String(), "update_check_rate_limited") {
		t.Fatalf("rate-limited status/body = %d/%s", postRecorder.Code, postRecorder.Body.String())
	}

	service.checkErr = errors.New("private upstream detail")
	post = httptest.NewRequest(http.MethodPost, "/api/v1/admin/updates/check", nil)
	post.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	postRecorder = httptest.NewRecorder()
	handler.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusInternalServerError || strings.Contains(postRecorder.Body.String(), "private upstream") {
		t.Fatalf("internal status/body = %d/%s", postRecorder.Code, postRecorder.Body.String())
	}
}
