// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	backupPayloadName          = "payload.tar.gz"
	backupManifestName         = "manifest.json"
	backupUploadMetadataName   = "upload.json"
	backupUploadPayloadName    = "payload.part"
	backupRepositoryLockName   = ".lock"
	backupManifestKeyBytes     = 32
	maximumBackupManifestBytes = 8 << 10
	maximumBackupHelperBytes   = 8 << 10
	maximumBackupTarOverhead   = uint64(64 << 20)
	backupHelperErrorPrefix    = "stackfort-backup-error:"
	backupHelperCreate         = "create"
	backupHelperRestore        = "restore"
	backupRestorePreviousName  = "previous"
	backupUploadSchemaVersion  = 1
)

type linuxBackupManager struct {
	executable     string
	repositoryRoot string
	keyPath        string
	now            func() time.Time
}

type backupManifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	BackupID      string                    `json:"backupId"`
	AccountID     string                    `json:"accountId"`
	Scope         agentprotocol.BackupScope `json:"scope"`
	SourcePath    string                    `json:"sourcePath,omitempty"`
	CreatedAt     time.Time                 `json:"createdAt"`
	PayloadBytes  uint64                    `json:"payloadBytes"`
	ContentBytes  uint64                    `json:"contentBytes"`
	EntryCount    uint64                    `json:"entryCount"`
	PayloadSHA256 string                    `json:"payloadSha256"`
	Signature     string                    `json:"signature"`
}

type unsignedBackupManifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	BackupID      string                    `json:"backupId"`
	AccountID     string                    `json:"accountId"`
	Scope         agentprotocol.BackupScope `json:"scope"`
	SourcePath    string                    `json:"sourcePath,omitempty"`
	CreatedAt     time.Time                 `json:"createdAt"`
	PayloadBytes  uint64                    `json:"payloadBytes"`
	ContentBytes  uint64                    `json:"contentBytes"`
	EntryCount    uint64                    `json:"entryCount"`
	PayloadSHA256 string                    `json:"payloadSha256"`
}

type backupHelperControl struct {
	ProtocolVersion      int                       `json:"protocolVersion"`
	Mode                 string                    `json:"mode"`
	Identity             hostingidentity.Spec      `json:"identity"`
	Scope                agentprotocol.BackupScope `json:"scope"`
	SourcePath           string                    `json:"sourcePath,omitempty"`
	OperationID          string                    `json:"operationId,omitempty"`
	PayloadBytes         uint64                    `json:"payloadBytes,omitempty"`
	ExpectedEntries      uint64                    `json:"expectedEntries,omitempty"`
	ExpectedContentBytes uint64                    `json:"expectedContentBytes,omitempty"`
}

type backupHelperResult struct {
	OperationID  string `json:"operationId"`
	EntryCount   uint64 `json:"entryCount"`
	ContentBytes uint64 `json:"contentBytes"`
	Completed    bool   `json:"completed"`
}

type backupUploadMetadata struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	UploadID       string                    `json:"uploadId"`
	AccountID      string                    `json:"accountId"`
	Scope          agentprotocol.BackupScope `json:"scope"`
	SourcePath     string                    `json:"sourcePath,omitempty"`
	SizeBytes      uint64                    `json:"sizeBytes"`
	ExpectedSHA256 string                    `json:"expectedSha256"`
	ReceivedBytes  uint64                    `json:"receivedBytes"`
	CreatedAt      time.Time                 `json:"createdAt"`
}

type backupDownloadBody struct {
	file       *os.File
	directory  int
	repository int
	unlock     func()
	once       sync.Once
}

func (body *backupDownloadBody) Read(content []byte) (int, error) { return body.file.Read(content) }

func (body *backupDownloadBody) Close() error {
	var result error
	body.once.Do(func() {
		result = body.file.Close()
		_ = unix.Close(body.directory)
		body.unlock()
		_ = unix.Close(body.repository)
	})
	return result
}

func newPlatformBackupManager(executable, repositoryRoot, keyPath string) platformBackupManager {
	return &linuxBackupManager{executable: executable, repositoryRoot: repositoryRoot, keyPath: keyPath, now: time.Now}
}

func (manager *linuxBackupManager) Execute(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if manager == nil || manager.executable == "" || os.Geteuid() != 0 {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	switch request.Action {
	case agentprotocol.FileWriteBackupCreate:
		return manager.create(ctx, request)
	case agentprotocol.FileWriteBackupList:
		return manager.list(ctx, request)
	case agentprotocol.FileWriteBackupInspect:
		return manager.inspect(ctx, request, false)
	case agentprotocol.FileWriteBackupVerify:
		return manager.inspect(ctx, request, true)
	case agentprotocol.FileWriteBackupRestore:
		return manager.restore(ctx, request)
	case agentprotocol.FileWriteBackupUploadInitiate:
		return manager.initiateUpload(ctx, request)
	case agentprotocol.FileWriteBackupUploadStatus:
		return manager.uploadStatus(ctx, request)
	case agentprotocol.FileWriteBackupUploadChunk:
		return manager.uploadChunk(ctx, request, body)
	case agentprotocol.FileWriteBackupUploadComplete:
		return manager.completeUpload(ctx, request)
	case agentprotocol.FileWriteBackupUploadCancel:
		return manager.cancelUpload(ctx, request)
	case agentprotocol.FileWriteBackupDelete:
		return manager.delete(ctx, request)
	default:
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
}

func (manager *linuxBackupManager) Open(
	ctx context.Context, request agentprotocol.BackupDownloadRequest,
) (Download, error) {
	if manager == nil || manager.executable == "" || os.Geteuid() != 0 {
		return Download{}, ErrUnavailable
	}
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return Download{}, err
	}
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		_ = unix.Close(repository)
		return Download{}, err
	}
	fail := func() {
		unlock()
		_ = unix.Close(repository)
	}
	key, err := manager.loadManifestKey(false)
	if err != nil {
		fail()
		return Download{}, err
	}
	record, err := readLocalBackup(ctx, repository, device, request.Identity.AccountID, request.BackupID, key, true)
	clear(key)
	if err != nil {
		fail()
		return Download{}, err
	}
	offset, length, partial, rangeErr := resolveDownloadRange(record.PayloadBytes, request.Range)
	if rangeErr != nil {
		fail()
		return Download{TotalSize: record.PayloadBytes}, rangeErr
	}
	directory, err := openRootBackupDirectory(repository, request.BackupID, device)
	if err != nil {
		fail()
		return Download{}, err
	}
	descriptor, err := unix.Openat(directory, backupPayloadName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		_ = unix.Close(directory)
		fail()
		return Download{}, classifyRootBackupError(err)
	}
	file := os.NewFile(uintptr(descriptor), "backup-download-payload")
	if err := validateUploadPayloadDescriptor(descriptor, device, record.PayloadBytes); err != nil {
		_ = file.Close()
		_ = unix.Close(directory)
		fail()
		return Download{}, err
	}
	if _, err := file.Seek(int64(offset), io.SeekStart); err != nil { // #nosec G115 -- backup sizes are capped at 4 GiB.
		_ = file.Close()
		_ = unix.Close(directory)
		fail()
		return Download{}, ErrUnavailable
	}
	body := &backupDownloadBody{file: file, directory: directory, repository: repository, unlock: unlock}
	return Download{TotalSize: record.PayloadBytes, Offset: offset, Length: length, Partial: partial,
		ModifiedAt: record.CreatedAt.Unix(), Body: body}, nil
}

func (manager *linuxBackupManager) create(
	ctx context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	key, err := manager.loadManifestKey(true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer clear(key)
	if record, existingErr := readLocalBackup(ctx, repository, device, request.Identity.AccountID,
		request.BackupID, key, true); existingErr == nil {
		if record.Scope != request.BackupScope || record.SourcePath != request.BackupPath {
			return agentprotocol.FileWriteResult{}, ErrConflict
		}
		return attachBackupRepositoryStatus(agentprotocol.FileWriteResult{Backup: &record, Completed: true},
			repository, device, request)
	} else if !errors.Is(existingErr, ErrNotFound) {
		return agentprotocol.FileWriteResult{}, existingErr
	}
	if err := enforceLocalBackupLimit(repository); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	stagingName := backupStagingName(request.BackupID)
	if err := unix.Mkdirat(repository, stagingName, 0o700); err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	staging, err := openRootBackupDirectory(repository, stagingName, device)
	if err != nil {
		_ = unix.Unlinkat(repository, stagingName, unix.AT_REMOVEDIR)
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeRootBackupTreeAt(repository, stagingName, device, 0)
		}
	}()
	payloadDescriptor, err := unix.Openat(staging, backupPayloadName,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	payload := os.NewFile(uintptr(payloadDescriptor), "local-backup-payload")
	payloadDigest := sha256.New()
	sink := &boundedArchiveWriter{writer: io.MultiWriter(payload, payloadDigest), remaining: agentprotocol.MaximumFileUploadBytes}
	control := backupHelperControl{ProtocolVersion: agentprotocol.WireVersion, Mode: backupHelperCreate,
		Identity: request.Identity, Scope: request.BackupScope, SourcePath: request.BackupPath}
	if err := manager.runCreateHelper(ctx, control, sink); err != nil {
		_ = payload.Close()
		return agentprotocol.FileWriteResult{}, err
	}
	if payload.Sync() != nil {
		_ = payload.Close()
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	status, err := payload.Stat()
	if err != nil || status.Size() <= 0 || uint64(status.Size()) > agentprotocol.MaximumFileUploadBytes { // #nosec G115 -- size is positive before conversion.
		_ = payload.Close()
		return agentprotocol.FileWriteResult{}, ErrTooLarge
	}
	payloadBytes := uint64(status.Size()) // #nosec G115 -- size is positive above.
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		_ = payload.Close()
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	entries, contentBytes, err := inspectBackupPayload(ctx, payload, int64(payloadBytes)) // #nosec G115 -- payload is capped at 4 GiB on amd64.
	if err != nil {
		_ = payload.Close()
		return agentprotocol.FileWriteResult{}, err
	}
	if payload.Close() != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	manifest := backupManifest{SchemaVersion: agentprotocol.BackupManifestSchemaVersion,
		BackupID: request.BackupID, AccountID: request.Identity.AccountID, Scope: request.BackupScope,
		SourcePath: request.BackupPath, CreatedAt: manager.now().UTC(), PayloadBytes: payloadBytes,
		ContentBytes: contentBytes, EntryCount: entries, PayloadSHA256: hex.EncodeToString(payloadDigest.Sum(nil))}
	encoded, err := signBackupManifest(manifest, key)
	if err != nil || len(encoded) > maximumBackupManifestBytes {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := writeRootBackupFile(staging, backupManifestName, encoded, device); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	repositoryStatus, err := measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if repositoryStatus.UsedBytes > repositoryStatus.LimitBytes {
		return agentprotocol.FileWriteResult{}, ErrBackupQuota
	}
	if unix.Fsync(staging) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Renameat2(repository, stagingName, repository, request.BackupID, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	if unix.Fsync(repository) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	cleanup = false
	manifestDigest := sha256.Sum256(encoded)
	record := manifest.record(hex.EncodeToString(manifestDigest[:]), true)
	return attachBackupRepositoryStatus(agentprotocol.FileWriteResult{Backup: &record, Completed: true},
		repository, device, request)
}

func (manager *linuxBackupManager) list(
	ctx context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if errors.Is(err, ErrNotFound) {
		status := agentprotocol.BackupRepositoryStatus{LimitBytes: effectiveBackupRepositoryLimit(request.BackupLimitBytes),
			MaximumBackups: agentprotocol.MaximumLocalBackups}
		return agentprotocol.FileWriteResult{Backups: []agentprotocol.BackupRecord{}, BackupRepository: &status}, nil
	}
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	names, err := localBackupNames(repository)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if request.Cursor != "" {
		filtered := names[:0]
		for _, name := range names {
			if name < request.Cursor {
				filtered = append(filtered, name)
			}
		}
		names = filtered
	}
	if len(names) == 0 {
		return attachBackupRepositoryStatus(agentprotocol.FileWriteResult{Backups: []agentprotocol.BackupRecord{}},
			repository, device, request)
	}
	key, err := manager.loadManifestKey(false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer clear(key)
	count := len(names)
	if count > agentprotocol.MaximumBackupListEntries {
		count = agentprotocol.MaximumBackupListEntries
	}
	records := make([]agentprotocol.BackupRecord, 0, count)
	for _, name := range names[:count] {
		record, err := readLocalBackup(ctx, repository, device, request.Identity.AccountID, name, key, false)
		if err != nil {
			return agentprotocol.FileWriteResult{}, err
		}
		records = append(records, record)
	}
	result := agentprotocol.FileWriteResult{Backups: records}
	if len(names) > count {
		result.Next = records[len(records)-1].BackupID
	}
	return attachBackupRepositoryStatus(result, repository, device, request)
}

func (manager *linuxBackupManager) inspect(
	ctx context.Context, request agentprotocol.FileWriteRequest, verify bool,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	key, err := manager.loadManifestKey(false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer clear(key)
	record, err := readLocalBackup(ctx, repository, device, request.Identity.AccountID, request.BackupID, key, verify)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return attachBackupRepositoryStatus(agentprotocol.FileWriteResult{Backup: &record}, repository, device, request)
}

func (manager *linuxBackupManager) restore(
	ctx context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	key, err := manager.loadManifestKey(false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer clear(key)
	record, err := readLocalBackup(ctx, repository, device, request.Identity.AccountID, request.BackupID, key, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	backupDirectory, err := openRootBackupDirectory(repository, request.BackupID, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(backupDirectory)
	payloadDescriptor, err := unix.Openat(backupDirectory, backupPayloadName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	payload := os.NewFile(uintptr(payloadDescriptor), "local-backup-restore-payload")
	defer payload.Close()
	control := backupHelperControl{ProtocolVersion: agentprotocol.WireVersion, Mode: backupHelperRestore,
		Identity: request.Identity, Scope: record.Scope, SourcePath: record.SourcePath,
		OperationID: request.OperationID, PayloadBytes: record.PayloadBytes,
		ExpectedEntries: record.EntryCount, ExpectedContentBytes: record.ContentBytes}
	helperResult, err := manager.runRestoreHelper(ctx, control, payload)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if !helperResult.Completed || helperResult.OperationID != request.OperationID || helperResult.EntryCount != record.EntryCount ||
		helperResult.ContentBytes != record.ContentBytes {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	result := agentprotocol.FileWriteResult{OperationID: request.OperationID, Backup: &record, Completed: true}
	return attachBackupRepositoryStatus(result, repository, device, request)
}

func (manager *linuxBackupManager) initiateUpload(
	ctx context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	status, err := measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if status.ActiveUploads >= agentprotocol.MaximumBackupUploads || status.BackupCount >= agentprotocol.MaximumLocalBackups {
		return agentprotocol.FileWriteResult{}, ErrBusy
	}
	if status.UsedBytes > status.LimitBytes || request.SizeBytes > status.LimitBytes-status.UsedBytes {
		return agentprotocol.FileWriteResult{}, ErrBackupQuota
	}
	uploadName := backupUploadName(request.UploadID)
	if err := unix.Mkdirat(repository, uploadName, 0o700); err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeRootBackupTreeAt(repository, uploadName, device, 0)
		}
	}()
	directory, err := openRootBackupDirectory(repository, uploadName, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(directory)
	metadata := backupUploadMetadata{SchemaVersion: backupUploadSchemaVersion, UploadID: request.UploadID,
		AccountID: request.Identity.AccountID, Scope: request.BackupScope, SourcePath: request.BackupPath,
		SizeBytes: request.SizeBytes, ExpectedSHA256: request.ExpectedSHA256, CreatedAt: manager.now().UTC()}
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) > maximumBackupManifestBytes {
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
	if err := writeRootBackupFile(directory, backupUploadMetadataName, encoded, device); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	descriptor, err := unix.Openat(directory, backupUploadPayloadName,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	file := os.NewFile(uintptr(descriptor), "backup-upload-payload")
	if unix.Ftruncate(descriptor, int64(request.SizeBytes)) != nil || file.Sync() != nil || file.Close() != nil || unix.Fsync(directory) != nil { // #nosec G115 -- upload size is capped at 4 GiB.
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	status, err = measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if status.UsedBytes > status.LimitBytes {
		return agentprotocol.FileWriteResult{}, ErrBackupQuota
	}
	cleanup = false
	return backupUploadResult(metadata, status), nil
}

func (manager *linuxBackupManager) uploadStatus(
	_ context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	metadata, err := readBackupUpload(repository, device, request.Identity.AccountID, request.UploadID)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	status, err := measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return backupUploadResult(metadata, status), nil
}

func (manager *linuxBackupManager) uploadChunk(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	metadata, directory, err := openBackupUpload(repository, device, request.Identity.AccountID, request.UploadID)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(directory)
	if metadata.ReceivedBytes != request.Offset || request.ChunkLength > metadata.SizeBytes-metadata.ReceivedBytes {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	descriptor, err := unix.Openat(directory, backupUploadPayloadName,
		unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	file := os.NewFile(uintptr(descriptor), "backup-upload-chunk")
	defer file.Close()
	if err := validateUploadPayloadDescriptor(descriptor, device, metadata.SizeBytes); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if _, err := file.Seek(int64(request.Offset), io.SeekStart); err != nil { // #nosec G115 -- upload offsets are capped at 4 GiB.
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	written, err := io.CopyN(file, &contextReader{ctx: ctx, reader: body}, int64(request.ChunkLength)) // #nosec G115 -- chunks are capped at 8 MiB.
	if err != nil || uint64(written) != request.ChunkLength || file.Sync() != nil {                    // #nosec G115 -- written is non-negative on success.
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	metadata.ReceivedBytes += request.ChunkLength
	if err := replaceBackupUploadMetadata(directory, device, metadata); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	status, err := measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return backupUploadResult(metadata, status), nil
}

func (manager *linuxBackupManager) completeUpload(
	ctx context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	metadata, directory, err := openBackupUpload(repository, device, request.Identity.AccountID, request.UploadID)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(directory)
	if metadata.Scope != request.BackupScope || metadata.SourcePath != request.BackupPath ||
		metadata.SizeBytes != request.SizeBytes || metadata.ExpectedSHA256 != request.ExpectedSHA256 ||
		metadata.ReceivedBytes != metadata.SizeBytes {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	descriptor, err := unix.Openat(directory, backupUploadPayloadName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	payload := os.NewFile(uintptr(descriptor), "backup-upload-complete")
	defer payload.Close()
	if err := validateUploadPayloadDescriptor(descriptor, device, metadata.SizeBytes); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, &contextReader{ctx: ctx, reader: payload}); err != nil {
		return agentprotocol.FileWriteResult{}, ErrIntegrity
	}
	payloadSHA256 := hex.EncodeToString(digest.Sum(nil))
	if metadata.ExpectedSHA256 != "" && payloadSHA256 != metadata.ExpectedSHA256 {
		return agentprotocol.FileWriteResult{}, ErrIntegrity
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	entries, contentBytes, err := inspectBackupPayload(ctx, payload, int64(metadata.SizeBytes)) // #nosec G115 -- upload is capped at 4 GiB.
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	key, err := manager.loadManifestKey(true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer clear(key)
	manifest := backupManifest{SchemaVersion: agentprotocol.BackupManifestSchemaVersion, BackupID: metadata.UploadID,
		AccountID: metadata.AccountID, Scope: metadata.Scope, SourcePath: metadata.SourcePath, CreatedAt: manager.now().UTC(),
		PayloadBytes: metadata.SizeBytes, ContentBytes: contentBytes, EntryCount: entries, PayloadSHA256: payloadSHA256}
	encoded, err := signBackupManifest(manifest, key)
	if err != nil || len(encoded) > maximumBackupManifestBytes {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := writeRootBackupFile(directory, backupManifestName, encoded, device); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	repositoryStatus, err := measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		_ = unix.Unlinkat(directory, backupManifestName, 0)
		return agentprotocol.FileWriteResult{}, err
	}
	if repositoryStatus.UsedBytes > repositoryStatus.LimitBytes {
		_ = unix.Unlinkat(directory, backupManifestName, 0)
		return agentprotocol.FileWriteResult{}, ErrBackupQuota
	}
	if err := unix.Renameat2(directory, backupUploadPayloadName, directory, backupPayloadName, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	if err := unix.Unlinkat(directory, backupUploadMetadataName, 0); err != nil || unix.Fsync(directory) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Renameat2(repository, backupUploadName(metadata.UploadID), repository, metadata.UploadID, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	if unix.Fsync(repository) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	manifestDigest := sha256.Sum256(encoded)
	record := manifest.record(hex.EncodeToString(manifestDigest[:]), true)
	result := agentprotocol.FileWriteResult{Backup: &record, Completed: true}
	return attachBackupRepositoryStatus(result, repository, device, request)
}

func (manager *linuxBackupManager) cancelUpload(
	_ context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	if _, err := readBackupUpload(repository, device, request.Identity.AccountID, request.UploadID); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if err := removeRootBackupTreeAt(repository, backupUploadName(request.UploadID), device, 0); err != nil || unix.Fsync(repository) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	result := agentprotocol.FileWriteResult{UploadID: request.UploadID, Completed: true}
	return attachBackupRepositoryStatus(result, repository, device, request)
}

func (manager *linuxBackupManager) delete(
	_ context.Context, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	repository, device, err := manager.openAccountRepository(request.Identity.AccountID, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(repository)
	unlock, err := lockBackupRepository(repository, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unlock()
	directory, err := openRootBackupDirectory(repository, request.BackupID, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	_ = unix.Close(directory)
	deleting := backupDeletingName(request.BackupID)
	if err := unix.Renameat2(repository, request.BackupID, repository, deleting, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyRootBackupError(err)
	}
	if unix.Fsync(repository) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := removeRootBackupTreeAt(repository, deleting, device, 0); err != nil || unix.Fsync(repository) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return attachBackupRepositoryStatus(agentprotocol.FileWriteResult{Completed: true}, repository, device, request)
}

func (manager *linuxBackupManager) runCreateHelper(
	ctx context.Context, control backupHelperControl, output io.Writer,
) error {
	encoded, err := json.Marshal(control)
	if err != nil || len(encoded) > maximumBackupHelperBytes {
		return ErrInvalid
	}
	command := exec.CommandContext(ctx, manager.executable, BackupHelperArgument) // #nosec G204 -- the executable is the running agent or qualification-owned binary.
	command.Stdin = bytes.NewReader(append(encoded, '\n'))
	command.Stdout = output
	var errorOutput boundedBackupError
	command.Stderr = &errorOutput
	command.Env = []string{"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.SysProcAttr = accountBackupCredential(control.Identity)
	if err := command.Run(); err != nil {
		return classifyBackupHelperFailure(ctx, errorOutput.String())
	}
	return nil
}

func (manager *linuxBackupManager) runRestoreHelper(
	ctx context.Context, control backupHelperControl, payload *os.File,
) (backupHelperResult, error) {
	encoded, err := json.Marshal(control)
	if err != nil || len(encoded) > maximumBackupHelperBytes {
		return backupHelperResult{}, ErrInvalid
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return backupHelperResult{}, ErrUnavailable
	}
	command := exec.CommandContext(ctx, manager.executable, BackupHelperArgument)                                                // #nosec G204 -- the executable is the running agent or qualification-owned binary.
	command.Stdin = io.MultiReader(bytes.NewReader(append(encoded, '\n')), io.LimitReader(payload, int64(control.PayloadBytes))) // #nosec G115 -- payload is capped at 4 GiB.
	var output, errorOutput boundedBackupError
	command.Stdout, command.Stderr = &output, &errorOutput
	command.Env = []string{"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.SysProcAttr = accountBackupCredential(control.Identity)
	if err := command.Run(); err != nil {
		return backupHelperResult{}, classifyBackupHelperFailure(ctx, errorOutput.String())
	}
	var result backupHelperResult
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || requireBackupJSONEnd(decoder) != nil {
		return backupHelperResult{}, ErrUnavailable
	}
	return result, nil
}

func accountBackupCredential(identity hostingidentity.Spec) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: identity.UID, Gid: identity.GID, NoSetGroups: true,
	}, Pdeathsig: syscall.SIGKILL}
}

type boundedBackupError struct{ bytes.Buffer }

func (buffer *boundedBackupError) Write(content []byte) (int, error) {
	original := len(content)
	remaining := maximumBackupHelperBytes - buffer.Len()
	if remaining > 0 {
		if len(content) > remaining {
			content = content[:remaining]
		}
		_, _ = buffer.Buffer.Write(content)
	}
	return original, nil
}

func classifyBackupHelperFailure(ctx context.Context, output string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	line := strings.TrimSpace(output)
	if !strings.HasPrefix(line, backupHelperErrorPrefix) || strings.Contains(line, "\n") {
		return ErrUnavailable
	}
	return writeErrorFromCode(agentprotocol.ErrorCode(strings.TrimPrefix(line, backupHelperErrorPrefix)))
}

func (manager *linuxBackupManager) openAccountRepository(accountID string, create bool) (int, uint64, error) {
	root, device, err := openRootOwnedAbsoluteDirectory(manager.repositoryRoot)
	if err != nil {
		return -1, 0, err
	}
	defer unix.Close(root)
	if create {
		if err := unix.Mkdirat(root, accountID, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, 0, classifyRootBackupError(err)
		}
	}
	directory, err := unix.Openat(root, accountID, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, 0, classifyRootBackupError(err)
	}
	var status unix.Stat_t
	if unix.Fstat(directory, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o700 || uint64(status.Dev) != device {
		_ = unix.Close(directory)
		return -1, 0, ErrConflict
	}
	return directory, device, nil
}

func openRootOwnedAbsoluteDirectory(path string) (int, uint64, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return -1, 0, ErrInvalid
	}
	current, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, 0, ErrUnavailable
	}
	for _, component := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, openErr := unix.Openat(current, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, 0, classifyRootBackupError(openErr)
		}
		current = next
		var status unix.Stat_t
		if unix.Fstat(current, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Uid != 0 ||
			status.Gid != 0 || status.Mode&0o022 != 0 {
			_ = unix.Close(current)
			return -1, 0, ErrConflict
		}
	}
	var status unix.Stat_t
	if unix.Fstat(current, &status) != nil {
		_ = unix.Close(current)
		return -1, 0, ErrUnavailable
	}
	return current, uint64(status.Dev), nil
}

func lockBackupRepository(repository int, device uint64) (func(), error) {
	descriptor, err := unix.Openat(repository, backupRepositoryLockName,
		unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return func() {}, classifyRootBackupError(err)
	}
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o600 || status.Nlink != 1 || uint64(status.Dev) != device {
		_ = unix.Close(descriptor)
		return func() {}, ErrConflict
	}
	if unix.Flock(descriptor, unix.LOCK_EX) != nil {
		_ = unix.Close(descriptor)
		return func() {}, ErrUnavailable
	}
	return func() {
		_ = unix.Flock(descriptor, unix.LOCK_UN)
		_ = unix.Close(descriptor)
	}, nil
}

func (manager *linuxBackupManager) loadManifestKey(create bool) ([]byte, error) {
	parent, device, err := openRootOwnedAbsoluteDirectory(filepath.Dir(manager.keyPath))
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	name := filepath.Base(manager.keyPath)
	descriptor, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) && create {
		key := make([]byte, backupManifestKeyBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, ErrUnavailable
		}
		created, createErr := unix.Openat(parent, name,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if createErr == nil {
			file := os.NewFile(uintptr(created), "backup-manifest-key")
			_, writeErr := file.Write(key)
			syncErr := file.Sync()
			closeErr := file.Close()
			if writeErr != nil || syncErr != nil || closeErr != nil || unix.Fsync(parent) != nil {
				clear(key)
				_ = unix.Unlinkat(parent, name, 0)
				return nil, ErrUnavailable
			}
			return key, nil
		}
		clear(key)
		if !errors.Is(createErr, unix.EEXIST) {
			return nil, classifyRootBackupError(createErr)
		}
		descriptor, err = unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, ErrIntegrity
		}
		return nil, classifyRootBackupError(err)
	}
	file := os.NewFile(uintptr(descriptor), "backup-manifest-key")
	defer file.Close()
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o600 || status.Nlink != 1 || status.Size != backupManifestKeyBytes ||
		uint64(status.Dev) != device {
		return nil, ErrIntegrity
	}
	key, err := io.ReadAll(io.LimitReader(file, backupManifestKeyBytes+1))
	if err != nil || len(key) != backupManifestKeyBytes {
		clear(key)
		return nil, ErrIntegrity
	}
	return key, nil
}

func enforceLocalBackupLimit(repository int) error {
	names, err := localBackupNames(repository)
	if err != nil {
		return err
	}
	if len(names) >= agentprotocol.MaximumLocalBackups {
		return ErrBusy
	}
	return nil
}

func effectiveBackupRepositoryLimit(value uint64) uint64 {
	if value == 0 {
		return agentprotocol.DefaultBackupRepositoryBytes
	}
	return value
}

func attachBackupRepositoryStatus(
	result agentprotocol.FileWriteResult, repository int, device uint64, request agentprotocol.FileWriteRequest,
) (agentprotocol.FileWriteResult, error) {
	status, err := measureBackupRepository(repository, device, request.BackupLimitBytes)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	result.BackupRepository = &status
	return result, nil
}

func measureBackupRepository(repository int, device uint64, limit uint64) (agentprotocol.BackupRepositoryStatus, error) {
	status := agentprotocol.BackupRepositoryStatus{LimitBytes: effectiveBackupRepositoryLimit(limit),
		MaximumBackups: agentprotocol.MaximumLocalBackups}
	names, err := readManagedDirectoryNames(repository,
		agentprotocol.MaximumLocalBackups+agentprotocol.MaximumBackupUploads+32)
	if err != nil {
		return agentprotocol.BackupRepositoryStatus{}, err
	}
	for _, name := range names {
		if name == backupRepositoryLockName {
			continue
		}
		kind, id := "backup", name
		for prefix, candidate := range map[string]string{
			".upload-": "upload", ".staging-": "staging", ".deleting-": "deleting",
		} {
			if strings.HasPrefix(name, prefix) {
				kind, id = candidate, strings.TrimPrefix(name, prefix)
				break
			}
		}
		parsed, parseErr := uuid.Parse(id)
		if parseErr != nil || parsed.Version() != 7 || parsed.String() != id {
			return agentprotocol.BackupRepositoryStatus{}, ErrConflict
		}
		directory, openErr := openRootBackupDirectory(repository, name, device)
		if openErr != nil {
			return agentprotocol.BackupRepositoryStatus{}, openErr
		}
		directoryNames, readErr := readManagedDirectoryNames(directory, 5)
		if readErr != nil {
			_ = unix.Close(directory)
			return agentprotocol.BackupRepositoryStatus{}, readErr
		}
		for _, child := range directoryNames {
			var fileStatus unix.Stat_t
			if unix.Fstatat(directory, child, &fileStatus, unix.AT_SYMLINK_NOFOLLOW) != nil ||
				fileStatus.Mode&unix.S_IFMT != unix.S_IFREG || fileStatus.Uid != 0 || fileStatus.Gid != 0 ||
				fileStatus.Mode&0o7777 != 0o600 || fileStatus.Nlink != 1 || fileStatus.Size < 0 || uint64(fileStatus.Dev) != device {
				_ = unix.Close(directory)
				return agentprotocol.BackupRepositoryStatus{}, ErrConflict
			}
			size := uint64(fileStatus.Size) // #nosec G115 -- negative sizes are rejected above.
			if status.UsedBytes > ^uint64(0)-size {
				_ = unix.Close(directory)
				return agentprotocol.BackupRepositoryStatus{}, ErrTooLarge
			}
			status.UsedBytes += size
		}
		_ = unix.Close(directory)
		switch kind {
		case "backup":
			status.BackupCount++
		case "upload":
			status.ActiveUploads++
		}
	}
	if status.BackupCount > agentprotocol.MaximumLocalBackups || status.ActiveUploads > agentprotocol.MaximumBackupUploads {
		return agentprotocol.BackupRepositoryStatus{}, ErrBusy
	}
	return status, nil
}

func backupUploadName(id string) string   { return ".upload-" + id }
func backupDeletingName(id string) string { return ".deleting-" + id }

func backupUploadResult(metadata backupUploadMetadata, status agentprotocol.BackupRepositoryStatus) agentprotocol.FileWriteResult {
	return agentprotocol.FileWriteResult{UploadID: metadata.UploadID, SizeBytes: metadata.SizeBytes,
		ReceivedBytes: metadata.ReceivedBytes, CreatedAt: metadata.CreatedAt, BackupRepository: &status}
}

func readBackupUpload(
	repository int, device uint64, accountID, uploadID string,
) (backupUploadMetadata, error) {
	metadata, directory, err := openBackupUpload(repository, device, accountID, uploadID)
	if directory >= 0 {
		_ = unix.Close(directory)
	}
	return metadata, err
}

func openBackupUpload(
	repository int, device uint64, accountID, uploadID string,
) (backupUploadMetadata, int, error) {
	directory, err := openRootBackupDirectory(repository, backupUploadName(uploadID), device)
	if err != nil {
		return backupUploadMetadata{}, -1, err
	}
	encoded, err := readRootBackupFile(directory, backupUploadMetadataName, device, maximumBackupManifestBytes)
	if err != nil {
		_ = unix.Close(directory)
		return backupUploadMetadata{}, -1, err
	}
	var metadata backupUploadMetadata
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&metadata) != nil || requireBackupJSONEnd(decoder) != nil ||
		validateBackupUploadMetadata(metadata, accountID, uploadID) != nil {
		_ = unix.Close(directory)
		return backupUploadMetadata{}, -1, ErrIntegrity
	}
	return metadata, directory, nil
}

func validateBackupUploadMetadata(metadata backupUploadMetadata, accountID, uploadID string) error {
	parsedUpload, uploadErr := uuid.Parse(metadata.UploadID)
	parsedAccount, accountErr := uuid.Parse(metadata.AccountID)
	if metadata.SchemaVersion != backupUploadSchemaVersion || uploadErr != nil || parsedUpload.Version() != 7 ||
		parsedUpload.String() != metadata.UploadID || metadata.UploadID != uploadID || accountErr != nil ||
		parsedAccount.Version() != 7 || parsedAccount.String() != metadata.AccountID || metadata.AccountID != accountID ||
		!agentprotocol.ValidBackupScopePath(metadata.Scope, metadata.SourcePath) || metadata.SizeBytes == 0 ||
		metadata.SizeBytes > agentprotocol.MaximumFileUploadBytes || metadata.ReceivedBytes > metadata.SizeBytes ||
		(metadata.ExpectedSHA256 != "" && !validBackupSHA256(metadata.ExpectedSHA256)) || metadata.CreatedAt.IsZero() || metadata.CreatedAt.Location() != time.UTC {
		return ErrIntegrity
	}
	return nil
}

func validateUploadPayloadDescriptor(descriptor int, device, size uint64) error {
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o600 || status.Nlink != 1 || status.Size < 0 ||
		uint64(status.Size) != size || uint64(status.Dev) != device { // #nosec G115 -- negative sizes are rejected first.
		return ErrIntegrity
	}
	return nil
}

func replaceBackupUploadMetadata(directory int, device uint64, metadata backupUploadMetadata) error {
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) > maximumBackupManifestBytes {
		return ErrInvalid
	}
	temporary := backupUploadMetadataName + ".next"
	_ = unix.Unlinkat(directory, temporary, 0)
	if err := writeRootBackupFile(directory, temporary, encoded, device); err != nil {
		return err
	}
	if err := unix.Renameat(directory, temporary, directory, backupUploadMetadataName); err != nil || unix.Fsync(directory) != nil {
		_ = unix.Unlinkat(directory, temporary, 0)
		return ErrUnavailable
	}
	return nil
}

func localBackupNames(repository int) ([]string, error) {
	names, err := readManagedDirectoryNames(repository,
		agentprotocol.MaximumLocalBackups+agentprotocol.MaximumBackupUploads+32)
	if err != nil {
		return nil, err
	}
	backups := make([]string, 0, len(names))
	for _, name := range names {
		if name == backupRepositoryLockName || strings.HasPrefix(name, ".staging-") ||
			strings.HasPrefix(name, ".upload-") || strings.HasPrefix(name, ".deleting-") {
			continue
		}
		parsed, err := uuid.Parse(name)
		if err != nil || parsed.Version() != 7 || parsed.String() != name {
			return nil, ErrConflict
		}
		backups = append(backups, name)
	}
	if len(backups) > agentprotocol.MaximumLocalBackups {
		return nil, ErrBusy
	}
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	return backups, nil
}

func openRootBackupDirectory(parent int, name string, device uint64) (int, error) {
	directory, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, classifyRootBackupError(err)
	}
	var status unix.Stat_t
	if unix.Fstat(directory, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o700 || uint64(status.Dev) != device {
		_ = unix.Close(directory)
		return -1, ErrConflict
	}
	return directory, nil
}

func readLocalBackup(
	ctx context.Context, repository int, device uint64, accountID, backupID string, key []byte, verifyPayload bool,
) (agentprotocol.BackupRecord, error) {
	directory, err := openRootBackupDirectory(repository, backupID, device)
	if err != nil {
		return agentprotocol.BackupRecord{}, err
	}
	defer unix.Close(directory)
	names, err := readManagedDirectoryNames(directory, 3)
	if err != nil {
		return agentprotocol.BackupRecord{}, err
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != backupManifestName || names[1] != backupPayloadName {
		return agentprotocol.BackupRecord{}, ErrIntegrity
	}
	encoded, err := readRootBackupFile(directory, backupManifestName, device, maximumBackupManifestBytes)
	if err != nil {
		return agentprotocol.BackupRecord{}, err
	}
	manifest, err := decodeBackupManifest(encoded, key, accountID, backupID)
	if err != nil {
		return agentprotocol.BackupRecord{}, err
	}
	payloadDescriptor, err := unix.Openat(directory, backupPayloadName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return agentprotocol.BackupRecord{}, classifyRootBackupError(err)
	}
	payload := os.NewFile(uintptr(payloadDescriptor), "local-backup-payload-read")
	defer payload.Close()
	var status unix.Stat_t
	if unix.Fstat(payloadDescriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o600 || status.Nlink != 1 || status.Size <= 0 ||
		uint64(status.Size) != manifest.PayloadBytes || uint64(status.Dev) != device { // #nosec G115 -- manifest payload is bounded and status is positive.
		return agentprotocol.BackupRecord{}, ErrIntegrity
	}
	manifestDigest := sha256.Sum256(encoded)
	record := manifest.record(hex.EncodeToString(manifestDigest[:]), false)
	if !verifyPayload {
		return record, nil
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, &contextReader{ctx: ctx, reader: io.LimitReader(payload, int64(manifest.PayloadBytes))}); err != nil { // #nosec G115 -- payload is capped at 4 GiB.
		return agentprotocol.BackupRecord{}, ErrUnavailable
	}
	if hex.EncodeToString(digest.Sum(nil)) != manifest.PayloadSHA256 {
		return agentprotocol.BackupRecord{}, ErrIntegrity
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return agentprotocol.BackupRecord{}, ErrUnavailable
	}
	entries, contentBytes, err := inspectBackupPayload(ctx, payload, status.Size)
	if err != nil || entries != manifest.EntryCount || contentBytes != manifest.ContentBytes {
		return agentprotocol.BackupRecord{}, ErrIntegrity
	}
	record.PayloadVerified = true
	return record, nil
}

func readRootBackupFile(parent int, name string, device uint64, maximum int64) ([]byte, error) {
	descriptor, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, classifyRootBackupError(err)
	}
	file := os.NewFile(uintptr(descriptor), "root-backup-metadata")
	defer file.Close()
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o600 || status.Nlink != 1 || status.Size <= 0 ||
		status.Size > maximum || uint64(status.Dev) != device {
		return nil, ErrIntegrity
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(encoded)) != status.Size {
		return nil, ErrIntegrity
	}
	return encoded, nil
}

func writeRootBackupFile(parent int, name string, content []byte, device uint64) error {
	descriptor, err := unix.Openat(parent, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return classifyRootBackupError(err)
	}
	file := os.NewFile(uintptr(descriptor), "root-backup-metadata")
	defer file.Close()
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != 0 ||
		status.Gid != 0 || status.Mode&0o7777 != 0o600 || status.Nlink != 1 || uint64(status.Dev) != device {
		return ErrConflict
	}
	if _, err := file.Write(content); err != nil || file.Sync() != nil {
		return ErrUnavailable
	}
	return nil
}

func signBackupManifest(manifest backupManifest, key []byte) ([]byte, error) {
	unsigned := manifest.unsigned()
	encoded, err := json.Marshal(unsigned)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(encoded)
	manifest.Signature = hex.EncodeToString(mac.Sum(nil))
	return json.Marshal(manifest)
}

func decodeBackupManifest(encoded, key []byte, accountID, backupID string) (backupManifest, error) {
	var manifest backupManifest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || requireBackupJSONEnd(decoder) != nil ||
		validateBackupManifest(manifest, accountID, backupID) != nil {
		return backupManifest{}, ErrIntegrity
	}
	unsigned, err := json.Marshal(manifest.unsigned())
	if err != nil {
		return backupManifest{}, ErrIntegrity
	}
	wanted, err := hex.DecodeString(manifest.Signature)
	if err != nil || len(wanted) != sha256.Size || manifest.Signature != hex.EncodeToString(wanted) {
		return backupManifest{}, ErrIntegrity
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(unsigned)
	if !hmac.Equal(wanted, mac.Sum(nil)) {
		return backupManifest{}, ErrIntegrity
	}
	return manifest, nil
}

func (manifest backupManifest) unsigned() unsignedBackupManifest {
	return unsignedBackupManifest{SchemaVersion: manifest.SchemaVersion, BackupID: manifest.BackupID,
		AccountID: manifest.AccountID, Scope: manifest.Scope, SourcePath: manifest.SourcePath,
		CreatedAt: manifest.CreatedAt, PayloadBytes: manifest.PayloadBytes, ContentBytes: manifest.ContentBytes,
		EntryCount: manifest.EntryCount, PayloadSHA256: manifest.PayloadSHA256}
}

func (manifest backupManifest) record(manifestSHA256 string, verified bool) agentprotocol.BackupRecord {
	return agentprotocol.BackupRecord{SchemaVersion: manifest.SchemaVersion, BackupID: manifest.BackupID,
		AccountID: manifest.AccountID, Scope: manifest.Scope, SourcePath: manifest.SourcePath,
		CreatedAt: manifest.CreatedAt, PayloadBytes: manifest.PayloadBytes, ContentBytes: manifest.ContentBytes,
		EntryCount: manifest.EntryCount, PayloadSHA256: manifest.PayloadSHA256, ManifestSHA256: manifestSHA256,
		ManifestAuthenticated: true, PayloadVerified: verified}
}

func validateBackupManifest(manifest backupManifest, accountID, backupID string) error {
	parsedBackup, backupErr := uuid.Parse(manifest.BackupID)
	parsedAccount, accountErr := uuid.Parse(manifest.AccountID)
	if manifest.SchemaVersion != agentprotocol.BackupManifestSchemaVersion || backupErr != nil ||
		parsedBackup.Version() != 7 || parsedBackup.String() != manifest.BackupID || accountErr != nil ||
		parsedAccount.Version() != 7 || parsedAccount.String() != manifest.AccountID || manifest.BackupID != backupID ||
		manifest.AccountID != accountID || !agentprotocol.ValidBackupScopePath(manifest.Scope, manifest.SourcePath) ||
		manifest.CreatedAt.IsZero() || manifest.CreatedAt.Location() != time.UTC || manifest.PayloadBytes == 0 ||
		manifest.PayloadBytes > agentprotocol.MaximumFileUploadBytes || manifest.ContentBytes > agentprotocol.MaximumFileUploadBytes ||
		manifest.EntryCount > agentprotocol.MaximumFileOperationEntries ||
		!validBackupSHA256(manifest.PayloadSHA256) || !validBackupSHA256(manifest.Signature) {
		return ErrIntegrity
	}
	return nil
}

func validBackupSHA256(value string) bool {
	digest, err := hex.DecodeString(value)
	return err == nil && len(digest) == sha256.Size && value == hex.EncodeToString(digest)
}

func inspectBackupPayload(ctx context.Context, payload *os.File, size int64) (uint64, uint64, error) {
	if size <= 0 || uint64(size) > agentprotocol.MaximumFileUploadBytes { // #nosec G115 -- positive checked first.
		return 0, 0, ErrTooLarge
	}
	manifest := newArchiveManifest(agentprotocol.MaximumFileUploadBytes)
	err := processTarGzipWithLimit(ctx, payload, size,
		agentprotocol.MaximumFileUploadBytes+maximumBackupTarOverhead,
		func(header *tar.Header, reader io.Reader) error {
			directory, sizeBytes, err := validateTarHeader(header)
			if err != nil {
				return err
			}
			if _, err := manifest.add(header.Name, directory, sizeBytes); err != nil {
				return err
			}
			if !directory {
				_, err = io.CopyN(io.Discard, &contextReader{ctx: ctx, reader: reader}, header.Size)
			}
			return err
		})
	if err != nil {
		return 0, 0, classifyArchiveError(err)
	}
	return manifest.entries, manifest.bytes, nil
}

func backupStagingName(backupID string) string { return ".staging-" + backupID }

func removeRootBackupTreeAt(parent int, name string, device uint64, depth int) error {
	if depth > 2 {
		return ErrConflict
	}
	var status unix.Stat_t
	if unix.Fstatat(parent, name, &status, unix.AT_SYMLINK_NOFOLLOW) != nil || status.Uid != 0 || status.Gid != 0 ||
		uint64(status.Dev) != device {
		return ErrConflict
	}
	switch status.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		return classifyRootBackupError(unix.Unlinkat(parent, name, 0))
	case unix.S_IFDIR:
		directory, err := openRootBackupDirectory(parent, name, device)
		if err != nil {
			return err
		}
		names, err := readManagedDirectoryNames(directory, 4)
		if err != nil {
			_ = unix.Close(directory)
			return err
		}
		for _, child := range names {
			if err := removeRootBackupTreeAt(directory, child, device, depth+1); err != nil {
				_ = unix.Close(directory)
				return err
			}
		}
		if unix.Fsync(directory) != nil || unix.Close(directory) != nil {
			return ErrUnavailable
		}
		return classifyRootBackupError(unix.Unlinkat(parent, name, unix.AT_REMOVEDIR))
	default:
		return ErrConflict
	}
}

func classifyRootBackupError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENOTDIR):
		return ErrNotFound
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ELOOP), errors.Is(err, unix.EXDEV):
		return ErrConflict
	case errors.Is(err, unix.EDQUOT), errors.Is(err, unix.ENOSPC):
		return ErrQuota
	default:
		return ErrUnavailable
	}
}

func requireBackupJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func runPlatformBackupHelper(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, maximumBackupHelperBytes+1)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) < 2 || len(line) > maximumBackupHelperBytes {
		return ErrInvalid
	}
	var control backupHelperControl
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&control) != nil || requireBackupJSONEnd(decoder) != nil || validateBackupHelperControl(control) != nil {
		return ErrInvalid
	}
	effectiveUID, effectiveGID := os.Geteuid(), os.Getegid()
	if effectiveUID < 0 || effectiveGID < 0 || uint32(effectiveUID) != control.Identity.UID || // #nosec G115 -- non-negative kernel identity is checked first.
		uint32(effectiveGID) != control.Identity.GID { // #nosec G115 -- non-negative kernel identity is checked first.
		return ErrInvalid
	}
	switch control.Mode {
	case backupHelperCreate:
		if _, err := reader.Peek(1); !errors.Is(err, io.EOF) {
			return ErrInvalid
		}
		return createBackupPayload(ctx, control, output)
	case backupHelperRestore:
		result, err := restoreBackupPayload(ctx, control, reader)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(result)
	default:
		return ErrInvalid
	}
}

func validateBackupHelperControl(control backupHelperControl) error {
	if control.ProtocolVersion != agentprotocol.WireVersion || hostingidentity.Validate(control.Identity) != nil ||
		!agentprotocol.ValidBackupScopePath(control.Scope, control.SourcePath) {
		return ErrInvalid
	}
	if control.Mode == backupHelperCreate {
		if control.OperationID != "" || control.PayloadBytes != 0 || control.ExpectedEntries != 0 ||
			control.ExpectedContentBytes != 0 {
			return ErrInvalid
		}
		return nil
	}
	parsed, err := uuid.Parse(control.OperationID)
	if control.Mode != backupHelperRestore || err != nil || parsed.Version() != 7 || parsed.String() != control.OperationID ||
		control.PayloadBytes == 0 || control.PayloadBytes > agentprotocol.MaximumFileUploadBytes ||
		control.ExpectedEntries > agentprotocol.MaximumFileOperationEntries ||
		control.ExpectedContentBytes > agentprotocol.MaximumFileUploadBytes {
		return ErrInvalid
	}
	return nil
}

func createBackupPayload(ctx context.Context, control backupHelperControl, output io.Writer) error {
	source, device, err := openAccountDirectoryForMutation(control.Identity, control.SourcePath)
	if err != nil {
		return err
	}
	defer unix.Close(source)
	sink := &boundedArchiveWriter{writer: output, remaining: agentprotocol.MaximumFileUploadBytes}
	gzipWriter, err := gzip.NewWriterLevel(sink, gzip.BestSpeed)
	if err != nil {
		return ErrUnavailable
	}
	emitter := &tarGzipArchiveEmitter{gzipWriter: gzipWriter, tarWriter: tar.NewWriter(gzipWriter)}
	budget := &fileOperationBudget{}
	names, err := readManagedDirectoryNames(source, int(agentprotocol.MaximumFileOperationEntries+4))
	if err != nil {
		_ = emitter.close()
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		if control.Scope == agentprotocol.BackupScopeAccountFiles && reservedBackupTopLevel(name) {
			continue
		}
		if !hostingpath.ValidFilename(name) {
			_ = emitter.close()
			return ErrConflict
		}
		if err := walkManagedArchiveSource(ctx, source, name, name, control.Identity, device, budget, 0, emitter); err != nil {
			_ = emitter.close()
			return err
		}
	}
	return classifyArchiveError(emitter.close())
}

func restoreBackupPayload(
	ctx context.Context, control backupHelperControl, input io.Reader,
) (backupHelperResult, error) {
	root, device, err := openAccountDirectoryForMutation(control.Identity, "")
	if err != nil {
		return backupHelperResult{}, err
	}
	defer unix.Close(root)
	request := agentprotocol.FileWriteRequest{Identity: control.Identity, OperationID: control.OperationID}
	operations, staging, cleanup, err := beginArchiveOperation(request, device)
	if err != nil {
		return backupHelperResult{}, err
	}
	defer unix.Close(operations)
	defer unix.Close(staging)
	defer func() { cleanup() }()
	if err := unix.Mkdirat(staging, fileOperationPayloadName, 0o700); err != nil {
		return backupHelperResult{}, classifyFileMutationError(err)
	}
	payload, err := unix.Openat(staging, fileOperationPayloadName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return backupHelperResult{}, classifyFileMutationError(err)
	}
	defer unix.Close(payload)
	manifest, err := extractBackupPayloadStream(ctx, input, control.PayloadBytes, payload, control.Identity, device)
	if err != nil {
		return backupHelperResult{}, err
	}
	if manifest.entries != control.ExpectedEntries || manifest.bytes != control.ExpectedContentBytes {
		return backupHelperResult{}, ErrIntegrity
	}
	if unix.Fsync(payload) != nil || unix.Fsync(staging) != nil {
		return backupHelperResult{}, ErrUnavailable
	}
	switch control.Scope {
	case agentprotocol.BackupScopeDocumentRoot:
		err = activateDocumentRootRestore(ctx, control, staging, payload, device)
	case agentprotocol.BackupScopeAccountFiles:
		err = activateAccountFilesRestore(ctx, control, root, staging, payload, device)
	default:
		err = ErrInvalid
	}
	if err != nil {
		return backupHelperResult{}, err
	}
	if err := finishArchiveOperation(operations, staging, control.OperationID); err != nil {
		return backupHelperResult{}, err
	}
	cleanup = func() {}
	return backupHelperResult{OperationID: control.OperationID, EntryCount: manifest.entries,
		ContentBytes: manifest.bytes, Completed: true}, nil
}

func extractBackupPayloadStream(
	ctx context.Context, input io.Reader, inputBytes uint64, root int, identity hostingidentity.Spec, device uint64,
) (*archiveManifest, error) {
	buffered := bufio.NewReaderSize(io.LimitReader(&contextReader{ctx: ctx, reader: input}, int64(inputBytes)), 32<<10) // #nosec G115 -- input is capped at 4 GiB.
	gzipReader, err := gzip.NewReader(buffered)                                                                         // #nosec G110 -- both compressed and fully decompressed streams have explicit bounds.
	if err != nil {
		return nil, ErrIntegrity
	}
	gzipReader.Multistream(false)
	decompressed := &io.LimitedReader{R: gzipReader,
		N: int64(agentprotocol.MaximumFileUploadBytes+maximumBackupTarOverhead) + 1} // #nosec G115 -- fixed 4 GiB plus 64 MiB bound.
	tarReader := tar.NewReader(decompressed)
	manifest := newArchiveManifest(agentprotocol.MaximumFileUploadBytes)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = gzipReader.Close()
			if decompressed.N == 0 {
				return nil, ErrTooLarge
			}
			return nil, ErrIntegrity
		}
		directory, sizeBytes, err := validateTarHeader(header)
		if err != nil {
			_ = gzipReader.Close()
			return nil, ErrIntegrity
		}
		components, err := manifest.add(header.Name, directory, sizeBytes)
		if err != nil {
			_ = gzipReader.Close()
			return nil, err
		}
		if reservedBackupArchivePath(components) {
			_ = gzipReader.Close()
			return nil, ErrIntegrity
		}
		if directory {
			err = ensureArchiveDirectory(root, components, identity, device)
		} else {
			err = writeExtractedArchiveFile(ctx, root, components, tarReader, sizeBytes, identity, device)
		}
		if err != nil {
			_ = gzipReader.Close()
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, decompressed); err != nil { // #nosec G110 -- LimitedReader bounds the entire decompressed stream.
		_ = gzipReader.Close()
		return nil, ErrIntegrity
	}
	if decompressed.N == 0 {
		_ = gzipReader.Close()
		return nil, ErrTooLarge
	}
	if gzipReader.Close() != nil {
		return nil, ErrIntegrity
	}
	if _, err := buffered.Peek(1); !errors.Is(err, io.EOF) {
		return nil, ErrIntegrity
	}
	return manifest, nil
}

func activateDocumentRootRestore(
	ctx context.Context, control backupHelperControl, staging, _ int, device uint64,
) error {
	components := strings.Split(control.SourcePath, "/")
	name := components[len(components)-1]
	parentPath := strings.Join(components[:len(components)-1], "/")
	parent, parentDevice, err := openAccountDirectoryForMutation(control.Identity, parentPath)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if parentDevice != device {
		return ErrConflict
	}
	status, err := validateManagedEntryAt(parent, name, control.Identity, device)
	if err != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrConflict
	}
	if err := inspectManagedTreeAt(ctx, parent, name, control.Identity, device, &fileOperationBudget{}, 0); err != nil {
		return err
	}
	if err := unix.Renameat2(parent, name, staging, backupRestorePreviousName, unix.RENAME_NOREPLACE); err != nil {
		return classifyFileMutationError(err)
	}
	if err := unix.Renameat2(staging, fileOperationPayloadName, parent, name, unix.RENAME_NOREPLACE); err != nil {
		rollbackErr := unix.Renameat2(staging, backupRestorePreviousName, parent, name, unix.RENAME_NOREPLACE)
		if rollbackErr != nil {
			return ErrUnavailable
		}
		return classifyFileMutationError(err)
	}
	if unix.Fsync(parent) != nil || unix.Fsync(staging) != nil {
		return ErrUnavailable
	}
	if err := removeManagedTreeAt(ctx, staging, backupRestorePreviousName, control.Identity, device,
		internalFileOperationCleanupBudget(), 0); err != nil {
		return err
	}
	return unix.Fsync(staging)
}

func activateAccountFilesRestore(
	ctx context.Context, control backupHelperControl, root, staging, payload int, device uint64,
) error {
	currentNames, err := visibleBackupRootNames(root)
	if err != nil {
		return err
	}
	budget := &fileOperationBudget{}
	for _, name := range currentNames {
		if err := inspectManagedTreeAt(ctx, root, name, control.Identity, device, budget, 0); err != nil {
			return err
		}
	}
	candidateNames, err := visibleBackupRootNames(payload)
	if err != nil {
		return err
	}
	if err := unix.Mkdirat(staging, backupRestorePreviousName, 0o700); err != nil {
		return classifyFileMutationError(err)
	}
	previous, err := unix.Openat(staging, backupRestorePreviousName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return classifyFileMutationError(err)
	}
	defer unix.Close(previous)
	if _, err := validateManagedDirectoryDescriptor(previous, control.Identity, device); err != nil {
		return err
	}
	movedCurrent := make([]string, 0, len(currentNames))
	activated := make([]string, 0, len(candidateNames))
	rollback := func() error {
		for index := len(activated) - 1; index >= 0; index-- {
			if err := unix.Renameat2(root, activated[index], payload, activated[index], unix.RENAME_NOREPLACE); err != nil {
				return ErrUnavailable
			}
		}
		for index := len(movedCurrent) - 1; index >= 0; index-- {
			if err := unix.Renameat2(previous, movedCurrent[index], root, movedCurrent[index], unix.RENAME_NOREPLACE); err != nil {
				return ErrUnavailable
			}
		}
		return nil
	}
	for _, name := range currentNames {
		if err := unix.Renameat2(root, name, previous, name, unix.RENAME_NOREPLACE); err != nil {
			_ = rollback()
			return classifyFileMutationError(err)
		}
		movedCurrent = append(movedCurrent, name)
	}
	remaining, err := visibleBackupRootNames(root)
	if err != nil || len(remaining) != 0 {
		_ = rollback()
		return ErrConflict
	}
	for _, name := range candidateNames {
		if err := unix.Renameat2(payload, name, root, name, unix.RENAME_NOREPLACE); err != nil {
			if rollback() != nil {
				return ErrUnavailable
			}
			return classifyFileMutationError(err)
		}
		activated = append(activated, name)
	}
	if unix.Fsync(root) != nil || unix.Fsync(payload) != nil || unix.Fsync(previous) != nil {
		return ErrUnavailable
	}
	if err := removeManagedTreeAt(ctx, staging, backupRestorePreviousName, control.Identity, device,
		internalFileOperationCleanupBudget(), 0); err != nil {
		return err
	}
	if err := unix.Unlinkat(staging, fileOperationPayloadName, unix.AT_REMOVEDIR); err != nil {
		return classifyFileMutationError(err)
	}
	return unix.Fsync(staging)
}

func visibleBackupRootNames(descriptor int) ([]string, error) {
	names, err := readManagedDirectoryNames(descriptor, int(agentprotocol.MaximumFileOperationEntries+4))
	if err != nil {
		return nil, err
	}
	visible := names[:0]
	for _, name := range names {
		if reservedBackupTopLevel(name) {
			continue
		}
		if !hostingpath.ValidFilename(name) {
			return nil, ErrConflict
		}
		visible = append(visible, name)
	}
	sort.Strings(visible)
	return visible, nil
}

func reservedBackupTopLevel(name string) bool {
	return name == agentprotocol.ReservedFileUploadDirectory ||
		name == agentprotocol.ReservedFileOperationDirectory || name == agentprotocol.ReservedFileTrashDirectory ||
		name == agentprotocol.ReservedOCIVolumeDirectory
}

func reservedBackupArchivePath(components []string) bool {
	return len(components) > 0 && reservedBackupTopLevel(components[0])
}
