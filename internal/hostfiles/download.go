// SPDX-License-Identifier: AGPL-3.0-or-later

package hostfiles

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
)

type Download struct {
	TotalSize  uint64
	Offset     uint64
	Length     uint64
	ModifiedAt int64
	Partial    bool
	Body       io.ReadCloser
}

const DownloadHelperArgument = "internal-file-download-v1"

type platformDownloader interface {
	Open(context.Context, agentprotocol.FileDownloadRequest) (Download, error)
}

type Downloader struct{ platform platformDownloader }

func NewDownloader() *Downloader {
	return NewDownloaderWithExecutable(platformDownloadExecutable())
}

// NewDownloaderWithExecutable is exposed for host qualification, where the
// test binary launches the separately built production agent helper.
func NewDownloaderWithExecutable(executable string) *Downloader {
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || len(executable) > 4_096 ||
		strings.ContainsRune(executable, 0) {
		executable = ""
	}
	return &Downloader{platform: newPlatformDownloader(executable)}
}

func (downloader *Downloader) Open(ctx context.Context, request agentprotocol.FileDownloadRequest) (Download, error) {
	if downloader == nil || downloader.platform == nil || ctx == nil ||
		agentprotocol.ValidateFileDownloadRequest(request) != nil ||
		hostingidentity.Validate(request.Identity) != nil {
		return Download{}, ErrInvalid
	}
	normalized, err := hostingpath.NormalizeFileManagerFile(request.Path)
	if err != nil || normalized != request.Path {
		return Download{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	return downloader.platform.Open(ctx, request)
}

// RunDownloadHelper executes the deliberately hidden account-credential mode
// of stackfort-agent. It is reached only through the parent agent process.
func RunDownloadHelper(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil || input == nil || output == nil {
		return ErrInvalid
	}
	return runPlatformDownloadHelper(ctx, input, output)
}

func downloadErrorFromCode(code agentprotocol.ErrorCode) error {
	switch code {
	case agentprotocol.ErrorInvalidRequest:
		return ErrInvalid
	case agentprotocol.ErrorFileNotFound:
		return ErrNotFound
	case agentprotocol.ErrorFileConflict:
		return ErrConflict
	case agentprotocol.ErrorFileDownloadTooLarge:
		return ErrTooLarge
	case agentprotocol.ErrorFileRangeNotSatisfiable:
		return ErrRange
	case agentprotocol.ErrorFileDownloadBusy:
		return ErrBusy
	default:
		return ErrUnavailable
	}
}

func downloadErrorCode(err error) (agentprotocol.ErrorCode, string) {
	switch {
	case errors.Is(err, ErrInvalid):
		return agentprotocol.ErrorInvalidRequest, "The managed file download request is invalid."
	case errors.Is(err, ErrNotFound):
		return agentprotocol.ErrorFileNotFound, "The requested managed file was not found."
	case errors.Is(err, ErrConflict):
		return agentprotocol.ErrorFileConflict, "The managed file conflicts with host state."
	case errors.Is(err, ErrTooLarge):
		return agentprotocol.ErrorFileDownloadTooLarge, "The requested download exceeds the response limit."
	case errors.Is(err, ErrRange):
		return agentprotocol.ErrorFileRangeNotSatisfiable, "The requested byte range is not satisfiable."
	default:
		return agentprotocol.ErrorFileDownloadUnavailable, "The managed file could not be downloaded."
	}
}

func resolveDownloadRange(total uint64, requested *agentprotocol.FileDownloadRange) (uint64, uint64, bool, error) {
	if requested == nil {
		if total > agentprotocol.MaximumFileDownloadBytes {
			return 0, 0, false, ErrTooLarge
		}
		return 0, total, false, nil
	}
	if total == 0 {
		return 0, 0, false, ErrRange
	}
	if requested.SuffixLength != nil {
		length := min(*requested.SuffixLength, total)
		return total - length, length, true, nil
	}
	if requested.Start == nil || *requested.Start >= total {
		return 0, 0, false, ErrRange
	}
	end := total - 1
	if requested.EndInclusive != nil {
		end = min(end, *requested.EndInclusive)
	}
	length := end - *requested.Start + 1
	if length > agentprotocol.MaximumFileDownloadBytes {
		return 0, 0, false, ErrTooLarge
	}
	return *requested.Start, length, true, nil
}

func checkedSignedDownloadValue(value uint64) (int64, error) {
	if value > uint64(^uint64(0)>>1) {
		return 0, ErrUnavailable
	}
	return int64(value), nil // #nosec G115 -- the signed range is checked above.
}
