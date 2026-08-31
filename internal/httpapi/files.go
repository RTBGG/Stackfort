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
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/fileworkspace"
	"github.com/google/uuid"
)

const maxFileMutationRequestBytes = 8 << 10

type FileWorkspaceService interface {
	List(context.Context, fileworkspace.ListParams) (agentprotocol.FileListResponse, error)
	Download(context.Context, fileworkspace.DownloadParams) (fileworkspace.Download, error)
	InitiateUpload(context.Context, fileworkspace.InitiateUploadParams) (agentprotocol.FileWriteResult, error)
	UploadStatus(context.Context, fileworkspace.UploadParams) (agentprotocol.FileWriteResult, error)
	WriteUploadChunk(context.Context, fileworkspace.UploadChunkParams) (agentprotocol.FileWriteResult, error)
	CompleteUpload(context.Context, fileworkspace.CompleteUploadParams) (agentprotocol.FileWriteResult, error)
	CancelUpload(context.Context, fileworkspace.UploadParams) (agentprotocol.FileWriteResult, error)
	CreateNode(context.Context, fileworkspace.CreateNodeParams) (agentprotocol.FileWriteResult, error)
	MutateNode(context.Context, fileworkspace.NodeMutationParams) (agentprotocol.FileWriteResult, error)
	MutateArchive(context.Context, fileworkspace.ArchiveMutationParams) (agentprotocol.FileWriteResult, error)
	TrashNode(context.Context, fileworkspace.TrashNodeParams) (agentprotocol.FileWriteResult, error)
	ListTrash(context.Context, fileworkspace.TrashListParams) (agentprotocol.FileWriteResult, error)
	RestoreTrash(context.Context, fileworkspace.TrashParams) (agentprotocol.FileWriteResult, error)
	PurgeTrash(context.Context, fileworkspace.TrashParams) (agentprotocol.FileWriteResult, error)
}

type fileUploadRequest struct {
	Directory      string `json:"directory"`
	Name           string `json:"name"`
	SizeBytes      uint64 `json:"sizeBytes"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
}

type fileNodeRequest struct {
	Directory string `json:"directory"`
	Name      string `json:"name"`
	Type      string `json:"type"`
}

type fileNodeOperationRequest struct {
	Action               string `json:"action"`
	SourceDirectory      string `json:"sourceDirectory"`
	SourceName           string `json:"sourceName"`
	DestinationDirectory string `json:"destinationDirectory"`
	DestinationName      string `json:"destinationName"`
}

type fileTrashRequest struct {
	Directory string `json:"directory"`
	Name      string `json:"name"`
}

type fileArchiveRequest struct {
	Action               string                          `json:"action"`
	Format               agentprotocol.FileArchiveFormat `json:"format"`
	SourceDirectory      string                          `json:"sourceDirectory"`
	SourceName           string                          `json:"sourceName"`
	DestinationDirectory string                          `json:"destinationDirectory"`
	DestinationName      string                          `json:"destinationName"`
}

func registerFileRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service FileWorkspaceService,
) {
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-uploads", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input fileUploadRequest
		if !decodeFileMutationJSON(w, request, &input) {
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.InitiateUpload(request.Context(), fileworkspace.InitiateUploadParams{
			MutationContext: mutation, Directory: input.Directory, Name: input.Name,
			SizeBytes: input.SizeBytes, ExpectedSHA256: input.ExpectedSHA256,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/file-uploads/{uploadID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		result, err := service.UploadStatus(request.Context(), fileworkspace.UploadParams{
			MutationContext: fileworkspace.MutationContext{Subject: authenticated.AuthorizationSubject(), AccountID: accountID},
			UploadID:        request.PathValue("uploadID"),
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("PUT /api/v1/accounts/{accountID}/file-uploads/{uploadID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		mediaType, _, mediaErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
		offset, offsetErr := parseCanonicalRangeUint(request.Header.Get("Upload-Offset"))
		if mediaErr != nil || !strings.EqualFold(mediaType, "application/octet-stream") || offsetErr != nil ||
			request.ContentLength <= 0 || uint64(request.ContentLength) > agentprotocol.MaximumFileUploadChunkBytes {
			writeAPIError(w, http.StatusBadRequest, "invalid_file_upload_chunk", "The file upload chunk is invalid.")
			return
		}
		request.Body = http.MaxBytesReader(w, request.Body, int64(agentprotocol.MaximumFileUploadChunkBytes))
		_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		streamContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.WriteUploadChunk(streamContext, fileworkspace.UploadChunkParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, UploadID: request.PathValue("uploadID"),
			Offset: offset, ChunkLength: uint64(request.ContentLength), Body: request.Body, // #nosec G115 -- positive, bounded length checked above.
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-uploads/{uploadID}/complete", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input fileUploadRequest
		if !decodeFileMutationJSON(w, request, &input) {
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		completeContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.CompleteUpload(completeContext, fileworkspace.CompleteUploadParams{
			UploadParams: fileworkspace.UploadParams{MutationContext: mutation, UploadID: request.PathValue("uploadID")},
			Directory:    input.Directory, Name: input.Name, SizeBytes: input.SizeBytes, ExpectedSHA256: input.ExpectedSHA256,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("DELETE /api/v1/accounts/{accountID}/file-uploads/{uploadID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		if _, err := service.CancelUpload(request.Context(), fileworkspace.UploadParams{
			MutationContext: mutation, UploadID: request.PathValue("uploadID"),
		}); err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-nodes", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input fileNodeRequest
		if !decodeFileMutationJSON(w, request, &input) || (input.Type != "file" && input.Type != "directory") {
			if input.Type != "file" && input.Type != "directory" {
				writeAPIError(w, http.StatusBadRequest, "invalid_file_node", "The file or directory request is invalid.")
			}
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.CreateNode(request.Context(), fileworkspace.CreateNodeParams{
			MutationContext: mutation, Directory: input.Directory, Name: input.Name, DirectoryNode: input.Type == "directory",
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-operations", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input fileNodeOperationRequest
		if !decodeFileMutationJSON(w, request, &input) {
			return
		}
		action := agentprotocol.FileWriteAction("")
		switch input.Action {
		case "rename":
			action = agentprotocol.FileWriteRename
		case "move":
			action = agentprotocol.FileWriteMove
		case "copy":
			action = agentprotocol.FileWriteCopy
		default:
			writeAPIError(w, http.StatusBadRequest, "invalid_file_operation", "The file operation is invalid.")
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.MutateNode(operationContext, fileworkspace.NodeMutationParams{
			MutationContext: mutation, Action: action, SourceDirectory: input.SourceDirectory, SourceName: input.SourceName,
			DestinationDirectory: input.DestinationDirectory, DestinationName: input.DestinationName,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-archives", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input fileArchiveRequest
		if !decodeFileMutationJSON(w, request, &input) {
			return
		}
		action := agentprotocol.FileWriteAction("")
		switch input.Action {
		case "create":
			action = agentprotocol.FileWriteArchiveCreate
		case "extract":
			action = agentprotocol.FileWriteArchiveExtract
		default:
			writeAPIError(w, http.StatusBadRequest, "invalid_file_archive", "The file archive request is invalid.")
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		operationContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		result, err := service.MutateArchive(operationContext, fileworkspace.ArchiveMutationParams{
			MutationContext: mutation, Action: action, Format: input.Format,
			SourceDirectory: input.SourceDirectory, SourceName: input.SourceName,
			DestinationDirectory: input.DestinationDirectory, DestinationName: input.DestinationName,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-trash", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input fileTrashRequest
		if !decodeFileMutationJSON(w, request, &input) {
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.TrashNode(request.Context(), fileworkspace.TrashNodeParams{
			MutationContext: mutation, Directory: input.Directory, Name: input.Name,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/file-trash", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		query := request.URL.Query()
		cursorValues := query["cursor"]
		if len(query) > 1 || len(cursorValues) > 1 || (len(query) == 1 && len(cursorValues) != 1) {
			writeAPIError(w, http.StatusBadRequest, "invalid_file_trash_cursor", "The trash cursor is invalid.")
			return
		}
		cursor := ""
		if len(cursorValues) == 1 {
			cursor = cursorValues[0]
		}
		result, err := service.ListTrash(request.Context(), fileworkspace.TrashListParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, Cursor: cursor,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/file-trash/{trashID}/restore", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		result, err := service.RestoreTrash(request.Context(), fileworkspace.TrashParams{
			MutationContext: mutation, TrashID: request.PathValue("trashID"),
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("DELETE /api/v1/accounts/{accountID}/file-trash/{trashID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		mutation, ok := fileMutationContext(w, request, logger, authenticated, accountID)
		if !ok {
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		purgeContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileWriteDuration)
		defer cancel()
		if _, err := service.PurgeTrash(purgeContext, fileworkspace.TrashParams{
			MutationContext: mutation, TrashID: request.PathValue("trashID"),
		}); err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/files", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		query := request.URL.Query()
		pathValues, cursorValues := query["path"], query["cursor"]
		if len(pathValues) > 1 || len(cursorValues) > 1 {
			writeAPIError(w, http.StatusBadRequest, "invalid_file_path", "The file path is invalid.")
			return
		}
		path, cursor := "", ""
		if len(pathValues) == 1 {
			path = pathValues[0]
		}
		if len(cursorValues) == 1 {
			cursor = cursorValues[0]
		}
		listing, err := service.List(request.Context(), fileworkspace.ListParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, Path: path, Cursor: cursor,
		})
		if err != nil {
			writeFileWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, listing)
	})
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/files/download", func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "The file download endpoint requires GET.")
			return
		}
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		query := request.URL.Query()
		pathValues := query["path"]
		if len(query) != 1 || len(pathValues) != 1 || pathValues[0] == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_file_path", "The file path is invalid.")
			return
		}
		downloadRange, err := parseSingleFileRange(request.Header.Values("Range"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_file_range", "The file range is invalid.")
			return
		}
		streamContext, cancel := context.WithTimeout(request.Context(), agentprotocol.MaximumFileDownloadDuration)
		defer cancel()
		download, err := service.Download(streamContext, fileworkspace.DownloadParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			Path: pathValues[0], Range: downloadRange,
		})
		if err != nil {
			var rangeError *fileworkspace.RangeError
			if errors.As(err, &rangeError) {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", rangeError.TotalSize))
				writeAPIError(w, http.StatusRequestedRangeNotSatisfiable, "file_range_not_satisfiable",
					"The requested file range is not satisfiable.")
				return
			}
			writeFileWorkspaceError(w, logger, err)
			return
		}
		defer func() {
			if err := download.Body.Close(); err != nil && request.Context().Err() == nil {
				logger.Error("close account file download")
			}
		}()
		copyLength, lengthErr := checkedDownloadLength(download.Length)
		if lengthErr != nil || download.Offset > download.TotalSize || download.Length > download.TotalSize-download.Offset ||
			(download.Partial && download.Length == 0) ||
			(!download.Partial && (download.Offset != 0 || download.Length != download.TotalSize)) {
			logger.Error("invalid account file download metadata")
			writeAPIError(w, http.StatusServiceUnavailable, "file_workspace_unavailable",
				"The account file workspace is temporarily unavailable.")
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": download.Name})
		if disposition == "" {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		w.Header().Set("Content-Disposition", disposition)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.FormatUint(download.Length, 10))
		w.Header().Set("Last-Modified", download.ModifiedAt.UTC().Format(http.TimeFormat))
		status := http.StatusOK
		if download.Partial {
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d",
				download.Offset, download.Offset+download.Length-1, download.TotalSize))
		}
		w.WriteHeader(status)
		if download.Length > 0 {
			if _, err := io.CopyN(w, download.Body, copyLength); err != nil && request.Context().Err() == nil {
				logger.Error("stream account file download")
			}
		}
	})
}

func decodeFileMutationJSON(w http.ResponseWriter, request *http.Request, target any) bool {
	if !decodeBoundedJSON(w, request, target, maxFileMutationRequestBytes) {
		return false
	}
	return true
}

func fileMutationContext(
	w http.ResponseWriter, request *http.Request, logger *slog.Logger,
	authenticated core.AuthenticatedSession, accountID core.ID,
) (fileworkspace.MutationContext, bool) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return fileworkspace.MutationContext{}, false
		}
		requestID = generated.String()
	}
	sourceAddress, ok := requestSourceAddress(w, request, logger)
	if !ok {
		return fileworkspace.MutationContext{}, false
	}
	return fileworkspace.MutationContext{Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
		RequestID: requestID, SourceAddress: sourceAddress}, true
}

func parseSingleFileRange(values []string) (*agentprotocol.FileDownloadRange, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) != 1 || values[0] != strings.TrimSpace(values[0]) ||
		!strings.HasPrefix(values[0], "bytes=") || strings.Contains(values[0], ",") {
		return nil, errors.New("invalid file range")
	}
	value := strings.TrimPrefix(values[0], "bytes=")
	parts := strings.Split(value, "-")
	if len(parts) != 2 || (parts[0] == "" && parts[1] == "") {
		return nil, errors.New("invalid file range")
	}
	if parts[0] == "" {
		suffix, err := parseCanonicalRangeUint(parts[1])
		if err != nil || suffix == 0 || suffix > agentprotocol.MaximumFileDownloadBytes {
			return nil, errors.New("invalid file range")
		}
		return &agentprotocol.FileDownloadRange{SuffixLength: &suffix}, nil
	}
	start, err := parseCanonicalRangeUint(parts[0])
	if err != nil {
		return nil, errors.New("invalid file range")
	}
	rangeValue := &agentprotocol.FileDownloadRange{Start: &start}
	if parts[1] != "" {
		end, err := parseCanonicalRangeUint(parts[1])
		if err != nil || end < start || end-start >= agentprotocol.MaximumFileDownloadBytes {
			return nil, errors.New("invalid file range")
		}
		rangeValue.EndInclusive = &end
	}
	return rangeValue, nil
}

func parseCanonicalRangeUint(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(parsed, 10) != value {
		return 0, errors.New("invalid range integer")
	}
	return parsed, nil
}

func checkedDownloadLength(length uint64) (int64, error) {
	if length > agentprotocol.MaximumFileDownloadBytes || length > uint64(^uint64(0)>>1) {
		return 0, errors.New("file download length exceeds the supported signed range")
	}
	return int64(length), nil // #nosec G115 -- both the protocol ceiling and signed range are checked above.
}

func writeFileWorkspaceError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound), errors.Is(err, fileworkspace.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_file_path", "The file path is invalid.")
	case errors.Is(err, fileworkspace.ErrNotReady):
		writeAPIError(w, http.StatusConflict, "file_workspace_not_ready", "The account file workspace is not ready.")
	case errors.Is(err, fileworkspace.ErrConflict):
		writeAPIError(w, http.StatusConflict, "file_workspace_conflict", "The account file workspace conflicts with host state.")
	case errors.Is(err, fileworkspace.ErrUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "file_workspace_unavailable", "The account file workspace is temporarily unavailable.")
	case errors.Is(err, fileworkspace.ErrTooLarge):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "file_download_too_large", "The requested file download is too large.")
	case errors.Is(err, fileworkspace.ErrBusy):
		w.Header().Set("Retry-After", "2")
		writeAPIError(w, http.StatusTooManyRequests, "file_download_busy", "File download capacity is temporarily exhausted.")
	case errors.Is(err, fileworkspace.ErrQuota):
		writeAPIError(w, http.StatusInsufficientStorage, "file_quota_exceeded", "The account storage quota is exhausted.")
	default:
		logger.Error("list account files", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
