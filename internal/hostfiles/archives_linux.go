// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const (
	archiveSnapshotName       = "source.archive"
	maximumZIPCentralBytes    = uint64(64 << 20)
	maximumZIPEntryExtraBytes = uint16(4 << 10)
	maximumZIPEntryComment    = uint16(1 << 10)
	maximumZIPCommentBytes    = uint16(1 << 10)
	archiveExpansionBase      = uint64(64 << 20)
	archiveExpansionRatio     = uint64(200)
)

type boundedArchiveWriter struct {
	writer    io.Writer
	remaining uint64
}

func (writer *boundedArchiveWriter) Write(content []byte) (int, error) {
	allowed := len(content)
	if uint64(allowed) > writer.remaining {
		allowed = int(writer.remaining) // #nosec G115 -- remaining is capped at 4 GiB on the required amd64 platform.
	}
	written, err := writer.writer.Write(content[:allowed])
	writer.remaining -= uint64(written) // #nosec G115 -- written is non-negative and bounded by allowed.
	if err != nil {
		return written, err
	}
	if written != allowed {
		return written, io.ErrShortWrite
	}
	if allowed != len(content) {
		return written, ErrTooLarge
	}
	return written, nil
}

type managedArchiveEmitter interface {
	directory(string, unix.Stat_t) error
	file(string, unix.Stat_t, io.Reader) error
	close() error
}

type zipArchiveEmitter struct{ writer *zip.Writer }

func (emitter *zipArchiveEmitter) directory(name string, status unix.Stat_t) error {
	header := &zip.FileHeader{Name: name + "/", Method: zip.Store, Modified: archiveModifiedTime(status)}
	header.SetMode(os.ModeDir | os.FileMode(status.Mode&0o770))
	_, err := emitter.writer.CreateHeader(header)
	return err
}

func (emitter *zipArchiveEmitter) file(name string, status unix.Stat_t, reader io.Reader) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: archiveModifiedTime(status)}
	header.SetMode(os.FileMode(status.Mode & 0o770))
	target, err := emitter.writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.CopyN(target, reader, status.Size)
	return err
}

func (emitter *zipArchiveEmitter) close() error { return emitter.writer.Close() }

type tarGzipArchiveEmitter struct {
	gzipWriter *gzip.Writer
	tarWriter  *tar.Writer
}

func (emitter *tarGzipArchiveEmitter) directory(name string, status unix.Stat_t) error {
	return emitter.tarWriter.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir,
		Mode: int64(status.Mode & 0o770), ModTime: archiveModifiedTime(status), Format: tar.FormatPAX})
}

func (emitter *tarGzipArchiveEmitter) file(name string, status unix.Stat_t, reader io.Reader) error {
	if err := emitter.tarWriter.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg,
		Mode: int64(status.Mode & 0o770), Size: status.Size, ModTime: archiveModifiedTime(status), Format: tar.FormatPAX}); err != nil {
		return err
	}
	_, err := io.CopyN(emitter.tarWriter, reader, status.Size)
	return err
}

func (emitter *tarGzipArchiveEmitter) close() error {
	return errors.Join(emitter.tarWriter.Close(), emitter.gzipWriter.Close())
}

func createManagedArchive(ctx context.Context, request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	source, device, err := openAccountDirectoryForMutation(request.Identity, request.SourceDirectory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(source)
	target, targetDevice, err := openAccountDirectoryForMutation(request.Identity, request.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(target)
	if targetDevice != device {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	if _, err := validateManagedEntryAt(source, request.SourceName, request.Identity, device); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	operations, staging, cleanup, err := beginArchiveOperation(request, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(operations)
	defer unix.Close(staging)
	defer func() { cleanup() }()
	descriptor, err := unix.Openat(staging, fileOperationPayloadName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	output := os.NewFile(uintptr(descriptor), "managed-archive-output")
	sink := &boundedArchiveWriter{writer: output, remaining: agentprotocol.MaximumFileUploadBytes}
	var emitter managedArchiveEmitter
	switch request.ArchiveFormat {
	case agentprotocol.FileArchiveZIP:
		emitter = &zipArchiveEmitter{writer: zip.NewWriter(sink)}
	case agentprotocol.FileArchiveTarGzip:
		gzipWriter, gzipErr := gzip.NewWriterLevel(sink, gzip.BestSpeed)
		if gzipErr != nil {
			_ = output.Close()
			return agentprotocol.FileWriteResult{}, ErrUnavailable
		}
		emitter = &tarGzipArchiveEmitter{gzipWriter: gzipWriter, tarWriter: tar.NewWriter(gzipWriter)}
	default:
		_ = output.Close()
		return agentprotocol.FileWriteResult{}, ErrInvalid
	}
	budget := &fileOperationBudget{}
	err = walkManagedArchiveSource(ctx, source, request.SourceName, request.SourceName, request.Identity,
		device, budget, 0, emitter)
	closeErr := emitter.close()
	if err != nil || closeErr != nil {
		_ = output.Close()
		return agentprotocol.FileWriteResult{}, classifyArchiveError(errors.Join(err, closeErr))
	}
	if output.Sync() != nil || output.Chmod(0o640) != nil || output.Close() != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	var status unix.Stat_t
	if unix.Fstatat(staging, fileOperationPayloadName, &status, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		status.Mode&unix.S_IFMT != unix.S_IFREG || status.Size <= 0 || uint64(status.Size) > agentprotocol.MaximumFileUploadBytes { // #nosec G115 -- size is checked positive first.
		return agentprotocol.FileWriteResult{}, ErrTooLarge
	}
	if unix.Fsync(staging) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Renameat2(staging, fileOperationPayloadName, target, request.Name, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	if unix.Fsync(target) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := finishArchiveOperation(operations, staging, request.OperationID); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	cleanup = func() {}
	return archiveResult(request, uint64(status.Size), budget.entries), nil // #nosec G115 -- size is positive above.
}

func extractManagedArchive(ctx context.Context, request agentprotocol.FileWriteRequest) (agentprotocol.FileWriteResult, error) {
	source, device, err := openAccountDirectoryForMutation(request.Identity, request.SourceDirectory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(source)
	target, targetDevice, err := openAccountDirectoryForMutation(request.Identity, request.Directory)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(target)
	if targetDevice != device {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	status, err := validateManagedEntryAt(source, request.SourceName, request.Identity, device)
	if err != nil || status.Mode&unix.S_IFMT != unix.S_IFREG || status.Size <= 0 ||
		uint64(status.Size) > agentprotocol.MaximumFileUploadBytes { // #nosec G115 -- size is checked positive first.
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	operations, staging, cleanup, err := beginArchiveOperation(request, device)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer unix.Close(operations)
	defer unix.Close(staging)
	defer func() { cleanup() }()
	snapshot, err := snapshotArchiveSource(ctx, source, request.SourceName, staging, request.Identity, device, status)
	if err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	defer snapshot.Close()
	if err := unix.Mkdirat(staging, fileOperationPayloadName, 0o700); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	payload, err := unix.Openat(staging, fileOperationPayloadName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	defer unix.Close(payload)
	if _, err := validateManagedDirectoryDescriptor(payload, request.Identity, device); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	manifest := newArchiveManifest(uint64(status.Size)) // #nosec G115 -- size is positive above.
	switch request.ArchiveFormat {
	case agentprotocol.FileArchiveZIP:
		err = extractZIPArchive(ctx, snapshot, status.Size, payload, request.Identity, device, manifest)
	case agentprotocol.FileArchiveTarGzip:
		err = extractTarGzipArchive(ctx, snapshot, status.Size, payload, request.Identity, device, manifest)
	default:
		err = ErrInvalid
	}
	if err != nil {
		return agentprotocol.FileWriteResult{}, classifyArchiveError(err)
	}
	if manifest.entries == 0 {
		return agentprotocol.FileWriteResult{}, ErrConflict
	}
	if unix.Fsync(payload) != nil || unix.Fsync(staging) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Renameat2(staging, fileOperationPayloadName, target, request.Name, unix.RENAME_NOREPLACE); err != nil {
		return agentprotocol.FileWriteResult{}, classifyFileMutationError(err)
	}
	if unix.Fsync(target) != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := unix.Unlinkat(staging, archiveSnapshotName, 0); err != nil {
		return agentprotocol.FileWriteResult{}, ErrUnavailable
	}
	if err := finishArchiveOperation(operations, staging, request.OperationID); err != nil {
		return agentprotocol.FileWriteResult{}, err
	}
	cleanup = func() {}
	return archiveResult(request, manifest.bytes, manifest.entries), nil
}

func beginArchiveOperation(
	request agentprotocol.FileWriteRequest, device uint64,
) (int, int, func(), error) {
	operations, _, err := openInternalDirectory(request.Identity, fileOperationStagingDirectory, true)
	if err != nil {
		return -1, -1, func() {}, err
	}
	if err := enforceInternalItemLimit(operations, maximumActiveFileOperations); err != nil {
		_ = unix.Close(operations)
		return -1, -1, func() {}, err
	}
	if err := unix.Mkdirat(operations, request.OperationID, 0o700); err != nil {
		_ = unix.Close(operations)
		return -1, -1, func() {}, classifyFileMutationError(err)
	}
	staging, err := openInternalItem(operations, request.OperationID, request.Identity, device)
	if err != nil {
		_ = unix.Unlinkat(operations, request.OperationID, unix.AT_REMOVEDIR)
		_ = unix.Close(operations)
		return -1, -1, func() {}, err
	}
	active := true
	cleanup := func() {
		if active {
			_ = removeManagedTreeAt(context.Background(), operations, request.OperationID, request.Identity,
				device, internalFileOperationCleanupBudget(), 0)
		}
	}
	return operations, staging, func() { cleanup(); active = false }, nil
}

func finishArchiveOperation(operations, staging int, operationID string) error {
	if unix.Fsync(staging) != nil || unix.Unlinkat(operations, operationID, unix.AT_REMOVEDIR) != nil ||
		unix.Fsync(operations) != nil {
		return ErrUnavailable
	}
	return nil
}

func snapshotArchiveSource(
	ctx context.Context, source int, name string, staging int, identity hostingidentity.Spec, device uint64, wanted unix.Stat_t,
) (*os.File, error) {
	descriptor, err := unix.Openat(source, name,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, classifyFileMutationError(err)
	}
	sourceFile := os.NewFile(uintptr(descriptor), "managed-archive-source")
	defer sourceFile.Close()
	if err := validateOwnedRegularDescriptor(descriptor, identity, device, uint64(wanted.Size)); err != nil { // #nosec G115 -- wanted size is positive before this helper.
		return nil, err
	}
	snapshotDescriptor, err := unix.Openat(staging, archiveSnapshotName,
		unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, classifyFileMutationError(err)
	}
	snapshot := os.NewFile(uintptr(snapshotDescriptor), "managed-archive-snapshot")
	failed := true
	defer func() {
		if failed {
			_ = snapshot.Close()
			_ = unix.Unlinkat(staging, archiveSnapshotName, 0)
		}
	}()
	if _, err := io.CopyN(snapshot, &contextReader{ctx: ctx, reader: sourceFile}, wanted.Size); err != nil {
		return nil, classifyFileMutationError(err)
	}
	if err := validateOwnedRegularDescriptor(descriptor, identity, device, uint64(wanted.Size)); err != nil { // #nosec G115 -- wanted size is positive before this helper.
		return nil, err
	}
	if snapshot.Sync() != nil {
		return nil, ErrUnavailable
	}
	if _, err := snapshot.Seek(0, io.SeekStart); err != nil {
		return nil, ErrUnavailable
	}
	failed = false
	return snapshot, nil
}

func walkManagedArchiveSource(
	ctx context.Context, parent int, name, archiveName string, identity hostingidentity.Spec, device uint64,
	budget *fileOperationBudget, depth uint32, emitter managedArchiveEmitter,
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
		descriptor, err := unix.Openat(parent, name,
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		file := os.NewFile(uintptr(descriptor), "managed-archive-entry")
		defer file.Close()
		if err := validateOwnedRegularDescriptor(descriptor, identity, device, uint64(status.Size)); err != nil { // #nosec G115 -- negative checked above.
			return err
		}
		if err := emitter.file(archiveName, status, &contextReader{ctx: ctx, reader: file}); err != nil {
			return err
		}
		if err := validateOwnedRegularDescriptor(descriptor, identity, device, uint64(status.Size)); err != nil { // #nosec G115 -- negative checked above.
			return err
		}
		budget.bytes += uint64(status.Size) // #nosec G115 -- negative checked above.
		return nil
	case unix.S_IFDIR:
		if err := emitter.directory(archiveName, status); err != nil {
			return err
		}
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
		sort.Strings(names)
		for _, child := range names {
			if !hostingpath.ValidFilename(child) {
				return ErrConflict
			}
			if err := walkManagedArchiveSource(ctx, directory, child, path.Join(archiveName, child), identity,
				device, budget, depth+1, emitter); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrConflict
	}
}

type archiveNodeKind uint8

const (
	archiveNodeImplicitDirectory archiveNodeKind = iota + 1
	archiveNodeDirectory
	archiveNodeFile
)

type archiveManifest struct {
	inputBytes uint64
	entries    uint64
	bytes      uint64
	nodes      map[string]archiveNodeKind
}

func newArchiveManifest(inputBytes uint64) *archiveManifest {
	return &archiveManifest{inputBytes: inputBytes, nodes: make(map[string]archiveNodeKind)}
}

func (manifest *archiveManifest) add(rawName string, directory bool, size uint64) ([]string, error) {
	name := rawName
	if directory {
		name = strings.TrimSuffix(name, "/")
	}
	if name == "" || strings.Contains(rawName, "\\") || strings.HasPrefix(rawName, "/") ||
		(!directory && strings.HasSuffix(rawName, "/")) {
		return nil, ErrConflict
	}
	normalized, err := hostingpath.NormalizeFileManagerDirectory(name)
	if err != nil || normalized != name {
		return nil, ErrConflict
	}
	components := strings.Split(normalized, "/")
	if len(components) > int(agentprotocol.MaximumFileOperationDepth) {
		return nil, ErrTooLarge
	}
	for index := 1; index < len(components); index++ {
		parent := strings.Join(components[:index], "/")
		switch manifest.nodes[parent] {
		case archiveNodeFile:
			return nil, ErrConflict
		case 0:
			manifest.nodes[parent] = archiveNodeImplicitDirectory
			manifest.entries++
		}
	}
	kind := archiveNodeFile
	if directory {
		kind = archiveNodeDirectory
	}
	existing := manifest.nodes[normalized]
	switch {
	case existing == 0:
		manifest.nodes[normalized] = kind
		manifest.entries++
	case existing == archiveNodeImplicitDirectory && directory:
		manifest.nodes[normalized] = archiveNodeDirectory
	default:
		return nil, ErrConflict
	}
	if manifest.entries > agentprotocol.MaximumFileOperationEntries || size > agentprotocol.MaximumFileUploadBytes-manifest.bytes {
		return nil, ErrTooLarge
	}
	manifest.bytes += size
	if manifest.bytes > archiveExpansionLimit(manifest.inputBytes) {
		return nil, ErrTooLarge
	}
	return components, nil
}

func archiveExpansionLimit(inputBytes uint64) uint64 {
	if inputBytes > (agentprotocol.MaximumFileUploadBytes-archiveExpansionBase)/archiveExpansionRatio {
		return agentprotocol.MaximumFileUploadBytes
	}
	limit := archiveExpansionBase + inputBytes*archiveExpansionRatio
	if limit > agentprotocol.MaximumFileUploadBytes {
		return agentprotocol.MaximumFileUploadBytes
	}
	return limit
}

func extractZIPArchive(
	ctx context.Context, source *os.File, size int64, root int, identity hostingidentity.Spec, device uint64,
	manifest *archiveManifest,
) error {
	if err := preflightZIPCentralDirectory(source, size); err != nil {
		return err
	}
	reader, err := zip.NewReader(source, size)
	if err != nil {
		return ErrConflict
	}
	for _, entry := range reader.File {
		if entry.Flags&0x1 != 0 || (entry.Method != zip.Store && entry.Method != zip.Deflate) {
			return ErrConflict
		}
		mode := entry.Mode()
		directory := entry.FileInfo().IsDir()
		if mode.Type() != 0 && !directory {
			return ErrConflict
		}
		if directory != strings.HasSuffix(entry.Name, "/") {
			return ErrConflict
		}
		components, err := manifest.add(entry.Name, directory, entry.UncompressedSize64)
		if err != nil {
			return err
		}
		if directory {
			if err := ensureArchiveDirectory(root, components, identity, device); err != nil {
				return err
			}
			continue
		}
		stream, err := entry.Open()
		if err != nil {
			return ErrConflict
		}
		err = writeExtractedArchiveFile(ctx, root, components, stream, entry.UncompressedSize64, identity, device)
		closeErr := stream.Close()
		if err != nil || closeErr != nil {
			return classifyArchiveError(errors.Join(err, closeErr))
		}
	}
	return nil
}

func preflightZIPCentralDirectory(source *os.File, size int64) error {
	if size < 22 {
		return ErrConflict
	}
	tailLength := int64(22 + 65_535)
	if size < tailLength {
		tailLength = size
	}
	tail := make([]byte, int(tailLength))
	if _, err := source.ReadAt(tail, size-tailLength); err != nil {
		return ErrConflict
	}
	eocd := -1
	for index := len(tail) - 22; index >= 0; index-- {
		if binary.LittleEndian.Uint32(tail[index:index+4]) == 0x06054b50 {
			commentLength := int(binary.LittleEndian.Uint16(tail[index+20 : index+22]))
			if index+22+commentLength == len(tail) {
				eocd = index
				break
			}
		}
	}
	if eocd < 0 {
		return ErrConflict
	}
	record := tail[eocd : eocd+22]
	entriesOnDisk := binary.LittleEndian.Uint16(record[8:10])
	entries := binary.LittleEndian.Uint16(record[10:12])
	centralSize := uint64(binary.LittleEndian.Uint32(record[12:16]))
	centralOffset := uint64(binary.LittleEndian.Uint32(record[16:20]))
	commentLength := binary.LittleEndian.Uint16(record[20:22])
	eocdOffset := uint64(size-tailLength) + uint64(eocd) // #nosec G115 -- eocd and size-tailLength are non-negative.
	if binary.LittleEndian.Uint16(record[4:6]) != 0 || binary.LittleEndian.Uint16(record[6:8]) != 0 ||
		entries == 0 || entries != entriesOnDisk || uint64(entries) > agentprotocol.MaximumFileOperationEntries ||
		entries == 0xffff || centralSize == 0xffffffff || centralOffset == 0xffffffff ||
		centralSize > maximumZIPCentralBytes || centralOffset > eocdOffset || centralSize != eocdOffset-centralOffset ||
		commentLength > maximumZIPCommentBytes {
		return ErrTooLarge
	}
	position := centralOffset
	end := centralOffset + centralSize
	header := make([]byte, 46)
	for index := uint16(0); index < entries; index++ {
		if position > end || end-position < uint64(len(header)) {
			return ErrConflict
		}
		if _, err := source.ReadAt(header, int64(position)); err != nil || binary.LittleEndian.Uint32(header[0:4]) != 0x02014b50 { // #nosec G115 -- position is bounded by the positive signed source size.
			return ErrConflict
		}
		nameLength := binary.LittleEndian.Uint16(header[28:30])
		extraLength := binary.LittleEndian.Uint16(header[30:32])
		entryComment := binary.LittleEndian.Uint16(header[32:34])
		if nameLength == 0 || uint64(nameLength) > hostingpath.MaximumFileManagerPathBytes ||
			extraLength > maximumZIPEntryExtraBytes || entryComment > maximumZIPEntryComment {
			return ErrTooLarge
		}
		position += uint64(len(header)) + uint64(nameLength) + uint64(extraLength) + uint64(entryComment)
	}
	if position != end {
		return ErrConflict
	}
	_, err := source.Seek(0, io.SeekStart)
	return err
}

func extractTarGzipArchive(
	ctx context.Context, source *os.File, size int64, root int, identity hostingidentity.Spec, device uint64,
	manifest *archiveManifest,
) error {
	if err := processTarGzip(ctx, source, size, func(header *tar.Header, reader io.Reader) error {
		directory, sizeBytes, err := validateTarHeader(header)
		if err != nil {
			return err
		}
		_, err = manifest.add(header.Name, directory, sizeBytes)
		if err != nil {
			return err
		}
		if !directory {
			_, err = io.CopyN(io.Discard, &contextReader{ctx: ctx, reader: reader}, header.Size)
		}
		return err
	}); err != nil {
		return err
	}
	return processTarGzip(ctx, source, size, func(header *tar.Header, reader io.Reader) error {
		directory, sizeBytes, err := validateTarHeader(header)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(header.Name, "/")
		components := strings.Split(name, "/")
		if directory {
			return ensureArchiveDirectory(root, components, identity, device)
		}
		return writeExtractedArchiveFile(ctx, root, components, reader, sizeBytes, identity, device)
	})
}

func processTarGzip(ctx context.Context, source *os.File, size int64, process func(*tar.Header, io.Reader) error) error {
	return processTarGzipWithLimit(ctx, source, size, archiveExpansionLimit(uint64(size)), process) // #nosec G115 -- callers require a positive bounded size.
}

func processTarGzipWithLimit(
	ctx context.Context, source *os.File, size int64, expansionLimit uint64,
	process func(*tar.Header, io.Reader) error,
) error {
	if size <= 0 || expansionLimit == 0 || expansionLimit > agentprotocol.MaximumFileUploadBytes+(64<<20) {
		return ErrTooLarge
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return ErrUnavailable
	}
	buffered := bufio.NewReaderSize(io.LimitReader(&contextReader{ctx: ctx, reader: source}, size), 32<<10)
	gzipReader, err := gzip.NewReader(buffered) // #nosec G110 -- headers are preflighted against 4 GiB and the bounded expansion ratio before extraction.
	if err != nil {
		return ErrConflict
	}
	gzipReader.Multistream(false)
	decompressed := &io.LimitedReader{R: gzipReader, N: int64(expansionLimit) + 1} // #nosec G115 -- the explicit ceiling is at most 4 GiB plus 64 MiB.
	tarReader := tar.NewReader(decompressed)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = gzipReader.Close()
			if decompressed.N == 0 {
				return ErrTooLarge
			}
			return ErrConflict
		}
		if err := process(header, tarReader); err != nil {
			_ = gzipReader.Close()
			return err
		}
	}
	if _, err := io.Copy(io.Discard, decompressed); err != nil { // #nosec G110 -- LimitedReader caps the complete decompressed stream to the expansion budget plus one byte.
		_ = gzipReader.Close()
		return ErrConflict
	}
	if decompressed.N == 0 {
		_ = gzipReader.Close()
		return ErrTooLarge
	}
	if err := gzipReader.Close(); err != nil {
		return ErrConflict
	}
	if _, err := buffered.Peek(1); !errors.Is(err, io.EOF) {
		return ErrConflict
	}
	return nil
}

func validateTarHeader(header *tar.Header) (bool, uint64, error) {
	if header == nil || header.Name == "" || header.Size < 0 || header.Linkname != "" {
		return false, 0, ErrConflict
	}
	switch header.Typeflag {
	case tar.TypeDir:
		return true, 0, nil
	case tar.TypeReg, tar.TypeRegA:
		return false, uint64(header.Size), nil // #nosec G115 -- size is non-negative above.
	default:
		return false, 0, ErrConflict
	}
}

func ensureArchiveDirectory(root int, components []string, identity hostingidentity.Spec, device uint64) error {
	parent := root
	ownedParent := false
	defer func() {
		if ownedParent {
			_ = unix.Close(parent)
		}
	}()
	for _, component := range components {
		if !hostingpath.ValidFilename(component) {
			return ErrConflict
		}
		if err := unix.Mkdirat(parent, component, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return classifyFileMutationError(err)
		}
		next, err := unix.Openat(parent, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return classifyFileMutationError(err)
		}
		if _, err := validateManagedDirectoryDescriptor(next, identity, device); err != nil {
			_ = unix.Close(next)
			return err
		}
		if unix.Fchmod(next, 0o750) != nil || unix.Fsync(parent) != nil {
			_ = unix.Close(next)
			return ErrUnavailable
		}
		if ownedParent {
			_ = unix.Close(parent)
		}
		parent, ownedParent = next, true
	}
	return nil
}

func writeExtractedArchiveFile(
	ctx context.Context, root int, components []string, reader io.Reader, size uint64,
	identity hostingidentity.Spec, device uint64,
) error {
	if len(components) == 0 || size > agentprotocol.MaximumFileUploadBytes {
		return ErrTooLarge
	}
	if err := ensureArchiveDirectory(root, components[:len(components)-1], identity, device); err != nil {
		return err
	}
	parent, err := openArchiveDirectory(root, components[:len(components)-1], identity, device)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	name := components[len(components)-1]
	descriptor, err := unix.Openat(parent, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return classifyFileMutationError(err)
	}
	file := os.NewFile(uintptr(descriptor), "managed-archive-extracted-file")
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = unix.Unlinkat(parent, name, 0)
		}
	}()
	written, err := io.Copy(file, io.LimitReader(&contextReader{ctx: ctx, reader: reader}, int64(size)+1))
	if err != nil || written < 0 || uint64(written) != size { // #nosec G115 -- written is checked non-negative first.
		return ErrConflict
	}
	if file.Sync() != nil || file.Chmod(0o640) != nil || file.Close() != nil || unix.Fsync(parent) != nil {
		return ErrUnavailable
	}
	cleanup = false
	return nil
}

func openArchiveDirectory(root int, components []string, identity hostingidentity.Spec, device uint64) (int, error) {
	current, err := unix.Dup(root)
	if err != nil {
		return -1, ErrUnavailable
	}
	for _, component := range components {
		next, err := unix.Openat(current, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(current)
		if err != nil {
			return -1, classifyFileMutationError(err)
		}
		if _, err := validateManagedDirectoryDescriptor(next, identity, device); err != nil {
			_ = unix.Close(next)
			return -1, err
		}
		current = next
	}
	return current, nil
}

func archiveModifiedTime(status unix.Stat_t) time.Time {
	return time.Unix(status.Mtim.Sec, status.Mtim.Nsec).UTC()
}

func archiveResult(request agentprotocol.FileWriteRequest, size, entries uint64) agentprotocol.FileWriteResult {
	return agentprotocol.FileWriteResult{OperationID: request.OperationID,
		SourceDirectory: request.SourceDirectory, SourceName: request.SourceName,
		Directory: request.Directory, Name: request.Name, ArchiveFormat: request.ArchiveFormat,
		SizeBytes: size, EntryCount: entries, Completed: true}
}

func classifyArchiveError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrNotFound), errors.Is(err, ErrConflict),
		errors.Is(err, ErrTooLarge), errors.Is(err, ErrBusy), errors.Is(err, ErrQuota), errors.Is(err, ErrUnavailable):
		return err
	default:
		return classifyFileMutationError(err)
	}
}
