// SPDX-License-Identifier: AGPL-3.0-or-later

package hostfiles

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

const WriteHelperArgument = "internal-file-write-v1"

type platformWriter interface {
	Execute(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error)
}

type Writer struct{ platform platformWriter }

func NewWriter() *Writer { return NewWriterWithExecutable(platformWriteExecutable()) }

// NewWriterWithExecutable is used by Linux host qualification to exercise the
// separately built production helper instead of the integration-test binary.
func NewWriterWithExecutable(executable string) *Writer {
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || len(executable) > 4_096 ||
		strings.ContainsRune(executable, 0) {
		executable = ""
	}
	return &Writer{platform: newPlatformWriter(executable)}
}

func (writer *Writer) Execute(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if writer == nil || writer.platform == nil || ctx == nil || body == nil ||
		agentprotocol.ValidateFileWriteRequest(request) != nil {
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return writer.platform.Execute(ctx, request, body)
}

// RunWriteHelper executes the hidden account-credential mode of the agent.
func RunWriteHelper(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil || input == nil || output == nil {
		return ErrInvalid
	}
	return runPlatformWriteHelper(ctx, input, output)
}

func writeErrorCode(err error) (agentprotocol.ErrorCode, string) {
	switch {
	case errors.Is(err, ErrInvalid):
		return agentprotocol.ErrorInvalidRequest, "The managed file mutation request is invalid."
	case errors.Is(err, ErrNotFound):
		return agentprotocol.ErrorFileNotFound, "The managed upload or target directory was not found."
	case errors.Is(err, ErrConflict):
		return agentprotocol.ErrorFileConflict, "The managed file mutation conflicts with host state."
	case errors.Is(err, ErrTooLarge):
		return agentprotocol.ErrorFileDownloadTooLarge, "The managed upload exceeds the supported size."
	case errors.Is(err, ErrBusy):
		return agentprotocol.ErrorFileDownloadBusy, "Managed file mutation capacity is temporarily exhausted."
	case errors.Is(err, ErrQuota):
		return agentprotocol.ErrorFileQuotaExceeded, "The managed account storage quota is exhausted."
	case errors.Is(err, ErrIntegrity):
		return agentprotocol.ErrorBackupIntegrity, "The managed backup failed integrity verification."
	default:
		return agentprotocol.ErrorFileUnavailable, "The managed file mutation could not be completed."
	}
}

func writeErrorFromCode(code agentprotocol.ErrorCode) error {
	switch code {
	case agentprotocol.ErrorInvalidRequest:
		return ErrInvalid
	case agentprotocol.ErrorFileNotFound:
		return ErrNotFound
	case agentprotocol.ErrorFileConflict:
		return ErrConflict
	case agentprotocol.ErrorFileDownloadTooLarge:
		return ErrTooLarge
	case agentprotocol.ErrorFileDownloadBusy:
		return ErrBusy
	case agentprotocol.ErrorFileQuotaExceeded:
		return ErrQuota
	case agentprotocol.ErrorBackupIntegrity:
		return ErrIntegrity
	default:
		return ErrUnavailable
	}
}
