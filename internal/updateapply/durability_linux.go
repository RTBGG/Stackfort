// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package updateapply

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

func syncDirectoryEntry(path string) error {
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

func persistReleaseTree(root string) error {
	releaseRoot, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer releaseRoot.Close()
	directories := make([]string, 0, 64)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("verified release tree contains an unsupported file type")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := releaseRoot.Open(relative)
		if err != nil {
			return err
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			return errors.Join(statErr, errors.New("verified release tree changed during persistence"))
		}
		syncErr := file.Sync()
		return errors.Join(syncErr, file.Close())
	})
	if err != nil {
		return err
	}
	sort.Slice(directories, func(left, right int) bool {
		return len(directories[left]) > len(directories[right])
	})
	for _, directory := range directories {
		if err := syncDirectoryEntry(directory); err != nil {
			return err
		}
	}
	return nil
}
