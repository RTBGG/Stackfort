// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostingpath defines account-relative paths shared by the control
// plane and privileged host agent. It validates names only; the host agent
// must still resolve every component through directory descriptors.
package hostingpath

import (
	"errors"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaximumDocumentRootBytes = 1024

var (
	ErrInvalidDocumentRoot = errors.New("invalid hosting document root")
	segmentPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)
)

// NormalizeDocumentRoot accepts only a canonical Linux-relative path. It
// deliberately rejects alternate separators and dot segments before any host
// filesystem operation is attempted.
func NormalizeDocumentRoot(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || containsControl(value) {
		return "", ErrInvalidDocumentRoot
	}
	if strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || path.IsAbs(value) {
		return "", ErrInvalidDocumentRoot
	}
	if len(value) > MaximumDocumentRootBytes || path.Clean(value) != value {
		return "", ErrInvalidDocumentRoot
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." || !segmentPattern.MatchString(segment) {
			return "", ErrInvalidDocumentRoot
		}
	}
	return value, nil
}

func containsControl(value string) bool {
	return strings.ContainsRune(value, utf8.RuneError) || strings.IndexFunc(value, unicode.IsControl) >= 0
}
