// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRemoveStaleSocketRejectsRegularFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := removeStaleSocket(path); err == nil {
		t.Fatal("removeStaleSocket accepted a regular file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("regular file was changed: %v", err)
	}
}

func TestRemoveStaleSocketAllowsMissingPath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.sock")
	if err := removeStaleSocket(path); err != nil {
		t.Fatalf("removeStaleSocket returned %v", err)
	}
}

func TestPrepareSocketDirectoryProtectsAndRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "runtime")
	socketPath := filepath.Join(directory, "agent.sock")
	if err := prepareSocketDirectory(socketPath, os.Getgid()); err != nil {
		t.Fatalf("prepareSocketDirectory: %v", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		t.Fatalf("inspect directory: %v", err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("directory mode = %o, want 750", info.Mode().Perm())
	}
	if err := prepareSocketDirectory("relative/agent.sock", os.Getgid()); err == nil {
		t.Fatal("relative socket path was accepted")
	}

	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o750); err != nil {
		t.Fatalf("create real directory: %v", err)
	}
	symlinkDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, symlinkDirectory); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := prepareSocketDirectory(filepath.Join(symlinkDirectory, "agent.sock"), os.Getgid()); err == nil {
		t.Fatal("symlinked socket directory was accepted")
	}
}

func TestResolveControlAPIIdentityUsesCompleteNumericOverride(t *testing.T) {
	previousUID := configuredControlAPIUID
	previousGID := configuredControlAPIGID
	t.Cleanup(func() {
		configuredControlAPIUID = previousUID
		configuredControlAPIGID = previousGID
	})
	configuredControlAPIUID = strconv.Itoa(os.Geteuid())
	configuredControlAPIGID = strconv.Itoa(os.Getegid())
	uid, gid, err := resolveControlAPIIdentity()
	if err != nil {
		t.Fatalf("resolveControlAPIIdentity: %v", err)
	}
	if uid != uint32(os.Geteuid()) || gid != os.Getegid() {
		t.Fatalf("identity uid=%d gid=%d", uid, gid)
	}
	configuredControlAPIGID = ""
	if _, _, err := resolveControlAPIIdentity(); err == nil {
		t.Fatal("partial identity override was accepted")
	}
}
