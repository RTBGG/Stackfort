// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/google/uuid"
)

const (
	FileWriteEndpoint            = "/stream/v1/files/write"
	FileWriteMediaType           = "application/vnd.stackfort.file-write"
	FileWriteControlHeader       = "X-Stackfort-Control-Length"
	MaxFileWriteControlBytes     = 8 << 10
	MaxFileWriteResponseBytes    = 64 << 10
	MaximumFileUploadBytes       = uint64(4 << 30)
	MaximumFileUploadChunkBytes  = uint64(8 << 20)
	MaximumFileOperationEntries  = uint64(10_000)
	MaximumFileOperationDepth    = uint32(64)
	MaximumFileTrashEntries      = 10
	MaximumBackupListEntries     = 20
	MaximumLocalBackups          = 256
	MaximumBackupUploads         = 8
	DefaultBackupRepositoryBytes = uint64(20 << 30)
	MaximumBackupRepositoryBytes = uint64(1 << 40)
	MaximumFileWriteDuration     = 30 * time.Minute
)

const (
	ReservedFileUploadDirectory    = ".stackfort-uploads"
	ReservedFileOperationDirectory = ".stackfort-operations"
	ReservedFileTrashDirectory     = ".stackfort-trash"
)

type FileWriteAction string

const (
	FileWriteInitiate             FileWriteAction = "upload.initiate"
	FileWriteStatus               FileWriteAction = "upload.status"
	FileWriteChunk                FileWriteAction = "upload.chunk"
	FileWriteComplete             FileWriteAction = "upload.complete"
	FileWriteCancel               FileWriteAction = "upload.cancel"
	FileWriteCreateFile           FileWriteAction = "node.create-file"
	FileWriteCreateDirectory      FileWriteAction = "node.create-directory"
	FileWriteRename               FileWriteAction = "node.rename"
	FileWriteMove                 FileWriteAction = "node.move"
	FileWriteCopy                 FileWriteAction = "node.copy"
	FileWriteArchiveCreate        FileWriteAction = "archive.create"
	FileWriteArchiveExtract       FileWriteAction = "archive.extract"
	FileWriteBackupCreate         FileWriteAction = "backup.create"
	FileWriteBackupList           FileWriteAction = "backup.list"
	FileWriteBackupInspect        FileWriteAction = "backup.inspect"
	FileWriteBackupVerify         FileWriteAction = "backup.verify"
	FileWriteBackupRestore        FileWriteAction = "backup.restore"
	FileWriteBackupUploadInitiate FileWriteAction = "backup.upload.initiate"
	FileWriteBackupUploadStatus   FileWriteAction = "backup.upload.status"
	FileWriteBackupUploadChunk    FileWriteAction = "backup.upload.chunk"
	FileWriteBackupUploadComplete FileWriteAction = "backup.upload.complete"
	FileWriteBackupUploadCancel   FileWriteAction = "backup.upload.cancel"
	FileWriteBackupDelete         FileWriteAction = "backup.delete"
	FileWriteTrash                FileWriteAction = "trash.put"
	FileWriteTrashList            FileWriteAction = "trash.list"
	FileWriteTrashRestore         FileWriteAction = "trash.restore"
	FileWriteTrashPurge           FileWriteAction = "trash.purge"
)

type FileArchiveFormat string

const (
	FileArchiveZIP     FileArchiveFormat = "zip"
	FileArchiveTarGzip FileArchiveFormat = "tar_gzip"
)

// FileAuditCorrelation binds a visible namespace mutation to an authorization
// event that was durably appended before the agent is called.
type FileAuditCorrelation struct {
	AuditEventID string `json:"auditEventId"`
	ActorID      string `json:"actorId"`
	SessionID    string `json:"sessionId"`
	AccountID    string `json:"accountId"`
	RequestID    string `json:"requestId"`
}

// FileWriteRequest is a closed control union. Raw bytes follow the encoded
// control prefix only for upload.chunk and are exactly ChunkLength bytes long.
type FileWriteRequest struct {
	ProtocolVersion  int                   `json:"protocolVersion"`
	RequestID        string                `json:"requestId"`
	Action           FileWriteAction       `json:"action"`
	Identity         hostingidentity.Spec  `json:"identity"`
	UploadID         string                `json:"uploadId,omitempty"`
	OperationID      string                `json:"operationId,omitempty"`
	TrashID          string                `json:"trashId,omitempty"`
	SourceDirectory  string                `json:"sourceDirectory,omitempty"`
	SourceName       string                `json:"sourceName,omitempty"`
	Directory        string                `json:"directory,omitempty"`
	Name             string                `json:"name,omitempty"`
	Cursor           string                `json:"cursor,omitempty"`
	ArchiveFormat    FileArchiveFormat     `json:"archiveFormat,omitempty"`
	BackupID         string                `json:"backupId,omitempty"`
	BackupScope      BackupScope           `json:"backupScope,omitempty"`
	BackupPath       string                `json:"backupPath,omitempty"`
	BackupLimitBytes uint64                `json:"backupLimitBytes,omitempty"`
	SizeBytes        uint64                `json:"sizeBytes,omitempty"`
	ExpectedSHA256   string                `json:"expectedSha256,omitempty"`
	Offset           uint64                `json:"offset,omitempty"`
	ChunkLength      uint64                `json:"chunkLength,omitempty"`
	Correlation      *FileAuditCorrelation `json:"correlation,omitempty"`
}

type FileWriteResult struct {
	UploadID         string                  `json:"uploadId,omitempty"`
	OperationID      string                  `json:"operationId,omitempty"`
	TrashID          string                  `json:"trashId,omitempty"`
	SourceDirectory  string                  `json:"sourceDirectory,omitempty"`
	SourceName       string                  `json:"sourceName,omitempty"`
	Directory        string                  `json:"directory,omitempty"`
	Name             string                  `json:"name,omitempty"`
	SizeBytes        uint64                  `json:"sizeBytes,omitempty"`
	ReceivedBytes    uint64                  `json:"receivedBytes,omitempty"`
	SHA256           string                  `json:"sha256,omitempty"`
	CreatedAt        time.Time               `json:"createdAt,omitempty"`
	Completed        bool                    `json:"completed,omitempty"`
	TrashEntries     []FileTrashEntry        `json:"trashEntries"`
	Next             string                  `json:"next,omitempty"`
	ArchiveFormat    FileArchiveFormat       `json:"archiveFormat,omitempty"`
	EntryCount       uint64                  `json:"entryCount,omitempty"`
	Backup           *BackupRecord           `json:"backup,omitempty"`
	Backups          []BackupRecord          `json:"backups,omitempty"`
	BackupRepository *BackupRepositoryStatus `json:"backupRepository,omitempty"`
}

type FileTrashEntry struct {
	TrashID   string        `json:"trashId"`
	Directory string        `json:"directory"`
	Name      string        `json:"name"`
	Type      FileEntryType `json:"type"`
	SizeBytes uint64        `json:"sizeBytes"`
	TrashedAt time.Time     `json:"trashedAt"`
}

type FileWriteResponse struct {
	ProtocolVersion int              `json:"protocolVersion"`
	RequestID       string           `json:"requestId"`
	Result          *FileWriteResult `json:"result,omitempty"`
	Error           *ResponseError   `json:"error,omitempty"`
}

var lowercaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidateFileWriteRequest(request FileWriteRequest) error {
	if request.ProtocolVersion != WireVersion || !validBoundedIdentifier(request.RequestID) ||
		hostingidentity.Validate(request.Identity) != nil {
		return errors.New("file write request is invalid")
	}
	if request.UploadID != "" && !validFileUploadID(request.UploadID) {
		return errors.New("file write upload id is invalid")
	}
	if request.OperationID != "" && !validFileUploadID(request.OperationID) {
		return errors.New("file write operation id is invalid")
	}
	if request.TrashID != "" && !validFileUploadID(request.TrashID) {
		return errors.New("file write trash id is invalid")
	}
	if request.BackupID != "" && !validFileUploadID(request.BackupID) {
		return errors.New("file backup id is invalid")
	}
	if request.Cursor != "" && !validFileUploadID(request.Cursor) {
		return errors.New("file write cursor is invalid")
	}
	if request.ExpectedSHA256 != "" && !lowercaseSHA256Pattern.MatchString(request.ExpectedSHA256) {
		return errors.New("file write checksum is invalid")
	}
	archiveAction := request.Action == FileWriteArchiveCreate || request.Action == FileWriteArchiveExtract
	if archiveAction != validFileArchiveFormat(request.ArchiveFormat) {
		return errors.New("file archive format is invalid")
	}
	backupAction := isFileBackupAction(request.Action)
	if !backupAction && (request.BackupID != "" || request.BackupScope != "" || request.BackupPath != "" || request.BackupLimitBytes != 0) {
		return errors.New("file backup fields are not allowed")
	}
	if request.BackupLimitBytes != 0 && (request.BackupLimitBytes < 1<<20 || request.BackupLimitBytes > MaximumBackupRepositoryBytes) {
		return errors.New("file backup repository limit is invalid")
	}
	if request.BackupPath != "" {
		normalized, err := hostingpath.NormalizeDocumentRoot(request.BackupPath)
		if err != nil || normalized != request.BackupPath || reservedFileManagerPath(request.BackupPath, "") {
			return errors.New("file backup path is invalid")
		}
	}
	if request.SizeBytes > MaximumFileUploadBytes || request.Offset > MaximumFileUploadBytes ||
		request.ChunkLength > MaximumFileUploadChunkBytes || request.Offset > MaximumFileUploadBytes-request.ChunkLength {
		return errors.New("file write size is invalid")
	}
	if request.Directory != "" {
		normalized, err := hostingpath.NormalizeFileManagerDirectory(request.Directory)
		if err != nil || normalized != request.Directory {
			return errors.New("file write directory is invalid")
		}
	}
	if request.Name != "" && !hostingpath.ValidFilename(request.Name) {
		return errors.New("file write name is invalid")
	}
	if request.SourceDirectory != "" {
		normalized, err := hostingpath.NormalizeFileManagerDirectory(request.SourceDirectory)
		if err != nil || normalized != request.SourceDirectory {
			return errors.New("file write source directory is invalid")
		}
	}
	if request.SourceName != "" && !hostingpath.ValidFilename(request.SourceName) {
		return errors.New("file write source name is invalid")
	}
	if reservedFileManagerPath(request.Directory, request.Name) ||
		reservedFileManagerPath(request.SourceDirectory, request.SourceName) {
		return errors.New("file write path is reserved")
	}

	mutation := request.Action == FileWriteInitiate || request.Action == FileWriteComplete ||
		request.Action == FileWriteCancel || request.Action == FileWriteCreateFile ||
		request.Action == FileWriteCreateDirectory || request.Action == FileWriteRename ||
		request.Action == FileWriteMove || request.Action == FileWriteCopy ||
		request.Action == FileWriteArchiveCreate || request.Action == FileWriteArchiveExtract ||
		request.Action == FileWriteBackupCreate || request.Action == FileWriteBackupRestore ||
		request.Action == FileWriteBackupUploadInitiate || request.Action == FileWriteBackupUploadComplete ||
		request.Action == FileWriteBackupUploadCancel || request.Action == FileWriteBackupDelete ||
		request.Action == FileWriteTrash || request.Action == FileWriteTrashRestore ||
		request.Action == FileWriteTrashPurge
	if mutation {
		if validateFileAuditCorrelation(request.Correlation, request.Identity.AccountID) != nil {
			return errors.New("file write audit correlation is invalid")
		}
	} else if request.Correlation != nil {
		return errors.New("file write audit correlation is not allowed")
	}

	switch request.Action {
	case FileWriteInitiate:
		if request.UploadID == "" || request.Name == "" || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" || request.Cursor != "" {
			return errors.New("file upload initiation is invalid")
		}
	case FileWriteStatus, FileWriteCancel:
		if request.UploadID == "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" || request.Cursor != "" {
			return errors.New("file upload lookup is invalid")
		}
	case FileWriteChunk:
		if request.UploadID == "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.ChunkLength == 0 || request.OperationID != "" || request.TrashID != "" ||
			request.SourceDirectory != "" || request.SourceName != "" || request.Cursor != "" {
			return errors.New("file upload chunk is invalid")
		}
	case FileWriteComplete:
		if request.UploadID == "" || request.Name == "" || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" || request.Cursor != "" {
			return errors.New("file upload completion is invalid")
		}
	case FileWriteCreateFile, FileWriteCreateDirectory:
		if request.UploadID != "" || request.Name == "" || request.SizeBytes != 0 || request.ExpectedSHA256 != "" ||
			request.Offset != 0 || request.ChunkLength != 0 || request.OperationID != "" || request.TrashID != "" ||
			request.SourceDirectory != "" || request.SourceName != "" || request.Cursor != "" {
			return errors.New("file node creation is invalid")
		}
	case FileWriteRename, FileWriteMove:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID != "" || request.SourceName == "" ||
			request.Name == "" || request.SizeBytes != 0 || request.ExpectedSHA256 != "" || request.Offset != 0 ||
			request.ChunkLength != 0 || request.Cursor != "" ||
			joinedManagerPath(request.SourceDirectory, request.SourceName) == joinedManagerPath(request.Directory, request.Name) ||
			(request.Action == FileWriteRename && request.SourceDirectory != request.Directory) ||
			(request.Action == FileWriteMove && request.SourceDirectory == request.Directory) {
			return errors.New("file node relocation is invalid")
		}
	case FileWriteCopy:
		if request.UploadID != "" || request.OperationID == "" || request.TrashID != "" || request.SourceName == "" ||
			request.Name == "" || request.SizeBytes != 0 || request.ExpectedSHA256 != "" || request.Offset != 0 ||
			request.ChunkLength != 0 || request.Cursor != "" ||
			joinedManagerPath(request.SourceDirectory, request.SourceName) == joinedManagerPath(request.Directory, request.Name) {
			return errors.New("file node copy is invalid")
		}
	case FileWriteArchiveCreate, FileWriteArchiveExtract:
		if request.UploadID != "" || request.OperationID == "" || request.TrashID != "" || request.SourceName == "" ||
			request.Name == "" || request.SizeBytes != 0 || request.ExpectedSHA256 != "" || request.Offset != 0 ||
			request.ChunkLength != 0 || request.Cursor != "" ||
			joinedManagerPath(request.SourceDirectory, request.SourceName) == joinedManagerPath(request.Directory, request.Name) ||
			(request.Action == FileWriteArchiveCreate && !archiveNameMatchesFormat(request.Name, request.ArchiveFormat)) ||
			(request.Action == FileWriteArchiveExtract && !archiveNameMatchesFormat(request.SourceName, request.ArchiveFormat)) {
			return errors.New("file archive request is invalid")
		}
	case FileWriteBackupCreate:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID != "" || request.BackupID == "" ||
			!ValidBackupScopePath(request.BackupScope, request.BackupPath) || request.SourceDirectory != "" ||
			request.SourceName != "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 || request.Cursor != "" {
			return errors.New("file backup creation is invalid")
		}
	case FileWriteBackupList:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID != "" || request.BackupID != "" ||
			request.BackupScope != "" || request.BackupPath != "" || request.SourceDirectory != "" ||
			request.SourceName != "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 {
			return errors.New("file backup listing is invalid")
		}
	case FileWriteBackupInspect, FileWriteBackupVerify:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID != "" || request.BackupID == "" ||
			request.BackupScope != "" || request.BackupPath != "" || request.SourceDirectory != "" ||
			request.SourceName != "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 || request.Cursor != "" {
			return errors.New("file backup inspection is invalid")
		}
	case FileWriteBackupRestore:
		if request.UploadID != "" || request.OperationID == "" || request.TrashID != "" || request.BackupID == "" ||
			request.BackupScope != "" || request.BackupPath != "" || request.SourceDirectory != "" ||
			request.SourceName != "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 || request.Cursor != "" {
			return errors.New("file backup restore is invalid")
		}
	case FileWriteBackupUploadInitiate:
		if request.UploadID == "" || request.BackupID != "" || !ValidBackupScopePath(request.BackupScope, request.BackupPath) ||
			request.SizeBytes == 0 || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" ||
			request.Directory != "" || request.Name != "" || request.Cursor != "" {
			return errors.New("file backup upload initiation is invalid")
		}
	case FileWriteBackupUploadStatus, FileWriteBackupUploadCancel:
		if request.UploadID == "" || request.BackupID != "" || request.BackupScope != "" || request.BackupPath != "" ||
			request.SizeBytes != 0 || request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" ||
			request.Directory != "" || request.Name != "" || request.Cursor != "" {
			return errors.New("file backup upload lookup is invalid")
		}
	case FileWriteBackupUploadChunk:
		if request.UploadID == "" || request.BackupID != "" || request.BackupScope != "" || request.BackupPath != "" ||
			request.SizeBytes != 0 || request.ExpectedSHA256 != "" || request.ChunkLength == 0 || request.OperationID != "" ||
			request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" || request.Directory != "" ||
			request.Name != "" || request.Cursor != "" {
			return errors.New("file backup upload chunk is invalid")
		}
	case FileWriteBackupUploadComplete:
		if request.UploadID == "" || request.BackupID != "" || !ValidBackupScopePath(request.BackupScope, request.BackupPath) ||
			request.SizeBytes == 0 || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" ||
			request.Directory != "" || request.Name != "" || request.Cursor != "" {
			return errors.New("file backup upload completion is invalid")
		}
	case FileWriteBackupDelete:
		if request.BackupID == "" || request.UploadID != "" || request.BackupScope != "" || request.BackupPath != "" ||
			request.SizeBytes != 0 || request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 ||
			request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" || request.SourceName != "" ||
			request.Directory != "" || request.Name != "" || request.Cursor != "" {
			return errors.New("file backup deletion is invalid")
		}
	case FileWriteTrash:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID == "" || request.SourceName == "" ||
			request.Directory != "" || request.Name != "" || request.SizeBytes != 0 || request.ExpectedSHA256 != "" ||
			request.Offset != 0 || request.ChunkLength != 0 || request.Cursor != "" {
			return errors.New("file trash request is invalid")
		}
	case FileWriteTrashList:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID != "" || request.SourceDirectory != "" ||
			request.SourceName != "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 {
			return errors.New("file trash listing is invalid")
		}
	case FileWriteTrashRestore, FileWriteTrashPurge:
		if request.UploadID != "" || request.OperationID != "" || request.TrashID == "" || request.SourceDirectory != "" ||
			request.SourceName != "" || request.Directory != "" || request.Name != "" || request.SizeBytes != 0 ||
			request.ExpectedSHA256 != "" || request.Offset != 0 || request.ChunkLength != 0 || request.Cursor != "" {
			return errors.New("file trash mutation is invalid")
		}
	default:
		return errors.New("file write action is invalid")
	}
	return nil
}

func DecodeFileWriteRequest(reader io.Reader) (FileWriteRequest, error) {
	var request FileWriteRequest
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return FileWriteRequest{}, fmt.Errorf("decode file write request: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return FileWriteRequest{}, err
	}
	if err := ValidateFileWriteRequest(request); err != nil {
		return FileWriteRequest{}, err
	}
	return request, nil
}

func ValidateFileWriteResponse(response FileWriteResponse, request FileWriteRequest) error {
	if response.ProtocolVersion != WireVersion || response.RequestID != request.RequestID ||
		!validBoundedIdentifier(response.RequestID) || (response.Result == nil) == (response.Error == nil) {
		return errors.New("file write response is invalid")
	}
	if response.Error != nil {
		if response.Error.Message == "" || len(response.Error.Message) > 256 || response.Error.Capability != nil ||
			!validFileWriteErrorCode(response.Error.Code) {
			return errors.New("file write error response is invalid")
		}
		return nil
	}
	result := *response.Result
	if result.UploadID != "" && !validFileUploadID(result.UploadID) {
		return errors.New("file write result upload id is invalid")
	}
	if result.OperationID != "" && !validFileUploadID(result.OperationID) {
		return errors.New("file write result operation id is invalid")
	}
	if result.TrashID != "" && !validFileUploadID(result.TrashID) {
		return errors.New("file write result trash id is invalid")
	}
	if result.Backup != nil && !validBackupRecord(*result.Backup, request.Identity.AccountID) {
		return errors.New("file backup result is invalid")
	}
	if result.Directory != "" {
		normalized, err := hostingpath.NormalizeFileManagerDirectory(result.Directory)
		if err != nil || normalized != result.Directory {
			return errors.New("file write result directory is invalid")
		}
	}
	if result.Name != "" && !hostingpath.ValidFilename(result.Name) {
		return errors.New("file write result name is invalid")
	}
	if result.SourceDirectory != "" {
		normalized, err := hostingpath.NormalizeFileManagerDirectory(result.SourceDirectory)
		if err != nil || normalized != result.SourceDirectory {
			return errors.New("file write result source directory is invalid")
		}
	}
	if result.SourceName != "" && !hostingpath.ValidFilename(result.SourceName) {
		return errors.New("file write result source name is invalid")
	}
	if result.SizeBytes > MaximumFileUploadBytes || result.ReceivedBytes > result.SizeBytes ||
		result.EntryCount > MaximumFileOperationEntries ||
		(result.SHA256 != "" && !lowercaseSHA256Pattern.MatchString(result.SHA256)) ||
		(!result.CreatedAt.IsZero() && result.CreatedAt.Location() != time.UTC) {
		return errors.New("file write result metadata is invalid")
	}
	archiveAction := request.Action == FileWriteArchiveCreate || request.Action == FileWriteArchiveExtract
	if !archiveAction && (result.ArchiveFormat != "" || result.EntryCount != 0) {
		return errors.New("file write result archive metadata is invalid")
	}
	backupAction := isFileBackupAction(request.Action)
	if !backupAction && (result.Backup != nil || result.Backups != nil || result.BackupRepository != nil) {
		return errors.New("file write result backup metadata is invalid")
	}
	if request.Action == FileWriteInitiate || request.Action == FileWriteStatus || request.Action == FileWriteChunk {
		if result.UploadID != request.UploadID || result.Name == "" || result.CreatedAt.IsZero() || result.Completed {
			return errors.New("file upload result is invalid")
		}
	}
	if request.Action == FileWriteComplete {
		if result.UploadID != request.UploadID || result.Name != request.Name || result.Directory != request.Directory ||
			result.SizeBytes != request.SizeBytes || result.ReceivedBytes != request.SizeBytes || result.SHA256 == "" ||
			!result.Completed {
			return errors.New("file upload completion result is invalid")
		}
	}
	if request.Action == FileWriteCancel && (result.UploadID != request.UploadID || !result.Completed) {
		return errors.New("file upload cancellation result is invalid")
	}
	if (request.Action == FileWriteCreateFile || request.Action == FileWriteCreateDirectory) &&
		(result.Directory != request.Directory || result.Name != request.Name || !result.Completed) {
		return errors.New("file node result is invalid")
	}
	if request.Action == FileWriteRename || request.Action == FileWriteMove || request.Action == FileWriteCopy {
		if result.SourceDirectory != request.SourceDirectory || result.SourceName != request.SourceName ||
			result.Directory != request.Directory || result.Name != request.Name || !result.Completed ||
			(request.Action == FileWriteCopy && result.OperationID != request.OperationID) ||
			(request.Action != FileWriteCopy && result.OperationID != "") || result.UploadID != "" || result.TrashID != "" ||
			result.SizeBytes != 0 || result.ReceivedBytes != 0 || result.SHA256 != "" || !result.CreatedAt.IsZero() ||
			result.TrashEntries != nil || result.Next != "" {
			return errors.New("file node mutation result is invalid")
		}
	}
	if archiveAction {
		if result.OperationID != request.OperationID || result.SourceDirectory != request.SourceDirectory ||
			result.SourceName != request.SourceName || result.Directory != request.Directory || result.Name != request.Name ||
			result.ArchiveFormat != request.ArchiveFormat || result.EntryCount == 0 || !result.Completed ||
			(request.Action == FileWriteArchiveCreate && result.SizeBytes == 0) || result.UploadID != "" || result.TrashID != "" ||
			result.ReceivedBytes != 0 || result.SHA256 != "" || !result.CreatedAt.IsZero() ||
			result.TrashEntries != nil || result.Next != "" {
			return errors.New("file archive result is invalid")
		}
	}
	if request.Action == FileWriteTrash && (result.TrashID != request.TrashID ||
		result.Directory != request.SourceDirectory || result.Name != request.SourceName || !result.Completed ||
		result.UploadID != "" || result.OperationID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
		result.SizeBytes != 0 || result.ReceivedBytes != 0 || result.SHA256 != "" || !result.CreatedAt.IsZero() ||
		result.TrashEntries != nil || result.Next != "") {
		return errors.New("file trash result is invalid")
	}
	if request.Action == FileWriteTrashList {
		if result.TrashEntries == nil || len(result.TrashEntries) > MaximumFileTrashEntries || result.Completed ||
			(result.Next != "" && !validFileUploadID(result.Next)) || result.UploadID != "" || result.OperationID != "" ||
			result.TrashID != "" || result.SourceDirectory != "" || result.SourceName != "" || result.Directory != "" ||
			result.Name != "" || result.SizeBytes != 0 || result.ReceivedBytes != 0 || result.SHA256 != "" ||
			!result.CreatedAt.IsZero() {
			return errors.New("file trash listing result is invalid")
		}
		previous := request.Cursor
		for _, entry := range result.TrashEntries {
			if !validFileUploadID(entry.TrashID) || (previous != "" && entry.TrashID <= previous) ||
				!validTrashEntry(entry) {
				return errors.New("file trash listing entry is invalid")
			}
			previous = entry.TrashID
		}
		if result.Next != "" && (len(result.TrashEntries) == 0 || result.Next != result.TrashEntries[len(result.TrashEntries)-1].TrashID) {
			return errors.New("file trash listing cursor is invalid")
		}
	}
	if request.Action == FileWriteTrashRestore && (result.TrashID != request.TrashID || result.Name == "" || !result.Completed ||
		result.UploadID != "" || result.OperationID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
		result.SizeBytes != 0 || result.ReceivedBytes != 0 || result.SHA256 != "" || !result.CreatedAt.IsZero() ||
		result.TrashEntries != nil || result.Next != "") {
		return errors.New("file trash restore result is invalid")
	}
	if request.Action == FileWriteTrashPurge && (result.TrashID != request.TrashID || !result.Completed ||
		result.UploadID != "" || result.OperationID != "" || result.SourceDirectory != "" || result.SourceName != "" ||
		result.Directory != "" || result.Name != "" || result.SizeBytes != 0 || result.ReceivedBytes != 0 ||
		result.SHA256 != "" || !result.CreatedAt.IsZero() || result.TrashEntries != nil || result.Next != "") {
		return errors.New("file trash purge result is invalid")
	}
	if backupAction && validateFileBackupResult(result, request) != nil {
		return errors.New("file backup operation result is invalid")
	}
	return nil
}

func validFileArchiveFormat(format FileArchiveFormat) bool {
	return format == FileArchiveZIP || format == FileArchiveTarGzip
}

func archiveNameMatchesFormat(name string, format FileArchiveFormat) bool {
	switch format {
	case FileArchiveZIP:
		return strings.HasSuffix(name, ".zip")
	case FileArchiveTarGzip:
		return strings.HasSuffix(name, ".tar.gz")
	default:
		return false
	}
}

func DecodeFileWriteResponse(reader io.Reader, request FileWriteRequest) (FileWriteResponse, error) {
	var response FileWriteResponse
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return FileWriteResponse{}, err
	}
	if err := requireJSONEnd(decoder); err != nil {
		return FileWriteResponse{}, err
	}
	if err := ValidateFileWriteResponse(response, request); err != nil {
		return FileWriteResponse{}, err
	}
	return response, nil
}

func validateFileAuditCorrelation(value *FileAuditCorrelation, accountID string) error {
	if value == nil || !validCanonicalUUIDv7(value.AuditEventID) || !validCanonicalUUIDv7(value.ActorID) ||
		!validCanonicalUUIDv7(value.SessionID) || value.AccountID != accountID ||
		!validCanonicalUUIDv7(value.AccountID) || !validBoundedIdentifier(value.RequestID) {
		return errors.New("file audit correlation is invalid")
	}
	return nil
}

func validFileUploadID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

func validFileWriteErrorCode(code ErrorCode) bool {
	return code == ErrorInvalidRequest || code == ErrorFileNotFound || code == ErrorFileConflict ||
		code == ErrorFileUnavailable || code == ErrorFileDownloadTooLarge || code == ErrorFileDownloadBusy ||
		code == ErrorFileQuotaExceeded || code == ErrorBackupNotFound || code == ErrorBackupConflict ||
		code == ErrorBackupUnavailable || code == ErrorBackupTooLarge || code == ErrorBackupBusy ||
		code == ErrorBackupIntegrity || code == ErrorBackupQuotaExceeded || code == ErrorMutationFailed || code == ErrorInternal
}

func reservedFileManagerPath(directory, name string) bool {
	for _, reserved := range []string{ReservedFileUploadDirectory, ReservedFileOperationDirectory, ReservedFileTrashDirectory} {
		if directory == reserved || strings.HasPrefix(directory, reserved+"/") || (directory == "" && name == reserved) {
			return true
		}
	}
	return false
}

func joinedManagerPath(directory, name string) string {
	if directory == "" {
		return name
	}
	return directory + "/" + name
}

func validTrashEntry(entry FileTrashEntry) bool {
	if entry.Directory != "" {
		normalized, err := hostingpath.NormalizeFileManagerDirectory(entry.Directory)
		if err != nil || normalized != entry.Directory {
			return false
		}
	}
	return hostingpath.ValidFilename(entry.Name) &&
		(entry.Type == FileEntryRegular || entry.Type == FileEntryDirectory) &&
		entry.SizeBytes <= MaximumFileUploadBytes && !entry.TrashedAt.IsZero() && entry.TrashedAt.Location() == time.UTC &&
		!reservedFileManagerPath(entry.Directory, entry.Name)
}
