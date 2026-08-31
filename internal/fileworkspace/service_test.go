// SPDX-License-Identifier: AGPL-3.0-or-later

package fileworkspace

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const fileTestAccountID = core.ID("019c1234-5678-7abc-8def-0123456789ad")

type fileRepositoryStub struct {
	authorizeErr   error
	ready          bool
	account        core.HostingAccount
	action         core.AuthorizationAction
	authorizeCalls int
	auditParams    core.AppendAuditEventParams
	auditCalls     int
	auditErr       error
}

func (stub *fileRepositoryStub) AppendAuditEvent(
	_ context.Context, params core.AppendAuditEventParams,
) (core.AuditEvent, error) {
	stub.auditCalls++
	stub.auditParams = params
	return core.AuditEvent{ID: "019c1234-5678-7abc-8def-0123456789ae"}, stub.auditErr
}

func (stub *fileRepositoryStub) Authorize(_ context.Context, params core.AuthorizeParams) (core.AuthorizationDecision, error) {
	stub.authorizeCalls++
	stub.action = params.Action
	return core.AuthorizationDecision{}, stub.authorizeErr
}
func (stub *fileRepositoryStub) GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error) {
	return stub.account, nil
}
func (stub *fileRepositoryStub) HostingAccountHostReady(context.Context, core.ID) (bool, error) {
	return stub.ready, nil
}

type fileAgentStub struct {
	request         agentprotocol.FileListRequest
	result          agentprotocol.FileListResponse
	err             error
	calls           int
	downloadRequest agentprotocol.FileDownloadRequest
	download        agentclient.FileDownload
	downloadErr     error
	downloadCalls   int
	writeRequest    agentprotocol.FileWriteRequest
	writeResult     agentprotocol.FileWriteResult
	writeErr        error
	writeCalls      int
}

func (stub *fileAgentStub) WriteHostingFile(
	_ context.Context, request agentprotocol.FileWriteRequest, _ io.Reader,
) (agentprotocol.FileWriteResult, error) {
	stub.writeCalls++
	stub.writeRequest = request
	return stub.writeResult, stub.writeErr
}

func (stub *fileAgentStub) DownloadHostingFile(
	_ context.Context, request agentprotocol.FileDownloadRequest,
) (agentclient.FileDownload, error) {
	stub.downloadCalls++
	stub.downloadRequest = request
	return stub.download, stub.downloadErr
}

func (stub *fileAgentStub) ListHostingFiles(_ context.Context, _ string, request agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error) {
	stub.calls++
	stub.request = request
	return stub.result, stub.err
}

func TestListAuthorizesAndDerivesManagedIdentity(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	agent := &fileAgentStub{result: agentprotocol.FileListResponse{Path: "public_html", Entries: []agentprotocol.FileEntry{}}}
	service, err := New(repository, agent)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	listing, err := service.List(t.Context(), ListParams{AccountID: fileTestAccountID, Path: "public_html"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Path != "public_html" || repository.action != core.AuthorizationAccountFilesView || agent.calls != 1 ||
		agent.request.Identity.AccountID != string(fileTestAccountID) || agent.request.Limit != agentprotocol.MaximumFileListingEntries {
		t.Fatalf("listing=%#v action=%s request=%#v", listing, repository.action, agent.request)
	}
}

func TestListRejectsTraversalBeforeAuthorizationOrAgent(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	agent := &fileAgentStub{}
	service, _ := New(repository, agent)
	if _, err := service.List(t.Context(), ListParams{AccountID: fileTestAccountID, Path: "../etc"}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("traversal error = %v", err)
	}
	if repository.authorizeCalls != 0 || agent.calls != 0 {
		t.Fatalf("authorization calls=%d agent calls=%d", repository.authorizeCalls, agent.calls)
	}
}

func TestListMapsTypedAgentPathFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		code agentprotocol.ErrorCode
		want error
	}{{agentprotocol.ErrorFileNotFound, ErrNotFound}, {agentprotocol.ErrorFileConflict, ErrConflict}, {agentprotocol.ErrorFileUnavailable, ErrUnavailable}} {
		repository := validFileRepository(t)
		agent := &fileAgentStub{err: &agentclient.RemoteError{StatusCode: 503, Code: test.code}}
		service, _ := New(repository, agent)
		if _, err := service.List(t.Context(), ListParams{AccountID: fileTestAccountID}); !errors.Is(err, test.want) {
			t.Errorf("code %s error = %v, want %v", test.code, err, test.want)
		}
	}
}

func TestDownloadAuthorizesAndStreamsDerivedManagedIdentity(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	content := "stackfort-download"
	agent := &fileAgentStub{download: agentclient.FileDownload{
		TotalSize: uint64(len(content)), Length: uint64(len(content)),
		ModifiedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
		Body:       io.NopCloser(strings.NewReader(content)),
	}}
	service, err := New(repository, agent)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	download, err := service.Download(t.Context(), DownloadParams{
		AccountID: fileTestAccountID, Path: "public_html/index.html",
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer download.Body.Close()
	body, err := io.ReadAll(download.Body)
	if err != nil || string(body) != content || download.Name != "index.html" ||
		repository.action != core.AuthorizationAccountFilesView || agent.downloadCalls != 1 ||
		agent.downloadRequest.Identity.AccountID != string(fileTestAccountID) {
		t.Fatalf("download=%#v action=%s request=%#v body=%q err=%v",
			download, repository.action, agent.downloadRequest, body, err)
	}
}

func TestDownloadRejectsTraversalBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	agent := &fileAgentStub{}
	service, _ := New(repository, agent)
	if _, err := service.Download(t.Context(), DownloadParams{
		AccountID: fileTestAccountID, Path: "../secret",
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("traversal error = %v", err)
	}
	if repository.authorizeCalls != 0 || agent.downloadCalls != 0 {
		t.Fatalf("authorization calls=%d download calls=%d", repository.authorizeCalls, agent.downloadCalls)
	}
	zero := uint64(0)
	if _, err := service.Download(t.Context(), DownloadParams{
		AccountID: fileTestAccountID, Path: "index.html",
		Range: &agentprotocol.FileDownloadRange{SuffixLength: &zero},
	}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("invalid range error = %v", err)
	}
	if repository.authorizeCalls != 0 || agent.downloadCalls != 0 {
		t.Fatalf("invalid range reached authorization=%d agent=%d", repository.authorizeCalls, agent.downloadCalls)
	}
}

func TestDownloadMapsUnsatisfiedRangeWithTotal(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	total := uint64(17)
	agent := &fileAgentStub{downloadErr: &agentclient.RemoteError{
		StatusCode: 416, Code: agentprotocol.ErrorFileRangeNotSatisfiable, TotalSize: &total,
	}}
	service, _ := New(repository, agent)
	_, err := service.Download(t.Context(), DownloadParams{AccountID: fileTestAccountID, Path: "index.html"})
	var rangeError *RangeError
	if !errors.As(err, &rangeError) || rangeError.TotalSize != total {
		t.Fatalf("range error = %#v", err)
	}
}

func TestInitiateUploadAuthorizesManagementAndPersistsAuditBeforeAgent(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	agent := &fileAgentStub{writeResult: agentprotocol.FileWriteResult{
		UploadID: "019c1234-5678-7abc-8def-0123456789af", Directory: "public_html", Name: "site.bin",
		SizeBytes: 8, CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	}}
	service, _ := New(repository, agent)
	result, err := service.InitiateUpload(t.Context(), InitiateUploadParams{MutationContext: MutationContext{
		Subject: validFileSubject(), AccountID: fileTestAccountID, RequestID: "upload-initiate", SourceAddress: "192.0.2.42",
	}, Directory: "public_html", Name: "site.bin", SizeBytes: 8})
	if err != nil || result.Name != "site.bin" || repository.action != core.AuthorizationAccountFilesManage ||
		repository.auditCalls != 1 || repository.auditParams.Action != "file.upload.initiate.authorized" ||
		repository.auditParams.SourceAddress != "192.0.2.42" || agent.writeCalls != 1 ||
		agent.writeRequest.Correlation == nil || agent.writeRequest.Correlation.AuditEventID == "" ||
		agent.writeRequest.Identity.AccountID != string(fileTestAccountID) {
		t.Fatalf("result=%#v err=%v action=%s audit=%#v request=%#v",
			result, err, repository.action, repository.auditParams, agent.writeRequest)
	}
}

func TestUploadMutationStopsWhenDurableAuditCannotBeWritten(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	repository.auditErr = errors.New("database unavailable")
	agent := &fileAgentStub{}
	service, _ := New(repository, agent)
	_, err := service.CreateNode(t.Context(), CreateNodeParams{MutationContext: MutationContext{
		Subject: validFileSubject(), AccountID: fileTestAccountID, RequestID: "create-file", SourceAddress: "192.0.2.42",
	}, Directory: "public_html", Name: "index.html"})
	if err == nil || agent.writeCalls != 0 || repository.auditCalls != 1 {
		t.Fatalf("error=%v agent calls=%d audit calls=%d", err, agent.writeCalls, repository.auditCalls)
	}
}

func TestCopyAndTrashMutationsUseDerivedIdentityAndDurableAudit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		auditAction string
		invoke      func(*Service) error
		wantAction  agentprotocol.FileWriteAction
	}{
		{name: "copy", auditAction: "file.copy.authorized", wantAction: agentprotocol.FileWriteCopy,
			invoke: func(service *Service) error {
				_, err := service.MutateNode(t.Context(), NodeMutationParams{MutationContext: MutationContext{
					Subject: validFileSubject(), AccountID: fileTestAccountID, RequestID: "copy-node", SourceAddress: "192.0.2.42",
				}, Action: agentprotocol.FileWriteCopy, SourceDirectory: "public_html", SourceName: "index.html",
					DestinationDirectory: "public_html/assets", DestinationName: "index.html"})
				return err
			}},
		{name: "trash", auditAction: "file.trash.authorized", wantAction: agentprotocol.FileWriteTrash,
			invoke: func(service *Service) error {
				_, err := service.TrashNode(t.Context(), TrashNodeParams{MutationContext: MutationContext{
					Subject: validFileSubject(), AccountID: fileTestAccountID, RequestID: "trash-node", SourceAddress: "192.0.2.42",
				}, Directory: "public_html", Name: "index.html"})
				return err
			}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := validFileRepository(t)
			agent := &fileAgentStub{}
			service, _ := New(repository, agent)
			if err := test.invoke(service); err != nil {
				t.Fatal(err)
			}
			if repository.auditCalls != 1 || repository.auditParams.Action != test.auditAction || agent.writeCalls != 1 ||
				agent.writeRequest.Action != test.wantAction || agent.writeRequest.Identity.AccountID != string(fileTestAccountID) ||
				agent.writeRequest.Correlation == nil {
				t.Fatalf("audit=%#v request=%#v", repository.auditParams, agent.writeRequest)
			}
			if test.wantAction == agentprotocol.FileWriteCopy && agent.writeRequest.OperationID == "" {
				t.Fatal("copy lacks opaque staging operation id")
			}
			if test.wantAction == agentprotocol.FileWriteTrash && agent.writeRequest.TrashID == "" {
				t.Fatal("trash lacks opaque recovery id")
			}
		})
	}
}

func TestArchiveMutationsUseOpaqueStagingAndDurableAudit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, auditAction string
		action            agentprotocol.FileWriteAction
		sourceName        string
		destinationName   string
	}{
		{"create", "file.archive.create.authorized", agentprotocol.FileWriteArchiveCreate, "assets", "assets.zip"},
		{"extract", "file.archive.extract.authorized", agentprotocol.FileWriteArchiveExtract, "assets.zip", "restored-assets"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := validFileRepository(t)
			agent := &fileAgentStub{}
			service, _ := New(repository, agent)
			_, err := service.MutateArchive(t.Context(), ArchiveMutationParams{MutationContext: MutationContext{
				Subject: validFileSubject(), AccountID: fileTestAccountID, RequestID: "archive-" + test.name,
				SourceAddress: "192.0.2.42",
			}, Action: test.action, Format: agentprotocol.FileArchiveZIP, SourceDirectory: "public_html",
				SourceName: test.sourceName, DestinationDirectory: "public_html", DestinationName: test.destinationName})
			if err != nil || repository.auditCalls != 1 || repository.auditParams.Action != test.auditAction ||
				agent.writeCalls != 1 || agent.writeRequest.Action != test.action || agent.writeRequest.OperationID == "" ||
				agent.writeRequest.ArchiveFormat != agentprotocol.FileArchiveZIP || agent.writeRequest.Correlation == nil {
				t.Fatalf("error=%v audit=%#v request=%#v", err, repository.auditParams, agent.writeRequest)
			}
		})
	}
}

func TestTrashListingIsReadOnlyButRequiresFileManagementPermission(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	agent := &fileAgentStub{writeResult: agentprotocol.FileWriteResult{TrashEntries: []agentprotocol.FileTrashEntry{}}}
	service, _ := New(repository, agent)
	result, err := service.ListTrash(t.Context(), TrashListParams{Subject: validFileSubject(), AccountID: fileTestAccountID})
	if err != nil || result.TrashEntries == nil || repository.action != core.AuthorizationAccountFilesManage ||
		repository.auditCalls != 0 || agent.writeRequest.Action != agentprotocol.FileWriteTrashList ||
		agent.writeRequest.Correlation != nil {
		t.Fatalf("result=%#v err=%v action=%s audit=%d request=%#v",
			result, err, repository.action, repository.auditCalls, agent.writeRequest)
	}
}

func TestFileMutationMapsQuotaExhaustion(t *testing.T) {
	t.Parallel()
	repository := validFileRepository(t)
	agent := &fileAgentStub{writeErr: &agentclient.RemoteError{StatusCode: 507, Code: agentprotocol.ErrorFileQuotaExceeded}}
	service, _ := New(repository, agent)
	_, err := service.MutateNode(t.Context(), NodeMutationParams{MutationContext: MutationContext{
		Subject: validFileSubject(), AccountID: fileTestAccountID, RequestID: "copy-quota", SourceAddress: "192.0.2.42",
	}, Action: agentprotocol.FileWriteCopy, SourceDirectory: "public_html", SourceName: "a",
		DestinationDirectory: "public_html/assets", DestinationName: "a"})
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("quota error = %v", err)
	}
}

func validFileSubject() core.AuthorizationSubject {
	return (core.AuthenticatedSession{Identity: core.Identity{ID: "019c1234-5678-7abc-8def-0123456789b0"},
		Session: core.Session{ID: "019c1234-5678-7abc-8def-0123456789b1"}}).AuthorizationSubject()
}

func validFileRepository(t *testing.T) *fileRepositoryStub {
	t.Helper()
	username, err := hostingidentity.UsernameForAccount(string(fileTestAccountID))
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(string(fileTestAccountID))
	if err != nil {
		t.Fatal(err)
	}
	return &fileRepositoryStub{ready: true, account: core.HostingAccount{
		ID: fileTestAccountID,
		UnixIdentity: core.HostingUnixIdentity{AccountID: fileTestAccountID, Username: username,
			UID: hostingidentity.MinimumID, GID: hostingidentity.MinimumID, HomeDirectory: home},
	}}
}
