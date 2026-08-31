// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
)

type platformAuthorizationStub struct {
	decision core.AuthorizationDecision
	err      error
	params   core.AuthorizeParams
	calls    int
}

func (stub *platformAuthorizationStub) Authorize(
	_ context.Context,
	params core.AuthorizeParams,
) (core.AuthorizationDecision, error) {
	stub.calls++
	stub.params = params
	return stub.decision, stub.err
}

type hostCapabilityServiceStub struct {
	report agentprotocol.CapabilityReport
	err    error
	key    string
	calls  int
}

func (stub *hostCapabilityServiceStub) InspectCapabilities(
	_ context.Context,
	idempotencyKey string,
) (agentprotocol.CapabilityReport, error) {
	stub.calls++
	stub.key = idempotencyKey
	return stub.report, stub.err
}

func TestHostCapabilitiesRequiresPlatformViewAndReturnsReasonCodes(t *testing.T) {
	t.Parallel()

	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	authorization := &platformAuthorizationStub{}
	capabilities := &hostCapabilityServiceStub{report: agentprotocol.CapabilityReport{
		InspectedAt: "2026-08-16T12:00:00Z",
		Platform: agentprotocol.PlatformCapabilities{
			DistributionID: "debian", VersionID: "13", Architecture: "amd64",
			KernelRelease: "6.12.0", Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		},
		Systemd: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		Packages: []agentprotocol.PackageCapability{{
			Key: "php-fpm", PackageName: "php8.4-fpm", Version: "8.4",
			Availability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		}},
		Filesystem: agentprotocol.FilesystemCapabilities{
			Target: agentprotocol.ManagedHostingRoot,
			ProjectQuota: agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnavailable, ReasonCode: "project-quota-not-mounted",
			},
		},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, PlatformAuthorization: authorization, HostCapabilities: capabilities,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/host/capabilities", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "project-quota-not-mounted") ||
		!strings.Contains(recorder.Body.String(), `"managedPhpVersions":["8.4"]`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if authorization.calls != 1 || authorization.params.Action != core.AuthorizationPlatformView {
		t.Fatalf("authorization = calls %d params %#v", authorization.calls, authorization.params)
	}
	if capabilities.calls != 1 || !strings.HasPrefix(capabilities.key, "host-capabilities-") {
		t.Fatalf("capability call = calls %d key %q", capabilities.calls, capabilities.key)
	}
}

func TestHostCapabilitiesRejectsNonAdministratorBeforeAgentCall(t *testing.T) {
	t.Parallel()

	authorization := &platformAuthorizationStub{err: core.ErrAuthorizationDenied}
	capabilities := &hostCapabilityServiceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication:        &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PlatformAuthorization: authorization, HostCapabilities: capabilities,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/host/capabilities", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden || capabilities.calls != 0 ||
		!strings.Contains(recorder.Body.String(), "permission_denied") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, capabilities.calls, recorder.Body.String())
	}
}

func TestHostCapabilitiesHidesAgentFailureAndRequiresAuthentication(t *testing.T) {
	t.Parallel()

	logger := discardHostTestLogger()
	capabilities := &hostCapabilityServiceStub{err: errors.New("dial /run/stackfort/agent.sock: internal detail")}
	handler := NewWithServices(logger, nil, Services{
		Authentication:        &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PlatformAuthorization: &platformAuthorizationStub{}, HostCapabilities: capabilities,
	})
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/admin/host/capabilities", nil))
	if unauthenticated.Code != http.StatusUnauthorized || capabilities.calls != 0 {
		t.Fatalf("unauthenticated status=%d calls=%d", unauthenticated.Code, capabilities.calls)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/host/capabilities", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable ||
		!strings.Contains(recorder.Body.String(), "host_agent_unavailable") ||
		strings.Contains(recorder.Body.String(), "stackfort/agent.sock") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func authenticatedHostTestSession() core.AuthenticatedSession {
	return core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000090"},
		Session:  core.Session{ID: "0198b935-b600-7000-8000-000000000091"},
	}
}

func discardHostTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
