// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"strconv"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
)

const MaximumFileListingEntries = 100

type FileEntryType string

const (
	FileEntryDirectory FileEntryType = "directory"
	FileEntryRegular   FileEntryType = "file"
	FileEntrySymlink   FileEntryType = "symlink"
	FileEntryOther     FileEntryType = "other"
)

type FileListRequest struct {
	Identity hostingidentity.Spec `json:"identity"`
	Path     string               `json:"path"`
	Cursor   string               `json:"cursor,omitempty"`
	Limit    uint32               `json:"limit"`
}

type FileEntry struct {
	Name       string        `json:"name"`
	Type       FileEntryType `json:"type"`
	SizeBytes  uint64        `json:"sizeBytes"`
	Mode       uint32        `json:"mode"`
	ModifiedAt time.Time     `json:"modifiedAt"`
	Hidden     bool          `json:"hidden"`
}

type FileListResponse struct {
	Path           string      `json:"path"`
	Entries        []FileEntry `json:"entries"`
	Next           string      `json:"next,omitempty"`
	OmittedEntries uint64      `json:"omittedEntries"`
}

func validateFileListRequest(request FileListRequest) error {
	if hostingidentity.Validate(request.Identity) != nil || request.Limit == 0 || request.Limit > MaximumFileListingEntries {
		return errors.New("file listing request is invalid")
	}
	normalized, err := hostingpath.NormalizeFileManagerDirectory(request.Path)
	if err != nil || normalized != request.Path || !validFileCursor(request.Cursor) {
		return errors.New("file listing request is invalid")
	}
	return nil
}

func validateFileListResponse(response FileListResponse, operation Operation) error {
	if operation != OperationListFiles || response.Entries == nil || len(response.Entries) > MaximumFileListingEntries {
		return errors.New("agent file listing response is malformed")
	}
	normalized, err := hostingpath.NormalizeFileManagerDirectory(response.Path)
	if err != nil || normalized != response.Path {
		return errors.New("agent file listing response is malformed")
	}
	names := make(map[string]struct{}, len(response.Entries))
	for _, entry := range response.Entries {
		if !hostingpath.ValidFilename(entry.Name) || !validFileEntryType(entry.Type) || entry.Mode > 0o7777 ||
			entry.ModifiedAt.IsZero() || entry.ModifiedAt.Location() != time.UTC || entry.Hidden != (entry.Name[0] == '.') {
			return errors.New("agent file listing entry is malformed")
		}
		if _, duplicate := names[entry.Name]; duplicate {
			return errors.New("agent file listing entries contain duplicates")
		}
		names[entry.Name] = struct{}{}
	}
	if response.Next != "" && !validFileCursor(response.Next) {
		return errors.New("agent file listing cursor is malformed")
	}
	return nil
}

func validFileCursor(value string) bool {
	if value == "" {
		return true
	}
	offset, err := strconv.ParseUint(value, 10, 63)
	return err == nil && offset > 0 && strconv.FormatUint(offset, 10) == value
}

func validFileEntryType(value FileEntryType) bool {
	return value == FileEntryDirectory || value == FileEntryRegular || value == FileEntrySymlink || value == FileEntryOther
}
