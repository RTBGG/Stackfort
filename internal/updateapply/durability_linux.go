// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package updateapply

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

func syncDirectoryEntry(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func persistReleaseTree(root string) error {
	directories := make([]string, 0, 64)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
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
		file, err := os.Open(path)
		if err != nil {
			return err
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
