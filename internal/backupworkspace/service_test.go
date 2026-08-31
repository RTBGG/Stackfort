// SPDX-License-Identifier: AGPL-3.0-or-later

package backupworkspace

import (
	"context"
	"io"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const backupTestAccountID = core.ID("019c1234-5678-7abc-8def-0123456789ad")

type backupRepositoryStub struct {
	action       core.AuthorizationAction
	ready        bool
	account      core.HostingAccount
	audit        core.AppendAuditEventParams
	authorizeErr error
}

func (stub *backupRepositoryStub) Authorize(_ context.Context, params core.AuthorizeParams) (core.AuthorizationDecision, error) {
	stub.action = params.Action
	return core.AuthorizationDecision{}, stub.authorizeErr
}
func (stub *backupRepositoryStub) GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error) {
	return stub.account, nil
}
func (stub *backupRepositoryStub) HostingAccountHostReady(context.Context, core.ID) (bool, error) {
	return stub.ready, nil
}
func (stub *backupRepositoryStub) AppendAuditEvent(_ context.Context, params core.AppendAuditEventParams) (core.AuditEvent, error) {
	stub.audit = params
	return core.AuditEvent{ID: "019c1234-5678-7abc-8def-0123456789ae"}, nil
}
func (stub *backupRepositoryStub) CurrentPackageAssignment(context.Context, core.ID) (core.PackageAssignment, error) {
	return core.PackageAssignment{}, nil
}

type backupAgentStub struct {
	request agentprotocol.FileWriteRequest
}

func (stub *backupAgentStub) WriteHostingFile(_ context.Context, request agentprotocol.FileWriteRequest, _ io.Reader) (agentprotocol.FileWriteResult, error) {
	stub.request = request
	return agentprotocol.FileWriteResult{}, nil
}
func (stub *backupAgentStub) DownloadHostingBackup(context.Context, agentprotocol.BackupDownloadRequest) (agentclient.FileDownload, error) {
	return agentclient.FileDownload{}, nil
}

func TestCreateAndRestoreUseSeparateAuthorizationAndDurableAudit(t *testing.T) {
	t.Parallel()
	repository := validBackupRepository(t)
	agent := &backupAgentStub{}
	service, _ := New(repository, agent)
	mutation := MutationContext{Subject: validBackupSubject(), AccountID: backupTestAccountID,
		RequestID: "backup-browser-request", SourceAddress: "192.0.2.10"}
	if _, err := service.Create(t.Context(), CreateParams{MutationContext: mutation,
		Scope: agentprotocol.BackupScopeDocumentRoot, SourcePath: "public_html"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repository.action != core.AuthorizationAccountBackupsManage || repository.audit.Action != "backup.create.authorized" ||
		agent.request.BackupID == "" || agent.request.BackupPath != "public_html" || agent.request.Correlation == nil {
		t.Fatalf("create action=%s audit=%#v request=%#v", repository.action, repository.audit, agent.request)
	}
	backupID := agent.request.BackupID
	if _, err := service.Restore(t.Context(), RestoreParams{MutationContext: mutation,
		BackupID: backupID, Confirmation: backupID}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if repository.action != core.AuthorizationAccountBackupsRestore || repository.audit.Action != "backup.restore.authorized" ||
		agent.request.OperationID == "" || agent.request.BackupID != backupID {
		t.Fatalf("restore action=%s audit=%#v request=%#v", repository.action, repository.audit, agent.request)
	}
}

func validBackupSubject() core.AuthorizationSubject {
	return (core.AuthenticatedSession{Identity: core.Identity{ID: "019c1234-5678-7abc-8def-0123456789b0"},
		Session: core.Session{ID: "019c1234-5678-7abc-8def-0123456789b1"}}).AuthorizationSubject()
}

func TestRestoreRejectsMismatchedConfirmationBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := validBackupRepository(t)
	service, _ := New(repository, &backupAgentStub{})
	_, err := service.Restore(t.Context(), RestoreParams{MutationContext: MutationContext{
		AccountID: backupTestAccountID, RequestID: "backup-restore"},
		BackupID: "019c1234-5678-7abc-8def-0123456789b0", Confirmation: "wrong"})
	if err == nil || repository.action != "" {
		t.Fatalf("error=%v authorization=%s", err, repository.action)
	}
}

func TestTransferAndDeletionUsePackageQuotaAndSeparateAuthorization(t *testing.T) {
	t.Parallel()
	repository := validBackupRepository(t)
	agent := &backupAgentStub{}
	service, _ := New(repository, agent)
	mutation := MutationContext{Subject: validBackupSubject(), AccountID: backupTestAccountID,
		RequestID: "backup-import", SourceAddress: "192.0.2.10"}
	result, err := service.InitiateUpload(t.Context(), InitiateUploadParams{MutationContext: mutation,
		Scope: agentprotocol.BackupScopeDocumentRoot, SourcePath: "public_html", SizeBytes: 128})
	if err != nil {
		t.Fatalf("InitiateUpload: %v result=%#v", err, result)
	}
	uploadID := agent.request.UploadID
	if repository.action != core.AuthorizationAccountBackupsManage || uploadID == "" ||
		agent.request.BackupLimitBytes != agentprotocol.DefaultBackupRepositoryBytes || agent.request.Correlation == nil {
		t.Fatalf("action=%s request=%#v", repository.action, agent.request)
	}
	if _, err := service.UploadStatus(t.Context(), UploadParams{Subject: validBackupSubject(),
		AccountID: backupTestAccountID, UploadID: uploadID}); err != nil || agent.request.Correlation != nil {
		t.Fatalf("UploadStatus error=%v request=%#v", err, agent.request)
	}
	if _, err := service.Delete(t.Context(), DeleteParams{MutationContext: mutation,
		BackupID: uploadID, Confirmation: uploadID}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repository.action != core.AuthorizationAccountBackupsDelete || repository.audit.Action != "backup.delete.authorized" ||
		agent.request.BackupID != uploadID || agent.request.Correlation == nil {
		t.Fatalf("delete action=%s audit=%#v request=%#v", repository.action, repository.audit, agent.request)
	}
}

func validBackupRepository(t *testing.T) *backupRepositoryStub {
	t.Helper()
	username, _ := hostingidentity.UsernameForAccount(string(backupTestAccountID))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(backupTestAccountID))
	return &backupRepositoryStub{ready: true, account: core.HostingAccount{ID: backupTestAccountID,
		UnixIdentity: core.HostingUnixIdentity{AccountID: backupTestAccountID, Username: username,
			UID: hostingidentity.MinimumID, GID: hostingidentity.MinimumID, HomeDirectory: home}}}
}
