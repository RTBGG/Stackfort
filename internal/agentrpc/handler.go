// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentrpc implements the local HTTP transport and strictly typed
// agent protocol dispatcher. It deliberately has no generic command method.
package agentrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/hostacme"
	"github.com/RTBGG/stackfort/internal/hostcache"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostdatabase"
	"github.com/RTBGG/stackfort/internal/hostfiles"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostjobs"
	"github.com/RTBGG/stackfort/internal/hostlogs"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/hostphp"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/hosttls"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
)

const (
	idempotencyCacheEntries = 2_048
	idempotencyCacheTTL     = 15 * time.Minute
)

type Handler struct {
	logger          *slog.Logger
	cache           *responseCache
	now             func() time.Time
	capabilities    capabilityInspector
	identities      identityReconciler
	filesystems     filesystemReconciler
	files           fileBrowser
	logs            hostingLogReader
	cacheManager    cacheManager
	downloads       fileDownloader
	downloadSlots   chan struct{}
	writes          fileWriter
	backups         backupManager
	writeSlots      chan struct{}
	resources       resourceReconciler
	nginx           nginxReconciler
	nginxSites      nginxSiteActivator
	acmeHTTP01      acmeHTTP01Presenter
	tlsCertificates tlsCertificateStager
	phpPools        phpPoolReconciler
	databases       databaseReconciler
	jobs            scheduledJobReconciler
}

type capabilityInspector interface {
	Inspect(context.Context) (agentprotocol.CapabilityReport, error)
}

type identityReconciler interface {
	Reconcile(context.Context, hostingidentity.Spec) (hostidentity.ReconcileResult, error)
	Delete(context.Context, hostingidentity.Spec) (hostidentity.DeleteResult, error)
}

type filesystemReconciler interface {
	Reconcile(context.Context, hostingstorage.Spec) (hostfilesystem.ReconcileResult, error)
	EnsureDocumentRoot(
		context.Context, hostingidentity.Spec, string, agentprotocol.DocumentRootAccess,
	) (hostfilesystem.DocumentRootResult, error)
}

type fileBrowser interface {
	List(context.Context, agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error)
}

type hostingLogReader interface {
	Read(context.Context, agentprotocol.HostingLogReadRequest) (agentprotocol.HostingLogReadResponse, error)
	ReadWAFEvents(context.Context, agentprotocol.WAFEventReadRequest) (agentprotocol.WAFEventReadResponse, error)
}

type cacheManager interface {
	Metrics(context.Context, agentprotocol.CacheMetricsRequest) (agentprotocol.CacheMetricsResponse, error)
	Purge(context.Context, agentprotocol.CachePurgeRequest) (agentprotocol.CachePurgeResponse, error)
}

type fileDownloader interface {
	Open(context.Context, agentprotocol.FileDownloadRequest) (hostfiles.Download, error)
}

type fileWriter interface {
	Execute(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error)
}

type backupManager interface {
	ExecuteStream(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error)
	OpenDownload(context.Context, agentprotocol.BackupDownloadRequest) (hostfiles.Download, error)
}

type resourceReconciler interface {
	Reconcile(context.Context, hostingresources.Spec) (hostresources.Result, error)
}

type nginxReconciler interface {
	Reconcile(context.Context) (hostnginx.Result, error)
}

type nginxSiteActivator interface {
	Activate(context.Context, hostnginx.ActivationSpec) (hostnginx.ActivationResult, error)
}

type acmeHTTP01Presenter interface {
	Reconcile(context.Context, string, acmehttp01.Intent) (hostacme.Result, error)
}

type tlsCertificateStager interface {
	Stage(context.Context, string, tlsartifact.Bundle) (hosttls.Result, error)
}

type phpPoolReconciler interface {
	Inspect(context.Context, agentprotocol.PHPPoolInspectRequest) (hostphp.Inspection, error)
	Reconcile(context.Context, phpruntime.PoolSetSpec) (hostphp.Result, error)
}

type databaseReconciler interface {
	Reconcile(
		context.Context, string, string, agentprotocol.DatabaseProvisionRequest,
	) (hostdatabase.Result, error)
	Drop(
		context.Context, string, string, agentprotocol.DatabaseDropRequest,
	) (hostdatabase.Result, error)
	RotatePassword(
		context.Context, string, string, agentprotocol.DatabasePasswordRotateRequest,
	) (hostdatabase.Result, error)
}

type scheduledJobReconciler interface {
	Reconcile(context.Context, scheduledjobs.Spec, bool) (hostjobs.Result, error)
}

type cachedResponse struct {
	digest    [sha256.Size]byte
	status    int
	response  agentprotocol.Response
	expiresAt time.Time
}

type responseCache struct {
	mu      sync.Mutex
	entries map[string]cachedResponse
	limit   int
	ttl     time.Duration
}

func NewHandler(logger *slog.Logger) *Handler {
	return newHandlerWithServices(logger, hostcapabilities.NewInspector(), hostidentity.NewReconciler())
}

func NewHandlerWithCapabilityInspector(logger *slog.Logger, inspector capabilityInspector) *Handler {
	return newHandlerWithServices(logger, inspector, hostidentity.NewReconciler())
}

func newHandlerWithServices(
	logger *slog.Logger,
	inspector capabilityInspector,
	identities identityReconciler,
) *Handler {
	return newHandlerWithFilesystemServices(logger, inspector, identities, hostfilesystem.NewReconciler())
}

func newHandlerWithFilesystemServices(
	logger *slog.Logger,
	inspector capabilityInspector,
	identities identityReconciler,
	filesystems filesystemReconciler,
) *Handler {
	return newHandlerWithResourceServices(
		logger, inspector, identities, filesystems, hostresources.NewReconciler(),
	)
}

func newHandlerWithResourceServices(
	logger *slog.Logger,
	inspector capabilityInspector,
	identities identityReconciler,
	filesystems filesystemReconciler,
	resources resourceReconciler,
) *Handler {
	return newHandlerWithNGINXServices(
		logger, inspector, identities, filesystems, resources, hostnginx.NewReconciler(),
	)
}

func newHandlerWithNGINXServices(
	logger *slog.Logger,
	inspector capabilityInspector,
	identities identityReconciler,
	filesystems filesystemReconciler,
	resources resourceReconciler,
	nginx nginxReconciler,
) *Handler {
	return newHandlerWithNGINXActivationServices(
		logger, inspector, identities, filesystems, resources, nginx, hostnginx.NewActivator(),
	)
}

func newHandlerWithNGINXActivationServices(
	logger *slog.Logger,
	inspector capabilityInspector,
	identities identityReconciler,
	filesystems filesystemReconciler,
	resources resourceReconciler,
	nginx nginxReconciler,
	nginxSites nginxSiteActivator,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if inspector == nil {
		inspector = hostcapabilities.NewInspector()
	}
	if identities == nil {
		identities = hostidentity.NewReconciler()
	}
	if filesystems == nil {
		filesystems = hostfilesystem.NewReconciler()
	}
	if resources == nil {
		resources = hostresources.NewReconciler()
	}
	if nginx == nil {
		nginx = hostnginx.NewReconciler()
	}
	if nginxSites == nil {
		nginxSites = hostnginx.NewActivator()
	}
	return &Handler{
		logger: logger,
		cache: &responseCache{
			entries: make(map[string]cachedResponse), limit: idempotencyCacheEntries,
			ttl: idempotencyCacheTTL,
		},
		now: time.Now, capabilities: inspector, identities: identities,
		filesystems: filesystems, resources: resources, nginx: nginx, nginxSites: nginxSites,
		files:           hostfiles.NewBrowser(),
		logs:            hostlogs.NewManager(),
		cacheManager:    hostcache.NewManager(agentexec.NewRunner()),
		downloads:       hostfiles.NewDownloader(),
		downloadSlots:   make(chan struct{}, 4),
		writes:          hostfiles.NewWriter(),
		backups:         hostfiles.NewBackupManager(),
		writeSlots:      make(chan struct{}, 4),
		acmeHTTP01:      hostacme.NewPresenter(),
		tlsCertificates: hosttls.NewStager(),
		phpPools:        hostphp.NewReconciler(),
		databases:       hostdatabase.NewReconciler(),
		jobs:            hostjobs.NewReconciler(),
	}
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/v1/health":
		handler.handleHealth(w)
	case request.Method == http.MethodPost && request.URL.Path == agentprotocol.Endpoint:
		handler.handleRPC(w, request)
	case request.Method == http.MethodPost && request.URL.Path == agentprotocol.FileDownloadEndpoint:
		handler.handleFileDownload(w, request)
	case request.Method == http.MethodPost && request.URL.Path == agentprotocol.FileWriteEndpoint:
		handler.handleFileWrite(w, request)
	case request.Method == http.MethodPost && request.URL.Path == agentprotocol.BackupDownloadEndpoint:
		handler.handleBackupDownload(w, request)
	case request.URL.Path == agentprotocol.FileDownloadEndpoint:
		w.Header().Set("Allow", http.MethodPost)
		handler.writeFileDownloadError(w, http.StatusMethodNotAllowed, "unavailable",
			agentprotocol.ErrorInvalidRequest, "The file download endpoint requires POST.", 0)
	case request.URL.Path == agentprotocol.FileWriteEndpoint:
		w.Header().Set("Allow", http.MethodPost)
		handler.writeFileWriteResponse(w, http.StatusMethodNotAllowed, "unavailable", nil,
			agentprotocol.ErrorInvalidRequest, "The file write endpoint requires POST.")
	case request.URL.Path == agentprotocol.BackupDownloadEndpoint:
		w.Header().Set("Allow", http.MethodPost)
		handler.writeBackupDownloadError(w, http.StatusMethodNotAllowed, "unavailable",
			agentprotocol.ErrorInvalidRequest, "The backup download endpoint requires POST.", 0)
	case request.URL.Path == agentprotocol.Endpoint:
		w.Header().Set("Allow", http.MethodPost)
		writeProtocolError(w, http.StatusMethodNotAllowed, "unavailable", agentprotocol.ErrorInvalidRequest,
			"The RPC endpoint requires POST.")
	default:
		writeProtocolError(w, http.StatusNotFound, "unavailable", agentprotocol.ErrorInvalidRequest,
			"The requested agent endpoint does not exist.")
	}
}

func (handler *Handler) handleFileWrite(w http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, agentprotocol.FileWriteMediaType) {
		handler.writeFileWriteResponse(w, http.StatusUnsupportedMediaType, "unavailable", nil,
			agentprotocol.ErrorInvalidRequest, "Content-Type must be the Stackfort file-write media type.")
		return
	}
	controlLength, err := strconv.ParseUint(request.Header.Get(agentprotocol.FileWriteControlHeader), 10, 16)
	if err != nil || controlLength == 0 || controlLength > agentprotocol.MaxFileWriteControlBytes ||
		strconv.FormatUint(controlLength, 10) != request.Header.Get(agentprotocol.FileWriteControlHeader) {
		handler.writeFileWriteResponse(w, http.StatusBadRequest, "unavailable", nil,
			agentprotocol.ErrorInvalidRequest, "The file write control length is invalid.")
		return
	}
	maximumBody := int64(agentprotocol.MaxFileWriteControlBytes) + int64(agentprotocol.MaximumFileUploadChunkBytes)
	request.Body = http.MaxBytesReader(w, request.Body, maximumBody)
	control := make([]byte, int(controlLength))
	if _, err := io.ReadFull(request.Body, control); err != nil {
		handler.writeFileWriteResponse(w, http.StatusBadRequest, "unavailable", nil,
			agentprotocol.ErrorInvalidRequest, "The file write control payload is incomplete.")
		return
	}
	decoded, err := agentprotocol.DecodeFileWriteRequest(bytes.NewReader(control))
	if err != nil || request.ContentLength != int64(controlLength)+int64(decoded.ChunkLength) { // #nosec G115 -- both values are protocol-bounded.
		handler.writeFileWriteResponse(w, http.StatusBadRequest, "unavailable", nil,
			agentprotocol.ErrorInvalidRequest, "The file write request is invalid.")
		return
	}
	select {
	case handler.writeSlots <- struct{}{}:
		defer func() { <-handler.writeSlots }()
	default:
		handler.writeFileWriteResponse(w, http.StatusTooManyRequests, decoded.RequestID, nil,
			agentprotocol.ErrorFileDownloadBusy, "File mutation capacity is temporarily exhausted.")
		return
	}
	streamContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
	defer cancel()
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	var result agentprotocol.FileWriteResult
	if isBackupWriteAction(decoded.Action) {
		if handler.backups == nil {
			err = hostfiles.ErrInvalid
		} else {
			result, err = handler.backups.ExecuteStream(streamContext, decoded, request.Body)
		}
	} else {
		result, err = handler.writes.Execute(streamContext, decoded, request.Body)
	}
	if err != nil {
		handler.logFileWriteAudit(decoded, "failed")
		handler.handleFileWriteError(w, decoded, err)
		return
	}
	handler.logFileWriteAudit(decoded, "succeeded")
	handler.writeFileWriteResponse(w, http.StatusOK, decoded.RequestID, &result, "", "")
}

func (handler *Handler) logFileWriteAudit(request agentprotocol.FileWriteRequest, outcome string) {
	if request.Correlation == nil {
		return
	}
	handler.logger.Info("managed file mutation",
		"request_id", request.RequestID, "action", request.Action, "outcome", outcome,
		"audit_event_id", request.Correlation.AuditEventID, "actor_id", request.Correlation.ActorID,
		"session_id", request.Correlation.SessionID, "account_id", request.Correlation.AccountID,
		"control_request_id", request.Correlation.RequestID,
	)
}

func (handler *Handler) handleFileWriteError(w http.ResponseWriter, request agentprotocol.FileWriteRequest, err error) {
	requestID := request.RequestID
	if isBackupWriteAction(request.Action) {
		status, code, message := http.StatusServiceUnavailable, agentprotocol.ErrorBackupUnavailable,
			"The local backup operation could not be completed."
		switch {
		case errors.Is(err, hostfiles.ErrInvalid):
			status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest,
				"The local backup request is invalid."
		case errors.Is(err, hostfiles.ErrNotFound):
			status, code, message = http.StatusNotFound, agentprotocol.ErrorBackupNotFound,
				"The local backup was not found."
		case errors.Is(err, hostfiles.ErrConflict):
			status, code, message = http.StatusConflict, agentprotocol.ErrorBackupConflict,
				"The local backup operation conflicts with host state."
		case errors.Is(err, hostfiles.ErrIntegrity):
			status, code, message = http.StatusUnprocessableEntity, agentprotocol.ErrorBackupIntegrity,
				"The local backup failed integrity verification."
		case errors.Is(err, hostfiles.ErrTooLarge):
			status, code, message = http.StatusRequestEntityTooLarge, agentprotocol.ErrorBackupTooLarge,
				"The local backup exceeds the supported limits."
		case errors.Is(err, hostfiles.ErrBusy):
			status, code, message = http.StatusTooManyRequests, agentprotocol.ErrorBackupBusy,
				"Local backup capacity is temporarily exhausted."
		case errors.Is(err, hostfiles.ErrQuota):
			status, code, message = http.StatusInsufficientStorage, agentprotocol.ErrorFileQuotaExceeded,
				"The managed account storage quota is exhausted during restore."
		case errors.Is(err, hostfiles.ErrBackupQuota):
			status, code, message = http.StatusInsufficientStorage, agentprotocol.ErrorBackupQuotaExceeded,
				"The local backup repository quota is exhausted."
		}
		handler.logger.Error("local backup operation rejected", "request_id", requestID, "status", status)
		handler.writeFileWriteResponse(w, status, requestID, nil, code, message)
		return
	}
	status, code, message := http.StatusServiceUnavailable, agentprotocol.ErrorFileUnavailable,
		"The managed file mutation could not be completed."
	switch {
	case errors.Is(err, hostfiles.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest,
			"The managed file mutation request is invalid."
	case errors.Is(err, hostfiles.ErrNotFound):
		status, code, message = http.StatusNotFound, agentprotocol.ErrorFileNotFound,
			"The managed upload or target directory was not found."
	case errors.Is(err, hostfiles.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorFileConflict,
			"The managed file mutation conflicts with host state."
	case errors.Is(err, hostfiles.ErrTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, agentprotocol.ErrorFileDownloadTooLarge,
			"The managed upload exceeds the supported size."
	case errors.Is(err, hostfiles.ErrBusy):
		status, code, message = http.StatusTooManyRequests, agentprotocol.ErrorFileDownloadBusy,
			"File mutation capacity is temporarily exhausted."
	case errors.Is(err, hostfiles.ErrQuota):
		status, code, message = http.StatusInsufficientStorage, agentprotocol.ErrorFileQuotaExceeded,
			"The managed account storage quota is exhausted."
	}
	handler.logger.Error("managed file mutation rejected", "request_id", requestID, "status", status)
	handler.writeFileWriteResponse(w, status, requestID, nil, code, message)
}

func isBackupWriteAction(action agentprotocol.FileWriteAction) bool {
	switch action {
	case agentprotocol.FileWriteBackupCreate, agentprotocol.FileWriteBackupList,
		agentprotocol.FileWriteBackupInspect, agentprotocol.FileWriteBackupVerify,
		agentprotocol.FileWriteBackupRestore, agentprotocol.FileWriteBackupUploadInitiate,
		agentprotocol.FileWriteBackupUploadStatus, agentprotocol.FileWriteBackupUploadChunk,
		agentprotocol.FileWriteBackupUploadComplete, agentprotocol.FileWriteBackupUploadCancel,
		agentprotocol.FileWriteBackupDelete:
		return true
	default:
		return false
	}
}

func (handler *Handler) writeFileWriteResponse(
	w http.ResponseWriter, status int, requestID string, result *agentprotocol.FileWriteResult,
	code agentprotocol.ErrorCode, message string,
) {
	w.Header().Set("Content-Type", agentprotocol.MediaType+"; charset=utf-8")
	w.Header().Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	response := agentprotocol.FileWriteResponse{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID, Result: result,
	}
	if code != "" {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	writeJSON(w, status, response)
}

func (handler *Handler) handleBackupDownload(w http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
		handler.writeBackupDownloadError(w, http.StatusUnsupportedMediaType, "unavailable",
			agentprotocol.ErrorInvalidRequest, "Content-Type must be application/json.", 0)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, agentprotocol.MaxFileDownloadRequestBytes)
	decoded, err := agentprotocol.DecodeBackupDownloadRequest(request.Body)
	if err != nil {
		handler.writeBackupDownloadError(w, http.StatusBadRequest, "unavailable",
			agentprotocol.ErrorInvalidRequest, "The backup download request is invalid.", 0)
		return
	}
	select {
	case handler.downloadSlots <- struct{}{}:
		defer func() { <-handler.downloadSlots }()
	default:
		handler.writeBackupDownloadError(w, http.StatusTooManyRequests, decoded.RequestID,
			agentprotocol.ErrorBackupBusy, "Backup download capacity is temporarily exhausted.", 0)
		return
	}
	streamContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileDownloadDuration)
	defer cancel()
	download, err := handler.backups.OpenDownload(streamContext, decoded)
	if err != nil {
		handler.handleBackupDownloadError(w, decoded.RequestID, download.TotalSize, err)
		return
	}
	defer download.Body.Close()
	copyLength, err := checkedDownloadLength(download.Length)
	if err != nil || download.Offset > download.TotalSize || download.Length > download.TotalSize-download.Offset ||
		(download.Partial && download.Length == 0) || (!download.Partial && (download.Offset != 0 || download.Length != download.TotalSize)) {
		handler.writeBackupDownloadError(w, http.StatusServiceUnavailable, decoded.RequestID,
			agentprotocol.ErrorBackupUnavailable, "The backup could not be downloaded.", 0)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"stackfort-backup-%s.tar.gz\"", decoded.BackupID))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatUint(download.Length, 10))
	w.Header().Set("Last-Modified", time.Unix(download.ModifiedAt, 0).UTC().Format(http.TimeFormat))
	w.Header().Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	w.Header().Set("X-Stackfort-Request-ID", decoded.RequestID)
	w.Header().Set("X-Stackfort-File-Size", strconv.FormatUint(download.TotalSize, 10))
	w.Header().Set("X-Stackfort-File-Offset", strconv.FormatUint(download.Offset, 10))
	status := http.StatusOK
	if download.Partial {
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", download.Offset,
			download.Offset+download.Length-1, download.TotalSize))
	}
	w.WriteHeader(status)
	if download.Length > 0 {
		if _, err := io.CopyN(w, download.Body, copyLength); err != nil && request.Context().Err() == nil {
			handler.logger.Error("stream backup download", "request_id", decoded.RequestID)
		}
	}
}

func (handler *Handler) handleBackupDownloadError(w http.ResponseWriter, requestID string, total uint64, err error) {
	status, code, message := http.StatusServiceUnavailable, agentprotocol.ErrorBackupUnavailable,
		"The backup could not be downloaded."
	switch {
	case errors.Is(err, hostfiles.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest, "The backup download request is invalid."
	case errors.Is(err, hostfiles.ErrNotFound):
		status, code, message = http.StatusNotFound, agentprotocol.ErrorBackupNotFound, "The backup was not found."
	case errors.Is(err, hostfiles.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorBackupConflict, "The backup conflicts with host state."
	case errors.Is(err, hostfiles.ErrIntegrity):
		status, code, message = http.StatusUnprocessableEntity, agentprotocol.ErrorBackupIntegrity, "The backup failed integrity verification."
	case errors.Is(err, hostfiles.ErrTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, agentprotocol.ErrorBackupTooLarge, "The backup download is too large."
	case errors.Is(err, hostfiles.ErrRange):
		status, code, message = http.StatusRequestedRangeNotSatisfiable, agentprotocol.ErrorFileRangeNotSatisfiable,
			"The requested byte range is not satisfiable."
	}
	handler.writeBackupDownloadError(w, status, requestID, code, message, total)
}

func (handler *Handler) writeBackupDownloadError(
	w http.ResponseWriter, status int, requestID string, code agentprotocol.ErrorCode, message string, total uint64,
) {
	w.Header().Set("Content-Type", agentprotocol.MediaType+"; charset=utf-8")
	w.Header().Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	if status == http.StatusRequestedRangeNotSatisfiable {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
	}
	writeJSON(w, status, agentprotocol.FileDownloadErrorResponse{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: requestID, Error: agentprotocol.ResponseError{Code: code, Message: message}})
}

func (handler *Handler) handleFileDownload(w http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
		handler.writeFileDownloadError(w, http.StatusUnsupportedMediaType, "unavailable",
			agentprotocol.ErrorInvalidRequest, "Content-Type must be application/json.", 0)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, agentprotocol.MaxFileDownloadRequestBytes)
	decoded, err := agentprotocol.DecodeFileDownloadRequest(request.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			handler.writeFileDownloadError(w, http.StatusRequestEntityTooLarge, "unavailable",
				agentprotocol.ErrorInvalidRequest, "The file download request is too large.", 0)
			return
		}
		handler.writeFileDownloadError(w, http.StatusBadRequest, "unavailable",
			agentprotocol.ErrorInvalidRequest, "The file download request is invalid.", 0)
		return
	}
	select {
	case handler.downloadSlots <- struct{}{}:
		defer func() { <-handler.downloadSlots }()
	default:
		handler.writeFileDownloadError(w, http.StatusTooManyRequests, decoded.RequestID,
			agentprotocol.ErrorFileDownloadBusy, "File download capacity is temporarily exhausted.", 0)
		return
	}
	streamContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileDownloadDuration)
	defer cancel()
	download, err := handler.downloads.Open(streamContext, decoded)
	if err != nil {
		handler.handleFileDownloadError(w, decoded.RequestID, download.TotalSize, err)
		return
	}
	defer func() {
		if err := download.Body.Close(); err != nil && request.Context().Err() == nil {
			handler.logger.Error("close managed file download", "request_id", decoded.RequestID)
		}
	}()
	copyLength, err := checkedDownloadLength(download.Length)
	if err != nil || download.Offset > download.TotalSize || download.Length > download.TotalSize-download.Offset ||
		(download.Partial && download.Length == 0) || (!download.Partial && (download.Offset != 0 || download.Length != download.TotalSize)) {
		handler.logger.Error("invalid managed file download metadata", "request_id", decoded.RequestID)
		handler.writeFileDownloadError(w, http.StatusServiceUnavailable, decoded.RequestID,
			agentprotocol.ErrorFileDownloadUnavailable, "The managed file could not be downloaded.", 0)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatUint(download.Length, 10))
	w.Header().Set("Last-Modified", time.Unix(download.ModifiedAt, 0).UTC().Format(http.TimeFormat))
	w.Header().Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	w.Header().Set("X-Stackfort-Request-ID", decoded.RequestID)
	w.Header().Set("X-Stackfort-File-Size", strconv.FormatUint(download.TotalSize, 10))
	w.Header().Set("X-Stackfort-File-Offset", strconv.FormatUint(download.Offset, 10))
	status := http.StatusOK
	if download.Partial {
		status = http.StatusPartialContent
		end := download.Offset + download.Length - 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", download.Offset, end, download.TotalSize))
	}
	w.WriteHeader(status)
	if download.Length == 0 {
		return
	}
	if _, err := io.CopyN(w, download.Body, copyLength); err != nil && request.Context().Err() == nil {
		handler.logger.Error("stream managed file download", "request_id", decoded.RequestID)
	}
}

func checkedDownloadLength(length uint64) (int64, error) {
	if length > agentprotocol.MaximumFileDownloadBytes || length > uint64(^uint64(0)>>1) {
		return 0, errors.New("file download length exceeds the supported signed range")
	}
	return int64(length), nil // #nosec G115 -- both the protocol ceiling and signed range are checked above.
}

func (handler *Handler) handleFileDownloadError(w http.ResponseWriter, requestID string, total uint64, err error) {
	status, code, message := http.StatusServiceUnavailable, agentprotocol.ErrorFileDownloadUnavailable,
		"The managed file could not be downloaded."
	switch {
	case errors.Is(err, hostfiles.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest,
			"The managed file download request is invalid."
	case errors.Is(err, hostfiles.ErrNotFound):
		status, code, message = http.StatusNotFound, agentprotocol.ErrorFileNotFound,
			"The requested managed file was not found."
	case errors.Is(err, hostfiles.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorFileConflict,
			"The managed file conflicts with host state."
	case errors.Is(err, hostfiles.ErrTooLarge):
		status, code, message = http.StatusRequestEntityTooLarge, agentprotocol.ErrorFileDownloadTooLarge,
			"The requested download exceeds the response limit."
	case errors.Is(err, hostfiles.ErrRange):
		status, code, message = http.StatusRequestedRangeNotSatisfiable, agentprotocol.ErrorFileRangeNotSatisfiable,
			"The requested byte range is not satisfiable."
	}
	handler.logger.Error("managed file download rejected", "request_id", requestID, "status", status)
	handler.writeFileDownloadError(w, status, requestID, code, message, total)
}

func (handler *Handler) writeFileDownloadError(
	w http.ResponseWriter, status int, requestID string, code agentprotocol.ErrorCode, message string, total uint64,
) {
	w.Header().Set("Content-Type", agentprotocol.MediaType+"; charset=utf-8")
	w.Header().Set("X-Stackfort-Protocol", strconv.Itoa(agentprotocol.WireVersion))
	if status == http.StatusRequestedRangeNotSatisfiable {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
	}
	writeJSON(w, status, agentprotocol.FileDownloadErrorResponse{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		Error: agentprotocol.ResponseError{Code: code, Message: message},
	})
}

func (handler *Handler) handleHealth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeJSON(w, http.StatusOK, struct {
		Service string         `json:"service"`
		Status  string         `json:"status"`
		Build   buildinfo.Info `json:"build"`
	}{Service: "stackfort-agent", Status: "ok", Build: buildinfo.Current()})
}

func (handler *Handler) handleRPC(w http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, agentprotocol.MediaType) {
		writeProtocolError(w, http.StatusUnsupportedMediaType, "unavailable",
			agentprotocol.ErrorUnsupportedMediaType, "Content-Type must be application/json.")
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, agentprotocol.MaxRequestBytes)
	decoded, err := agentprotocol.DecodeRequest(request.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		switch {
		case errors.As(err, &sizeError):
			writeProtocolError(w, http.StatusRequestEntityTooLarge, "unavailable",
				agentprotocol.ErrorRequestTooLarge, "The agent request is too large.")
		case errors.Is(err, agentprotocol.ErrUnsupportedWireVersion):
			writeProtocolError(w, http.StatusUpgradeRequired, "unavailable",
				agentprotocol.ErrorIncompatibleProtocol, "The protocol wire version is incompatible.")
		case errors.Is(err, agentprotocol.ErrUnsupportedOperation):
			writeProtocolError(w, http.StatusBadRequest, "unavailable",
				agentprotocol.ErrorUnsupportedOperation, "The operation is not supported.")
		default:
			writeProtocolError(w, http.StatusBadRequest, "unavailable",
				agentprotocol.ErrorInvalidRequest, "The agent request is invalid.")
		}
		return
	}
	if decoded.ProvisionDatabase != nil {
		defer clear(decoded.ProvisionDatabase.Password)
	}
	if decoded.RotateDatabasePassword != nil {
		defer clear(decoded.RotateDatabasePassword.Password)
	}
	digest, err := agentprotocol.SemanticDigest(decoded)
	if err != nil {
		logRPCRejected(request.Context(), handler.logger, decoded,
			http.StatusInternalServerError, "semantic_digest_failed")
		writeProtocolError(w, http.StatusInternalServerError, decoded.RequestID,
			agentprotocol.ErrorInternal, "The agent request could not be completed.")
		return
	}

	status, response, replayed, conflict := handler.cache.execute(
		decoded.IdempotencyKey, digest, decoded.RequestID, handler.now().UTC(),
		func() (int, agentprotocol.Response) { return handler.dispatch(request.Context(), decoded) },
	)
	if conflict {
		logRPCRejected(request.Context(), handler.logger, decoded, http.StatusConflict, "idempotency_conflict")
		writeProtocolError(w, http.StatusConflict, decoded.RequestID,
			agentprotocol.ErrorIdempotencyConflict, "The idempotency key was reused with different input.")
		return
	}
	logRPCCompleted(request.Context(), handler.logger, decoded, status, replayed)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Stackfort-Protocol", "1")
	writeJSON(w, status, response)
}

func (handler *Handler) dispatch(ctx context.Context, request agentprotocol.Request) (int, agentprotocol.Response) {
	response := agentprotocol.Response{
		ProtocolVersion: agentprotocol.WireVersion,
		RequestID:       request.RequestID,
	}
	switch request.Operation {
	case agentprotocol.OperationHandshake:
		minimum := max(request.Handshake.MinimumVersion, agentprotocol.MinimumVersion)
		maximum := min(request.Handshake.MaximumVersion, agentprotocol.MaximumVersion)
		if minimum > maximum {
			response.Error = &agentprotocol.ResponseError{
				Code:    agentprotocol.ErrorIncompatibleProtocol,
				Message: "The client and agent protocol versions do not overlap.",
			}
			return http.StatusUpgradeRequired, response
		}
		response.Handshake = &agentprotocol.HandshakeResponse{
			SelectedVersion: maximum, AgentMinimumVersion: agentprotocol.MinimumVersion,
			AgentMaximumVersion: agentprotocol.MaximumVersion, AgentBuild: buildinfo.Current(),
			SupportedOperations: agentprotocol.SupportedOperations(),
		}
		return http.StatusOK, response
	case agentprotocol.OperationInspectCapabilities:
		report, err := handler.capabilities.Inspect(ctx)
		if err == nil {
			err = agentprotocol.ValidateCapabilityReport(report)
		}
		if err != nil {
			handler.logger.Error("inspect host capabilities", "request_id", request.RequestID)
			response.Error = &agentprotocol.ResponseError{
				Code: agentprotocol.ErrorInternal, Message: "Host capabilities could not be inspected.",
			}
			return http.StatusInternalServerError, response
		}
		response.Capabilities = &report
		return http.StatusOK, response
	case agentprotocol.OperationReconcileIdentity:
		result, err := handler.identities.Reconcile(ctx, request.ReconcileIdentity.Identity)
		if err != nil {
			return handler.identityError(response, request, err)
		}
		response.HostingIdentity = &agentprotocol.HostingIdentityResponse{
			Changed: result.Changed(), GroupCreated: result.GroupCreated,
			UserCreated: result.UserCreated, UserRepaired: result.UserRepaired,
			DirectoryCreated: result.DirectoryCreated, OwnershipRepaired: result.OwnershipRepaired,
			SubUIDsConfigured: result.SubUIDsConfigured, SubGIDsConfigured: result.SubGIDsConfigured,
			LingerEnabled: result.LingerEnabled, RuntimePrepared: result.RuntimePrepared,
		}
		return http.StatusOK, response
	case agentprotocol.OperationDeleteIdentity:
		result, err := handler.identities.Delete(ctx, request.DeleteIdentity.Identity)
		if err != nil {
			return handler.identityError(response, request, err)
		}
		response.HostingIdentity = &agentprotocol.HostingIdentityResponse{
			Changed: result.Changed(), UserDeleted: result.UserDeleted, GroupDeleted: result.GroupDeleted,
			RuntimeRemoved: result.RuntimeRemoved, SubUIDsRemoved: result.SubUIDsRemoved,
			SubGIDsRemoved: result.SubGIDsRemoved, LingerDisabled: result.LingerDisabled,
		}
		return http.StatusOK, response
	case agentprotocol.OperationReconcileFilesystem:
		result, err := handler.filesystems.Reconcile(ctx, request.ReconcileFilesystem.Storage)
		if err != nil {
			return handler.filesystemError(response, request, err)
		}
		response.HostingFilesystem = &agentprotocol.HostingFilesystemResponse{
			ProjectID:          request.ReconcileFilesystem.Storage.ProjectID,
			ProjectAssigned:    result.Layout.ProjectAssigned,
			DirectoriesCreated: result.Layout.DirectoriesCreated,
			QuotaApplied:       result.QuotaApplied, Capability: result.Capability,
		}
		return http.StatusOK, response
	case agentprotocol.OperationListFiles:
		result, err := handler.files.List(ctx, *request.ListFiles)
		if err != nil {
			return handler.fileListingError(response, request, err)
		}
		response.FileListing = &result
		return http.StatusOK, response
	case agentprotocol.OperationReadHostingLogs:
		result, err := handler.logs.Read(ctx, *request.ReadHostingLogs)
		if err != nil {
			return handler.hostingLogError(response, request, err)
		}
		response.HostingLogs = &result
		return http.StatusOK, response
	case agentprotocol.OperationReadWAFEvents:
		result, err := handler.logs.ReadWAFEvents(ctx, *request.ReadWAFEvents)
		if err != nil {
			return handler.hostingLogError(response, request, err)
		}
		response.WAFEvents = &result
		return http.StatusOK, response
	case agentprotocol.OperationInspectCacheMetrics:
		result, err := handler.cacheManager.Metrics(ctx, *request.InspectCacheMetrics)
		if err != nil {
			return handler.cacheError(response, request, err)
		}
		response.CacheMetrics = &result
		return http.StatusOK, response
	case agentprotocol.OperationPurgeCache:
		result, err := handler.cacheManager.Purge(ctx, *request.PurgeCache)
		if err != nil {
			return handler.cacheError(response, request, err)
		}
		response.CachePurge = &result
		return http.StatusOK, response
	case agentprotocol.OperationReconcileResources:
		result, err := handler.resources.Reconcile(ctx, request.ReconcileResources.Resources)
		if err != nil {
			return handler.resourceError(response, request, err)
		}
		response.HostingResources = &agentprotocol.HostingResourcesResponse{
			UID:      request.ReconcileResources.Resources.Identity.UID,
			UnitName: result.UnitName, ControlGroup: result.ControlGroup,
			UnitsChanged: result.UnitsChanged, LimitsApplied: result.LimitsApplied,
			Capability: result.Capability,
		}
		return http.StatusOK, response
	case agentprotocol.OperationEnsureDocumentRoot:
		result, err := handler.filesystems.EnsureDocumentRoot(
			ctx, request.EnsureDocumentRoot.Identity, request.EnsureDocumentRoot.RelativePath,
			request.EnsureDocumentRoot.Access,
		)
		if err != nil {
			return handler.filesystemError(response, request, err)
		}
		response.DocumentRoot = &agentprotocol.DocumentRootResponse{
			RelativePath: result.RelativePath, Created: result.Created,
		}
		return http.StatusOK, response
	case agentprotocol.OperationReconcileNGINXBaseline:
		result, err := handler.nginx.Reconcile(ctx)
		if err != nil {
			return handler.nginxError(response, request, err)
		}
		response.NGINXBaseline = &agentprotocol.NGINXBaselineResponse{
			Changed: result.Changed, ConfigurationTested: result.ConfigurationTested,
			ServiceActive: true, ServiceEnabled: true, ActivationPerformed: result.ActivationPerformed,
			ConfigurationRoot:         nginxbaseline.ManagedRoot,
			MainConfiguration:         nginxbaseline.MainConfiguration,
			PanelIncludeDirectory:     nginxbaseline.PanelDirectory,
			SitesIncludeDirectory:     nginxbaseline.SitesDirectory,
			HTTPDefaultRejectsUnknown: true, HTTPSDefaultRejectsUnknown: true,
			TrustedProxyHops: []string{nginxbaseline.LoopbackIPv4, nginxbaseline.LoopbackIPv6},
			Capability:       result.Capability,
		}
		return http.StatusOK, response
	case agentprotocol.OperationActivateNGINXSites:
		requestSpec := request.ActivateNGINXSites
		result, err := handler.nginxSites.Activate(ctx, hostnginx.ActivationSpec{
			Identity: requestSpec.Identity, RevisionID: request.Correlation.OperationID,
			DesiredStateRevisionID: requestSpec.DesiredStateRevisionID,
			Domains:                requestSpec.Domains, Options: requestSpec.Options,
		})
		if err != nil {
			return handler.nginxActivationError(response, request, err)
		}
		response.NGINXActivation = &agentprotocol.NGINXActivationResponse{
			Changed: result.Changed, ConfigurationTested: result.ConfigurationTested,
			ReloadPerformed: result.ReloadPerformed, HealthChecked: result.HealthChecked,
			RecoveryPerformed: result.RecoveryPerformed,
			ActiveRevisionID:  result.ActiveRevisionID, PreviousRevisionID: result.PreviousRevisionID,
			DesiredStateRevisionID: result.DesiredStateRevisionID,
			ConfigDigest:           hex.EncodeToString(result.ConfigDigest[:]), RenderedDomains: result.RenderedDomains,
		}
		return http.StatusOK, response
	case agentprotocol.OperationReconcileACMEHTTP01:
		result, err := handler.acmeHTTP01.Reconcile(
			ctx, request.Correlation.OperationID, request.ReconcileACMEHTTP01.Intent,
		)
		if err != nil {
			return handler.acmeHTTP01Error(response, request, err)
		}
		response.ACMEHTTP01 = &agentprotocol.ACMEHTTP01Response{
			Action:  request.ReconcileACMEHTTP01.Intent.Action,
			Changed: result.Changed, Presented: result.Presented,
		}
		return http.StatusOK, response
	case agentprotocol.OperationStageTLSCertificate:
		result, err := handler.tlsCertificates.Stage(
			ctx, request.Correlation.OperationID, request.StageTLSCertificate.Bundle,
		)
		if err != nil {
			return handler.tlsCertificateError(response, request, err)
		}
		response.TLSCertificate = &agentprotocol.TLSCertificateStageResponse{
			CertificateID: result.CertificateID, Changed: result.Changed,
		}
		return http.StatusOK, response
	case agentprotocol.OperationInspectPHPPools:
		result, err := handler.phpPools.Inspect(ctx, *request.InspectPHPPools)
		if err != nil {
			return handler.phpPoolInspectionError(response, request, err)
		}
		response.PHPPoolInspection = &agentprotocol.PHPPoolInspectResponse{
			Pools: make([]agentprotocol.PHPPoolStatus, 0, len(result.Pools)),
		}
		for _, pool := range result.Pools {
			response.PHPPoolInspection.Pools = append(response.PHPPoolInspection.Pools, agentprotocol.PHPPoolStatus{
				Version: pool.Version, State: pool.State, MemoryBytes: pool.MemoryBytes,
				CPUTimeNanosec: pool.CPUTimeNanosec, Processes: pool.Processes,
			})
		}
		return http.StatusOK, response
	case agentprotocol.OperationReconcilePHPPools:
		result, err := handler.phpPools.Reconcile(ctx, request.ReconcilePHPPools.Pools)
		if err != nil {
			return handler.phpPoolError(response, request, err)
		}
		response.PHPPools = &agentprotocol.PHPPoolSetResponse{
			Versions: result.Versions, Changed: result.Changed, Active: result.Active, Capability: result.Capability,
		}
		return http.StatusOK, response
	case agentprotocol.OperationProvisionDatabase:
		result, err := handler.databases.Reconcile(
			ctx, request.Correlation.OperationID, request.Correlation.AccountID,
			*request.ProvisionDatabase,
		)
		if err != nil {
			return handler.databaseError(response, request, err)
		}
		response.Database = &agentprotocol.DatabaseProvisionResponse{
			DatabaseName: request.ProvisionDatabase.DatabaseName,
			Username:     request.ProvisionDatabase.Username, Host: request.ProvisionDatabase.Host,
			Preset: request.ProvisionDatabase.Preset, Changed: result.Changed, Active: result.Active,
		}
		return http.StatusOK, response
	case agentprotocol.OperationRotateDatabasePassword:
		result, err := handler.databases.RotatePassword(
			ctx, request.Correlation.OperationID, request.Correlation.AccountID,
			*request.RotateDatabasePassword,
		)
		if err != nil {
			return handler.databaseError(response, request, err)
		}
		response.DatabasePasswordRotation = &agentprotocol.DatabasePasswordRotateResponse{
			Username: request.RotateDatabasePassword.Username,
			Host:     request.RotateDatabasePassword.Host,
			Changed:  result.Changed, Active: result.Active,
		}
		return http.StatusOK, response
	case agentprotocol.OperationDropDatabase:
		result, err := handler.databases.Drop(
			ctx, request.Correlation.OperationID, request.Correlation.AccountID,
			*request.DropDatabase,
		)
		if err != nil {
			return handler.databaseError(response, request, err)
		}
		response.DatabaseDrop = &agentprotocol.DatabaseDropResponse{
			Kind: request.DropDatabase.Kind, Name: request.DropDatabase.Name,
			Changed: result.Changed, Deleted: result.Active,
		}
		return http.StatusOK, response
	case agentprotocol.OperationReconcileScheduledJob:
		intent := request.ReconcileScheduledJob
		result, err := handler.jobs.Reconcile(ctx, scheduledjobs.Spec{
			Identity: intent.Identity, Definition: intent.Definition,
		}, intent.Present)
		if err != nil {
			return handler.scheduledJobError(response, request, err)
		}
		response.ScheduledJob = &agentprotocol.ScheduledJobReconcileResponse{
			JobID: result.JobID, Changed: result.Changed, Present: result.Present, Enabled: result.Enabled,
			ServiceUnit: result.ServiceUnit, TimerUnit: result.TimerUnit, Capability: result.Capability,
		}
		return http.StatusOK, response
	default:
		response.Error = &agentprotocol.ResponseError{
			Code:    agentprotocol.ErrorUnsupportedOperation,
			Message: "The operation is not supported.",
		}
		return http.StatusBadRequest, response
	}
}

func (handler *Handler) scheduledJobError(
	response agentprotocol.Response, request agentprotocol.Request, err error,
) (int, agentprotocol.Response) {
	status, code, message := http.StatusInternalServerError, agentprotocol.ErrorMutationFailed,
		"The scheduled job could not be reconciled."
	var capabilityError *hostjobs.CapabilityError
	switch {
	case errors.As(err, &capabilityError):
		status, code, message = http.StatusUnprocessableEntity, agentprotocol.ErrorScheduledJobUnavailable,
			"Scheduled jobs are unavailable on this host."
		response.Error = &agentprotocol.ResponseError{
			Code: code, Message: message, Capability: &capabilityError.Capability,
		}
	case errors.Is(err, hostjobs.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorScheduledJobInvalid,
			"The scheduled job intent is invalid."
	case errors.Is(err, hostjobs.ErrNotFound):
		status, code, message = http.StatusNotFound, agentprotocol.ErrorScheduledJobNotFound,
			"The scheduled job script was not found."
	case errors.Is(err, hostjobs.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorScheduledJobConflict,
			"The scheduled job conflicts with existing host state."
	case errors.Is(err, hostjobs.ErrUnavailable):
		status, code, message = http.StatusServiceUnavailable, agentprotocol.ErrorScheduledJobUnavailable,
			"Scheduled jobs are temporarily unavailable."
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message, Capability: &agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "scheduled-job-host-unavailable",
		}}
	}
	handler.logger.Error("scheduled job reconciliation failed",
		"request_id", request.RequestID, "operation_id", request.Correlation.OperationID,
		"account_id", request.Correlation.AccountID,
	)
	if response.Error == nil {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return status, response
}

func (handler *Handler) fileListingError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status, code, message := http.StatusInternalServerError, agentprotocol.ErrorFileUnavailable,
		"The managed account directory could not be listed."
	switch {
	case errors.Is(err, hostfiles.ErrNotFound):
		status, code, message = http.StatusNotFound, agentprotocol.ErrorFileNotFound,
			"The requested managed account directory was not found."
	case errors.Is(err, hostfiles.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest,
			"The managed account directory request is invalid."
	case errors.Is(err, hostfiles.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorFileConflict,
			"The managed account directory conflicts with host state."
	}
	handler.logger.Error("managed account directory listing failed",
		"request_id", request.RequestID, "operation", request.Operation,
	)
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	return status, response
}

func (handler *Handler) hostingLogError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status, code, message := http.StatusServiceUnavailable, agentprotocol.ErrorHostingLogUnavailable,
		"The managed domain logs could not be read."
	switch {
	case errors.Is(err, hostlogs.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest,
			"The managed domain log request is invalid."
	case errors.Is(err, hostlogs.ErrNotFound):
		status, code, message = http.StatusNotFound, agentprotocol.ErrorHostingLogNotFound,
			"The managed domain log is not available."
	case errors.Is(err, hostlogs.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorHostingLogConflict,
			"The managed domain log conflicts with root-owned host state."
	}
	handler.logger.Error("managed domain log read failed", "request_id", request.RequestID, "operation", request.Operation)
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	return status, response
}

func (handler *Handler) cacheError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status, code, message := http.StatusServiceUnavailable, agentprotocol.ErrorCacheUnavailable,
		"Vinyl Cache is temporarily unavailable."
	switch {
	case errors.Is(err, hostcache.ErrInvalid):
		status, code, message = http.StatusBadRequest, agentprotocol.ErrorInvalidRequest,
			"The cache request is invalid."
	case errors.Is(err, hostcache.ErrConflict):
		status, code, message = http.StatusConflict, agentprotocol.ErrorCacheConflict,
			"The cache state conflicts with managed host policy."
	}
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	if code == agentprotocol.ErrorCacheUnavailable {
		response.Error.Capability = &agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "vinyl-cache-unavailable",
		}
	}
	handler.logger.Error("managed cache operation failed", "request_id", request.RequestID, "operation", request.Operation)
	return status, response
}

func (handler *Handler) databaseError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorDatabaseMutation
	message := "The managed database operation failed."
	var managedError *hostdatabase.Error
	if errors.As(err, &managedError) {
		switch managedError.Kind {
		case hostdatabase.ErrorConflict:
			status, code, message = http.StatusConflict, agentprotocol.ErrorDatabaseConflict,
				"The managed database state conflicts with the requested account."
		case hostdatabase.ErrorUnavailable:
			status, code, message = http.StatusServiceUnavailable, agentprotocol.ErrorDatabaseUnavailable,
				"The local MariaDB service is unavailable."
		case hostdatabase.ErrorValidation:
			status, code, message = http.StatusUnprocessableEntity, agentprotocol.ErrorDatabaseValidation,
				"The managed database intent is invalid."
		}
	}
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	handler.logger.Error("managed database operation failed",
		"request_id", request.RequestID, "operation", request.Operation,
	)
	return status, response
}

func (handler *Handler) phpPoolInspectionError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	response.Error = &agentprotocol.ResponseError{
		Code: agentprotocol.ErrorInternal, Message: "Managed PHP-FPM pool status could not be inspected.",
	}
	var capabilityError *hostphp.CapabilityError
	if errors.As(err, &capabilityError) {
		status = http.StatusUnprocessableEntity
		response.Error = &agentprotocol.ResponseError{
			Code: agentprotocol.ErrorPHPUnavailable, Message: "The requested PHP runtime is unavailable on this host.",
			Capability: &capabilityError.Capability,
		}
	}
	handler.logger.Error("PHP-FPM pool inspection failed",
		"request_id", request.RequestID, "operation", request.Operation,
	)
	return status, response
}

func (handler *Handler) phpPoolError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorPHPActivation
	message := "The managed PHP-FPM pools could not be activated."
	var capabilityError *hostphp.CapabilityError
	switch {
	case errors.As(err, &capabilityError):
		status, code = http.StatusUnprocessableEntity, agentprotocol.ErrorPHPUnavailable
		message = "The requested PHP runtime is unavailable on this host."
		response.Error = &agentprotocol.ResponseError{
			Code: code, Message: message, Capability: &capabilityError.Capability,
		}
	case errors.Is(err, hostphp.ErrConflict):
		status, code = http.StatusConflict, agentprotocol.ErrorPHPConflict
		message = "The managed PHP-FPM state conflicts with existing host state."
	case errors.Is(err, hostphp.ErrValidation):
		code = agentprotocol.ErrorPHPValidation
		message = "The generated PHP-FPM configuration did not pass validation."
	}
	handler.logger.Error("PHP-FPM pool reconciliation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
	)
	if response.Error == nil {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return status, response
}

func (handler *Handler) tlsCertificateError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorTLSCertificateUnavailable
	message := "The TLS certificate artifact could not be staged."
	if errors.Is(err, hosttls.ErrConflict) {
		status, code = http.StatusConflict, agentprotocol.ErrorTLSCertificateConflict
		message = "The TLS certificate artifact conflicts with managed host state."
	}
	handler.logger.Error("TLS certificate staging failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
		"error", err,
	)
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	return status, response
}

func (handler *Handler) acmeHTTP01Error(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorACMEHTTP01Unavailable
	message := "The ACME HTTP-01 response could not be reconciled."
	if errors.Is(err, hostacme.ErrConflict) {
		status, code = http.StatusConflict, agentprotocol.ErrorACMEHTTP01Conflict
		message = "The ACME HTTP-01 response conflicts with managed host state."
	}
	handler.logger.Error("ACME HTTP-01 reconciliation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
	)
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	return status, response
}

func (handler *Handler) nginxActivationError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorNGINXActivation
	message := "The managed NGINX site revision could not be activated."
	var capabilityError *hostnginx.CapabilityError
	switch {
	case errors.As(err, &capabilityError):
		status, code = http.StatusUnprocessableEntity, agentprotocol.ErrorNGINXUnavailable
		message = "NGINX site activation is unavailable on this host."
		response.Error = &agentprotocol.ResponseError{
			Code: code, Message: message, Capability: &capabilityError.Capability,
		}
	case errors.Is(err, hostnginx.ErrConflict):
		status, code = http.StatusConflict, agentprotocol.ErrorNGINXConflict
		message = "The managed NGINX site state conflicts with the activation request."
	case errors.Is(err, hostnginx.ErrValidationFailed):
		code = agentprotocol.ErrorNGINXValidation
		message = "The complete candidate NGINX configuration did not pass validation."
	case errors.Is(err, hostnginx.ErrHealthCheckFailed):
		status, code = http.StatusBadGateway, agentprotocol.ErrorNGINXHealthCheck
		message = "The activated NGINX site revision did not pass its local health check."
	}
	handler.logger.Error("NGINX site activation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
	)
	if response.Error == nil {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return status, response
}

func (handler *Handler) nginxError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorMutationFailed
	message := "The managed NGINX baseline could not be changed."
	var capabilityError *hostnginx.CapabilityError
	switch {
	case errors.As(err, &capabilityError):
		status, code = http.StatusUnprocessableEntity, agentprotocol.ErrorNGINXUnavailable
		message = "NGINX baseline management is unavailable on this host."
		response.Error = &agentprotocol.ResponseError{
			Code: code, Message: message, Capability: &capabilityError.Capability,
		}
	case errors.Is(err, hostnginx.ErrConflict):
		status, code = http.StatusConflict, agentprotocol.ErrorNGINXConflict
		message = "The managed NGINX baseline conflicts with existing host state."
	case errors.Is(err, hostnginx.ErrValidationFailed):
		code = agentprotocol.ErrorNGINXValidation
		message = "The managed NGINX configuration did not pass validation."
	}
	handler.logger.Error("NGINX baseline mutation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID,
	)
	if response.Error == nil {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return status, response
}

func (handler *Handler) resourceError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorMutationFailed
	message := "The managed account resource boundary could not be changed."
	var capabilityError *hostresources.CapabilityError
	switch {
	case errors.As(err, &capabilityError):
		status, code = http.StatusUnprocessableEntity, agentprotocol.ErrorResourceControlUnavailable
		message = "Systemd cgroup-v2 resource control is unavailable."
		response.Error = &agentprotocol.ResponseError{
			Code: code, Message: message, Capability: &capabilityError.Capability,
		}
	case errors.Is(err, hostresources.ErrConflict):
		status, code = http.StatusConflict, agentprotocol.ErrorResourceControlConflict
		message = "The managed account resource unit conflicts with existing host state."
	}
	handler.logger.Error("hosting resource mutation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
	)
	if response.Error == nil {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return status, response
}

func (handler *Handler) filesystemError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorMutationFailed
	message := "The managed account filesystem could not be changed."
	var capabilityError *hostfilesystem.CapabilityError
	switch {
	case errors.As(err, &capabilityError):
		status, code = http.StatusUnprocessableEntity, agentprotocol.ErrorQuotaUnavailable
		message = "Project quota enforcement is unavailable on the managed filesystem."
		response.Error = &agentprotocol.ResponseError{
			Code: code, Message: message, Capability: &capabilityError.Capability,
		}
	case errors.Is(err, hostfilesystem.ErrConflict):
		status, code = http.StatusConflict, agentprotocol.ErrorFilesystemConflict
		message = "The managed account filesystem conflicts with existing host state."
	case errors.Is(err, hostfilesystem.ErrMigrationRequired):
		status, code = http.StatusConflict, agentprotocol.ErrorFilesystemMigration
		message = "The existing account tree requires an offline project quota migration."
	}
	handler.logger.Error("hosting filesystem mutation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
	)
	if response.Error == nil {
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return status, response
}

func (handler *Handler) identityError(
	response agentprotocol.Response,
	request agentprotocol.Request,
	err error,
) (int, agentprotocol.Response) {
	status := http.StatusInternalServerError
	code := agentprotocol.ErrorMutationFailed
	message := "The managed hosting identity could not be changed."
	switch {
	case errors.Is(err, hostidentity.ErrIdentityConflict):
		status, code = http.StatusConflict, agentprotocol.ErrorIdentityConflict
		message = "The managed hosting identity conflicts with existing host state."
	case errors.Is(err, hostidentity.ErrArchiveRequired):
		status, code = http.StatusConflict, agentprotocol.ErrorArchiveRequired
		message = "The managed account directory must be archived before identity deletion."
	case errors.Is(err, hostidentity.ErrRuntimeUnavailable):
		status, code = http.StatusUnprocessableEntity, agentprotocol.ErrorOCIRuntimeUnavailable
		message = "The required rootless OCI runtime is unavailable."
	}
	handler.logger.Error("hosting identity mutation failed",
		"request_id", request.RequestID, "operation", request.Operation,
		"operation_id", request.Correlation.OperationID, "account_id", request.Correlation.AccountID,
	)
	response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	var capabilityError *hostidentity.RuntimeCapabilityError
	if errors.As(err, &capabilityError) {
		response.Error.Capability = &capabilityError.Capability
	} else if code == agentprotocol.ErrorOCIRuntimeUnavailable {
		response.Error.Capability = &agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnknown, ReasonCode: "runtime-inspection-failed",
		}
	}
	return status, response
}

func (cache *responseCache) execute(
	key string,
	digest [sha256.Size]byte,
	requestID string,
	now time.Time,
	execute func() (int, agentprotocol.Response),
) (int, agentprotocol.Response, bool, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.prune(now)
	if existing, ok := cache.entries[key]; ok {
		if existing.digest != digest {
			return 0, agentprotocol.Response{}, false, true
		}
		response := existing.response
		response.RequestID = requestID
		return existing.status, response, true, false
	}
	status, response := execute()
	if status >= http.StatusInternalServerError {
		return status, response, false, false
	}
	if len(cache.entries) >= cache.limit {
		cache.evictOldest()
	}
	cache.entries[key] = cachedResponse{
		digest: digest, status: status, response: response, expiresAt: now.Add(cache.ttl),
	}
	return status, response, false, false
}

func (cache *responseCache) prune(now time.Time) {
	for key, entry := range cache.entries {
		if !entry.expiresAt.After(now) {
			delete(cache.entries, key)
		}
	}
}

func (cache *responseCache) evictOldest() {
	oldestKey := ""
	var oldestTime time.Time
	for key, entry := range cache.entries {
		if oldestKey == "" || entry.expiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.expiresAt
		}
	}
	if oldestKey != "" {
		delete(cache.entries, oldestKey)
	}
}

func writeProtocolError(w http.ResponseWriter, status int, requestID string, code agentprotocol.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Stackfort-Protocol", "1")
	writeJSON(w, status, agentprotocol.Response{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		Error: &agentprotocol.ResponseError{Code: code, Message: message},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
