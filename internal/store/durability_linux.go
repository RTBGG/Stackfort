// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package store

import "os"

func syncBackupDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
