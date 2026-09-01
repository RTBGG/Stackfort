// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestFileWriteContractSeparatesChunksAndRequiresMutationAudit(t *testing.T) {
	t.Parallel()
	request := validFileWriteRequest(FileWriteInitiate)
	if err := ValidateFileWriteRequest(request); err != nil {
		t.Fatalf("valid initiate: %v", err)
	}
	withoutAudit := request
	withoutAudit.Correlation = nil
	if err := ValidateFileWriteRequest(withoutAudit); err == nil {
		t.Fatal("mutation without audit correlation accepted")
	}
	chunk := validFileWriteRequest(FileWriteChunk)
	chunk.Correlation = request.Correlation
	if err := ValidateFileWriteRequest(chunk); err == nil {
		t.Fatal("chunk accepted visible-mutation audit correlation")
	}
	chunk.Correlation = nil
	if err := ValidateFileWriteRequest(chunk); err != nil {
		t.Fatalf("valid chunk: %v", err)
	}
}

func TestFileWriteContractRejectsTraversalReservedPathsAndOversizedChunks(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*FileWriteRequest){
		func(value *FileWriteRequest) { value.Directory = "../etc" },
		func(value *FileWriteRequest) { value.Directory = ReservedFileUploadDirectory },
		func(value *FileWriteRequest) { value.Directory, value.Name = "", ReservedFileUploadDirectory },
		func(value *FileWriteRequest) { value.ChunkLength = MaximumFileUploadChunkBytes + 1 },
		func(value *FileWriteRequest) { value.Directory, value.Name = "", ReservedFileTrashDirectory },
		func(value *FileWriteRequest) { value.Directory, value.Name = "", ReservedOCIVolumeDirectory },
	} {
		request := validFileWriteRequest(FileWriteInitiate)
		if strings.Contains(request.Directory, "etc") || request.ChunkLength != 0 {
			t.Fatal("test fixture unexpectedly mutated")
		}
		if mutate(&request); ValidateFileWriteRequest(request) == nil {
			t.Fatalf("invalid request accepted: %#v", request)
		}
	}
}

func TestFileNodeAndTrashContractsAreClosedAndCorrelated(t *testing.T) {
	t.Parallel()
	for _, action := range []FileWriteAction{
		FileWriteRename, FileWriteMove, FileWriteCopy, FileWriteTrash,
		FileWriteTrashRestore, FileWriteTrashPurge, FileWriteArchiveCreate, FileWriteArchiveExtract,
		FileWriteBackupCreate, FileWriteBackupRestore,
		FileWriteBackupUploadInitiate, FileWriteBackupUploadComplete,
		FileWriteBackupUploadCancel, FileWriteBackupDelete,
	} {
		request := validFileWriteRequest(action)
		if err := ValidateFileWriteRequest(request); err != nil {
			t.Errorf("valid %s: %v", action, err)
		}
		request.Correlation = nil
		if err := ValidateFileWriteRequest(request); err == nil {
			t.Errorf("%s accepted without durable audit correlation", action)
		}
	}
	listing := validFileWriteRequest(FileWriteTrashList)
	if listing.Correlation != nil || ValidateFileWriteRequest(listing) != nil {
		t.Fatalf("valid read-only trash listing = %#v", listing)
	}
}

func TestBackupContractsAreClosedVersionedAndResponseBound(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	for _, action := range []FileWriteAction{FileWriteBackupCreate, FileWriteBackupList, FileWriteBackupInspect,
		FileWriteBackupVerify, FileWriteBackupRestore} {
		request := validFileWriteRequest(action)
		if err := ValidateFileWriteRequest(request); err != nil {
			t.Fatalf("valid %s request: %v", action, err)
		}
		backupID := request.BackupID
		if backupID == "" {
			backupID = "019c1234-5678-7abc-8def-0123456789c6"
		}
		record := BackupRecord{SchemaVersion: BackupManifestSchemaVersion, BackupID: backupID,
			AccountID: request.Identity.AccountID, Scope: BackupScopeDocumentRoot, SourcePath: "public_html",
			CreatedAt: createdAt, PayloadBytes: 128, ContentBytes: 16, EntryCount: 2,
			PayloadSHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64),
			ManifestAuthenticated: true}
		result := &FileWriteResult{Backup: &record, BackupRepository: &BackupRepositoryStatus{
			LimitBytes: DefaultBackupRepositoryBytes, MaximumBackups: MaximumLocalBackups, BackupCount: 1,
		}}
		switch action {
		case FileWriteBackupCreate:
			record.Scope, record.SourcePath, record.PayloadVerified = request.BackupScope, request.BackupPath, true
			result.Completed = true
		case FileWriteBackupList:
			result.Backup, result.Backups = nil, []BackupRecord{record}
		case FileWriteBackupVerify:
			record.PayloadVerified = true
		case FileWriteBackupRestore:
			record.PayloadVerified, result.Completed, result.OperationID = true, true, request.OperationID
		}
		response := FileWriteResponse{ProtocolVersion: WireVersion, RequestID: request.RequestID, Result: result}
		if err := ValidateFileWriteResponse(response, request); err != nil {
			t.Fatalf("valid %s response: %v", action, err)
		}
		if action == FileWriteBackupCreate || action == FileWriteBackupRestore {
			request.Correlation = nil
			if ValidateFileWriteRequest(request) == nil {
				t.Fatalf("%s accepted without audit correlation", action)
			}
		}
	}
}

func TestBackupTransferRetentionAndQuotaContracts(t *testing.T) {
	t.Parallel()
	createdAt := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	status := &BackupRepositoryStatus{UsedBytes: 512, LimitBytes: DefaultBackupRepositoryBytes,
		BackupCount: 1, MaximumBackups: MaximumLocalBackups, ActiveUploads: 1}
	for _, action := range []FileWriteAction{FileWriteBackupUploadInitiate, FileWriteBackupUploadStatus,
		FileWriteBackupUploadChunk, FileWriteBackupUploadCancel} {
		request := validFileWriteRequest(action)
		if err := ValidateFileWriteRequest(request); err != nil {
			t.Fatalf("valid %s request: %v", action, err)
		}
		result := &FileWriteResult{UploadID: request.UploadID, SizeBytes: 128, ReceivedBytes: 64,
			CreatedAt: createdAt, BackupRepository: status}
		if action == FileWriteBackupUploadCancel {
			result = &FileWriteResult{UploadID: request.UploadID, Completed: true,
				BackupRepository: &BackupRepositoryStatus{LimitBytes: DefaultBackupRepositoryBytes,
					MaximumBackups: MaximumLocalBackups}}
		}
		response := FileWriteResponse{ProtocolVersion: WireVersion, RequestID: request.RequestID, Result: result}
		if err := ValidateFileWriteResponse(response, request); err != nil {
			t.Fatalf("valid %s response: %v", action, err)
		}
	}
	complete := validFileWriteRequest(FileWriteBackupUploadComplete)
	record := BackupRecord{SchemaVersion: BackupManifestSchemaVersion, BackupID: complete.UploadID,
		AccountID: complete.Identity.AccountID, Scope: complete.BackupScope, SourcePath: complete.BackupPath,
		CreatedAt: createdAt, PayloadBytes: complete.SizeBytes, EntryCount: 1,
		PayloadSHA256: complete.ExpectedSHA256, ManifestSHA256: strings.Repeat("b", 64),
		ManifestAuthenticated: true, PayloadVerified: true}
	response := FileWriteResponse{ProtocolVersion: WireVersion, RequestID: complete.RequestID,
		Result: &FileWriteResult{Backup: &record, Completed: true, BackupRepository: status}}
	if err := ValidateFileWriteResponse(response, complete); err != nil {
		t.Fatalf("valid completion response: %v", err)
	}
	deleted := validFileWriteRequest(FileWriteBackupDelete)
	response = FileWriteResponse{ProtocolVersion: WireVersion, RequestID: deleted.RequestID,
		Result: &FileWriteResult{Completed: true, BackupRepository: status}}
	if err := ValidateFileWriteResponse(response, deleted); err != nil {
		t.Fatalf("valid deletion response: %v", err)
	}
	deleted.BackupLimitBytes = MaximumBackupRepositoryBytes + 1
	if ValidateFileWriteRequest(deleted) == nil {
		t.Fatal("oversized backup repository limit accepted")
	}
}

func TestFileArchiveContractsBindFormatPathsAndResult(t *testing.T) {
	t.Parallel()
	for _, action := range []FileWriteAction{FileWriteArchiveCreate, FileWriteArchiveExtract} {
		request := validFileWriteRequest(action)
		if err := ValidateFileWriteRequest(request); err != nil {
			t.Fatalf("valid %s request: %v", action, err)
		}
		response := FileWriteResponse{ProtocolVersion: WireVersion, RequestID: request.RequestID,
			Result: &FileWriteResult{OperationID: request.OperationID, SourceDirectory: request.SourceDirectory,
				SourceName: request.SourceName, Directory: request.Directory, Name: request.Name,
				ArchiveFormat: request.ArchiveFormat, EntryCount: 2, SizeBytes: 128, Completed: true}}
		if err := ValidateFileWriteResponse(response, request); err != nil {
			t.Fatalf("valid %s response: %v", action, err)
		}
		request.ArchiveFormat = "rar"
		if err := ValidateFileWriteRequest(request); err == nil {
			t.Fatalf("%s accepted an unsupported archive format", action)
		}
	}
}

func TestFileTrashResponseUsesBoundedOrderedPages(t *testing.T) {
	t.Parallel()
	request := validFileWriteRequest(FileWriteTrashList)
	first := "019c1234-5678-7abc-8def-0123456789c0"
	second := "019c1234-5678-7abc-8def-0123456789c1"
	response := FileWriteResponse{ProtocolVersion: WireVersion, RequestID: request.RequestID,
		Result: &FileWriteResult{TrashEntries: []FileTrashEntry{
			{TrashID: first, Directory: "public_html", Name: "a.txt", Type: FileEntryRegular,
				SizeBytes: 4, TrashedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)},
			{TrashID: second, Directory: "public_html", Name: "assets", Type: FileEntryDirectory,
				TrashedAt: time.Date(2026, 8, 29, 12, 1, 0, 0, time.UTC)},
		}, Next: second}}
	if err := ValidateFileWriteResponse(response, request); err != nil {
		t.Fatalf("valid trash page: %v", err)
	}
	response.Result.TrashEntries[1].TrashID = first
	if err := ValidateFileWriteResponse(response, request); err == nil {
		t.Fatal("duplicate/unordered trash page accepted")
	}
}

func TestFileWriteResponseBindsUploadProgressToRequest(t *testing.T) {
	t.Parallel()
	request := validFileWriteRequest(FileWriteChunk)
	response := FileWriteResponse{ProtocolVersion: WireVersion, RequestID: request.RequestID,
		Result: &FileWriteResult{UploadID: request.UploadID, Directory: "public_html", Name: "site.bin",
			SizeBytes: 16, ReceivedBytes: 8, CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}}
	if err := ValidateFileWriteResponse(response, request); err != nil {
		t.Fatalf("valid response: %v", err)
	}
	response.Result.UploadID = "019c1234-5678-7abc-8def-0123456789af"
	if err := ValidateFileWriteResponse(response, request); err == nil {
		t.Fatal("mismatched upload response accepted")
	}
}

func validFileWriteRequest(action FileWriteAction) FileWriteRequest {
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{AccountID: accountID, Username: username,
		UID: hostingidentity.MinimumID, GID: hostingidentity.MinimumID, HomeDirectory: home}
	request := FileWriteRequest{ProtocolVersion: WireVersion, RequestID: "file-write-test", Action: action,
		Identity: identity, UploadID: "019c1234-5678-7abc-8def-0123456789ae"}
	request.Correlation = &FileAuditCorrelation{AuditEventID: "019c1234-5678-7abc-8def-0123456789af",
		ActorID: "019c1234-5678-7abc-8def-0123456789b0", SessionID: "019c1234-5678-7abc-8def-0123456789b1",
		AccountID: identity.AccountID, RequestID: "browser-file-write"}
	switch action {
	case FileWriteInitiate, FileWriteComplete:
		request.Directory, request.Name, request.SizeBytes = "public_html", "site.bin", 16
	case FileWriteChunk:
		request.Correlation, request.Offset, request.ChunkLength = nil, 0, 8
	case FileWriteStatus:
		request.Correlation = nil
	case FileWriteCancel:
	case FileWriteCreateFile, FileWriteCreateDirectory:
		request.UploadID, request.Directory, request.Name = "", "public_html", "site.bin"
	case FileWriteRename:
		request.UploadID, request.SourceDirectory, request.SourceName = "", "public_html", "site.bin"
		request.Directory, request.Name = "public_html", "renamed.bin"
	case FileWriteMove:
		request.UploadID, request.SourceDirectory, request.SourceName = "", "public_html", "site.bin"
		request.Directory, request.Name = "public_html/assets", "site.bin"
	case FileWriteCopy:
		request.UploadID, request.OperationID = "", "019c1234-5678-7abc-8def-0123456789c2"
		request.SourceDirectory, request.SourceName = "public_html", "site.bin"
		request.Directory, request.Name = "public_html/assets", "site.bin"
	case FileWriteArchiveCreate:
		request.UploadID, request.OperationID = "", "019c1234-5678-7abc-8def-0123456789c4"
		request.SourceDirectory, request.SourceName = "public_html", "assets"
		request.Directory, request.Name = "public_html", "assets.zip"
		request.ArchiveFormat = FileArchiveZIP
	case FileWriteArchiveExtract:
		request.UploadID, request.OperationID = "", "019c1234-5678-7abc-8def-0123456789c5"
		request.SourceDirectory, request.SourceName = "public_html", "assets.tar.gz"
		request.Directory, request.Name = "public_html", "restored-assets"
		request.ArchiveFormat = FileArchiveTarGzip
	case FileWriteBackupCreate:
		request.UploadID, request.BackupID = "", "019c1234-5678-7abc-8def-0123456789c6"
		request.BackupScope, request.BackupPath = BackupScopeDocumentRoot, "public_html"
	case FileWriteBackupList:
		request.UploadID, request.Correlation = "", nil
	case FileWriteBackupInspect, FileWriteBackupVerify:
		request.UploadID, request.BackupID, request.Correlation = "", "019c1234-5678-7abc-8def-0123456789c6", nil
	case FileWriteBackupRestore:
		request.UploadID, request.BackupID = "", "019c1234-5678-7abc-8def-0123456789c6"
		request.OperationID = "019c1234-5678-7abc-8def-0123456789c7"
	case FileWriteBackupUploadInitiate, FileWriteBackupUploadComplete:
		request.BackupScope, request.BackupPath = BackupScopeDocumentRoot, "public_html"
		request.SizeBytes, request.ExpectedSHA256 = 128, strings.Repeat("a", 64)
	case FileWriteBackupUploadStatus:
		request.Correlation = nil
	case FileWriteBackupUploadChunk:
		request.Correlation, request.Offset, request.ChunkLength = nil, 0, 64
	case FileWriteBackupUploadCancel:
	case FileWriteBackupDelete:
		request.UploadID, request.BackupID = "", "019c1234-5678-7abc-8def-0123456789c6"
	case FileWriteTrash:
		request.UploadID, request.TrashID = "", "019c1234-5678-7abc-8def-0123456789c3"
		request.SourceDirectory, request.SourceName = "public_html", "site.bin"
	case FileWriteTrashList:
		request.UploadID, request.Correlation = "", nil
	case FileWriteTrashRestore, FileWriteTrashPurge:
		request.UploadID, request.TrashID = "", "019c1234-5678-7abc-8def-0123456789c3"
	}
	return request
}
