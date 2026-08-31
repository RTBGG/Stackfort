// SPDX-License-Identifier: AGPL-3.0-or-later

package backupworkspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"path"
	"regexp"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/google/uuid"
)

var (
	ErrNotReady        = errors.New("hosting account backup workspace is not ready")
	ErrNotFound        = errors.New("local backup was not found")
	ErrConflict        = errors.New("local backup conflicts with host state")
	ErrUnavailable     = errors.New("local backup service is unavailable")
	ErrTooLarge        = errors.New("local backup exceeds supported limits")
	ErrBusy            = errors.New("local backup capacity is exhausted")
	ErrIntegrity       = errors.New("local backup integrity verification failed")
	ErrQuota           = errors.New("hosting account quota is exhausted during restore")
	ErrRepositoryQuota = errors.New("backup repository quota is exhausted")
	requestIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	sha256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	AppendAuditEvent(context.Context, core.AppendAuditEventParams) (core.AuditEvent, error)
	CurrentPackageAssignment(context.Context, core.ID) (core.PackageAssignment, error)
}

type Agent interface {
	WriteHostingFile(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error)
	DownloadHostingBackup(context.Context, agentprotocol.BackupDownloadRequest) (agentclient.FileDownload, error)
}

type Service struct {
	repository Repository
	agent      Agent
}

type ListParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	Cursor    string
}

type LookupParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	BackupID  string
}

type MutationContext struct {
	Subject       core.AuthorizationSubject
	AccountID     core.ID
	RequestID     string
	SourceAddress string
}

type CreateParams struct {
	MutationContext
	Scope      agentprotocol.BackupScope
	SourcePath string
}

type RestoreParams struct {
	MutationContext
	BackupID     string
	Confirmation string
}

type DownloadParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	BackupID  string
	Range     *agentprotocol.FileDownloadRange
}

type Download struct {
	Name       string
	TotalSize  uint64
	Offset     uint64
	Length     uint64
	ModifiedAt time.Time
	Partial    bool
	Body       io.ReadCloser
}

type RangeError struct{ TotalSize uint64 }

func (err *RangeError) Error() string { return "backup download range is not satisfiable" }

type InitiateUploadParams struct {
	MutationContext
	Scope          agentprotocol.BackupScope
	SourcePath     string
	SizeBytes      uint64
	ExpectedSHA256 string
}

type UploadParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	UploadID  string
}

type UploadChunkParams struct {
	UploadParams
	Offset uint64
	Length uint64
	Body   io.Reader
}

type CompleteUploadParams struct {
	MutationContext
	UploadID       string
	Scope          agentprotocol.BackupScope
	SourcePath     string
	SizeBytes      uint64
	ExpectedSHA256 string
}

type CancelUploadParams struct {
	MutationContext
	UploadID string
}

type DeleteParams struct {
	MutationContext
	BackupID     string
	Confirmation string
}

func New(repository Repository, agent Agent) (*Service, error) {
	if repository == nil || agent == nil {
		return nil, errors.New("backup workspace requires repository and agent")
	}
	return &Service{repository: repository, agent: agent}, nil
}

func (service *Service) List(ctx context.Context, params ListParams) (agentprotocol.FileWriteResult, error) {
	if params.Cursor != "" && !canonicalUUIDv7(params.Cursor) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	identity, limit, err := service.prepare(ctx, params.Subject, params.AccountID, core.AuthorizationAccountBackupsView)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.call(ctx, agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupList,
		Identity: identity, Cursor: params.Cursor, BackupLimitBytes: limit}, nil)
}

func (service *Service) Inspect(ctx context.Context, params LookupParams) (agentprotocol.FileWriteResult, error) {
	return service.lookup(ctx, params, agentprotocol.FileWriteBackupInspect)
}

func (service *Service) Verify(ctx context.Context, params LookupParams) (agentprotocol.FileWriteResult, error) {
	return service.lookup(ctx, params, agentprotocol.FileWriteBackupVerify)
}

func (service *Service) lookup(
	ctx context.Context, params LookupParams, action agentprotocol.FileWriteAction,
) (agentprotocol.FileWriteResult, error) {
	if !canonicalUUIDv7(params.BackupID) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	identity, limit, err := service.prepare(ctx, params.Subject, params.AccountID, core.AuthorizationAccountBackupsView)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.call(ctx, agentprotocol.FileWriteRequest{Action: action, Identity: identity,
		BackupID: params.BackupID, BackupLimitBytes: limit}, nil)
}

func (service *Service) Create(
	ctx context.Context, params CreateParams,
) (agentprotocol.FileWriteResult, error) {
	if !agentprotocol.ValidBackupScopePath(params.Scope, params.SourcePath) || !validMutationContext(params.MutationContext) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	backupID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	request := agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupCreate,
		BackupID: backupID.String(), BackupScope: params.Scope, BackupPath: params.SourcePath}
	return service.mutate(ctx, params.MutationContext, core.AuthorizationAccountBackupsManage,
		"backup.create", request, map[string]any{"scope": params.Scope, "sourcePath": params.SourcePath})
}

func (service *Service) Restore(
	ctx context.Context, params RestoreParams,
) (agentprotocol.FileWriteResult, error) {
	if !canonicalUUIDv7(params.BackupID) || params.Confirmation != params.BackupID ||
		!validMutationContext(params.MutationContext) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	operationID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	request := agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupRestore,
		BackupID: params.BackupID, OperationID: operationID.String()}
	return service.mutate(ctx, params.MutationContext, core.AuthorizationAccountBackupsRestore,
		"backup.restore", request, map[string]any{"backupId": params.BackupID, "operationId": operationID.String()})
}

func (service *Service) Download(ctx context.Context, params DownloadParams) (Download, error) {
	if !canonicalUUIDv7(params.BackupID) || agentprotocol.ValidateFileDownloadRange(params.Range) != nil {
		return Download{}, core.ErrInvalidInput
	}
	identity, _, err := service.prepare(ctx, params.Subject, params.AccountID, core.AuthorizationAccountBackupsView)
	if err != nil {
		return Download{}, err
	}
	stream, err := service.agent.DownloadHostingBackup(ctx, agentprotocol.BackupDownloadRequest{
		Identity: identity, BackupID: params.BackupID, Range: params.Range,
	})
	if err != nil {
		var remote *agentclient.RemoteError
		if errors.As(err, &remote) {
			switch remote.Code {
			case agentprotocol.ErrorBackupNotFound:
				return Download{}, ErrNotFound
			case agentprotocol.ErrorBackupConflict:
				return Download{}, ErrConflict
			case agentprotocol.ErrorBackupIntegrity:
				return Download{}, ErrIntegrity
			case agentprotocol.ErrorBackupTooLarge:
				return Download{}, ErrTooLarge
			case agentprotocol.ErrorBackupBusy:
				return Download{}, ErrBusy
			case agentprotocol.ErrorFileRangeNotSatisfiable:
				if remote.TotalSize != nil {
					return Download{}, &RangeError{TotalSize: *remote.TotalSize}
				}
			}
		}
		return Download{}, ErrUnavailable
	}
	return Download{Name: path.Base("stackfort-backup-" + params.BackupID + ".tar.gz"), TotalSize: stream.TotalSize,
		Offset: stream.Offset, Length: stream.Length, ModifiedAt: stream.ModifiedAt, Partial: stream.Partial, Body: stream.Body}, nil
}

func (service *Service) InitiateUpload(ctx context.Context, params InitiateUploadParams) (agentprotocol.FileWriteResult, error) {
	if !validMutationContext(params.MutationContext) || !agentprotocol.ValidBackupScopePath(params.Scope, params.SourcePath) ||
		params.SizeBytes == 0 || params.SizeBytes > agentprotocol.MaximumFileUploadBytes ||
		(params.ExpectedSHA256 != "" && !validSHA256(params.ExpectedSHA256)) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	id, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	request := agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupUploadInitiate, UploadID: id.String(),
		BackupScope: params.Scope, BackupPath: params.SourcePath, SizeBytes: params.SizeBytes, ExpectedSHA256: params.ExpectedSHA256}
	return service.mutate(ctx, params.MutationContext, core.AuthorizationAccountBackupsManage, "backup.upload.initiate", request,
		map[string]any{"scope": params.Scope, "sourcePath": params.SourcePath, "sizeBytes": params.SizeBytes})
}

func (service *Service) UploadStatus(ctx context.Context, params UploadParams) (agentprotocol.FileWriteResult, error) {
	if !canonicalUUIDv7(params.UploadID) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	identity, limit, err := service.prepare(ctx, params.Subject, params.AccountID, core.AuthorizationAccountBackupsManage)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.call(ctx, agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupUploadStatus,
		Identity: identity, UploadID: params.UploadID, BackupLimitBytes: limit}, nil)
}

func (service *Service) WriteUploadChunk(ctx context.Context, params UploadChunkParams) (agentprotocol.FileWriteResult, error) {
	if !canonicalUUIDv7(params.UploadID) || params.Body == nil || params.Length == 0 ||
		params.Length > agentprotocol.MaximumFileUploadChunkBytes || params.Offset > agentprotocol.MaximumFileUploadBytes-params.Length {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	identity, limit, err := service.prepare(ctx, params.Subject, params.AccountID, core.AuthorizationAccountBackupsManage)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.call(ctx, agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupUploadChunk,
		Identity: identity, UploadID: params.UploadID, Offset: params.Offset, ChunkLength: params.Length,
		BackupLimitBytes: limit}, params.Body)
}

func (service *Service) CompleteUpload(ctx context.Context, params CompleteUploadParams) (agentprotocol.FileWriteResult, error) {
	if !validMutationContext(params.MutationContext) || !canonicalUUIDv7(params.UploadID) ||
		!agentprotocol.ValidBackupScopePath(params.Scope, params.SourcePath) || params.SizeBytes == 0 ||
		params.SizeBytes > agentprotocol.MaximumFileUploadBytes ||
		(params.ExpectedSHA256 != "" && !validSHA256(params.ExpectedSHA256)) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	request := agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupUploadComplete, UploadID: params.UploadID,
		BackupScope: params.Scope, BackupPath: params.SourcePath, SizeBytes: params.SizeBytes, ExpectedSHA256: params.ExpectedSHA256}
	return service.mutate(ctx, params.MutationContext, core.AuthorizationAccountBackupsManage, "backup.upload.complete", request,
		map[string]any{"uploadId": params.UploadID})
}

func (service *Service) CancelUpload(ctx context.Context, params CancelUploadParams) (agentprotocol.FileWriteResult, error) {
	if !validMutationContext(params.MutationContext) || !canonicalUUIDv7(params.UploadID) {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	request := agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupUploadCancel, UploadID: params.UploadID}
	return service.mutate(ctx, params.MutationContext, core.AuthorizationAccountBackupsManage, "backup.upload.cancel", request,
		map[string]any{"uploadId": params.UploadID})
}

func (service *Service) Delete(ctx context.Context, params DeleteParams) (agentprotocol.FileWriteResult, error) {
	if !validMutationContext(params.MutationContext) || !canonicalUUIDv7(params.BackupID) || params.Confirmation != params.BackupID {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	request := agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteBackupDelete, BackupID: params.BackupID}
	return service.mutate(ctx, params.MutationContext, core.AuthorizationAccountBackupsDelete, "backup.delete", request,
		map[string]any{"backupId": params.BackupID})
}

func (service *Service) mutate(
	ctx context.Context, mutation MutationContext, authorization core.AuthorizationAction, auditAction string,
	request agentprotocol.FileWriteRequest, details map[string]any,
) (agentprotocol.FileWriteResult, error) {
	identity, limit, err := service.prepare(ctx, mutation.Subject, mutation.AccountID, authorization)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	actorID, sessionID, accountID := mutation.Subject.IdentityID(), mutation.Subject.SessionID(), mutation.AccountID
	event, err := service.repository.AppendAuditEvent(ctx, core.AppendAuditEventParams{
		ActorID: &actorID, SessionID: &sessionID, SourceAddress: mutation.SourceAddress,
		Action: auditAction + ".authorized", TargetType: "account_backup", TargetID: backupTargetID(request),
		AccountID: &accountID, RequestID: mutation.RequestID, Result: core.AuditSuccess, Details: details,
	})
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	request.Identity = identity
	request.BackupLimitBytes = limit
	request.Correlation = &agentprotocol.FileAuditCorrelation{AuditEventID: string(event.ID), ActorID: string(actorID),
		SessionID: string(sessionID), AccountID: string(accountID), RequestID: mutation.RequestID}
	return service.call(ctx, request, nil)
}

func (service *Service) prepare(
	ctx context.Context, subject core.AuthorizationSubject, accountID core.ID, action core.AuthorizationAction,
) (hostingidentity.Spec, uint64, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return hostingidentity.Spec{}, 0, ErrUnavailable
	}
	if _, err := core.ParseID(string(accountID)); err != nil {
		return hostingidentity.Spec{}, 0, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{Subject: subject,
		Action: action, AccountID: &accountID}); err != nil {
		return hostingidentity.Spec{}, 0, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, accountID)
	if err != nil {
		return hostingidentity.Spec{}, 0, err
	}
	if !ready {
		return hostingidentity.Spec{}, 0, ErrNotReady
	}
	account, err := service.repository.GetHostingAccount(ctx, accountID)
	if err != nil {
		return hostingidentity.Spec{}, 0, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return hostingidentity.Spec{}, 0, ErrUnavailable
	}
	assignment, err := service.repository.CurrentPackageAssignment(ctx, accountID)
	if err != nil {
		return hostingidentity.Spec{}, 0, err
	}
	limit := agentprotocol.DefaultBackupRepositoryBytes
	if assignment.EffectiveLimits.BackupStorageBytes != nil {
		value := *assignment.EffectiveLimits.BackupStorageBytes
		if value < 1<<20 || uint64(value) > agentprotocol.MaximumBackupRepositoryBytes {
			return hostingidentity.Spec{}, 0, ErrUnavailable
		}
		limit = uint64(value)
	}
	return identity, limit, nil
}

func (service *Service) call(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	request.ProtocolVersion, request.RequestID = agentprotocol.WireVersion, "backup-validation"
	if agentprotocol.ValidateFileWriteRequest(request) != nil {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	if body == nil {
		body = bytes.NewReader(nil)
	}
	result, err := service.agent.WriteHostingFile(ctx, request, body)
	if err == nil {
		return result, nil
	}
	var remote *agentclient.RemoteError
	if errors.As(err, &remote) {
		switch remote.Code {
		case agentprotocol.ErrorInvalidRequest:
			return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
		case agentprotocol.ErrorBackupNotFound:
			return agentprotocol.FileWriteResult{}, ErrNotFound
		case agentprotocol.ErrorBackupConflict:
			return agentprotocol.FileWriteResult{}, ErrConflict
		case agentprotocol.ErrorBackupTooLarge:
			return agentprotocol.FileWriteResult{}, ErrTooLarge
		case agentprotocol.ErrorBackupBusy:
			return agentprotocol.FileWriteResult{}, ErrBusy
		case agentprotocol.ErrorBackupIntegrity:
			return agentprotocol.FileWriteResult{}, ErrIntegrity
		case agentprotocol.ErrorFileQuotaExceeded:
			return agentprotocol.FileWriteResult{}, ErrQuota
		case agentprotocol.ErrorBackupQuotaExceeded:
			return agentprotocol.FileWriteResult{}, ErrRepositoryQuota
		}
	}
	return agentprotocol.FileWriteResult{}, ErrUnavailable
}

func backupTargetID(request agentprotocol.FileWriteRequest) string {
	if request.BackupID != "" {
		return request.BackupID
	}
	return request.UploadID
}

func validSHA256(value string) bool { return sha256Pattern.MatchString(value) }

func validMutationContext(mutation MutationContext) bool {
	if !requestIDPattern.MatchString(mutation.RequestID) {
		return false
	}
	if mutation.SourceAddress != "" {
		address, err := netip.ParseAddr(mutation.SourceAddress)
		return err == nil && address.String() == mutation.SourceAddress
	}
	return true
}

func canonicalUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}
