// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/store"
)

type bootstrapServiceStub struct {
	status    core.BootstrapStatus
	statusErr error
	identity  core.Identity
	err       error
	params    core.BootstrapAdministratorParams
	calls     int
}

func (stub *bootstrapServiceStub) AdministratorBootstrapStatus(context.Context) (core.BootstrapStatus, error) {
	return stub.status, stub.statusErr
}

func (stub *bootstrapServiceStub) BootstrapAdministrator(_ context.Context, params core.BootstrapAdministratorParams) (core.Identity, error) {
	stub.calls++
	stub.params = params
	return stub.identity, stub.err
}

func TestBootstrapStatusContainsNoCapabilityMaterial(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, time.August, 16, 12, 15, 0, 0, time.UTC)
	service := &bootstrapServiceStub{status: core.BootstrapStatus{
		Required: true, CapabilityActive: true, ExpiresAt: &expiresAt,
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	New(discardLogger(), healthChecker{}, service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["required"] != true || response["capabilityActive"] != true || response["expiresAt"] != expiresAt.Format(time.RFC3339) {
		t.Fatalf("unexpected status response: %#v", response)
	}
	if _, exists := response["token"]; exists {
		t.Fatal("status response exposed token material")
	}
}

func TestBootstrapPostForwardsCanonicalDirectSourceWithoutSecretsInLogs(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	service := &bootstrapServiceStub{identity: core.Identity{
		ID: "0198b935-b600-7000-8000-000000000001", Email: "Admin@example.com",
		DisplayName: "Administrator", Locale: core.LocaleGerman, CreatedAt: createdAt,
	}}
	const rawToken = "sfb_secret-capability"
	const password = "correct horse battery staple"
	body := `{"token":"` + rawToken + `","email":"Admin@example.com","displayName":"Administrator","password":"` + password + `","locale":"de"}`
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", strings.NewReader(body))
	request.RemoteAddr = "[::ffff:192.0.2.24]:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	request.Header.Set("X-Request-ID", "request-1")
	New(logger, healthChecker{}, service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if service.calls != 1 || service.params.SourceAddress != "192.0.2.24" || service.params.RequestID != "request-1" {
		t.Fatalf("service call = %d params=%#v", service.calls, service.params)
	}
	if service.params.Token != rawToken || service.params.Password != password {
		t.Fatal("handler changed submitted secret values")
	}
	combinedOutput := logs.String() + recorder.Body.String()
	if strings.Contains(combinedOutput, rawToken) || strings.Contains(combinedOutput, password) {
		t.Fatalf("logs or response exposed request secrets: %s", combinedOutput)
	}
}

func TestBootstrapPostUsesGenericDenialAndDoesNotLogSecrets(t *testing.T) {
	t.Parallel()

	service := &bootstrapServiceStub{err: core.ErrBootstrapDenied}
	const rawToken = "sfb_do-not-log-this"
	const password = "do not log this password"
	body := `{"token":"` + rawToken + `","email":"admin@example.com","displayName":"Administrator","password":"` + password + `","locale":"en"}`
	var logs bytes.Buffer
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", strings.NewReader(body))
	New(slog.New(slog.NewJSONHandler(&logs, nil)), healthChecker{}, service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	output := logs.String() + recorder.Body.String()
	if strings.Contains(output, rawToken) || strings.Contains(output, password) {
		t.Fatalf("denial leaked request secrets: %s", output)
	}
	var response errorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != "bootstrap_denied" {
		t.Fatalf("error code = %q, want bootstrap_denied", response.Code)
	}
}

func TestBootstrapPostReturnsRetryAfter(t *testing.T) {
	t.Parallel()

	service := &bootstrapServiceStub{err: &core.BootstrapRateLimitError{RetryAfter: 1500 * time.Millisecond}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", strings.NewReader(validBootstrapJSON()))
	New(discardLogger(), healthChecker{}, service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d Retry-After=%q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
}

func TestBootstrapPostRejectsMalformedAndOversizedBodiesBeforeService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"token":"x","unknown":true}`},
		{name: "multiple values", body: validBootstrapJSON() + `{}`},
		{name: "oversized", body: `{"padding":"` + strings.Repeat("x", maxBootstrapRequestBytes) + `"}`},
		{name: "malformed", body: `{"token":`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &bootstrapServiceStub{}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", strings.NewReader(test.body))
			New(discardLogger(), healthChecker{}, service).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || service.calls != 0 {
				t.Fatalf("status=%d service calls=%d", recorder.Code, service.calls)
			}
		})
	}
}

func TestBootstrapStatusFailureIsGeneric(t *testing.T) {
	t.Parallel()

	service := &bootstrapServiceStub{statusErr: errors.New("database details")}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
	New(discardLogger(), healthChecker{}, service).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "database details") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBootstrapHTTPIntegrationCreatesFirstAdministrator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := core.NewRepository(state)
	if err != nil {
		t.Fatalf("core.NewRepository: %v", err)
	}
	capability, err := repository.CreateBootstrapCapability(ctx, core.CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	encoded, err := json.Marshal(bootstrapRequest{
		Token: capability.Token, Email: "admin@example.com", DisplayName: "Administrator",
		Password: "correct horse battery staple", Locale: core.LocaleEnglish,
	})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	handler := New(discardLogger(), state, repository)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", bytes.NewReader(encoded))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	status, err := repository.AdministratorBootstrapStatus(ctx)
	if err != nil {
		t.Fatalf("AdministratorBootstrapStatus: %v", err)
	}
	if status.Required || status.CapabilityActive {
		t.Fatalf("bootstrap remained active: %#v", status)
	}

	replayRecorder := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap", bytes.NewReader(encoded))
	handler.ServeHTTP(replayRecorder, replayRequest)
	if replayRecorder.Code != http.StatusConflict || !strings.Contains(replayRecorder.Body.String(), "bootstrap_disabled") {
		t.Fatalf("replay status=%d body=%s", replayRecorder.Code, replayRecorder.Body.String())
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func validBootstrapJSON() string {
	return `{"token":"sfb_example","email":"admin@example.com","displayName":"Administrator","password":"correct horse battery staple","locale":"en"}`
}
