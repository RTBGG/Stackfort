// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/backupworkspace"
)

type backupWorkspaceStub struct {
	createParams  backupworkspace.CreateParams
	restoreParams backupworkspace.RestoreParams
	listParams    backupworkspace.ListParams
	deleteParams  backupworkspace.DeleteParams
	createCalls   int
	restoreCalls  int
	listCalls     int
	deleteCalls   int
}

func (stub *backupWorkspaceStub) List(_ context.Context, params backupworkspace.ListParams) (agentprotocol.FileWriteResult, error) {
	stub.listCalls++
	stub.listParams = params
	return agentprotocol.FileWriteResult{}, nil
}

func (*backupWorkspaceStub) Inspect(context.Context, backupworkspace.LookupParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}

func (*backupWorkspaceStub) Verify(context.Context, backupworkspace.LookupParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}

func (stub *backupWorkspaceStub) Create(_ context.Context, params backupworkspace.CreateParams) (agentprotocol.FileWriteResult, error) {
	stub.createCalls++
	stub.createParams = params
	return agentprotocol.FileWriteResult{Backup: &agentprotocol.BackupRecord{BackupID: "019d413d-98f0-7abc-8def-0123456789ab"}}, nil
}

func (stub *backupWorkspaceStub) Restore(_ context.Context, params backupworkspace.RestoreParams) (agentprotocol.FileWriteResult, error) {
	stub.restoreCalls++
	stub.restoreParams = params
	return agentprotocol.FileWriteResult{Backup: &agentprotocol.BackupRecord{BackupID: params.BackupID}}, nil
}

func (*backupWorkspaceStub) Download(context.Context, backupworkspace.DownloadParams) (backupworkspace.Download, error) {
	return backupworkspace.Download{}, backupworkspace.ErrUnavailable
}
func (*backupWorkspaceStub) InitiateUpload(context.Context, backupworkspace.InitiateUploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (*backupWorkspaceStub) UploadStatus(context.Context, backupworkspace.UploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (*backupWorkspaceStub) WriteUploadChunk(context.Context, backupworkspace.UploadChunkParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (*backupWorkspaceStub) CompleteUpload(context.Context, backupworkspace.CompleteUploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (*backupWorkspaceStub) CancelUpload(context.Context, backupworkspace.CancelUploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *backupWorkspaceStub) Delete(_ context.Context, params backupworkspace.DeleteParams) (agentprotocol.FileWriteResult, error) {
	stub.deleteCalls++
	stub.deleteParams = params
	return agentprotocol.FileWriteResult{Completed: true}, nil
}

func TestBackupCreateRouteIsCSRFAuthorizedAndAccountScoped(t *testing.T) {
	t.Parallel()
	service := &backupWorkspaceStub{}
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, BackupWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/backups",
		bytes.NewBufferString(`{"scope":"document_root","sourcePath":"public_html"}`))
	request.RemoteAddr = "192.0.2.42:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "backup-create")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.createCalls != 1 || !authentication.authParams.RequireCSRF ||
		service.createParams.AccountID != "0198b935-b600-7000-8000-000000000411" ||
		service.createParams.Scope != agentprotocol.BackupScopeDocumentRoot ||
		service.createParams.SourcePath != "public_html" || service.createParams.RequestID != "backup-create" ||
		service.createParams.SourceAddress != "192.0.2.42" {
		t.Fatalf("status=%d calls=%d auth=%#v params=%#v body=%s", recorder.Code,
			service.createCalls, authentication.authParams, service.createParams, recorder.Body.String())
	}
}

func TestBackupRestoreRouteCarriesExactConfirmation(t *testing.T) {
	t.Parallel()
	service := &backupWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, BackupWorkspace: service,
	})
	backupID := "019d413d-98f0-7abc-8def-0123456789ab"
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/backups/"+backupID+"/restore",
		bytes.NewBufferString(`{"confirmation":"`+backupID+`"}`))
	request.RemoteAddr = "192.0.2.42:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.restoreCalls != 1 || service.restoreParams.BackupID != backupID ||
		service.restoreParams.Confirmation != backupID {
		t.Fatalf("status=%d calls=%d params=%#v body=%s", recorder.Code,
			service.restoreCalls, service.restoreParams, recorder.Body.String())
	}
}

func TestBackupListRouteRejectsUnknownQuery(t *testing.T) {
	t.Parallel()
	service := &backupWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, BackupWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/backups?limit=100", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.listCalls != 0 ||
		!strings.Contains(recorder.Body.String(), "invalid_backup_cursor") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.listCalls, recorder.Body.String())
	}
}

func TestBackupDeleteRouteIsCSRFProtectedAndCarriesExactConfirmation(t *testing.T) {
	t.Parallel()
	service := &backupWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, BackupWorkspace: service,
	})
	backupID := "019d413d-98f0-7abc-8def-0123456789ab"
	request := httptest.NewRequest(http.MethodDelete,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/backups/"+backupID,
		bytes.NewBufferString(`{"confirmation":"`+backupID+`"}`))
	request.RemoteAddr = "192.0.2.42:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.deleteCalls != 1 || service.deleteParams.BackupID != backupID ||
		service.deleteParams.Confirmation != backupID {
		t.Fatalf("status=%d calls=%d params=%#v body=%s", recorder.Code, service.deleteCalls,
			service.deleteParams, recorder.Body.String())
	}
}
