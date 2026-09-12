// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRuntimeDirectoryFailuresCloseOwnedDescriptorsAndPreserveCaller(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"invalid-mode", "invalid-owner", "mid-chain-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			local := filepath.Join(root, ".local")
			if err := os.Mkdir(local, 0o700); err != nil {
				t.Fatal(err)
			}
			uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
			components := []string{".local"}
			switch scenario {
			case "invalid-mode":
				if err := os.Chmod(local, 0o750); err != nil {
					t.Fatal(err)
				}
			case "invalid-owner":
				uid++ // Existing directories must be rejected, never chowned.
			case "mid-chain-symlink":
				if err := os.Symlink("unresolved-target", filepath.Join(local, "share")); err != nil {
					t.Fatal(err)
				}
				components = append(components, "share")
			}
			parent, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(parent)
			var original unix.Stat_t
			if err := unix.Fstat(parent, &original); err != nil {
				t.Fatal(err)
			}
			before := countRuntimeFixtureDescriptors(t, local)
			for attempt := 0; attempt < 32; attempt++ {
				if _, err := ensureDirectoryChain(parent, components, uid, gid, 0o700); err == nil {
					t.Fatal("unsafe directory chain was accepted")
				}
			}
			if after := countRuntimeFixtureDescriptors(t, local); after != before {
				t.Fatalf("failed chain retained fixture descriptors: before=%d after=%d", before, after)
			}
			var retained unix.Stat_t
			if err := unix.Fstat(parent, &retained); err != nil || retained.Dev != original.Dev || retained.Ino != original.Ino {
				t.Fatalf("directory helper closed or changed caller descriptor: %v", err)
			}
		})
	}
}

func TestAbsoluteRuntimeDirectoryFailureClosesOwnedParent(t *testing.T) {
	// /tmp is deliberately unsuitable for root-owned Quadlet parents. Inspect
	// its metadata first so the helper must reject before any mkdir can occur.
	var temporary unix.Stat_t
	if err := unix.Lstat("/tmp", &temporary); err != nil || temporary.Mode&unix.S_IFMT != unix.S_IFDIR || temporary.Mode&0o022 == 0 {
		t.Skip("requires an existing writable /tmp directory for the no-mutation parent failure")
	}
	before := countRuntimeFixtureDescriptors(t, "/tmp")
	for attempt := 0; attempt < 32; attempt++ {
		if _, err := ensureAbsoluteDirectoryChain([]string{"tmp", "must-never-be-created-by-this-test"}, 0, 0, 0o755); err == nil {
			t.Fatal("writable absolute parent was accepted")
		}
	}
	if after := countRuntimeFixtureDescriptors(t, "/tmp"); after != before {
		t.Fatalf("failed absolute chain retained parent descriptors: before=%d after=%d", before, after)
	}
}

func countRuntimeFixtureDescriptors(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil {
			// ReadDir's own descriptor is already closed; unrelated descriptors
			// may also close concurrently. Only this exact fixture is counted.
			continue
		}
		if target == path {
			count++
		}
	}
	return count
}
