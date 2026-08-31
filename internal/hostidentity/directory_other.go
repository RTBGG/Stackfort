// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostidentity

import "github.com/RTBGG/stackfort/internal/hostingidentity"

type unsupportedDirectoryManager struct{}

func newDirectoryManager() directoryManager { return unsupportedDirectoryManager{} }

func (unsupportedDirectoryManager) Ensure(hostingidentity.Spec) (bool, bool, error) {
	return false, false, ErrMutationFailed
}

func (unsupportedDirectoryManager) RequireArchived(hostingidentity.Spec) error {
	return ErrMutationFailed
}
