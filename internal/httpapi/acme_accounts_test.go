// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/acmeaccounts"
	"github.com/RTBGG/stackfort/internal/core"
)

func TestACMEAccountAdminEndpointQueuesCSRFBoundRegistrationWithoutAcceptingDirectoryURL(t *testing.T) {
	t.Parallel()
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	service := &acmeAccountServiceStub{operation: core.Operation{
		ID: "019c1234-5678-7abc-8def-0123456789ab", Status: core.OperationPending,
	}}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, ACMEAccounts: service,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/acme/accounts", bytes.NewBufferString(
		`{"environment":"letsencrypt-staging","contactEmail":"admin@example.test","termsAccepted":true}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "acme-http-key")
	request.Header.Set("X-Request-ID", "acme-http-request")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || service.queueCalls != 1 ||
		service.command.Environment != core.ACMELetsEncryptStaging ||
		service.command.IdempotencyKey != "acme-http-key" || !authentication.authParams.RequireCSRF {
		t.Fatalf("status/calls/command/auth = %d/%d/%#v/%#v body=%s",
			recorder.Code, service.queueCalls, service.command, authentication.authParams, recorder.Body.String())
	}

	unsafeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/acme/accounts", bytes.NewBufferString(
		`{"environment":"letsencrypt-staging","contactEmail":"admin@example.test","termsAccepted":true,"directoryUrl":"https://attacker.example"}`,
	))
	unsafeRequest.Header.Set("Content-Type", "application/json")
	unsafeRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	unsafeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unsafeRecorder, unsafeRequest)
	if unsafeRecorder.Code != http.StatusBadRequest || service.queueCalls != 1 {
		t.Fatalf("unsafe directory status/calls/body = %d/%d/%s",
			unsafeRecorder.Code, service.queueCalls, unsafeRecorder.Body.String())
	}
}

func TestACMEAccountListResponseContainsMetadataButNoCredentialsOrAuthorityAccountURLs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service := &acmeAccountServiceStub{accounts: []core.ACMEAccount{{
		ID:           "019c1234-5678-7abc-8def-0123456789ab",
		Environment:  core.ACMELetsEncryptStaging,
		DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
		ContactEmail: "admin@example.test", Status: core.ACMEAccountValid,
		AccountURI:          "https://authority.example/private-account-url",
		OrdersURL:           "https://authority.example/private-orders-url",
		PublicKeyThumbprint: strings.Repeat("a", 43), TermsAgreedAt: now,
		CreatedAt: now, UpdatedAt: now, RegisteredAt: &now,
	}}}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		ACMEAccounts:   service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/acme/accounts", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "letsencrypt-staging") ||
		strings.Contains(body, "private-account-url") || strings.Contains(body, "private-orders-url") ||
		strings.Contains(body, strings.Repeat("a", 43)) || strings.Contains(strings.ToLower(body), "ciphertext") {
		t.Fatalf("status/body = %d/%s", recorder.Code, body)
	}
}

type acmeAccountServiceStub struct {
	operation  core.Operation
	command    acmeaccounts.RegisterCommand
	queueCalls int
	accounts   []core.ACMEAccount
	err        error
}

func (service *acmeAccountServiceStub) QueueRegistration(
	_ context.Context,
	command acmeaccounts.RegisterCommand,
) (core.Operation, error) {
	service.queueCalls++
	service.command = command
	return service.operation, service.err
}

func (service *acmeAccountServiceStub) List(
	context.Context,
	core.AuthorizationSubject,
) ([]core.ACMEAccount, error) {
	return service.accounts, service.err
}
