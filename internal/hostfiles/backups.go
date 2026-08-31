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

const (
	BackupHelperArgument         = "internal-backup-payload-v1"
	DefaultBackupRepositoryRoot  = "/srv/hosting/backups"
	DefaultBackupManifestKeyPath = "/var/lib/stackfort-agent/backup-manifest.key"
)

var (
	ErrIntegrity   = errors.New("managed backup integrity verification failed")
	ErrBackupQuota = errors.New("managed backup repository quota is exhausted")
)

type platformBackupManager interface {
	Execute(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error)
	Open(context.Context, agentprotocol.BackupDownloadRequest) (Download, error)
}

func (manager *BackupManager) OpenDownload(
	ctx context.Context, request agentprotocol.BackupDownloadRequest,
) (Download, error) {
	if manager == nil || manager.platform == nil || ctx == nil || agentprotocol.ValidateBackupDownloadRequest(request) != nil {
		return Download{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Download{}, err
	}
	return manager.platform.Open(ctx, request)
}

type BackupManager struct{ platform platformBackupManager }

func NewBackupManager() *BackupManager {
	return NewBackupManagerWithExecutable(platformWriteExecutable())
}

// NewBackupManagerWithExecutable lets disposable Linux qualification execute
// the separately built production helper.
func NewBackupManagerWithExecutable(executable string) *BackupManager {
	if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable || len(executable) > 4_096 ||
		strings.ContainsRune(executable, 0) {
		executable = ""
	}
	return &BackupManager{platform: newPlatformBackupManager(
		executable, DefaultBackupRepositoryRoot, DefaultBackupManifestKeyPath,
	)}
}

func (manager *BackupManager) Execute(
	ctx context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	if manager == nil || manager.platform == nil || ctx == nil || !isBackupRequest(request) ||
		agentprotocol.ValidateFileWriteRequest(request) != nil {
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return manager.platform.Execute(ctx, request, strings.NewReader(""))
}

func (manager *BackupManager) ExecuteStream(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if manager == nil || manager.platform == nil || ctx == nil || body == nil || !isBackupRequest(request) ||
		agentprotocol.ValidateFileWriteRequest(request) != nil {
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return manager.platform.Execute(ctx, request, body)
}

func isBackupRequest(request agentprotocol.FileWriteRequest) bool {
	switch request.Action {
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

// RunBackupHelper executes the hidden account-credential backup payload mode.
func RunBackupHelper(ctx context.Context, input io.Reader, output, errorOutput io.Writer) error {
	if ctx == nil || input == nil || output == nil || errorOutput == nil {
		return ErrInvalid
	}
	err := runPlatformBackupHelper(ctx, input, output)
	if err != nil {
		code, _ := writeErrorCode(err)
		_, _ = io.WriteString(errorOutput, "stackfort-backup-error:"+string(code)+"\n")
	}
	return err
}
