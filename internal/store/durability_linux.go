// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package store

import (
	"os"

	"golang.org/x/sys/unix"
)

func syncBackupDirectory(path string) error {
	fileDescriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fileDescriptor), path)
	if directory == nil {
		_ = unix.Close(fileDescriptor)
		return os.ErrInvalid
	}
	defer directory.Close()
	return directory.Sync()
}
