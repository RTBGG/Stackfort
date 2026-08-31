// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	certificateapp "github.com/RTBGG/stackfort/internal/certificates"
	"github.com/RTBGG/stackfort/internal/core"
)

func TestTLSIssueEndpointIsCSRFBoundAndCertificateListOmitsKeyAndAuthorityData(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000101")
	domainID := core.ID("0198b935-b600-7000-8000-000000000102")
	operationID := core.ID("0198b935-b600-7000-8000-000000000103")
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	service := &tlsCertificateServiceStub{
		operation: core.Operation{ID: operationID, Status: core.OperationPending},
		certificates: []core.TLSCertificate{{
			ID: "0198b935-b600-7000-8000-000000000104", Status: core.TLSCertificateActive,
			Names: []string{"example.test", "www.example.test"}, FullChainPEM: "private-response-fixture",
			CertificateURL: "https://authority.example/internal-cert-url", Issuer: "Test CA",
			FingerprintSHA256: strings.Repeat("a", 64), CreatedAt: now, ExpiresAt: &now,
		}},
	}
	handler := NewWithServices(slog.New(slog.NewTextHandler(io.Discard, nil)), healthChecker{}, Services{
		Authentication: authentication, TLSCertificates: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/"+string(accountID)+"/domains/"+string(domainID)+"/tls/issue", nil)
	request.Header.Set("Idempotency-Key", "tls-issue-http")
	request.Header.Set("X-Request-ID", "tls-issue-request")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || service.issueCalls != 1 ||
		service.command.AccountID != accountID || service.command.DomainID != domainID ||
		service.command.IdempotencyKey != "tls-issue-http" || !authentication.authParams.RequireCSRF {
		t.Fatalf("status/calls/command/auth = %d/%d/%#v/%#v body=%s",
			recorder.Code, service.issueCalls, service.command, authentication.authParams, recorder.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/"+string(accountID)+"/domains/"+string(domainID)+"/tls/certificates", nil)
	listRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	body := listRecorder.Body.String()
	if listRecorder.Code != http.StatusOK || !strings.Contains(body, "Test CA") ||
		strings.Contains(body, "private-response-fixture") || strings.Contains(body, "internal-cert-url") ||
		strings.Contains(strings.ToLower(body), "privatekey") {
		t.Fatalf("certificate list status/body = %d/%s", listRecorder.Code, body)
	}
}

type tlsCertificateServiceStub struct {
	operation    core.Operation
	command      certificateapp.IssueCommand
	issueCalls   int
	certificates []core.TLSCertificate
}

func (stub *tlsCertificateServiceStub) QueueIssue(
	_ context.Context,
	command certificateapp.IssueCommand,
) (core.Operation, error) {
	stub.issueCalls++
	stub.command = command
	return stub.operation, nil
}

func (stub *tlsCertificateServiceStub) List(
	context.Context,
	core.AuthorizationSubject,
	core.ID,
	core.ID,
) ([]core.TLSCertificate, error) {
	return stub.certificates, nil
}
