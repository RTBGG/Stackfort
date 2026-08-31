// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"golang.org/x/sys/unix"
)

type linuxDirectoryManager struct{}

func newDirectoryManager() directoryManager { return linuxDirectoryManager{} }

func (linuxDirectoryManager) Ensure(spec hostingidentity.Spec) (bool, bool, error) {
	parent, _, err := openAccountsRoot(true)
	if err != nil {
		return false, false, err
	}
	defer unix.Close(parent)
	created := false
	account, openErr := openDirectoryAt(parent, spec.AccountID)
	if errors.Is(openErr, unix.ENOENT) {
		if err := unix.Mkdirat(parent, spec.AccountID, 0o750); err != nil && !errors.Is(err, unix.EEXIST) {
			return false, false, err
		}
		created = true
		account, openErr = openDirectoryAt(parent, spec.AccountID)
	}
	if openErr != nil {
		return false, false, openErr
	}
	defer unix.Close(account)
	var status unix.Stat_t
	if err := unix.Fstat(account, &status); err != nil {
		return false, false, err
	}
	repaired := false
	if status.Uid != spec.UID || status.Gid != spec.GID {
		if err := unix.Fchown(account, int(spec.UID), int(spec.GID)); err != nil {
			return false, false, err
		}
		repaired = !created
	}
	if status.Mode&0o7777 != 0o750 {
		if err := unix.Fchmod(account, 0o750); err != nil {
			return false, false, err
		}
		repaired = repaired || !created
	}
	return created, repaired, nil
}

func (linuxDirectoryManager) RequireArchived(spec hostingidentity.Spec) error {
	parent, missing, err := openAccountsRoot(false)
	if err != nil {
		return err
	}
	if missing {
		return nil
	}
	defer unix.Close(parent)
	account, err := openDirectoryAt(parent, spec.AccountID)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = unix.Close(account)
	return ErrArchiveRequired
}

func openAccountsRoot(create bool) (descriptor int, missing bool, err error) {
	descriptor, err = unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, false, err
	}
	for _, component := range []string{"srv", "hosting", "accounts"} {
		next, openErr := openDirectoryAt(descriptor, component)
		if errors.Is(openErr, unix.ENOENT) {
			if !create {
				_ = unix.Close(descriptor)
				return -1, true, nil
			}
			if mkdirErr := unix.Mkdirat(descriptor, component, 0o755); mkdirErr != nil &&
				!errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(descriptor)
				return -1, false, mkdirErr
			}
			next, openErr = openDirectoryAt(descriptor, component)
		}
		if openErr != nil {
			_ = unix.Close(descriptor)
			return -1, false, openErr
		}
		_ = unix.Close(descriptor)
		descriptor = next
		var status unix.Stat_t
		if statErr := unix.Fstat(descriptor, &status); statErr != nil || status.Uid != 0 || status.Gid != 0 ||
			status.Mode&0o022 != 0 {
			_ = unix.Close(descriptor)
			if statErr != nil {
				return -1, false, statErr
			}
			return -1, false, ErrMutationFailed
		}
	}
	return descriptor, false, nil
}

func openDirectoryAt(parent int, component string) (int, error) {
	return unix.Openat(parent, component,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}
