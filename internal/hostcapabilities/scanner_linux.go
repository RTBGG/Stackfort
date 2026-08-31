// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostcapabilities

import (
	"errors"
	"os"
	"syscall"
)

func trustedScannerExecutable(path string) (trusted bool, present bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	trusted = ok && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 &&
		info.Mode().Perm() == 0o755 && status.Uid == 0 && status.Gid == 0 && status.Nlink == 1
	return trusted, true, nil
}
