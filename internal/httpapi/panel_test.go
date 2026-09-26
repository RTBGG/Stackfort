// SPDX-License-Identifier: AGPL-3.0-or-later
package httpapi

import (
	"context"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type panelServiceStub struct {
	calls int
	input agentprotocol.PanelIssueRequest
	key   string
	err   error
}

func (s *panelServiceStub) Status(context.Context, core.AuthorizationSubject) (agentprotocol.PanelStatusResponse, error) {
	return agentprotocol.PanelStatusResponse{}, s.err
}
func (s *panelServiceStub) Queue(_ context.Context, _ core.AuthorizationSubject, input agentprotocol.PanelIssueRequest, _ string, key string) (core.Operation, error) {
	s.calls++
	s.input = input
	s.key = key
	return core.Operation{ID: "queued", Status: core.OperationPending}, s.err
}

func TestPanelHTTPRequiresCSRFAndClosedExplicitIntent(t *testing.T) {
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	service := &panelServiceStub{}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{Authentication: authentication, PanelHostname: service})
	valid := `{"hostname":"panel.example.com","email":"admin@example.com","acceptTerms":true,"confirmOriginChange":true}`
	send := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/panel/issue", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "panel-browser")
		request.Header.Set(csrfHeaderName, "csrf-bound")
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
		request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	recorder := send(valid)
	if recorder.Code != 202 || service.calls != 1 || service.key != "panel-browser" || !authentication.authParams.RequireCSRF {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	for _, body := range []string{
		strings.Replace(valid, `"acceptTerms":true`, `"acceptTerms":false`, 1),
		strings.Replace(valid, `"confirmOriginChange":true`, `"confirmOriginChange":false`, 1),
		strings.TrimSuffix(valid, "}") + `,"privateKeyPath":"/etc/shadow"}`,
		strings.TrimSuffix(valid, "}") + `,"directoryUrl":"https://attacker.example"}`,
		valid + " {}", strings.Repeat(" ", 4097) + valid,
	} {
		if recorder := send(body); recorder.Code != 400 || service.calls != 1 {
			t.Fatalf("accepted unsafe request: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	authentication.authErr = core.ErrSessionInvalid
	if recorder := send(valid); recorder.Code != 401 || service.calls != 1 {
		t.Fatalf("unauthenticated request: %d", recorder.Code)
	}
}
