// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/databaseworkspace"
)

func TestDatabaseWorkspaceOmitsPhysicalNamesAndCredentials(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000501")
	service := &databaseWorkspaceStub{workspace: core.DatabaseWorkspace{
		Databases: []core.ManagedDatabase{{
			ID: "0198b935-b600-7000-8000-000000000502", AccountID: accountID,
			Alias: "application", PhysicalName: "sf_secret_physical_database",
			Status: core.ManagedDatabaseActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
		Users: []core.ManagedDatabaseUser{{
			ID: "0198b935-b600-7000-8000-000000000503", AccountID: accountID,
			Alias: "application", PhysicalName: "sf_secret_physical_user", Host: "localhost",
			Status: core.ManagedDatabaseActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication:    &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		DatabaseWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+string(accountID)+"/databases", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || service.listCalls != 1 ||
		!strings.Contains(body, `"alias":"application"`) {
		t.Fatalf("status/calls/body = %d/%d/%s", recorder.Code, service.listCalls, body)
	}
	for _, forbidden := range []string{"sf_secret_physical", "physicalName", "password", "ciphertext", "wrappedKey"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("database workspace leaked %q: %s", forbidden, body)
		}
	}
}

func TestDatabaseDeletionRequiresCSRFAndPassesExactTypedConfirmation(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000511")
	targetID := core.ID("0198b935-b600-7000-8000-000000000512")
	operationID := core.ID("0198b935-b600-7000-8000-000000000513")
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	service := &databaseWorkspaceStub{deletion: core.ManagedDatabaseDeletion{
		Operation: core.Operation{ID: operationID, Status: core.OperationPending},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, DatabaseWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodDelete,
		"/api/v1/accounts/"+string(accountID)+"/databases/"+string(targetID),
		bytes.NewBufferString(`{"confirmation":"records"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "drop-records")
	request.Header.Set("X-Request-ID", "drop-records-request")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || service.deleteCalls != 1 ||
		service.deleteCommand.AccountID != accountID || service.deleteCommand.TargetID != targetID ||
		service.deleteCommand.TargetKind != core.DatabaseDeletionDatabase ||
		service.deleteCommand.Confirmation != "records" ||
		service.deleteCommand.IdempotencyKey != "drop-records" ||
		!authentication.authParams.RequireCSRF ||
		!strings.Contains(recorder.Body.String(), `"operationId":"`+string(operationID)+`"`) {
		t.Fatalf("status/auth/command/body = %d/%#v/%#v/%s",
			recorder.Code, authentication.authParams, service.deleteCommand, recorder.Body.String())
	}
}

func TestPHPMyAdminHandoffRequiresCSRFAndReturnsTokenOnlyInBody(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000521")
	userID := core.ID("0198b935-b600-7000-8000-000000000522")
	token := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE"
	expiresAt := time.Now().UTC().Add(30 * time.Second)
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	service := &databaseWorkspaceStub{handoff: core.PHPMyAdminHandoff{
		Token: token, ExpiresAt: expiresAt,
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, DatabaseWorkspace: service,
	})
	path := "/api/v1/accounts/" + string(accountID) + "/database-users/" +
		string(userID) + "/phpmyadmin-handoffs"
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.Header.Set("X-Request-ID", "phpmyadmin-launch-request")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.handoffCalls != 1 ||
		service.handoffParams.AccountID != accountID || service.handoffParams.UserID != userID ||
		service.handoffParams.RequestID != "phpmyadmin-launch-request" ||
		!authentication.authParams.RequireCSRF {
		t.Fatalf("status/auth/params = %d/%#v/%#v", recorder.Code, authentication.authParams, service.handoffParams)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"handoffToken":"`+token+`"`) ||
		!strings.Contains(body, `"launchPath":"/phpmyadmin/stackfort-launch.php"`) {
		t.Fatalf("body = %s", body)
	}
	if strings.Contains(path, token) || recorder.Header().Get("Location") != "" {
		t.Fatal("handoff bearer escaped the response body")
	}

	authentication.authErr = core.ErrCSRFInvalid
	request = httptest.NewRequest(http.MethodPost, path, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || service.handoffCalls != 1 {
		t.Fatalf("missing CSRF status/calls = %d/%d", recorder.Code, service.handoffCalls)
	}
}

func TestDatabaseCredentialRotationRequiresCSRFAndIdempotency(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000531")
	userID := core.ID("0198b935-b600-7000-8000-000000000532")
	operationID := core.ID("0198b935-b600-7000-8000-000000000533")
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	service := &databaseWorkspaceStub{rotation: core.ManagedDatabaseCredentialRotation{
		Operation: core.Operation{ID: operationID, Status: core.OperationPending},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, DatabaseWorkspace: service,
	})
	path := "/api/v1/accounts/" + string(accountID) + "/database-users/" +
		string(userID) + "/credential/rotate"
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.Header.Set("Idempotency-Key", "rotate-database-user")
	request.Header.Set("X-Request-ID", "rotate-database-user-request")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || service.rotationCalls != 1 ||
		service.rotationCommand.AccountID != accountID || service.rotationCommand.UserID != userID ||
		service.rotationCommand.IdempotencyKey != "rotate-database-user" ||
		service.rotationCommand.RequestID != "rotate-database-user-request" ||
		!authentication.authParams.RequireCSRF ||
		!strings.Contains(recorder.Body.String(), `"operationId":"`+string(operationID)+`"`) {
		t.Fatalf("status/auth/command/body = %d/%#v/%#v/%s",
			recorder.Code, authentication.authParams, service.rotationCommand, recorder.Body.String())
	}
}

type databaseWorkspaceStub struct {
	workspace       core.DatabaseWorkspace
	deletion        core.ManagedDatabaseDeletion
	handoff         core.PHPMyAdminHandoff
	rotation        core.ManagedDatabaseCredentialRotation
	listCalls       int
	deleteCalls     int
	handoffCalls    int
	rotationCalls   int
	deleteCommand   databaseworkspace.DeleteCommand
	handoffParams   databaseworkspace.IssuePHPMyAdminHandoffParams
	rotationCommand databaseworkspace.RotateCredentialCommand
}

func (stub *databaseWorkspaceStub) RotateCredential(_ context.Context, command databaseworkspace.RotateCredentialCommand) (core.ManagedDatabaseCredentialRotation, error) {
	stub.rotationCalls++
	stub.rotationCommand = command
	return stub.rotation, nil
}

func (*databaseWorkspaceStub) PrepareWizard(context.Context, databaseworkspace.WizardCommand) (core.ManagedDatabaseProvisioning, error) {
	return core.ManagedDatabaseProvisioning{}, nil
}

func (stub *databaseWorkspaceStub) List(context.Context, databaseworkspace.ListParams) (core.DatabaseWorkspace, error) {
	stub.listCalls++
	return stub.workspace, nil
}

func (*databaseWorkspaceStub) OperationStatus(context.Context, databaseworkspace.OperationStatusParams) (core.Operation, error) {
	return core.Operation{}, nil
}

func (*databaseWorkspaceStub) RevealCredential(context.Context, databaseworkspace.RevealCredentialParams) (core.RevealedDatabaseCredential, error) {
	return core.RevealedDatabaseCredential{}, nil
}

func (stub *databaseWorkspaceStub) IssuePHPMyAdminHandoff(_ context.Context, params databaseworkspace.IssuePHPMyAdminHandoffParams) (core.PHPMyAdminHandoff, error) {
	stub.handoffCalls++
	stub.handoffParams = params
	return stub.handoff, nil
}

func (stub *databaseWorkspaceStub) Delete(_ context.Context, command databaseworkspace.DeleteCommand) (core.ManagedDatabaseDeletion, error) {
	stub.deleteCalls++
	stub.deleteCommand = command
	return stub.deletion, nil
}
