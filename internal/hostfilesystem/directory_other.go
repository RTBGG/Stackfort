// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostfilesystem

import (
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

type unsupportedDirectoryManager struct{}

func newDirectoryManager() directoryManager { return unsupportedDirectoryManager{} }

func (unsupportedDirectoryManager) EnsureLayout(hostingstorage.Spec) (LayoutResult, error) {
	return LayoutResult{}, ErrMutationFailed
}

func (unsupportedDirectoryManager) EnsureDocumentRoot(hostingidentity.Spec, string) (bool, error) {
	return false, ErrMutationFailed
}
