// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/fileworkspace"
)

type fileWorkspaceStub struct {
	params         fileworkspace.ListParams
	result         agentprotocol.FileListResponse
	err            error
	calls          int
	downloadParams fileworkspace.DownloadParams
	download       fileworkspace.Download
	downloadErr    error
	downloadCalls  int
	initiateParams fileworkspace.InitiateUploadParams
	initiateResult agentprotocol.FileWriteResult
	initiateCalls  int
	chunkParams    fileworkspace.UploadChunkParams
	chunkResult    agentprotocol.FileWriteResult
	chunkCalls     int
	mutationParams fileworkspace.NodeMutationParams
	mutationCalls  int
	archiveParams  fileworkspace.ArchiveMutationParams
	archiveCalls   int
	trashParams    fileworkspace.TrashNodeParams
	trashCalls     int
	trashList      agentprotocol.FileWriteResult
}

func (stub *fileWorkspaceStub) InitiateUpload(_ context.Context, params fileworkspace.InitiateUploadParams) (agentprotocol.FileWriteResult, error) {
	stub.initiateCalls++
	stub.initiateParams = params
	return stub.initiateResult, nil
}
func (stub *fileWorkspaceStub) UploadStatus(context.Context, fileworkspace.UploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) WriteUploadChunk(_ context.Context, params fileworkspace.UploadChunkParams) (agentprotocol.FileWriteResult, error) {
	stub.chunkCalls++
	stub.chunkParams = params
	return stub.chunkResult, nil
}

func TestFileUploadInitiationIsCSRFAuthorizedAndAccountScoped(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{initiateResult: agentprotocol.FileWriteResult{
		UploadID: "019c1234-5678-7abc-8def-0123456789ae", Directory: "public_html",
		Name: "site.bin", SizeBytes: 8, CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	}}
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/file-uploads",
		bytes.NewBufferString(`{"directory":"public_html","name":"site.bin","sizeBytes":8}`))
	request.RemoteAddr = "192.0.2.42:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "file-upload-initiate")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.initiateCalls != 1 || !authentication.authParams.RequireCSRF ||
		service.initiateParams.Directory != "public_html" || service.initiateParams.Name != "site.bin" ||
		service.initiateParams.SourceAddress != "192.0.2.42" || service.initiateParams.RequestID != "file-upload-initiate" {
		t.Fatalf("status=%d calls=%d auth=%#v params=%#v body=%s", recorder.Code,
			service.initiateCalls, authentication.authParams, service.initiateParams, recorder.Body.String())
	}
}

func TestFileUploadChunkStreamsRawBodyWithExactOffset(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{chunkResult: agentprotocol.FileWriteResult{
		UploadID: "019c1234-5678-7abc-8def-0123456789ae", Directory: "public_html", Name: "site.bin",
		SizeBytes: 8, ReceivedBytes: 4, CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPut,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/file-uploads/019c1234-5678-7abc-8def-0123456789ae",
		strings.NewReader("fort"))
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Upload-Offset", "0")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body, _ := io.ReadAll(service.chunkParams.Body)
	if recorder.Code != http.StatusOK || service.chunkCalls != 1 || service.chunkParams.Offset != 0 ||
		service.chunkParams.ChunkLength != 4 || string(body) != "fort" {
		t.Fatalf("status=%d calls=%d params=%#v streamed=%q response=%s",
			recorder.Code, service.chunkCalls, service.chunkParams, body, recorder.Body.String())
	}
}

func TestFileCopyRouteIsClosedCSRFAuthorizedAndAccountScoped(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/file-operations",
		bytes.NewBufferString(`{"action":"copy","sourceDirectory":"public_html","sourceName":"index.html","destinationDirectory":"public_html/assets","destinationName":"index.html"}`))
	request.RemoteAddr = "192.0.2.42:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "file-copy")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.mutationCalls != 1 || !authentication.authParams.RequireCSRF ||
		service.mutationParams.Action != agentprotocol.FileWriteCopy || service.mutationParams.SourceName != "index.html" ||
		service.mutationParams.DestinationDirectory != "public_html/assets" ||
		service.mutationParams.SourceAddress != "192.0.2.42" || service.mutationParams.RequestID != "file-copy" {
		t.Fatalf("status=%d calls=%d auth=%#v params=%#v body=%s", recorder.Code,
			service.mutationCalls, authentication.authParams, service.mutationParams, recorder.Body.String())
	}
}

func TestFileTrashRouteRejectsUnknownOperationBeforeService(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/file-operations",
		bytes.NewBufferString(`{"action":"delete","sourceDirectory":"public_html","sourceName":"index.html","destinationDirectory":"","destinationName":""}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.mutationCalls != 0 ||
		!strings.Contains(recorder.Body.String(), "invalid_file_operation") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.mutationCalls, recorder.Body.String())
	}
}

func TestFileArchiveRouteIsClosedCSRFAuthorizedAndAccountScoped(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	authentication := &authenticationServiceStub{authenticated: authenticatedHostTestSession()}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/file-archives",
		bytes.NewBufferString(`{"action":"extract","format":"zip","sourceDirectory":"public_html","sourceName":"assets.zip","destinationDirectory":"public_html","destinationName":"assets-restored"}`))
	request.RemoteAddr = "192.0.2.42:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "file-archive-extract")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.archiveCalls != 1 || !authentication.authParams.RequireCSRF ||
		service.archiveParams.Action != agentprotocol.FileWriteArchiveExtract ||
		service.archiveParams.Format != agentprotocol.FileArchiveZIP || service.archiveParams.SourceName != "assets.zip" ||
		service.archiveParams.DestinationName != "assets-restored" || service.archiveParams.SourceAddress != "192.0.2.42" ||
		service.archiveParams.RequestID != "file-archive-extract" {
		t.Fatalf("status=%d calls=%d auth=%#v params=%#v body=%s", recorder.Code,
			service.archiveCalls, authentication.authParams, service.archiveParams, recorder.Body.String())
	}
}

func TestFileArchiveRouteRejectsUnknownActionBeforeService(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/file-archives",
		bytes.NewBufferString(`{"action":"inspect","format":"zip","sourceDirectory":"public_html","sourceName":"assets.zip","destinationDirectory":"public_html","destinationName":"assets"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.archiveCalls != 0 ||
		!strings.Contains(recorder.Body.String(), "invalid_file_archive") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.archiveCalls, recorder.Body.String())
	}
}
func (stub *fileWorkspaceStub) CompleteUpload(context.Context, fileworkspace.CompleteUploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) CancelUpload(context.Context, fileworkspace.UploadParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) CreateNode(context.Context, fileworkspace.CreateNodeParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) MutateNode(_ context.Context, params fileworkspace.NodeMutationParams) (agentprotocol.FileWriteResult, error) {
	stub.mutationCalls++
	stub.mutationParams = params
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) MutateArchive(_ context.Context, params fileworkspace.ArchiveMutationParams) (agentprotocol.FileWriteResult, error) {
	stub.archiveCalls++
	stub.archiveParams = params
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) TrashNode(_ context.Context, params fileworkspace.TrashNodeParams) (agentprotocol.FileWriteResult, error) {
	stub.trashCalls++
	stub.trashParams = params
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) ListTrash(context.Context, fileworkspace.TrashListParams) (agentprotocol.FileWriteResult, error) {
	return stub.trashList, nil
}
func (stub *fileWorkspaceStub) RestoreTrash(context.Context, fileworkspace.TrashParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *fileWorkspaceStub) PurgeTrash(context.Context, fileworkspace.TrashParams) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, nil
}

func (stub *fileWorkspaceStub) Download(_ context.Context, params fileworkspace.DownloadParams) (fileworkspace.Download, error) {
	stub.downloadCalls++
	stub.downloadParams = params
	return stub.download, stub.downloadErr
}

func (stub *fileWorkspaceStub) List(_ context.Context, params fileworkspace.ListParams) (agentprotocol.FileListResponse, error) {
	stub.calls++
	stub.params = params
	return stub.result, stub.err
}

func TestFileDownloadStreamsAttachmentAndParsesSingleRange(t *testing.T) {
	t.Parallel()
	content := "fort"
	service := &fileWorkspaceStub{download: fileworkspace.Download{
		Name: "site data.txt", TotalSize: 10, Offset: 6, Length: uint64(len(content)), Partial: true,
		ModifiedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
		Body:       io.NopCloser(strings.NewReader(content)),
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/files/download?path=public_html%2Fsite+data.txt", nil)
	request.Header.Set("Range", "bytes=6-9")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != content ||
		recorder.Header().Get("Content-Range") != "bytes 6-9/10" ||
		!strings.Contains(recorder.Header().Get("Content-Disposition"), "attachment") ||
		service.downloadCalls != 1 || service.downloadParams.Path != "public_html/site data.txt" ||
		service.downloadParams.Range == nil || service.downloadParams.Range.Start == nil ||
		*service.downloadParams.Range.Start != 6 {
		t.Fatalf("status=%d headers=%v calls=%d params=%#v body=%q",
			recorder.Code, recorder.Header(), service.downloadCalls, service.downloadParams, recorder.Body.String())
	}
}

func TestFileDownloadRejectsMultipleRangesBeforeService(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/files/download?path=index.html", nil)
	request.Header.Set("Range", "bytes=0-1,3-4")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.downloadCalls != 0 ||
		!strings.Contains(recorder.Body.String(), "invalid_file_range") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.downloadCalls, recorder.Body.String())
	}
}

func TestFileDownloadRejectsHeadWithoutOpeningStream(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodHead,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/files/download?path=index.html", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet ||
		service.downloadCalls != 0 {
		t.Fatalf("status=%d allow=%q calls=%d", recorder.Code, recorder.Header().Get("Allow"), service.downloadCalls)
	}
}

func TestFileDownloadReturnsRFCUnsatisfiedRange(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{downloadErr: &fileworkspace.RangeError{TotalSize: 12}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/files/download?path=index.html", nil)
	request.Header.Set("Range", "bytes=20-")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestedRangeNotSatisfiable ||
		recorder.Header().Get("Content-Range") != "bytes */12" {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestFileListingIsAuthenticatedAccountScopedAndMetadataOnly(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{result: agentprotocol.FileListResponse{
		Path: "public_html", Entries: []agentprotocol.FileEntry{{
			Name: "index.html", Type: agentprotocol.FileEntryRegular, SizeBytes: 12, Mode: 0o640,
			ModifiedAt: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC),
		}},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/files?path=public_html", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || service.calls != 1 || service.params.Path != "public_html" ||
		!strings.Contains(body, `"name":"index.html"`) || strings.Contains(body, "homeDirectory") || strings.Contains(body, "uid") {
		t.Fatalf("status=%d params=%#v body=%s", recorder.Code, service.params, body)
	}
}

func TestFileListingRejectsAmbiguousQueryBeforeService(t *testing.T) {
	t.Parallel()
	service := &fileWorkspaceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()}, FileWorkspace: service,
	})
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/accounts/0198b935-b600-7000-8000-000000000411/files?path=a&path=b", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.calls != 0 || !strings.Contains(recorder.Body.String(), "invalid_file_path") {
		t.Fatalf("status=%d calls=%d body=%s", recorder.Code, service.calls, recorder.Body.String())
	}
}
