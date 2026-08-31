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
	"github.com/RTBGG/stackfort/internal/phpworkspace"
)

type phpWorkspaceStub struct {
	status phpworkspace.Status
	err    error
	params phpworkspace.Params
	calls  int
}

func (stub *phpWorkspaceStub) Status(_ context.Context, params phpworkspace.Params) (phpworkspace.Status, error) {
	stub.calls++
	stub.params = params
	return stub.status, stub.err
}

func TestAccountPHPStatusIsAuthenticatedScopedAndOmitsHostInternals(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000401")
	memory, cpu, processes := uint64(32<<20), uint64(10_000_000), uint64(2)
	service := &phpWorkspaceStub{status: phpworkspace.Status{
		RuntimeCapability:    agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		HostApprovedVersions: []string{"8.4"}, PackageAllowedVersions: []string{"8.4", "8.5"},
		AvailableVersions: []string{"8.4"}, Pools: []phpworkspace.Pool{{
			Version: "8.4", State: agentprotocol.PHPPoolActive, ConfiguredDomains: 1,
			MemoryBytes: &memory, CPUTimeNanosec: &cpu, Processes: &processes,
		}},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PHPWorkspace:   service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+string(accountID)+"/php", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || service.calls != 1 || service.params.AccountID != accountID ||
		!strings.Contains(body, `"availableVersions":["8.4"]`) ||
		!strings.Contains(body, `"configuredDomains":1`) {
		t.Fatalf("status=%d params=%#v body=%s", recorder.Code, service.params, body)
	}
	for _, forbidden := range []string{"unitName", "socket", "controlGroup", "arguments", "username", "uid"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("PHP response leaked %q: %s", forbidden, body)
		}
	}
}

func TestAccountPHPStatusMakesDeniedAccountOpaque(t *testing.T) {
	t.Parallel()
	service := &phpWorkspaceStub{err: core.ErrAuthorizationDenied}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		PHPWorkspace:   service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/php", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "resource_not_found") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
