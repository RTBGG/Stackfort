// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostfiles

import (
	"context"
	"io"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

type unsupportedDownloader struct{}

func platformDownloadExecutable() string { return "" }

func newPlatformDownloader(string) platformDownloader { return unsupportedDownloader{} }

func (unsupportedDownloader) Open(context.Context, agentprotocol.FileDownloadRequest) (Download, error) {
	return Download{}, ErrUnavailable
}

func runPlatformDownloadHelper(context.Context, io.Reader, io.Writer) error { return ErrUnavailable }
