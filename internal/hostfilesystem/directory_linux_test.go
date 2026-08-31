// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfilesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEnsureRelativeDirectoriesDoesNotFollowSymlinks(t *testing.T) {
	accountPath := t.TempDir()
	account, err := unix.Open(accountPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(account)
	var status unix.Stat_t
	if err := unix.Fstat(account, &status); err != nil {
		t.Fatal(err)
	}
	created, err := ensureRelativeDirectories(
		account, uint64(status.Dev), uint32(os.Getuid()), uint32(os.Getgid()), "domains/example.test/public",
	)
	if err != nil || !created {
		t.Fatalf("first ensure = %v, %v", created, err)
	}
	created, err = ensureRelativeDirectories(
		account, uint64(status.Dev), uint32(os.Getuid()), uint32(os.Getgid()), "domains/example.test/public",
	)
	if err != nil || created {
		t.Fatalf("second ensure = %v, %v", created, err)
	}

	escape := t.TempDir()
	if err := os.Symlink(escape, filepath.Join(accountPath, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureRelativeDirectories(
		account, uint64(status.Dev), uint32(os.Getuid()), uint32(os.Getgid()), "escape/nested",
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("symlink ensure error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(escape, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escape target was changed: %v", err)
	}
}
