// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	fileOperationStagingDirectory = agentprotocol.ReservedFileOperationDirectory
	fileTrashDirectory            = agentprotocol.ReservedFileTrashDirectory
	fileOperationPayloadName      = "payload"
	fileTrashMetadataName         = "metadata.json"
	maximumTrashMetadataBytes     = hostingpath.MaximumFileManagerPathBytes + hostingpath.MaximumFilenameBytes + 1_024
	maximumInternalScanEntries    = 256
	maximumActiveFileOperations   = 8
	maximumTrashItems             = 256
)

type fileOperationBudget struct {
	entries    uint64
	bytes      uint64
	entryLimit uint64
	byteLimit  uint64
}

func (budget *fileOperationBudget) maximumEntries() uint64 {
	if budget != nil && budget.entryLimit != 0 {
		return budget.entryLimit
	}
	return agentprotocol.MaximumFileOperationEntries
}

func (budget *fileOperationBudget) maximumBytes() uint64 {
	if budget != nil && budget.byteLimit != 0 {
		return budget.byteLimit
	}
	return agentprotocol.MaximumFileUploadBytes
}

func (budget *fileOperationBudget) remainingEntries() uint64 {
	if budget.entries >= budget.maximumEntries() {
		return 0
	}
	return budget.maximumEntries() - budget.entries
}

func (budget *fileOperationBudget) remainingBytes() uint64 {
	if budget.bytes >= budget.maximumBytes() {
		return 0
	}
	return budget.maximumBytes() - budget.bytes
}

func internalFileOperationCleanupBudget() *fileOperationBudget {
	return &fileOperationBudget{entryLimit: agentprotocol.MaximumFileOperationEntries + 4,
		byteLimit: agentprotocol.MaximumFileUploadBytes * 2}
}

type trashMetadata struct {
	TrashID   string                      `json:"trashId"`
	Directory string                      `json:"directory"`
	Name      string                      `json:"name"`
	Type      agentprotocol.FileEntryType `json:"type"`
	SizeBytes uint64                      `json:"sizeBytes"`
	TrashedAt time.Time                   `json:"trashedAt"`
}

func relocateManagedNode(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	source, sourceDevice, err := openAccountDirectoryForMutation(request.Identity, request.SourceDirectory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(source)
	target, targetDevice, err := openAccountDirectoryForMutation(request.Identity, request.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(target)
	if sourceDevice != targetDevice {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	if _, err := validateManagedEntryAt(source, request.SourceName, request.Identity, sourceDevice); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Renameat2(source, request.SourceName, target, request.Name, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	if err := unix.Fsync(target); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if source != target && unix.Fsync(source) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return nodeMutationResult(request), nil
}

func copyManagedNode(ctx context.Context, request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	source, sourceDevice, err := openAccountDirectoryForMutation(request.Identity, request.SourceDirectory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(source)
	target, targetDevice, err := openAccountDirectoryForMutation(request.Identity, request.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(target)
	if sourceDevice != targetDevice {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	operations, _, err := openInternalDirectory(request.Identity, fileOperationStagingDirectory, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(operations)
	if err := enforceInternalItemLimit(operations, maximumActiveFileOperations); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Mkdirat(operations, request.OperationID, 0o700); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	staging, err := openInternalItem(operations, request.OperationID, request.Identity, sourceDevice)
	if err != nil {
		_ = unix.Unlinkat(operations, request.OperationID, unix.AT_REMOVEDIR)
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(staging)
	cleanup := true
	defer func() {
		if cleanup {
			_ = removeManagedTreeAt(context.Background(), operations, request.OperationID, request.Identity,
				sourceDevice, internalFileOperationCleanupBudget(), 0)
		}
	}()
	budget := &fileOperationBudget{}
	if err := copyManagedEntryAt(ctx, source, request.SourceName, staging, fileOperationPayloadName,
		request.Identity, sourceDevice, budget, 0); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Fsync(staging); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Renameat2(staging, fileOperationPayloadName, target, request.Name, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	if err := unix.Fsync(target); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	cleanup = false
	if err := unix.Unlinkat(operations, request.OperationID, unix.AT_REMOVEDIR); err != nil || unix.Fsync(operations) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return nodeMutationResult(request), nil
}

func trashManagedNode(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	source, device, err := openAccountDirectoryForMutation(request.Identity, request.SourceDirectory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(source)
	status, err := validateManagedEntryAt(source, request.SourceName, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	trash, _, err := openInternalDirectory(request.Identity, fileTrashDirectory, true)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(trash)
	if err := enforceInternalItemLimit(trash, maximumTrashItems); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Mkdirat(trash, request.TrashID, 0o700); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	item, err := openInternalItem(trash, request.TrashID, request.Identity, device)
	if err != nil {
		_ = unix.Unlinkat(trash, request.TrashID, unix.AT_REMOVEDIR)
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(item)
	entryType := entryType(status.Mode)
	metadata := trashMetadata{TrashID: request.TrashID, Directory: request.SourceDirectory,
		Name: request.SourceName, Type: entryType, TrashedAt: time.Now().UTC()}
	if entryType == agentprotocol.FileEntryRegular {
		metadata.SizeBytes = uint64(status.Size) // #nosec G115 -- regular-file size was validated non-negative.
	}
	if err := createTrashMetadata(item, metadata, request.Identity, device); err != nil {
		_ = unix.Unlinkat(trash, request.TrashID, unix.AT_REMOVEDIR)
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Fsync(item); err != nil {
		_ = unix.Unlinkat(item, fileTrashMetadataName, 0)
		_ = unix.Unlinkat(trash, request.TrashID, unix.AT_REMOVEDIR)
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Renameat2(source, request.SourceName, item, fileOperationPayloadName, unix.RENAME_NOREPLACE); err != nil {
		_ = unix.Unlinkat(item, fileTrashMetadataName, 0)
		_ = unix.Unlinkat(trash, request.TrashID, unix.AT_REMOVEDIR)
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	if unix.Fsync(source) != nil || unix.Fsync(item) != nil || unix.Fsync(trash) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return agentprotocol.FileWriteResult{TrashID: request.TrashID, Directory: request.SourceDirectory,
		Name: request.SourceName, Completed: true}, nil
}

func listManagedTrash(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	trash, device, err := openInternalDirectory(request.Identity, fileTrashDirectory, false)
	if errors.Is(err, ErrNotFound) {
		return agentprotocol.FileWriteResult{TrashEntries: []agentprotocol.FileTrashEntry{}}, nil
	}
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(trash)
	names, err := readManagedDirectoryNames(trash, maximumInternalScanEntries)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		parsed, parseErr := uuid.Parse(name)
		if parseErr != nil || parsed.Version() != 7 || parsed.String() != name {
			return agentprotocol.FileWriteResult{}, ErrConflict
		}
		if request.Cursor == "" || name > request.Cursor {
			ids = append(ids, name)
		}
	}
	sort.Strings(ids)
	more := len(ids) > agentprotocol.MaximumFileTrashEntries
	if more {
		ids = ids[:agentprotocol.MaximumFileTrashEntries]
	}
	entries := make([]agentprotocol.FileTrashEntry, 0, len(ids))
	for _, id := range ids {
		item, openErr := openInternalItem(trash, id, request.Identity, device)
		if openErr != nil {
			return agentprotocol.FileWriteResult{}, openErr
		}
		if unix.Flock(item, unix.LOCK_SH) != nil {
			_ = unix.Close(item)
			return agentprotocol.FileWriteResult{}, ErrUnavailable
		}
		metadata, readErr := readTrashMetadata(item, id, request.Identity, device)
		_ = unix.Flock(item, unix.LOCK_UN)
		_ = unix.Close(item)
		if readErr != nil {
			return agentprotocol.FileWriteResult{}, readErr
		}
		entries = append(entries, metadata.entry())
	}
	result := agentprotocol.FileWriteResult{TrashEntries: entries}
	if more && len(entries) > 0 {
		result.Next = entries[len(entries)-1].TrashID
	}
	return result, nil
}

func restoreManagedTrash(request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	trash, device, err := openInternalDirectory(request.Identity, fileTrashDirectory, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(trash)
	item, err := openInternalItem(trash, request.TrashID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(item)
	if unix.Flock(item, unix.LOCK_EX) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	defer unix.Flock(item, unix.LOCK_UN)
	metadata, err := readTrashMetadata(item, request.TrashID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	target, targetDevice, err := openAccountDirectoryForMutation(request.Identity, metadata.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(target)
	if targetDevice != device {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	if err := unix.Renameat2(item, fileOperationPayloadName, target, metadata.Name, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	if err := unix.Unlinkat(item, fileTrashMetadataName, 0); err != nil || unix.Fsync(target) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Unlinkat(trash, request.TrashID, unix.AT_REMOVEDIR); err != nil || unix.Fsync(trash) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return agentprotocol.FileWriteResult{TrashID: request.TrashID, Directory: metadata.Directory,
		Name: metadata.Name, Completed: true}, nil
}

func purgeManagedTrash(ctx context.Context, request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	trash, device, err := openInternalDirectory(request.Identity, fileTrashDirectory, false)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(trash)
	item, err := openInternalItem(trash, request.TrashID, request.Identity, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	if unix.Flock(item, unix.LOCK_EX) != nil {
		_ = unix.Close(item)
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if _, err := readTrashMetadata(item, request.TrashID, request.Identity, device); err != nil {
		_ = unix.Flock(item, unix.LOCK_UN)
		_ = unix.Close(item)
		return agentprotocol.FileWriteResult{}, err
	}
	if err := inspectManagedTreeAt(ctx, item, fileOperationPayloadName, request.Identity, device,
		&fileOperationBudget{}, 0); err != nil {
		_ = unix.Flock(item, unix.LOCK_UN)
		_ = unix.Close(item)
		return agentprotocol.FileWriteResult{}, err
	}
	if err := removeManagedTreeAt(ctx, item, fileOperationPayloadName, request.Identity, device,
		&fileOperationBudget{}, 0); err != nil {
		_ = unix.Flock(item, unix.LOCK_UN)
		_ = unix.Close(item)
		return agentprotocol.FileWriteResult{}, err
	}
	if err := unix.Unlinkat(item, fileTrashMetadataName, 0); err != nil {
		_ = unix.Flock(item, unix.LOCK_UN)
		_ = unix.Close(item)
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	_ = unix.Flock(item, unix.LOCK_UN)
	if err := unix.Close(item); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Unlinkat(trash, request.TrashID, unix.AT_REMOVEDIR); err != nil || unix.Fsync(trash) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	return agentprotocol.FileWriteResult{TrashID: request.TrashID, Completed: true}, nil
}

func copyManagedEntryAt(
	ctx context.Context, sourceParent int, sourceName string, targetParent int, targetName string,
	identity hostingidentity.Spec, device uint64, budget *fileOperationBudget, depth uint32,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > agentprotocol.MaximumFileOperationDepth || budget.entries >= budget.maximumEntries() {
		return ErrTooLarge
	}
	status, err := validateManagedEntryAt(sourceParent, sourceName, identity, device)
	if err != nil {
		return err
	}
	budget.entries++
	switch status.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		if status.Size < 0 || uint64(status.Size) > budget.remainingBytes() { // #nosec G115 -- negative checked first.
			return ErrTooLarge
		}
		source, err := unix.Openat(sourceParent, sourceName,
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		sourceFile := os.NewFile(uintptr(source), "managed-copy-source")
		defer sourceFile.Close()
		if err := validateOwnedRegularDescriptor(source, identity, device, uint64(status.Size)); err != nil { // #nosec G115 -- negative checked above.
			return err
		}
		target, err := unix.Openat(targetParent, targetName,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
		if err != nil {
			return classifyFileMutationError(err)
		}
		targetFile := os.NewFile(uintptr(target), "managed-copy-target")
		defer targetFile.Close()
		copyLength := status.Size
		if copyLength > 0 {
			if _, err := io.CopyN(targetFile, &contextReader{ctx: ctx, reader: sourceFile}, copyLength); err != nil {
				return classifyFileMutationError(err)
			}
		}
		if err := validateOwnedRegularDescriptor(source, identity, device, uint64(status.Size)); err != nil { // #nosec G115 -- negative checked above.
			return err
		}
		if err := unix.Fchmod(target, status.Mode&0o770); err != nil {
			return classifyFileMutationError(err)
		}
		if err := targetFile.Sync(); err != nil {
			return classifyFileMutationError(err)
		}
		budget.bytes += uint64(status.Size) // #nosec G115 -- negative checked above.
		return nil
	case unix.S_IFDIR:
		if err := unix.Mkdirat(targetParent, targetName, 0o700); err != nil {
			return classifyFileMutationError(err)
		}
		source, err := unix.Openat(sourceParent, sourceName,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		defer unix.Close(source)
		if _, err := validateManagedDirectoryDescriptor(source, identity, device); err != nil {
			return err
		}
		target, err := unix.Openat(targetParent, targetName,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		defer unix.Close(target)
		names, err := readManagedDirectoryNames(source, int(budget.remainingEntries())) // #nosec G115 -- the operation limit is at most 10,004.
		if err != nil {
			return err
		}
		for _, name := range names {
			if !hostingpath.ValidFilename(name) {
				return ErrConflict
			}
			if err := copyManagedEntryAt(ctx, source, name, target, name, identity, device, budget, depth+1); err != nil {
				return err
			}
		}
		if err := unix.Fchmod(target, status.Mode&0o770); err != nil {
			return classifyFileMutationError(err)
		}
		if err := unix.Fsync(target); err != nil {
			return classifyFileMutationError(err)
		}
		return nil
	default:
		return ErrConflict
	}
}

func removeManagedTreeAt(
	ctx context.Context, parent int, name string, identity hostingidentity.Spec, device uint64,
	budget *fileOperationBudget, depth uint32,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > agentprotocol.MaximumFileOperationDepth || budget.entries >= budget.maximumEntries() {
		return ErrTooLarge
	}
	status, err := validateManagedEntryAt(parent, name, identity, device)
	if err != nil {
		return err
	}
	budget.entries++
	switch status.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		if status.Size < 0 || uint64(status.Size) > budget.remainingBytes() { // #nosec G115 -- negative checked first.
			return ErrTooLarge
		}
		budget.bytes += uint64(status.Size) // #nosec G115 -- negative checked above.
		if err := unix.Unlinkat(parent, name, 0); err != nil {
			return classifyFileMutationError(err)
		}
		return nil
	case unix.S_IFDIR:
		directory, err := unix.Openat(parent, name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		if _, err := validateManagedDirectoryDescriptor(directory, identity, device); err != nil {
			_ = unix.Close(directory)
			return err
		}
		names, err := readManagedDirectoryNames(directory, int(budget.remainingEntries())) // #nosec G115 -- the operation limit is at most 10,004.
		if err != nil {
			_ = unix.Close(directory)
			return err
		}
		for _, child := range names {
			if err := removeManagedTreeAt(ctx, directory, child, identity, device, budget, depth+1); err != nil {
				_ = unix.Close(directory)
				return err
			}
		}
		if unix.Fsync(directory) != nil || unix.Close(directory) != nil {
			return ErrUnavailable
		}
		if err := unix.Unlinkat(parent, name, unix.AT_REMOVEDIR); err != nil {
			return classifyFileMutationError(err)
		}
		return nil
	default:
		return ErrConflict
	}
}

func inspectManagedTreeAt(
	ctx context.Context, parent int, name string, identity hostingidentity.Spec, device uint64,
	budget *fileOperationBudget, depth uint32,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > agentprotocol.MaximumFileOperationDepth || budget.entries >= budget.maximumEntries() {
		return ErrTooLarge
	}
	status, err := validateManagedEntryAt(parent, name, identity, device)
	if err != nil {
		return err
	}
	budget.entries++
	switch status.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		if status.Size < 0 || uint64(status.Size) > budget.remainingBytes() { // #nosec G115 -- negative checked first.
			return ErrTooLarge
		}
		budget.bytes += uint64(status.Size) // #nosec G115 -- negative checked above.
		return nil
	case unix.S_IFDIR:
		directory, err := unix.Openat(parent, name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		defer unix.Close(directory)
		if _, err := validateManagedDirectoryDescriptor(directory, identity, device); err != nil {
			return err
		}
		names, err := readManagedDirectoryNames(directory, int(budget.remainingEntries())) // #nosec G115 -- the operation limit is at most 10,004.
		if err != nil {
			return err
		}
		for _, child := range names {
			if err := inspectManagedTreeAt(ctx, directory, child, identity, device, budget, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrConflict
	}
}

func validateManagedEntryAt(
	parent int, name string, identity hostingidentity.Spec, device uint64,
) (unix.Stat_t, error) {
	var status unix.Stat_t
	if err := unix.Fstatat(parent, name, &status, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return unix.Stat_t{}, classifyFileMutationError(err)
	}
	if status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device ||
		(status.Mode&unix.S_IFMT != unix.S_IFREG && status.Mode&unix.S_IFMT != unix.S_IFDIR) || status.Size < 0 {
		return unix.Stat_t{}, ErrConflict
	}
	return status, nil
}

func validateManagedDirectoryDescriptor(
	descriptor int, identity hostingidentity.Spec, device uint64,
) (unix.Stat_t, error) {
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		return unix.Stat_t{}, ErrUnavailable
	}
	if status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Uid != identity.UID || status.Gid != identity.GID ||
		uint64(status.Dev) != device {
		return unix.Stat_t{}, ErrConflict
	}
	return status, nil
}

func openInternalDirectory(identity hostingidentity.Spec, name string, create bool) (int, uint64, error) {
	root, device, err := openManagedDownloadRoot(identity, nil)
	if err != nil {
		return -1, 0, err
	}
	defer unix.Close(root)
	if create {
		if err := unix.Mkdirat(root, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, 0, classifyFileMutationError(err)
		}
	}
	directory, err := unix.Openat(root, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, 0, classifyFileMutationError(err)
	}
	status, err := validateManagedDirectoryDescriptor(directory, identity, device)
	if err != nil || status.Mode&0o077 != 0 {
		_ = unix.Close(directory)
		return -1, 0, ErrConflict
	}
	return directory, device, nil
}

func openInternalItem(parent int, name string, identity hostingidentity.Spec, device uint64) (int, error) {
	descriptor, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, classifyFileMutationError(err)
	}
	status, err := validateManagedDirectoryDescriptor(descriptor, identity, device)
	if err != nil || status.Mode&0o077 != 0 {
		_ = unix.Close(descriptor)
		return -1, ErrConflict
	}
	return descriptor, nil
}

func createTrashMetadata(
	item int, metadata trashMetadata, identity hostingidentity.Spec, device uint64,
) error {
	encoded, err := json.Marshal(metadata)
	if err != nil || len(encoded) > maximumTrashMetadataBytes {
		return ErrInvalid
	}
	descriptor, err := unix.Openat(item, fileTrashMetadataName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return classifyFileMutationError(err)
	}
	file := os.NewFile(uintptr(descriptor), "managed-trash-metadata")
	defer file.Close()
	if err := validateOwnedRegularDescriptor(descriptor, identity, device, 0); err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		return classifyFileMutationError(err)
	}
	if err := file.Sync(); err != nil {
		return classifyFileMutationError(err)
	}
	return nil
}

func readTrashMetadata(
	item int, expectedID string, identity hostingidentity.Spec, device uint64,
) (trashMetadata, error) {
	descriptor, err := unix.Openat(item, fileTrashMetadataName,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return trashMetadata{}, classifyFileMutationError(err)
	}
	file := os.NewFile(uintptr(descriptor), "managed-trash-metadata")
	defer file.Close()
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil || status.Mode&unix.S_IFMT != unix.S_IFREG ||
		status.Uid != identity.UID || status.Gid != identity.GID || uint64(status.Dev) != device ||
		status.Size <= 0 || status.Size > maximumTrashMetadataBytes {
		return trashMetadata{}, ErrConflict
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maximumTrashMetadataBytes+1))
	if err != nil || len(encoded) > maximumTrashMetadataBytes {
		return trashMetadata{}, ErrConflict
	}
	var metadata trashMetadata
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return trashMetadata{}, ErrConflict
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return trashMetadata{}, ErrConflict
	}
	if metadata.TrashID != expectedID {
		return trashMetadata{}, ErrConflict
	}
	payload, err := validateManagedEntryAt(item, fileOperationPayloadName, identity, device)
	if err != nil || entryType(payload.Mode) != metadata.Type ||
		(metadata.Type == agentprotocol.FileEntryRegular && uint64(payload.Size) != metadata.SizeBytes) { // #nosec G115 -- payload size is validated non-negative.
		return trashMetadata{}, ErrConflict
	}
	validationRequest := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "trash-metadata-validation", Action: agentprotocol.FileWriteTrashList, Identity: identity}
	validationResponse := agentprotocol.FileWriteResponse{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: validationRequest.RequestID, Result: &agentprotocol.FileWriteResult{TrashEntries: []agentprotocol.FileTrashEntry{metadata.entry()}}}
	if agentprotocol.ValidateFileWriteResponse(validationResponse, validationRequest) != nil {
		return trashMetadata{}, ErrConflict
	}
	return metadata, nil
}

func (metadata trashMetadata) entry() agentprotocol.FileTrashEntry {
	return agentprotocol.FileTrashEntry{TrashID: metadata.TrashID, Directory: metadata.Directory,
		Name: metadata.Name, Type: metadata.Type, SizeBytes: metadata.SizeBytes, TrashedAt: metadata.TrashedAt}
}

func readManagedDirectoryNames(descriptor int, maximum int) ([]string, error) {
	if maximum <= 0 || maximum > int(agentprotocol.MaximumFileOperationEntries+4) {
		return nil, ErrTooLarge
	}
	if _, err := unix.Seek(descriptor, 0, io.SeekStart); err != nil {
		return nil, ErrUnavailable
	}
	buffer := make([]byte, 32<<10)
	names := make([]string, 0)
	for {
		count, err := unix.Getdents(descriptor, buffer)
		if err != nil {
			return nil, ErrUnavailable
		}
		if count == 0 {
			return names, nil
		}
		for position := 0; position < count; {
			name, _, length, ok := parseDirent(buffer[position:count])
			if !ok {
				return nil, ErrUnavailable
			}
			position += length
			if name == "." || name == ".." {
				continue
			}
			if len(names) >= maximum {
				return nil, ErrTooLarge
			}
			names = append(names, name)
		}
	}
}

func enforceInternalItemLimit(descriptor int, maximum int) error {
	names, err := readManagedDirectoryNames(descriptor, maximumInternalScanEntries)
	if errors.Is(err, ErrTooLarge) {
		return ErrBusy
	}
	if err != nil {
		return err
	}
	if len(names) >= maximum {
		return ErrBusy
	}
	return nil
}

func classifyFileMutationError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EDQUOT), errors.Is(err, unix.ENOSPC):
		return ErrQuota
	case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENOTDIR):
		return ErrNotFound
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ENOTEMPTY), errors.Is(err, unix.ELOOP),
		errors.Is(err, unix.EXDEV), errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM), errors.Is(err, unix.EINVAL):
		return ErrConflict
	default:
		return ErrUnavailable
	}
}

func nodeMutationResult(request agentprotocol.FileWriteRequest) agentprotocol.FileWriteResult {
	return agentprotocol.FileWriteResult{OperationID: request.OperationID, SourceDirectory: request.SourceDirectory,
		SourceName: request.SourceName, Directory: request.Directory, Name: request.Name, Completed: true}
}
