// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fileworkspace couples account authorization and readiness to the
// typed, metadata-only host file browser.
package fileworkspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/google/uuid"
)

var (
	ErrNotReady    = errors.New("hosting account file tree is not ready")
	ErrNotFound    = errors.New("hosting account directory was not found")
	ErrConflict    = errors.New("hosting account file tree conflicts with host state")
	ErrUnavailable = errors.New("hosting account files are unavailable")
	ErrTooLarge    = errors.New("hosting account file download is too large")
	ErrRange       = errors.New("hosting account file range is not satisfiable")
	ErrBusy        = errors.New("hosting account file download capacity is exhausted")
	ErrQuota       = errors.New("hosting account storage quota is exhausted")
)

var fileMutationRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	HostingAccountHostReady(context.Context, core.ID) (bool, error)
	AppendAuditEvent(context.Context, core.AppendAuditEventParams) (core.AuditEvent, error)
}

type Agent interface {
	ListHostingFiles(context.Context, string, agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error)
	DownloadHostingFile(context.Context, agentprotocol.FileDownloadRequest) (agentclient.FileDownload, error)
	WriteHostingFile(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error)
}

type ListParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	Path      string
	Cursor    string
}

type DownloadParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	Path      string
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

func (err *RangeError) Error() string { return ErrRange.Error() }
func (err *RangeError) Unwrap() error { return ErrRange }

type MutationContext struct {
	Subject       core.AuthorizationSubject
	AccountID     core.ID
	RequestID     string
	SourceAddress string
}

type InitiateUploadParams struct {
	MutationContext
	Directory      string
	Name           string
	SizeBytes      uint64
	ExpectedSHA256 string
}

type UploadParams struct {
	MutationContext
	UploadID string
}

type UploadChunkParams struct {
	Subject     core.AuthorizationSubject
	AccountID   core.ID
	UploadID    string
	Offset      uint64
	ChunkLength uint64
	Body        io.Reader
}

type CompleteUploadParams struct {
	UploadParams
	Directory      string
	Name           string
	SizeBytes      uint64
	ExpectedSHA256 string
}

type CreateNodeParams struct {
	MutationContext
	Directory     string
	Name          string
	DirectoryNode bool
}

type NodeMutationParams struct {
	MutationContext
	Action               agentprotocol.FileWriteAction
	SourceDirectory      string
	SourceName           string
	DestinationDirectory string
	DestinationName      string
}

type ArchiveMutationParams struct {
	MutationContext
	Action               agentprotocol.FileWriteAction
	Format               agentprotocol.FileArchiveFormat
	SourceDirectory      string
	SourceName           string
	DestinationDirectory string
	DestinationName      string
}

type TrashNodeParams struct {
	MutationContext
	Directory string
	Name      string
}

type TrashListParams struct {
	Subject   core.AuthorizationSubject
	AccountID core.ID
	Cursor    string
}

type TrashParams struct {
	MutationContext
	TrashID string
}

type Service struct {
	repository Repository
	agent      Agent
}

func New(repository Repository, agent Agent) (*Service, error) {
	if repository == nil || agent == nil {
		return nil, fmt.Errorf("file workspace requires repository and agent")
	}
	return &Service{repository: repository, agent: agent}, nil
}

func (service *Service) List(ctx context.Context, params ListParams) (agentprotocol.FileListResponse, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return agentprotocol.FileListResponse{}, ErrUnavailable
	}
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return agentprotocol.FileListResponse{}, core.ErrInvalidInput
	}
	path, err := hostingpath.NormalizeFileManagerDirectory(params.Path)
	if err != nil || path != params.Path || !validCursor(params.Cursor) {
		return agentprotocol.FileListResponse{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountFilesView, AccountID: &params.AccountID,
	}); err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, params.AccountID)
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	if !ready {
		return agentprotocol.FileListResponse{}, ErrNotReady
	}
	account, err := service.repository.GetHostingAccount(ctx, params.AccountID)
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return agentprotocol.FileListResponse{}, ErrUnavailable
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileListResponse{}, ErrUnavailable
	}
	listing, err := service.agent.ListHostingFiles(ctx, "file-list-"+requestID.String(), agentprotocol.FileListRequest{
		Identity: identity, Path: path, Cursor: params.Cursor, Limit: agentprotocol.MaximumFileListingEntries,
	})
	if err != nil {
		var remote *agentclient.RemoteError
		if errors.As(err, &remote) {
			switch remote.Code {
			case agentprotocol.ErrorFileNotFound:
				return agentprotocol.FileListResponse{}, ErrNotFound
			case agentprotocol.ErrorFileConflict:
				return agentprotocol.FileListResponse{}, ErrConflict
			}
		}
		return agentprotocol.FileListResponse{}, ErrUnavailable
	}
	if listing.Path != path {
		return agentprotocol.FileListResponse{}, ErrUnavailable
	}
	return listing, nil
}

func (service *Service) Download(ctx context.Context, params DownloadParams) (Download, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return Download{}, ErrUnavailable
	}
	if _, err := core.ParseID(string(params.AccountID)); err != nil {
		return Download{}, core.ErrInvalidInput
	}
	normalized, err := hostingpath.NormalizeFileManagerFile(params.Path)
	if err != nil || normalized != params.Path || agentprotocol.ValidateFileDownloadRange(params.Range) != nil {
		return Download{}, core.ErrInvalidInput
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{
		Subject: params.Subject, Action: core.AuthorizationAccountFilesView, AccountID: &params.AccountID,
	}); err != nil {
		return Download{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, params.AccountID)
	if err != nil {
		return Download{}, err
	}
	if !ready {
		return Download{}, ErrNotReady
	}
	account, err := service.repository.GetHostingAccount(ctx, params.AccountID)
	if err != nil {
		return Download{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return Download{}, ErrUnavailable
	}
	request := agentprotocol.FileDownloadRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "validation", Identity: identity, Path: normalized, Range: params.Range}
	if err := agentprotocol.ValidateFileDownloadRequest(request); err != nil {
		return Download{}, core.ErrInvalidInput
	}
	stream, err := service.agent.DownloadHostingFile(ctx, agentprotocol.FileDownloadRequest{
		Identity: identity, Path: normalized, Range: params.Range,
	})
	if err != nil {
		var remote *agentclient.RemoteError
		if errors.As(err, &remote) {
			switch remote.Code {
			case agentprotocol.ErrorFileNotFound:
				return Download{}, ErrNotFound
			case agentprotocol.ErrorFileConflict:
				return Download{}, ErrConflict
			case agentprotocol.ErrorFileDownloadTooLarge:
				return Download{}, ErrTooLarge
			case agentprotocol.ErrorFileRangeNotSatisfiable:
				if remote.TotalSize != nil {
					return Download{}, &RangeError{TotalSize: *remote.TotalSize}
				}
				return Download{}, ErrUnavailable
			case agentprotocol.ErrorFileDownloadBusy:
				return Download{}, ErrBusy
			}
		}
		return Download{}, ErrUnavailable
	}
	return Download{Name: path.Base(normalized), TotalSize: stream.TotalSize, Offset: stream.Offset,
		Length: stream.Length, ModifiedAt: stream.ModifiedAt, Partial: stream.Partial, Body: stream.Body}, nil
}

func (service *Service) InitiateUpload(
	ctx context.Context, params InitiateUploadParams,
) (agentprotocol.FileWriteResult, error) {
	uploadID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: agentprotocol.FileWriteInitiate, UploadID: uploadID.String(), Directory: params.Directory,
		Name: params.Name, SizeBytes: params.SizeBytes, ExpectedSHA256: params.ExpectedSHA256}
	return service.mutate(ctx, params.MutationContext, "file.upload.initiate", request, nil)
}

func (service *Service) UploadStatus(
	ctx context.Context, params UploadParams,
) (agentprotocol.FileWriteResult, error) {
	identity, err := service.prepareFileWrite(ctx, params.MutationContext, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.callWrite(ctx, agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteStatus,
		Identity: identity, UploadID: params.UploadID}, nil)
}

func (service *Service) WriteUploadChunk(
	ctx context.Context, params UploadChunkParams,
) (agentprotocol.FileWriteResult, error) {
	if params.Body == nil {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	identity, err := service.prepareFileWrite(ctx, MutationContext{
		Subject: params.Subject, AccountID: params.AccountID, RequestID: "chunk-validation",
	}, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.callWrite(ctx, agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteChunk,
		Identity: identity, UploadID: params.UploadID, Offset: params.Offset, ChunkLength: params.ChunkLength}, params.Body)
}

func (service *Service) CompleteUpload(
	ctx context.Context, params CompleteUploadParams,
) (agentprotocol.FileWriteResult, error) {
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: agentprotocol.FileWriteComplete, UploadID: params.UploadID, Directory: params.Directory,
		Name: params.Name, SizeBytes: params.SizeBytes, ExpectedSHA256: params.ExpectedSHA256}
	return service.mutate(ctx, params.MutationContext, "file.upload.complete", request, nil)
}

func (service *Service) CancelUpload(
	ctx context.Context, params UploadParams,
) (agentprotocol.FileWriteResult, error) {
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: agentprotocol.FileWriteCancel, UploadID: params.UploadID}
	return service.mutate(ctx, params.MutationContext, "file.upload.cancel", request, nil)
}

func (service *Service) CreateNode(
	ctx context.Context, params CreateNodeParams,
) (agentprotocol.FileWriteResult, error) {
	action, auditAction := agentprotocol.FileWriteCreateFile, "file.create"
	if params.DirectoryNode {
		action, auditAction = agentprotocol.FileWriteCreateDirectory, "directory.create"
	}
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: action, Directory: params.Directory, Name: params.Name}
	return service.mutate(ctx, params.MutationContext, auditAction, request, nil)
}

func (service *Service) MutateNode(
	ctx context.Context, params NodeMutationParams,
) (agentprotocol.FileWriteResult, error) {
	auditAction := ""
	switch params.Action {
	case agentprotocol.FileWriteRename:
		auditAction = "file.rename"
	case agentprotocol.FileWriteMove:
		auditAction = "file.move"
	case agentprotocol.FileWriteCopy:
		auditAction = "file.copy"
	default:
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: params.Action, SourceDirectory: params.SourceDirectory, SourceName: params.SourceName,
		Directory: params.DestinationDirectory, Name: params.DestinationName}
	if params.Action == agentprotocol.FileWriteCopy {
		operationID, err := uuid.NewV7()
		if err != nil {
			return agentprotocol.FileWriteResult{}, ErrUnavailable
		}
		request.OperationID = operationID.String()
	}
	return service.mutate(ctx, params.MutationContext, auditAction, request, nil)
}

func (service *Service) MutateArchive(
	ctx context.Context, params ArchiveMutationParams,
) (agentprotocol.FileWriteResult, error) {
	auditAction := ""
	switch params.Action {
	case agentprotocol.FileWriteArchiveCreate:
		auditAction = "file.archive.create"
	case agentprotocol.FileWriteArchiveExtract:
		auditAction = "file.archive.extract"
	default:
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	operationID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: params.Action, OperationID: operationID.String(), ArchiveFormat: params.Format,
		SourceDirectory: params.SourceDirectory, SourceName: params.SourceName,
		Directory: params.DestinationDirectory, Name: params.DestinationName}
	return service.mutate(ctx, params.MutationContext, auditAction, request, nil)
}

func (service *Service) TrashNode(
	ctx context.Context, params TrashNodeParams,
) (agentprotocol.FileWriteResult, error) {
	trashID, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: agentprotocol.FileWriteTrash, TrashID: trashID.String(),
		SourceDirectory: params.Directory, SourceName: params.Name}
	return service.mutate(ctx, params.MutationContext, "file.trash", request, nil)
}

func (service *Service) ListTrash(
	ctx context.Context, params TrashListParams,
) (agentprotocol.FileWriteResult, error) {
	identity, err := service.prepareFileWrite(ctx, MutationContext{Subject: params.Subject, AccountID: params.AccountID}, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return service.callWrite(ctx, agentprotocol.FileWriteRequest{Action: agentprotocol.FileWriteTrashList,
		Identity: identity, Cursor: params.Cursor}, nil)
}

func (service *Service) RestoreTrash(
	ctx context.Context, params TrashParams,
) (agentprotocol.FileWriteResult, error) {
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: agentprotocol.FileWriteTrashRestore, TrashID: params.TrashID}
	return service.mutate(ctx, params.MutationContext, "file.trash.restore", request, nil)
}

func (service *Service) PurgeTrash(
	ctx context.Context, params TrashParams,
) (agentprotocol.FileWriteResult, error) {
	request := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion, RequestID: "validation",
		Action: agentprotocol.FileWriteTrashPurge, TrashID: params.TrashID}
	return service.mutate(ctx, params.MutationContext, "file.trash.purge", request, nil)
}

func (service *Service) mutate(
	ctx context.Context, mutation MutationContext, action string,
	request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	identity, err := service.prepareFileWrite(ctx, mutation, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	request.Identity = identity
	request.Correlation = &agentprotocol.FileAuditCorrelation{
		AuditEventID: "019c1234-5678-7abc-8def-0123456789af",
		ActorID:      "019c1234-5678-7abc-8def-0123456789b0",
		SessionID:    "019c1234-5678-7abc-8def-0123456789b1",
		AccountID:    string(mutation.AccountID), RequestID: mutation.RequestID,
	}
	if err := validateWriteRequest(request); err != nil {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
	}
	actorID, sessionID, accountID := mutation.Subject.IdentityID(), mutation.Subject.SessionID(), mutation.AccountID
	targetPath := joinedFilePath(request.Directory, request.Name)
	sourcePath := joinedFilePath(request.SourceDirectory, request.SourceName)
	if targetPath == "" {
		targetPath = sourcePath
	}
	if targetPath == "" && request.TrashID != "" {
		targetPath = "trash/" + request.TrashID
	}
	details := map[string]any{"path": targetPath}
	if sourcePath != "" {
		details["sourcePath"] = sourcePath
	}
	if request.Directory != "" || request.Name != "" {
		details["destinationPath"] = joinedFilePath(request.Directory, request.Name)
	}
	if request.UploadID != "" {
		details["uploadId"] = request.UploadID
	}
	if request.SizeBytes != 0 {
		details["sizeBytes"] = request.SizeBytes
	}
	if request.OperationID != "" {
		details["operationId"] = request.OperationID
	}
	if request.TrashID != "" {
		details["trashId"] = request.TrashID
	}
	if request.ArchiveFormat != "" {
		details["archiveFormat"] = request.ArchiveFormat
	}
	event, err := service.repository.AppendAuditEvent(ctx, core.AppendAuditEventParams{
		ActorID: &actorID, SessionID: &sessionID, SourceAddress: mutation.SourceAddress,
		Action: action + ".authorized", TargetType: "account_file", TargetID: targetPath,
		AccountID: &accountID, RequestID: mutation.RequestID, Result: core.AuditSuccess, Details: details,
	})
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	request.Correlation = &agentprotocol.FileAuditCorrelation{AuditEventID: string(event.ID),
		ActorID: string(actorID), SessionID: string(sessionID), AccountID: string(accountID), RequestID: mutation.RequestID}
	return service.callWrite(ctx, request, body)
}

func (service *Service) prepareFileWrite(
	ctx context.Context, mutation MutationContext, requireAuditContext bool,
) (hostingidentity.Spec, error) {
	if service == nil || service.repository == nil || service.agent == nil {
		return hostingidentity.Spec{}, ErrUnavailable
	}
	if _, err := core.ParseID(string(mutation.AccountID)); err != nil {
		return hostingidentity.Spec{}, core.ErrInvalidInput
	}
	if requireAuditContext {
		if !fileMutationRequestIDPattern.MatchString(mutation.RequestID) {
			return hostingidentity.Spec{}, core.ErrInvalidInput
		}
		if mutation.SourceAddress != "" {
			address, err := netip.ParseAddr(mutation.SourceAddress)
			if err != nil || address.String() != mutation.SourceAddress {
				return hostingidentity.Spec{}, core.ErrInvalidInput
			}
		}
	}
	if _, err := service.repository.Authorize(ctx, core.AuthorizeParams{Subject: mutation.Subject,
		Action: core.AuthorizationAccountFilesManage, AccountID: &mutation.AccountID}); err != nil {
		return hostingidentity.Spec{}, err
	}
	ready, err := service.repository.HostingAccountHostReady(ctx, mutation.AccountID)
	if err != nil {
		return hostingidentity.Spec{}, err
	}
	if !ready {
		return hostingidentity.Spec{}, ErrNotReady
	}
	account, err := service.repository.GetHostingAccount(ctx, mutation.AccountID)
	if err != nil {
		return hostingidentity.Spec{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return hostingidentity.Spec{}, ErrUnavailable
	}
	return identity, nil
}

func (service *Service) callWrite(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if body == nil {
		body = strings.NewReader("")
	}
	if err := validateWriteRequest(request); err != nil {
		return agentprotocol.FileWriteResult{}, core.ErrInvalidInput
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
		case agentprotocol.ErrorFileNotFound:
			return agentprotocol.FileWriteResult{}, ErrNotFound
		case agentprotocol.ErrorFileConflict:
			return agentprotocol.FileWriteResult{}, ErrConflict
		case agentprotocol.ErrorFileDownloadTooLarge:
			return agentprotocol.FileWriteResult{}, ErrTooLarge
		case agentprotocol.ErrorFileDownloadBusy:
			return agentprotocol.FileWriteResult{}, ErrBusy
		case agentprotocol.ErrorFileQuotaExceeded:
			return agentprotocol.FileWriteResult{}, ErrQuota
		}
	}
	return agentprotocol.FileWriteResult{}, ErrUnavailable
}

func validateWriteRequest(request agentprotocol.FileWriteRequest) error {
	request.ProtocolVersion = agentprotocol.WireVersion
	request.RequestID = "file-write-validation"
	return agentprotocol.ValidateFileWriteRequest(request)
}

func joinedFilePath(directory, name string) string {
	if name == "" {
		return directory
	}
	if directory == "" {
		return name
	}
	return directory + "/" + name
}

func validCursor(value string) bool {
	if value == "" {
		return true
	}
	offset, err := strconv.ParseUint(value, 10, 63)
	return err == nil && offset > 0 && strconv.FormatUint(offset, 10) == value
}
