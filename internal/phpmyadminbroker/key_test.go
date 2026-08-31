// SPDX-License-Identifier: AGPL-3.0-or-later

package phpmyadminbroker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSharedKeyRejectsMalformedAndSymlinkFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	validPath := filepath.Join(directory, "broker.key")
	wanted := bytes.Repeat([]byte{0x5a}, SharedKeyBytes)
	if err := os.WriteFile(validPath, wanted, 0o640); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSharedKey(validPath)
	if err != nil || !bytes.Equal(loaded[:], wanted) {
		t.Fatalf("loaded key/error = %x/%v", loaded, err)
	}
	malformedPath := filepath.Join(directory, "malformed.key")
	if err := os.WriteFile(malformedPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSharedKey(malformedPath); err == nil {
		t.Fatal("malformed key was accepted")
	}
	symlinkPath := filepath.Join(directory, "linked.key")
	if err := os.Symlink(validPath, symlinkPath); err == nil {
		if _, err := LoadSharedKey(symlinkPath); err == nil {
			t.Fatal("symbolic link key was accepted")
		}
	}
}
