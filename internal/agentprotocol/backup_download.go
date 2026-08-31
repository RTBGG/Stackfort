// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const BackupDownloadEndpoint = "/stream/v1/backups/download"

type BackupDownloadRequest struct {
	ProtocolVersion int                  `json:"protocolVersion"`
	RequestID       string               `json:"requestId"`
	Identity        hostingidentity.Spec `json:"identity"`
	BackupID        string               `json:"backupId"`
	Range           *FileDownloadRange   `json:"range,omitempty"`
}

func ValidateBackupDownloadRequest(request BackupDownloadRequest) error {
	if request.ProtocolVersion != WireVersion || !validBoundedIdentifier(request.RequestID) ||
		hostingidentity.Validate(request.Identity) != nil || !validFileUploadID(request.BackupID) ||
		ValidateFileDownloadRange(request.Range) != nil {
		return errors.New("backup download request is invalid")
	}
	return nil
}

func DecodeBackupDownloadRequest(reader io.Reader) (BackupDownloadRequest, error) {
	var request BackupDownloadRequest
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return BackupDownloadRequest{}, fmt.Errorf("decode backup download request: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return BackupDownloadRequest{}, err
	}
	if err := ValidateBackupDownloadRequest(request); err != nil {
		return BackupDownloadRequest{}, err
	}
	return request, nil
}

func DecodeBackupDownloadErrorResponse(reader io.Reader, requestID string) (FileDownloadErrorResponse, error) {
	var response FileDownloadErrorResponse
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return FileDownloadErrorResponse{}, err
	}
	if err := requireJSONEnd(decoder); err != nil {
		return FileDownloadErrorResponse{}, err
	}
	if response.ProtocolVersion != WireVersion || response.RequestID != requestID || !validBoundedIdentifier(response.RequestID) ||
		response.Error.Message == "" || len(response.Error.Message) > 256 || response.Error.Capability != nil ||
		!validBackupDownloadErrorCode(response.Error.Code) {
		return FileDownloadErrorResponse{}, errors.New("backup download error response is invalid")
	}
	return response, nil
}

func validBackupDownloadErrorCode(code ErrorCode) bool {
	return code == ErrorInvalidRequest || code == ErrorBackupNotFound || code == ErrorBackupConflict ||
		code == ErrorBackupUnavailable || code == ErrorBackupTooLarge || code == ErrorBackupBusy ||
		code == ErrorBackupIntegrity || code == ErrorFileRangeNotSatisfiable || code == ErrorInternal
}
