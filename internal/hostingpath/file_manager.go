// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingpath

import (
	"errors"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaximumFileManagerPathBytes = 4096
	MaximumFilenameBytes        = 255
)

var ErrInvalidFileManagerPath = errors.New("invalid file-manager path")

// NormalizeFileManagerDirectory accepts the account root as an empty string
// and otherwise requires a canonical, account-relative Linux path. Host code
// must still resolve every component through directory descriptors.
func NormalizeFileManagerDirectory(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > MaximumFileManagerPathBytes || strings.TrimSpace(value) != value ||
		!utf8.ValidString(value) || containsControl(value) || strings.Contains(value, "\\") ||
		strings.HasPrefix(value, "/") || path.IsAbs(value) || path.Clean(value) != value {
		return "", ErrInvalidFileManagerPath
	}
	for _, component := range strings.Split(value, "/") {
		if !ValidFilename(component) {
			return "", ErrInvalidFileManagerPath
		}
	}
	return value, nil
}

// NormalizeFileManagerFile applies the same canonical account-relative path
// contract as directory navigation, but rejects the account root. Host code
// must still prove that the final descriptor is a regular file.
func NormalizeFileManagerFile(value string) (string, error) {
	normalized, err := NormalizeFileManagerDirectory(value)
	if err != nil || normalized == "" {
		return "", ErrInvalidFileManagerPath
	}
	return normalized, nil
}

// ValidFilename is shared by listing cursors and response validation. It
// excludes names that cannot be safely round-tripped through the JSON/UI path
// contract; the host listing reports how many such entries were omitted.
func ValidFilename(value string) bool {
	return value != "" && value != "." && value != ".." && len(value) <= MaximumFilenameBytes &&
		strings.TrimSpace(value) == value && utf8.ValidString(value) && !containsControl(value) &&
		!strings.ContainsAny(value, "/\\")
}
