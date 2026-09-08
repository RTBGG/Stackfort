// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingoci"
	"golang.org/x/sys/unix"
)

func TestStorageLabelIsBoundedAndFailsClosed(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"empty", "already-correct", "populated", "wrong-mode", "wrong-owner", "unreadable", "write-denied", "verification-failed"} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if name == "populated" {
				if err := os.WriteFile(filepath.Join(directory, "tenant-data"), []byte("unchanged"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "wrong-mode" {
				if err := os.Chmod(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			label := ""
			if name == "already-correct" {
				label = hostingoci.StorageSELinuxContext + "\x00"
			}
			writes := 0
			get := func(actualFD int, attribute string, value []byte) (int, error) {
				if actualFD != fd || attribute != "security.selinux" || len(value) != 4096 {
					t.Fatal("unbounded or unanchored attribute read")
				}
				if name == "unreadable" {
					return -1, unix.EACCES
				}
				if label == "" {
					return -1, unix.ENODATA
				}
				return copy(value, label), nil
			}
			set := func(actualFD int, attribute string, value []byte, flags int) error {
				if actualFD != fd || attribute != "security.selinux" || flags != 0 || string(value) != hostingoci.StorageSELinuxContext+"\x00" {
					t.Fatal("unanchored or caller-selected label write")
				}
				writes++
				if name == "write-denied" {
					return unix.EPERM
				}
				if name != "verification-failed" {
					label = string(value)
				}
				return nil
			}
			uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
			if name == "wrong-owner" {
				uid++
			}
			changed, err := labelEmptyStorageFD(fd, uid, gid, get, set)
			switch name {
			case "empty":
				if err != nil || !changed || writes != 1 {
					t.Fatalf("label: %t/%v/%d", changed, err, writes)
				}
			case "already-correct":
				if err != nil || changed || writes != 0 {
					t.Fatalf("no-op: %t/%v/%d", changed, err, writes)
				}
			default:
				if err == nil || changed {
					t.Fatalf("unsafe label accepted: %t/%v", changed, err)
				}
				if name != "write-denied" && name != "verification-failed" && writes != 0 {
					t.Fatal("mutated an unverified store")
				}
			}
			if name == "populated" {
				content, err := os.ReadFile(filepath.Join(directory, "tenant-data"))
				if err != nil || string(content) != "unchanged" {
					t.Fatal("tenant data changed")
				}
			}
		})
	}
}

func TestStorageLabelRejectsNonCanonicalPath(t *testing.T) {
	t.Parallel()
	spec, err := hostingoci.ForIdentity(testSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	spec.StorageRoot = t.TempDir()
	if changed, err := labelEmptyContainerStorage(spec); changed || !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("noncanonical path accepted: %t/%v", changed, err)
	}
}
