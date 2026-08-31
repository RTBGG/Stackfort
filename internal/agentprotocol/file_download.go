// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
)

const (
	FileDownloadEndpoint        = "/stream/v1/files/download"
	MaxFileDownloadRequestBytes = 8 << 10
	MaxFileDownloadErrorBytes   = 4 << 10
	MaximumFileDownloadBytes    = uint64(4 << 30)
	MaximumDownloadFrameBytes   = 4 << 10
	MaximumFileDownloadDuration = 30 * time.Minute
)

// FileDownloadRange is a closed single-range union. Start may be combined
// with EndInclusive; SuffixLength is mutually exclusive with both.
type FileDownloadRange struct {
	Start        *uint64 `json:"start,omitempty"`
	EndInclusive *uint64 `json:"endInclusive,omitempty"`
	SuffixLength *uint64 `json:"suffixLength,omitempty"`
}

type FileDownloadRequest struct {
	ProtocolVersion int                  `json:"protocolVersion"`
	RequestID       string               `json:"requestId"`
	Identity        hostingidentity.Spec `json:"identity"`
	Path            string               `json:"path"`
	Range           *FileDownloadRange   `json:"range,omitempty"`
}

type FileDownloadErrorResponse struct {
	ProtocolVersion int           `json:"protocolVersion"`
	RequestID       string        `json:"requestId"`
	Error           ResponseError `json:"error"`
}

// FileDownloadFrame is the bounded child-to-agent prelude. Raw file bytes
// follow its terminating newline only when Error is nil.
type FileDownloadFrame struct {
	TotalSize  uint64         `json:"totalSize"`
	Offset     uint64         `json:"offset"`
	Length     uint64         `json:"length"`
	ModifiedAt time.Time      `json:"modifiedAt"`
	Partial    bool           `json:"partial"`
	Error      *ResponseError `json:"error,omitempty"`
}

func ValidateFileDownloadRequest(request FileDownloadRequest) error {
	if request.ProtocolVersion != WireVersion || !validBoundedIdentifier(request.RequestID) ||
		hostingidentity.Validate(request.Identity) != nil {
		return errors.New("file download request is invalid")
	}
	normalized, err := hostingpath.NormalizeFileManagerFile(request.Path)
	if err != nil || normalized != request.Path || ValidateFileDownloadRange(request.Range) != nil {
		return errors.New("file download request is invalid")
	}
	return nil
}

func DecodeFileDownloadRequest(reader io.Reader) (FileDownloadRequest, error) {
	var request FileDownloadRequest
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return FileDownloadRequest{}, fmt.Errorf("decode file download request: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return FileDownloadRequest{}, err
	}
	if err := ValidateFileDownloadRequest(request); err != nil {
		return FileDownloadRequest{}, err
	}
	return request, nil
}

func ValidateFileDownloadFrame(frame FileDownloadFrame) error {
	if frame.Error != nil {
		if frame.Error.Message == "" || len(frame.Error.Message) > 256 || !validFileDownloadErrorCode(frame.Error.Code) ||
			frame.Error.Capability != nil || frame.Offset != 0 || frame.Length != 0 || frame.Partial ||
			(!frame.ModifiedAt.IsZero()) {
			return errors.New("file download error frame is invalid")
		}
		return nil
	}
	if frame.ModifiedAt.IsZero() || frame.ModifiedAt.Location() != time.UTC ||
		frame.Offset > frame.TotalSize || frame.Length > MaximumFileDownloadBytes ||
		frame.Length > frame.TotalSize-frame.Offset || (frame.Partial && frame.Length == 0) ||
		(!frame.Partial && (frame.Offset != 0 || frame.Length != frame.TotalSize)) {
		return errors.New("file download frame is invalid")
	}
	return nil
}

func DecodeFileDownloadFrame(reader io.Reader) (FileDownloadFrame, error) {
	var frame FileDownloadFrame
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frame); err != nil {
		return FileDownloadFrame{}, err
	}
	if err := requireJSONEnd(decoder); err != nil {
		return FileDownloadFrame{}, err
	}
	if err := ValidateFileDownloadFrame(frame); err != nil {
		return FileDownloadFrame{}, err
	}
	return frame, nil
}

func ValidateFileDownloadErrorResponse(response FileDownloadErrorResponse, requestID string) error {
	if response.ProtocolVersion != WireVersion || response.RequestID != requestID ||
		!validBoundedIdentifier(response.RequestID) || response.Error.Message == "" ||
		len(response.Error.Message) > 256 || response.Error.Capability != nil ||
		!validFileDownloadErrorCode(response.Error.Code) {
		return errors.New("file download error response is invalid")
	}
	return nil
}

func DecodeFileDownloadErrorResponse(reader io.Reader, requestID string) (FileDownloadErrorResponse, error) {
	var response FileDownloadErrorResponse
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return FileDownloadErrorResponse{}, err
	}
	if err := requireJSONEnd(decoder); err != nil {
		return FileDownloadErrorResponse{}, err
	}
	if err := ValidateFileDownloadErrorResponse(response, requestID); err != nil {
		return FileDownloadErrorResponse{}, err
	}
	return response, nil
}

func ValidateFileDownloadRange(value *FileDownloadRange) error {
	if value == nil {
		return nil
	}
	if value.SuffixLength != nil {
		if value.Start != nil || value.EndInclusive != nil || *value.SuffixLength == 0 ||
			*value.SuffixLength > MaximumFileDownloadBytes {
			return errors.New("file download range is invalid")
		}
		return nil
	}
	if value.Start == nil || (value.EndInclusive != nil && (*value.EndInclusive < *value.Start ||
		*value.EndInclusive-*value.Start >= MaximumFileDownloadBytes)) {
		return errors.New("file download range is invalid")
	}
	return nil
}

func validFileDownloadErrorCode(code ErrorCode) bool {
	return code == ErrorInvalidRequest || code == ErrorFileNotFound || code == ErrorFileConflict ||
		code == ErrorFileDownloadUnavailable || code == ErrorFileDownloadTooLarge ||
		code == ErrorFileRangeNotSatisfiable || code == ErrorFileDownloadBusy || code == ErrorInternal
}
