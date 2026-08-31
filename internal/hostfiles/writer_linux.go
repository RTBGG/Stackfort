// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"golang.org/x/sys/unix"
)

const (
	fileUploadStagingDirectory = agentprotocol.ReservedFileUploadDirectory
	maximumActiveFileUploads   = 8
	maximumStagingScanEntries  = 256
	maximumUploadMetadataBytes = 2 << 10
)

type linuxWriter struct{ executable string }

type uploadMetadata struct {
	UploadID       string    `json:"uploadId"`
	Directory      string    `json:"directory"`
	Name           string    `json:"name"`
	SizeBytes      uint64    `json:"sizeBytes"`
	ExpectedSHA256 string    `json:"expectedSha256,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

func platformWriteExecutable() string { return "/proc/self/exe" }

func newPlatformWriter(executable string) platformWriter { return &linuxWriter{executable: executable} }

func (writer *linuxWriter) Execute(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if writer == nil || writer.executable == "" {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > agentprotocol.MaxFileWriteControlBytes {
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
	framed := io.MultiReader(bytes.NewReader(append(encoded, '\n')), io.LimitReader(body, int64(request.ChunkLength))) // #nosec G115 -- chunk length is capped at 8 MiB.
	command := exec.CommandContext(ctx, writer.executable, WriteHelperArgument)                                        // #nosec G204 -- executable is the running agent or qualification-owned binary.
	command.Stdin = framed
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{
		Uid: request.Identity.UID, Gid: request.Identity.GID, NoSetGroups: true,
	}, Pdeathsig: syscall.SIGKILL}
	output, err := command.Output()
	if err != nil || len(output) > agentprotocol.MaxFileWriteResponseBytes {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	response, err := agentprotocol.DecodeFileWriteResponse(bytes.NewReader(output), request)
	if err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if response.Error != nil {
		return agentprotocol.FileWriteResult{}, writeErrorFromCode(response.Error.Code)
	}
	return *response.Result, nil
}

func runPlatformWriteHelper(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, agentprotocol.MaxFileWriteControlBytes+1)
	line, err := reader.ReadSlice('\n')
	if err != nil || len(line) < 2 || len(line) > agentprotocol.MaxFileWriteControlBytes {
		return ErrInvalid
	}
	request, err := agentprotocol.DecodeFileWriteRequest(bytes.NewReader(line))
	effectiveUID, effectiveGID := os.Geteuid(), os.Getegid()
	if err != nil || effectiveUID < 0 || effectiveGID < 0 ||
		uint32(effectiveUID) != request.Identity.UID || // #nosec G115 -- non-negative kernel identity is checked first.
		uint32(effectiveGID) != request.Identity.GID { // #nosec G115 -- non-negative kernel identity is checked first.
		return ErrInvalid
	}
	result, operationErr := executeFileWrite(ctx, request, reader)
	response := agentprotocol.FileWriteResponse{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: request.RequestID, Result: &result,
	}
	if operationErr != nil {
		code, message := writeErrorCode(operationErr)
		response.Result = nil
		response.Error = &agentprotocol.ResponseError{Code: code, Message: message}
	}
	return json.NewEncoder(output).Encode(response)
}

func executeFileWrite(
	ctx context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	if err := ctx.Err(); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	switch request.Action {
	case agentprotocol.FileWriteInitiate:
		return initiateUpload(request)
	case agentprotocol.FileWriteStatus:
		return inspectUpload(request)
	case agentprotocol.FileWriteChunk:
		return writeUploadChunk(request, body)
	case agentprotocol.FileWriteComplete:
		return completeUpload(ctx, request)
	case agentprotocol.FileWriteCancel:
		return cancelUpload(request)
	case agentprotocol.FileWriteCreateFile:
		return createManagedNode(request, false)
	case agentprotocol.FileWriteCreateDirectory:
		return createManagedNode(request, true)
	case agentprotocol.FileWriteRename, agentprotocol.FileWriteMove:
		return relocateManagedNode(request)
	case agentprotocol.FileWriteCopy:
		return copyManagedNode(ctx, request)
	case agentprotocol.FileWriteArchiveCreate:
		return createManagedArchive(ctx, request)
	case agentprotocol.FileWriteArchiveExtract:
		return extractManagedArchive(ctx, request)
	case agentprotocol.FileWriteTrash:
		return trashManagedNode(request)
	case agentprotocol.FileWriteTrashList:
		return listManagedTrash(request)
	case agentprotocol.FileWriteTrashRestore:
		return restoreManagedTrash(request)
	case agentprotocol.FileWriteTrashPurge:
		return purgeManagedTrash(ctx, request)
	default:
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
}

func initiateUpload(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	staging, device, err := openUploadStaging(request.Identity, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	if err := enforceUploadSessionLimit(staging); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	partName, metadataName := uploadPartName(request.UploadID), uploadMetadataName(request.UploadID)
	part, err := unix.Openat(staging, partName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		if errors.Is(err, unix.EEXIST) {
			return agentprotocol.FileWriteResult{}, ErrConflict
		}
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := validateOwnedRegularDescriptor(part, request.Identity, device, 0); err != nil {
		_ = unix.Close(part)
		_ = unix.Unlinkat(staging, partName, 0)
		return agentprotocol.FileWriteResult{}, err
	}
	_ = unix.Close(part)
	createdAt := time.Now().UTC()
	metadata := uploadMetadata{UploadID: request.UploadID, Directory: request.Directory, Name: request.Name,
		SizeBytes: request.SizeBytes, ExpectedSHA256: request.ExpectedSHA256, CreatedAt: createdAt}
	if err := createUploadMetadata(staging, metadataName, metadata, request.Identity, device); err != nil {
		_ = unix.Unlinkat(staging, partName, 0)
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Fsync(staging); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return uploadResult(metadata, 0), nil
}

func inspectUpload(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	staging, device, err := openUploadStaging(request.Identity, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	metadata, err := readUploadMetadata(staging, request.UploadID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	received, err := inspectUploadPart(staging, request.UploadID, request.Identity, device)
	if err != nil || received > metadata.SizeBytes {
		if err == nil {
			err = ErrConflict
		}
		return agentprotocol.FileWriteResult{}, err
	}
	return uploadResult(metadata, received), nil
}

func writeUploadChunk(request agentprotocol.FileWriteRequest, body io.Reader) (agentprotocol.FileWriteResult, error) {
	staging, device, err := openUploadStaging(request.Identity, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	metadata, err := readUploadMetadata(staging, request.UploadID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if request.Offset > metadata.SizeBytes || request.ChunkLength > metadata.SizeBytes-request.Offset {
		return agentprotocol.FileWriteResult{}, ErrTooLarge
	}
	descriptor, err := unix.Openat(staging, uploadPartName(request.UploadID),
		unix.O_WRONLY|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return agentprotocol.FileWriteResult{}, ErrNotFound
		}
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	file := os.NewFile(uintptr(descriptor), "managed-upload-part")
	defer func() { _ = file.Close() }()
	if err := unix.Flock(descriptor, unix.LOCK_EX); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	defer func() { _ = unix.Flock(descriptor, unix.LOCK_UN) }()
	if err := validateOwnedRegularDescriptor(descriptor, request.Identity, device, request.Offset); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	copyLength := int64(request.ChunkLength) // #nosec G115 -- protocol validation caps chunks at 8 MiB.
	if _, err := io.CopyN(file, body, copyLength); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := file.Sync(); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	received := request.Offset + request.ChunkLength
	if err := validateOwnedRegularDescriptor(descriptor, request.Identity, device, received); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	return uploadResult(metadata, received), nil
}

func completeUpload(ctx context.Context, request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	staging, device, err := openUploadStaging(request.Identity, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	metadata, err := readUploadMetadata(staging, request.UploadID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if metadata.Directory != request.Directory || metadata.Name != request.Name || metadata.SizeBytes != request.SizeBytes ||
		metadata.ExpectedSHA256 != request.ExpectedSHA256 {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	descriptor, err := unix.Openat(staging, uploadPartName(request.UploadID),
		unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return agentprotocol.FileWriteResult{}, ErrNotFound
		}
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	file := os.NewFile(uintptr(descriptor), "managed-upload-complete")
	defer func() { _ = file.Close() }()
	if err := unix.Flock(descriptor, unix.LOCK_EX); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	defer func() { _ = unix.Flock(descriptor, unix.LOCK_UN) }()
	if err := validateOwnedRegularDescriptor(descriptor, request.Identity, device, request.SizeBytes); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	digest := sha256.New()
	buffer := make([]byte, 128<<10)
	if _, err := io.CopyBuffer(digest, &contextReader{ctx: ctx, reader: file}, buffer); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	actualSHA256 := hex.EncodeToString(digest.Sum(nil))
	if request.ExpectedSHA256 != "" && actualSHA256 != request.ExpectedSHA256 {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	if err := unix.Fchmod(descriptor, 0o640); err != nil || file.Sync() != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	target, targetDevice, err := openAccountDirectoryForMutation(request.Identity, request.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(target)
	if targetDevice != device {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	if err := unix.Renameat2(staging, uploadPartName(request.UploadID), target, request.Name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return agentprotocol.FileWriteResult{}, ErrConflict
		}
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
			return agentprotocol.FileWriteResult{}, ErrNotFound
		}
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	_ = unix.Unlinkat(staging, uploadMetadataName(request.UploadID), 0)
	if unix.Fsync(target) != nil || unix.Fsync(staging) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	result := uploadResult(metadata, metadata.SizeBytes)
	result.SHA256, result.Completed = actualSHA256, true
	return result, nil
}

func cancelUpload(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	staging, device, err := openUploadStaging(request.Identity, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	metadata, err := readUploadMetadata(staging, request.UploadID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Unlinkat(staging, uploadPartName(request.UploadID), 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Unlinkat(staging, uploadMetadataName(request.UploadID), 0); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Fsync(staging); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return agentprotocol.FileWriteResult{UploadID: metadata.UploadID, Completed: true}, nil
}

func createManagedNode(request agentprotocol.FileWriteRequest, directory bool) (agentprotocol.FileWriteResult, error) {
	parent, device, err := openAccountDirectoryForMutation(request.Identity, request.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(parent)
	if directory {
		if err := unix.Mkdirat(parent, request.Name, 0o750); err != nil {
			if errors.Is(err, unix.EEXIST) {
				return agentprotocol.FileWriteResult{}, ErrConflict
			}
			return agentprotocol.FileWriteResult{}, ErrUnavailable
		}
		var status unix.Stat_t
		if err := unix.Fstatat(parent, request.Name, &status, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
			status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Uid != request.Identity.UID ||
			status.Gid != request.Identity.GID || uint64(status.Dev) != device {
			_ = unix.Unlinkat(parent, request.Name, unix.AT_REMOVEDIR)
			return agentprotocol.FileWriteResult{}, ErrConflict
		}
	} else {
		descriptor, err := unix.Openat(parent, request.Name,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o640)
		if err != nil {
			if errors.Is(err, unix.EEXIST) {
				return agentprotocol.FileWriteResult{}, ErrConflict
			}
			return agentprotocol.FileWriteResult{}, ErrUnavailable
		}
		if err := validateOwnedRegularDescriptor(descriptor, request.Identity, device, 0); err != nil {
			_ = unix.Close(descriptor)
			_ = unix.Unlinkat(parent, request.Name, 0)
			return agentprotocol.FileWriteResult{}, err
		}
		if err := unix.Fsync(descriptor); err != nil {
			_ = unix.Close(descriptor)
			return agentprotocol.FileWriteResult{}, ErrUnavailable
		}
		_ = unix.Close(descriptor)
	}
	if err := unix.Fsync(parent); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return agentprotocol.FileWriteResult{Directory: request.Directory, Name: request.Name, Completed: true}, nil
}

func openUploadStaging(identity hostingidentity.Spec, create bool) (int, uint64, error) {
	root, device, err := openManagedDownloadRoot(identity, nil)
	if err != nil {
		return -1, 0, err
	}
	defer unix.Close(root)
	if create {
		if err := unix.Mkdirat(root, fileUploadStagingDirectory, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, 0, ErrUnavailable
		}
	}
	staging, err := unix.Openat(root, fileUploadStagingDirectory,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return -1, 0, ErrNotFound
		}
		return -1, 0, ErrConflict
	}
	var status unix.Stat_t
	if err := unix.Fstat(staging, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR ||
		status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device || status.Mode&0o077 != 0 {
		_ = unix.Close(staging)
		return -1, 0, ErrConflict
	}
	return staging, device, nil
}

func openAccountDirectoryForMutation(identity hostingidentity.Spec, relativePath string) (int, uint64, error) {
	descendants := []string(nil)
	if relativePath != "" {
		descendants = strings.Split(relativePath, "/")
	}
	pathDescriptor, device, err := openManagedDownloadRoot(identity, descendants)
	if err != nil {
		return -1, 0, err
	}
	defer unix.Close(pathDescriptor)
	descriptor, err := unix.Openat(pathDescriptor, ".",
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
			return -1, 0, ErrNotFound
		}
		return -1, 0, ErrConflict
	}
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFDIR ||
		status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device {
		_ = unix.Close(descriptor)
		return -1, 0, ErrConflict
	}
	return descriptor, device, nil
}

func enforceUploadSessionLimit(staging int) error {
	if _, err := unix.Seek(staging, 0, io.SeekStart); err != nil {
		return ErrUnavailable
	}
	buffer := make([]byte, 8<<10)
	entries, sessions := 0, 0
	for {
		count, err := unix.Getdents(staging, buffer)
		if err != nil {
			return ErrUnavailable
		}
		if count == 0 {
			return nil
		}
		for position := 0; position < count; {
			name, _, length, ok := parseDirent(buffer[position:count])
			if !ok {
				return ErrUnavailable
			}
			position += length
			if name == "." || name == ".." {
				continue
			}
			entries++
			if strings.HasSuffix(name, ".json") {
				sessions++
			}
			if sessions >= maximumActiveFileUploads || entries > maximumStagingScanEntries {
				return ErrBusy
			}
		}
	}
}

func createUploadMetadata(
	staging int, name string, metadata uploadMetadata, identity hostingidentity.Spec, device uint64,
) error {
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) > maximumUploadMetadataBytes {
		return ErrInvalid
	}
	descriptor, err := unix.Openat(staging, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		if errors.Is(err, unix.EEXIST) {
			return ErrConflict
		}
		return ErrUnavailable
	}
	file := os.NewFile(uintptr(descriptor), "managed-upload-metadata")
	defer func() { _ = file.Close() }()
	if err := validateOwnedRegularDescriptor(descriptor, identity, device, 0); err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil || file.Sync() != nil {
		return ErrUnavailable
	}
	return nil
}

func readUploadMetadata(
	staging int, uploadID string, identity hostingidentity.Spec, device uint64,
) (uploadMetadata, error) {
	descriptor, err := unix.Openat(staging, uploadMetadataName(uploadID),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return uploadMetadata{}, ErrNotFound
		}
		return uploadMetadata{}, ErrConflict
	}
	file := os.NewFile(uintptr(descriptor), "managed-upload-metadata")
	defer func() { _ = file.Close() }()
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFREG ||
		status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device ||
		status.Size <= 0 || status.Size > maximumUploadMetadataBytes {
		return uploadMetadata{}, ErrConflict
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maximumUploadMetadataBytes+1))
	if err != nil || len(encoded) > maximumUploadMetadataBytes {
		return uploadMetadata{}, ErrConflict
	}
	var metadata uploadMetadata
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return uploadMetadata{}, ErrConflict
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return uploadMetadata{}, ErrConflict
	}
	validation := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "metadata-validation", Action: agentprotocol.FileWriteInitiate, Identity: identity,
		UploadID: metadata.UploadID, Directory: metadata.Directory, Name: metadata.Name,
		SizeBytes: metadata.SizeBytes, ExpectedSHA256: metadata.ExpectedSHA256,
		Correlation: &agentprotocol.FileAuditCorrelation{AuditEventID: "019c1234-5678-7abc-8def-0123456789ab",
			ActorID: "019c1234-5678-7abc-8def-0123456789ac", SessionID: "019c1234-5678-7abc-8def-0123456789ad",
			AccountID: identity.AccountID, RequestID: "metadata-validation"}}
	if metadata.UploadID != uploadID || metadata.CreatedAt.IsZero() || metadata.CreatedAt.Location() != time.UTC ||
		agentprotocol.ValidateFileWriteRequest(validation) != nil {
		return uploadMetadata{}, ErrConflict
	}
	return metadata, nil
}

func inspectUploadPart(staging int, uploadID string, identity hostingidentity.Spec, device uint64) (uint64, error) {
	descriptor, err := unix.Openat(staging, uploadPartName(uploadID),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return 0, ErrNotFound
		}
		return 0, ErrConflict
	}
	defer unix.Close(descriptor)
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFREG ||
		status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device || status.Size < 0 {
		return 0, ErrConflict
	}
	return uint64(status.Size), nil
}

func validateOwnedRegularDescriptor(
	descriptor int, identity hostingidentity.Spec, device uint64, exactSize uint64,
) error {
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return ErrUnavailable
	}
	if status.Mode&unix.S_IFMT != unix.S_IFREG || status.Uid != identity.UID || status.Gid != identity.GID ||
		uint64(status.Dev) != device || status.Size < 0 || uint64(status.Size) != exactSize {
		return ErrConflict
	}
	return nil
}

func uploadResult(metadata uploadMetadata, received uint64) agentprotocol.FileWriteResult {
	return agentprotocol.FileWriteResult{UploadID: metadata.UploadID, Directory: metadata.Directory,
		Name: metadata.Name, SizeBytes: metadata.SizeBytes, ReceivedBytes: received, CreatedAt: metadata.CreatedAt}
}

func uploadPartName(uploadID string) string     { return uploadID + ".part" }
func uploadMetadataName(uploadID string) string { return uploadID + ".json" }

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
