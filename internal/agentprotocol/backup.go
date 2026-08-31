// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingpath"
)

const BackupManifestSchemaVersion = 1

type BackupScope string

const (
	BackupScopeAccountFiles BackupScope = "account_files"
	BackupScopeDocumentRoot BackupScope = "document_root"
)

// BackupRecord is bounded metadata about one root-owned local backup. The
// manifest signature itself never leaves the privileged host boundary.
type BackupRecord struct {
	SchemaVersion         int         `json:"schemaVersion"`
	BackupID              string      `json:"backupId"`
	AccountID             string      `json:"accountId"`
	Scope                 BackupScope `json:"scope"`
	SourcePath            string      `json:"sourcePath,omitempty"`
	CreatedAt             time.Time   `json:"createdAt"`
	PayloadBytes          uint64      `json:"payloadBytes"`
	ContentBytes          uint64      `json:"contentBytes"`
	EntryCount            uint64      `json:"entryCount"`
	PayloadSHA256         string      `json:"payloadSha256"`
	ManifestSHA256        string      `json:"manifestSha256"`
	ManifestAuthenticated bool        `json:"manifestAuthenticated"`
	PayloadVerified       bool        `json:"payloadVerified"`
}

// BackupRepositoryStatus is measured by the privileged host. Upload payloads
// reserve their declared apparent size so interrupted transfers cannot bypass
// the package limit.
type BackupRepositoryStatus struct {
	UsedBytes      uint64 `json:"usedBytes"`
	LimitBytes     uint64 `json:"limitBytes"`
	BackupCount    int    `json:"backupCount"`
	MaximumBackups int    `json:"maximumBackups"`
	ActiveUploads  int    `json:"activeUploads"`
}

func isFileBackupAction(action FileWriteAction) bool {
	return action == FileWriteBackupCreate || action == FileWriteBackupList || action == FileWriteBackupInspect ||
		action == FileWriteBackupVerify || action == FileWriteBackupRestore ||
		action == FileWriteBackupUploadInitiate || action == FileWriteBackupUploadStatus ||
		action == FileWriteBackupUploadChunk || action == FileWriteBackupUploadComplete ||
		action == FileWriteBackupUploadCancel || action == FileWriteBackupDelete
}

func validBackupRepositoryStatus(status *BackupRepositoryStatus, request FileWriteRequest) bool {
	return status != nil && status.LimitBytes == effectiveBackupLimit(request.BackupLimitBytes) &&
		status.BackupCount >= 0 && status.BackupCount <= MaximumLocalBackups &&
		status.MaximumBackups == MaximumLocalBackups && status.ActiveUploads >= 0 && status.ActiveUploads <= MaximumBackupUploads
}

func effectiveBackupLimit(value uint64) uint64 {
	if value == 0 {
		return DefaultBackupRepositoryBytes
	}
	return value
}

func ValidBackupScopePath(scope BackupScope, sourcePath string) bool {
	switch scope {
	case BackupScopeAccountFiles:
		return sourcePath == ""
	case BackupScopeDocumentRoot:
		if sourcePath == "" {
			return false
		}
		normalized, err := hostingpath.NormalizeDocumentRoot(sourcePath)
		return err == nil && normalized == sourcePath
	default:
		return false
	}
}

func validBackupRecord(record BackupRecord, accountID string) bool {
	return record.SchemaVersion == BackupManifestSchemaVersion && validFileUploadID(record.BackupID) &&
		record.AccountID == accountID && validFileUploadID(record.AccountID) &&
		ValidBackupScopePath(record.Scope, record.SourcePath) && !record.CreatedAt.IsZero() &&
		record.CreatedAt.Location() == time.UTC && record.PayloadBytes > 0 &&
		record.PayloadBytes <= MaximumFileUploadBytes && record.ContentBytes <= MaximumFileUploadBytes &&
		record.EntryCount <= MaximumFileOperationEntries &&
		lowercaseSHA256Pattern.MatchString(record.PayloadSHA256) &&
		lowercaseSHA256Pattern.MatchString(record.ManifestSHA256)
}

func validateFileBackupResult(result FileWriteResult, request FileWriteRequest) error {
	if !validBackupRepositoryStatus(result.BackupRepository, request) {
		return errors.New("backup repository status is invalid")
	}
	if request.Action == FileWriteBackupUploadInitiate || request.Action == FileWriteBackupUploadStatus ||
		request.Action == FileWriteBackupUploadChunk {
		if result.UploadID != request.UploadID || result.SizeBytes == 0 || result.ReceivedBytes > result.SizeBytes ||
			result.CreatedAt.IsZero() || result.CreatedAt.Location() != time.UTC || result.Completed ||
			result.Backup != nil || result.Backups != nil || result.OperationID != "" || result.TrashID != "" ||
			result.SourceDirectory != "" || result.SourceName != "" || result.Directory != "" || result.Name != "" ||
			result.SHA256 != "" || result.TrashEntries != nil || result.Next != "" || result.ArchiveFormat != "" || result.EntryCount != 0 {
			return errors.New("backup upload result is invalid")
		}
		return nil
	}
	if request.Action == FileWriteBackupUploadComplete {
		if result.UploadID != "" || result.Backup == nil || !result.Completed || result.Backup.BackupID != request.UploadID ||
			!result.Backup.ManifestAuthenticated || !result.Backup.PayloadVerified || result.Backups != nil ||
			result.OperationID != "" || result.TrashID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
			result.Directory != "" || result.Name != "" || result.SizeBytes != 0 || result.ReceivedBytes != 0 ||
			result.SHA256 != "" || !result.CreatedAt.IsZero() || result.TrashEntries != nil || result.Next != "" ||
			result.ArchiveFormat != "" || result.EntryCount != 0 {
			return errors.New("backup upload completion result is invalid")
		}
		return nil
	}
	if request.Action == FileWriteBackupUploadCancel {
		if result.UploadID != request.UploadID || !result.Completed || result.Backup != nil || result.Backups != nil ||
			result.OperationID != "" || result.TrashID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
			result.Directory != "" || result.Name != "" || result.SizeBytes != 0 || result.ReceivedBytes != 0 ||
			result.SHA256 != "" || !result.CreatedAt.IsZero() || result.TrashEntries != nil || result.Next != "" ||
			result.ArchiveFormat != "" || result.EntryCount != 0 {
			return errors.New("backup upload cancellation result is invalid")
		}
		return nil
	}
	if request.Action == FileWriteBackupDelete {
		if !result.Completed || result.Backup != nil || result.Backups != nil || result.UploadID != "" ||
			result.OperationID != "" || result.TrashID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
			result.Directory != "" || result.Name != "" || result.SizeBytes != 0 || result.ReceivedBytes != 0 ||
			result.SHA256 != "" || !result.CreatedAt.IsZero() || result.TrashEntries != nil || result.Next != "" ||
			result.ArchiveFormat != "" || result.EntryCount != 0 {
			return errors.New("backup deletion result is invalid")
		}
		return nil
	}
	if result.UploadID != "" || result.TrashID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
		result.Directory != "" || result.Name != "" || result.SizeBytes != 0 || result.ReceivedBytes != 0 ||
		result.SHA256 != "" || !result.CreatedAt.IsZero() || result.TrashEntries != nil ||
		result.ArchiveFormat != "" || result.EntryCount != 0 {
		return errors.New("backup result contains unrelated metadata")
	}
	switch request.Action {
	case FileWriteBackupCreate:
		if result.OperationID != "" || result.Backup == nil || result.Backups != nil || result.Next != "" ||
			!result.Completed || result.Backup.BackupID != request.BackupID || result.Backup.Scope != request.BackupScope ||
			result.Backup.SourcePath != request.BackupPath || !result.Backup.ManifestAuthenticated ||
			!result.Backup.PayloadVerified {
			return errors.New("backup creation result is invalid")
		}
	case FileWriteBackupList:
		if result.OperationID != "" || result.Backup != nil || result.Backups == nil || result.Completed ||
			len(result.Backups) > MaximumBackupListEntries || (result.Next != "" && !validFileUploadID(result.Next)) {
			return errors.New("backup listing result is invalid")
		}
		previous := request.Cursor
		for _, record := range result.Backups {
			if !validBackupRecord(record, request.Identity.AccountID) || !record.ManifestAuthenticated ||
				record.PayloadVerified || (previous != "" && record.BackupID >= previous) {
				return errors.New("backup listing entry is invalid")
			}
			previous = record.BackupID
		}
		if result.Next != "" && (len(result.Backups) == 0 || result.Next != result.Backups[len(result.Backups)-1].BackupID) {
			return errors.New("backup listing cursor is invalid")
		}
	case FileWriteBackupInspect, FileWriteBackupVerify:
		verified := request.Action == FileWriteBackupVerify
		if result.OperationID != "" || result.Backup == nil || result.Backups != nil || result.Next != "" ||
			result.Completed || result.Backup.BackupID != request.BackupID ||
			!result.Backup.ManifestAuthenticated || result.Backup.PayloadVerified != verified {
			return errors.New("backup inspection result is invalid")
		}
	case FileWriteBackupRestore:
		if result.OperationID != request.OperationID || result.Backup == nil || result.Backups != nil || result.Next != "" ||
			!result.Completed || result.Backup.BackupID != request.BackupID ||
			!result.Backup.ManifestAuthenticated || !result.Backup.PayloadVerified {
			return errors.New("backup restore result is invalid")
		}
	default:
		return errors.New("backup action is invalid")
	}
	return nil
}
