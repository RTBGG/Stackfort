// SPDX-License-Identifier: AGPL-3.0-or-later

package secretstore

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreateMasterKeyPersistsExactPrivateKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "master.key")
	first, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("create master key: %v", err)
	}
	second, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("reload master key: %v", err)
	}
	if !bytes.Equal(first[:], second[:]) {
		t.Fatal("reloaded master key changed")
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Size() != MasterKeyBytes {
		t.Fatalf("master key size = %d", info.Size())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("master key mode = %o", info.Mode().Perm())
	}
}

func TestLoadOrCreateMasterKeyRejectsMalformedAndSymlinkFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	malformed := filepath.Join(directory, "malformed.key")
	if err := os.WriteFile(malformed, []byte("short"), 0o600); err != nil {
		t.Fatalf("write malformed key: %v", err)
	}
	if _, err := LoadOrCreateMasterKey(malformed); err == nil {
		t.Fatal("malformed key was accepted")
	}

	target := filepath.Join(directory, "target.key")
	if err := os.WriteFile(target, make([]byte, MasterKeyBytes), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	symlink := filepath.Join(directory, "link.key")
	if err := os.Symlink(target, symlink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := LoadOrCreateMasterKey(symlink); err == nil {
		t.Fatal("symlink key was accepted")
	}
}
