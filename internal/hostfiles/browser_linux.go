// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"golang.org/x/sys/unix"
)

type linuxBrowser struct{}

func newPlatformBrowser() platformBrowser { return linuxBrowser{} }

func (linuxBrowser) List(ctx context.Context, request agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error) {
	directory, device, err := openManagedDirectory(request.Identity, request.Path)
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	defer unix.Close(directory)

	names, omitted, next, err := pageNames(ctx, directory, request.Cursor, int(request.Limit))
	if err != nil {
		return agentprotocol.FileListResponse{}, err
	}
	entries := make([]agentprotocol.FileEntry, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return agentprotocol.FileListResponse{}, err
		}
		var status unix.Stat_t
		if err := unix.Fstatat(directory, name, &status, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			return agentprotocol.FileListResponse{}, ErrUnavailable
		}
		if uint64(status.Dev) != device {
			return agentprotocol.FileListResponse{}, ErrConflict
		}
		size := uint64(0)
		if status.Size > 0 {
			size = uint64(status.Size)
		}
		entries = append(entries, agentprotocol.FileEntry{
			Name: name, Type: entryType(status.Mode), SizeBytes: size,
			Mode:       uint32(status.Mode) & 0o7777,
			ModifiedAt: time.Unix(status.Mtim.Sec, status.Mtim.Nsec).UTC(), Hidden: strings.HasPrefix(name, "."),
		})
	}
	response := agentprotocol.FileListResponse{Path: request.Path, Entries: entries, OmittedEntries: omitted}
	response.Next = next
	return response, nil
}

const maximumScannedEntriesPerPage = 4096

func pageNames(ctx context.Context, descriptor int, cursor string, limit int) ([]string, uint64, string, error) {
	if cursor != "" {
		offset, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil || offset <= 0 {
			return nil, 0, "", ErrInvalid
		}
		if _, err := unix.Seek(descriptor, offset, 0); err != nil {
			return nil, 0, "", ErrInvalid
		}
	}
	names := make([]string, 0, limit+1)
	offsets := make([]int64, 0, limit+1)
	omittedAtName := make([]uint64, 0, limit+1)
	var omitted uint64
	buffer := make([]byte, 16<<10)
	scanned := 0
	lastOffset := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, "", err
		}
		count, readErr := unix.Getdents(descriptor, buffer)
		if readErr != nil {
			return nil, 0, "", ErrUnavailable
		}
		if count == 0 {
			return names, omitted, "", nil
		}
		for position := 0; position < count; {
			name, offset, recordLength, ok := parseDirent(buffer[position:count])
			if !ok {
				return nil, 0, "", ErrUnavailable
			}
			position += recordLength
			lastOffset = offset
			if name == "." || name == ".." {
				continue
			}
			// Account-owned staging and trash are implementation details and must
			// never become actionable through normal file-manager navigation.
			if name == agentprotocol.ReservedFileUploadDirectory ||
				name == agentprotocol.ReservedFileOperationDirectory ||
				name == agentprotocol.ReservedFileTrashDirectory {
				continue
			}
			scanned++
			if !hostingpath.ValidFilename(name) {
				omitted++
			} else {
				names = append(names, name)
				offsets = append(offsets, offset)
				omittedAtName = append(omittedAtName, omitted)
			}
			if len(names) > limit {
				return names[:limit], omittedAtName[limit-1], strconv.FormatInt(offsets[limit-1], 10), nil
			}
			if scanned >= maximumScannedEntriesPerPage {
				return names, omitted, strconv.FormatInt(lastOffset, 10), nil
			}
		}
	}
}

func parseDirent(record []byte) (string, int64, int, bool) {
	// #nosec G103 -- parsing the fixed Linux getdents64 ABI requires the
	// architecture-specific offsets published by x/sys/unix.Dirent. Bounds are
	// checked before each fixed-width read and the record length before slicing.
	var layout unix.Dirent
	offOffset := int(unsafe.Offsetof(layout.Off))
	reclenOffset := int(unsafe.Offsetof(layout.Reclen))
	nameOffset := int(unsafe.Offsetof(layout.Name))
	if len(record) < nameOffset+1 || len(record) < reclenOffset+2 || len(record) < offOffset+8 {
		return "", 0, 0, false
	}
	recordLength := int(*(*uint16)(unsafe.Pointer(&record[reclenOffset]))) // #nosec G103 -- bounded Linux ABI read
	if recordLength < nameOffset+1 || recordLength > len(record) {
		return "", 0, 0, false
	}
	offset := *(*int64)(unsafe.Pointer(&record[offOffset])) // #nosec G103 -- bounded Linux ABI read
	if offset <= 0 {
		return "", 0, 0, false
	}
	nameBytes := record[nameOffset:recordLength]
	terminator := bytes.IndexByte(nameBytes, 0)
	if terminator < 0 {
		return "", 0, 0, false
	}
	return string(nameBytes[:terminator]), offset, recordLength, true
}

func openManagedDirectory(identity hostingidentity.Spec, relativePath string) (int, uint64, error) {
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, 0, ErrUnavailable
	}
	var managedDevice uint64
	components := []string{"srv", "hosting", "accounts", identity.AccountID}
	if relativePath != "" {
		components = append(components, strings.Split(relativePath, "/")...)
	}
	for index, component := range components {
		next, openErr := unix.Openat(descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(descriptor)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) || errors.Is(openErr, unix.ENOTDIR) {
				return -1, 0, ErrNotFound
			}
			return -1, 0, ErrConflict
		}
		descriptor = next
		var status unix.Stat_t
		if err := unix.Fstat(descriptor, &status); err != nil {
			_ = unix.Close(descriptor)
			return -1, 0, ErrUnavailable
		}
		if index < 3 && (status.Uid != 0 || status.Gid != 0 || status.Mode&0o022 != 0) {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
		if index == 1 {
			managedDevice = uint64(status.Dev)
		}
		if index > 1 && uint64(status.Dev) != managedDevice {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
		if index >= 3 && (status.Uid != identity.UID || status.Gid != identity.GID) {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
	}
	return descriptor, managedDevice, nil
}

func entryType(mode uint32) agentprotocol.FileEntryType {
	switch mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return agentprotocol.FileEntryDirectory
	case unix.S_IFREG:
		return agentprotocol.FileEntryRegular
	case unix.S_IFLNK:
		return agentprotocol.FileEntrySymlink
	default:
		return agentprotocol.FileEntryOther
	}
}
