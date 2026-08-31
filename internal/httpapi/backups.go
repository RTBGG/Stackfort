// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/backupworkspace"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/google/uuid"
)

const maxBackupRequestBytes = 8 << 10

type BackupWorkspaceService interface {
	List(context.Context, backupworkspace.ListParams) (agentprotocol.FileWriteResult, error)
	Inspect(context.Context, backupworkspace.LookupParams) (agentprotocol.FileWriteResult, error)
	Verify(context.Context, backupworkspace.LookupParams) (agentprotocol.FileWriteResult, error)
	Create(context.Context, backupworkspace.CreateParams) (agentprotocol.FileWriteResult, error)
	Restore(context.Context, backupworkspace.RestoreParams) (agentprotocol.FileWriteResult, error)
	Download(context.Context, backupworkspace.DownloadParams) (backupworkspace.Download, error)
	InitiateUpload(context.Context, backupworkspace.InitiateUploadParams) (agentprotocol.FileWriteResult, error)
	UploadStatus(context.Context, backupworkspace.UploadParams) (agentprotocol.FileWriteResult, error)
	WriteUploadChunk(context.Context, backupworkspace.UploadChunkParams) (agentprotocol.FileWriteResult, error)
	CompleteUpload(context.Context, backupworkspace.CompleteUploadParams) (agentprotocol.FileWriteResult, error)
	CancelUpload(context.Context, backupworkspace.CancelUploadParams) (agentprotocol.FileWriteResult, error)
	Delete(context.Context, backupworkspace.DeleteParams) (agentprotocol.FileWriteResult, error)
}

type backupCreateRequest struct {
	Scope      agentprotocol.BackupScope `json:"scope"`
	SourcePath string                    `json:"sourcePath,omitempty"`
}

type backupRestoreRequest struct {
	Confirmation string `json:"confirmation"`
}

type backupUploadRequest struct {
	Scope          agentprotocol.BackupScope `json:"scope"`
	SourcePath     string                    `json:"sourcePath,omitempty"`
	SizeBytes      uint64                    `json:"sizeBytes"`
	ExpectedSHA256 string                    `json:"expectedSha256"`
}

type emptyBackupRequest struct{}

func registerBackupRoutes(
	mux *http.ServeMux, logger *slog.Logger, authentication AuthenticationService, service BackupWorkspaceService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/backups", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, false)
		if !ok {
			return
		}
		query, cursor := request.URL.Query(), ""
		cursorValues := query["cursor"]
		if len(query) > 1 || len(cursorValues) > 1 || (len(query) == 1 && len(cursorValues) != 1) {
			writeAPIError(w, http.StatusBadRequest, "invalid_backup_cursor", "The backup cursor is invalid.")
			return
		}
		if len(cursorValues) == 1 {
			cursor = cursorValues[0]
		}
		result, err := service.List(request.Context(), backupworkspace.ListParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, Cursor: cursor,
		})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/backups", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input backupCreateRequest
		if !decodeBoundedJSON(w, request, &input, maxBackupRequestBytes) {
			return
		}
		mutation, ok := backupMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.Create(operationContext, backupworkspace.CreateParams{
			MutationContext: mutation, Scope: input.Scope, SourcePath: input.SourcePath,
		})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/backups/{backupID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, false)
		if !ok {
			return
		}
		result, err := service.Inspect(request.Context(), backupworkspace.LookupParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, BackupID: request.PathValue("backupID"),
		})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/backups/{backupID}/verify", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input emptyBackupRequest
		if !decodeBoundedJSON(w, request, &input, maxBackupRequestBytes) {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.Verify(operationContext, backupworkspace.LookupParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, BackupID: request.PathValue("backupID"),
		})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/backups/{backupID}/restore", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input backupRestoreRequest
		if !decodeBoundedJSON(w, request, &input, maxBackupRequestBytes) {
			return
		}
		mutation, ok := backupMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.Restore(operationContext, backupworkspace.RestoreParams{
			MutationContext: mutation, BackupID: request.PathValue("backupID"), Confirmation: input.Confirmation,
		})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/backups/{backupID}/download", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, false)
		if !ok {
			return
		}
		downloadRange, err := parseSingleFileRange(request.Header.Values("Range"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_backup_range", "The backup range is invalid.")
			return
		}
		streamContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileDownloadDuration)
		defer cancel()
		download, err := service.Download(streamContext, backupworkspace.DownloadParams{Subject: authenticated.AuthorizationSubject(),
			AccountID: accountID, BackupID: request.PathValue("backupID"), Range: downloadRange})
		if err != nil {
			var rangeError *backupworkspace.RangeError
			if errors.As(err, &rangeError) {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", rangeError.TotalSize))
				writeAPIError(w, http.StatusRequestedRangeNotSatisfiable, "backup_range_not_satisfiable",
					"The requested backup range is not satisfiable.")
				return
			}
			writeBackupError(w, logger, err)
			return
		}
		defer download.Body.Close()
		copyLength, lengthErr := checkedDownloadLength(download.Length)
		if lengthErr != nil || download.Offset > download.TotalSize || download.Length > download.TotalSize-download.Offset ||
			(download.Partial && download.Length == 0) || (!download.Partial && (download.Offset != 0 || download.Length != download.TotalSize)) {
			writeBackupError(w, logger, backupworkspace.ErrUnavailable)
			return
		}
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": download.Name})
		if disposition == "" {
			writeBackupError(w, logger, backupworkspace.ErrUnavailable)
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Disposition", disposition)
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.FormatUint(download.Length, 10))
		w.Header().Set("Last-Modified", download.ModifiedAt.UTC().Format(http.TimeFormat))
		status := http.StatusOK
		if download.Partial {
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", download.Offset,
				download.Offset+download.Length-1, download.TotalSize))
		}
		w.WriteHeader(status)
		if download.Length > 0 {
			if _, err := io.CopyN(w, download.Body, copyLength); err != nil && request.Context().Err() == nil {
				logger.Error("stream account backup download")
			}
		}
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/backup-uploads", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input backupUploadRequest
		if !decodeBoundedJSON(w, request, &input, maxBackupRequestBytes) {
			return
		}
		mutation, ok := backupMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.InitiateUpload(request.Context(), backupworkspace.InitiateUploadParams{MutationContext: mutation,
			Scope: input.Scope, SourcePath: input.SourcePath, SizeBytes: input.SizeBytes, ExpectedSHA256: input.ExpectedSHA256})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/backup-uploads/{uploadID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, false)
		if !ok {
			return
		}
		result, err := service.UploadStatus(request.Context(), backupworkspace.UploadParams{Subject: authenticated.AuthorizationSubject(),
			AccountID: accountID, UploadID: request.PathValue("uploadID")})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("PUT /api/v1/accounts/{accountID}/backup-uploads/{uploadID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		mediaType, _, mediaErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
		offset, offsetErr := parseCanonicalRangeUint(request.Header.Get("Upload-Offset"))
		if mediaErr != nil || !strings.EqualFold(mediaType, "application/octet-stream") || offsetErr != nil ||
			request.ContentLength <= 0 || uint64(request.ContentLength) > agentprotocol.MaximumFileUploadChunkBytes {
			writeAPIError(w, http.StatusBadRequest, "invalid_backup_upload_chunk", "The backup upload chunk is invalid.")
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, int64(agentprotocol.MaximumFileUploadChunkBytes))
		_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.WriteUploadChunk(operationContext, backupworkspace.UploadChunkParams{UploadParams: backupworkspace.UploadParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, UploadID: request.PathValue("uploadID")},
			Offset: offset, Length: uint64(request.ContentLength), Body: request.Body}) // #nosec G115 -- positive bounded content length checked above.
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/backup-uploads/{uploadID}/complete", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input backupUploadRequest
		if !decodeBoundedJSON(w, request, &input, maxBackupRequestBytes) {
			return
		}
		mutation, ok := backupMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.CompleteUpload(operationContext, backupworkspace.CompleteUploadParams{MutationContext: mutation,
			UploadID: request.PathValue("uploadID"), Scope: input.Scope, SourcePath: input.SourcePath,
			SizeBytes: input.SizeBytes, ExpectedSHA256: input.ExpectedSHA256})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("DELETE /api/v1/accounts/{accountID}/backup-uploads/{uploadID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		mutation, ok := backupMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.CancelUpload(request.Context(), backupworkspace.CancelUploadParams{MutationContext: mutation,
			UploadID: request.PathValue("uploadID")})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("DELETE /api/v1/accounts/{accountID}/backups/{backupID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, ok := authenticateBackupRoute(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input backupRestoreRequest
		if !decodeBoundedJSON(w, request, &input, maxBackupRequestBytes) {
			return
		}
		mutation, ok := backupMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.Delete(request.Context(), backupworkspace.DeleteParams{MutationContext: mutation,
			BackupID: request.PathValue("backupID"), Confirmation: input.Confirmation})
		if err != nil {
			writeBackupError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func authenticateBackupRoute(
	w http.ResponseWriter, request *http.Request, logger *slog.Logger, authentication AuthenticationService, requireCSRF bool,
) (core.AuthenticatedSession, core.ID, bool) {
	authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, requireCSRF)
	if !ok {
		return core.AuthenticatedSession{}, "", false
	}
	accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
	return authenticated, accountID, ok
}

func backupMutationContext(
	w http.ResponseWriter, request *http.Request, logger *slog.Logger,
	authenticated core.AuthenticatedSession, accountID core.ID,
) (backupworkspace.MutationContext, bool) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			writeBackupError(w, logger, err)
			return backupworkspace.MutationContext{}, false
		}
		requestID = generated.String()
	}
	sourceAddress, ok := requestSourceAddress(w, request, logger)
	if !ok {
		return backupworkspace.MutationContext{}, false
	}
	return backupworkspace.MutationContext{Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
		RequestID: requestID, SourceAddress: sourceAddress}, true
}

func writeBackupError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound), errors.Is(err, backupworkspace.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Recent authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_backup_request", "The backup request is invalid.")
	case errors.Is(err, backupworkspace.ErrNotReady):
		writeAPIError(w, http.StatusConflict, "backup_workspace_not_ready", "The account backup workspace is not ready.")
	case errors.Is(err, backupworkspace.ErrConflict):
		writeAPIError(w, http.StatusConflict, "backup_conflict", "The backup conflicts with current host state.")
	case errors.Is(err, backupworkspace.ErrIntegrity):
		writeAPIError(w, http.StatusUnprocessableEntity, "backup_integrity_failed", "The backup failed integrity verification.")
	case errors.Is(err, backupworkspace.ErrTooLarge):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "backup_too_large", "The backup exceeds supported limits.")
	case errors.Is(err, backupworkspace.ErrBusy):
		w.Header().Set("Retry-After", "2")
		writeAPIError(w, http.StatusTooManyRequests, "backup_busy", "Backup capacity is temporarily exhausted.")
	case errors.Is(err, backupworkspace.ErrQuota):
		writeAPIError(w, http.StatusInsufficientStorage, "file_quota_exceeded", "The account storage quota is exhausted during restore.")
	case errors.Is(err, backupworkspace.ErrRepositoryQuota):
		writeAPIError(w, http.StatusInsufficientStorage, "backup_repository_quota_exceeded", "The backup repository quota is exhausted.")
	case errors.Is(err, backupworkspace.ErrUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "backup_unavailable", "The backup service is temporarily unavailable.")
	default:
		logger.Error("process account backup", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
