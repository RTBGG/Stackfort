// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostfiles

import (
	"context"
	"io"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

type unavailableBackupManager struct{}

func newPlatformBackupManager(string, string, string) platformBackupManager {
	return unavailableBackupManager{}
}

func (unavailableBackupManager) Execute(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, ErrUnavailable
}

func (unavailableBackupManager) Open(context.Context, agentprotocol.BackupDownloadRequest) (Download, error) {
	return Download{}, ErrUnavailable
}

func runPlatformBackupHelper(context.Context, io.Reader, io.Writer) error { return ErrUnavailable }
