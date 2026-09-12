// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package storageprep

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func rootTestDirectory(t *testing.T) (string, int) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned journal tests require Linux root in a disposable test environment")
	}
	directory := t.TempDir()
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return directory, fd
}

func openTestStore(t *testing.T, path string) *FileStore {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	store, err := lockStoreAt(fd)
	if err != nil {
		_ = unix.Close(fd)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestFileStoreDurabilityLockAndProductionGate(t *testing.T) {
	directory, fd := rootTestDirectory(t)
	if err := checkInactiveAt(fd); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatal("read-only gate created state")
	}
	store := openTestStore(t, directory)
	// Compete with the same filename/flock used by installapply.FileStore.
	other, err := unix.Openat(fd, "install.lock", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(other)
	if err := unix.Flock(other, unix.LOCK_EX|unix.LOCK_NB); err == nil {
		t.Fatal("installation lock was not exclusive")
	}
	backend := newBackend(store)
	decision, err := Advance(t.Context(), store, backend, testPlan())
	if err != nil || !decision.Waiting {
		t.Fatalf("prepare=%+v %v", decision, err)
	}
	if err := checkInactiveAt(fd); !errors.Is(err, ErrNotQualified) {
		t.Fatalf("production accepted awaiting-reboot: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("closed store can load")
	}
	if err := store.Save(decision.State); err == nil {
		t.Fatal("closed store can write")
	}
	store = openTestStore(t, directory)
	state, exists, err := store.Load()
	if err != nil || !exists || state != decision.State {
		t.Fatalf("reopened state=%+v %v", state, err)
	}
	backend = newBackend(store)
	backend.observation.BootID = nextBoot
	decision, err = Advance(t.Context(), store, backend, testPlan())
	if err != nil || !decision.Ready {
		t.Fatalf("resume=%+v %v", decision, err)
	}
	if err := checkInactiveAt(fd); !errors.Is(err, ErrNotQualified) {
		t.Fatalf("unqualified Ready admitted: %v", err)
	}
	info, err := os.Stat(filepath.Join(directory, journalName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("unsafe saved file mode")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 2 {
		t.Fatalf("temporary state leaked: %v %v", entries, err)
	}
	before, err := os.ReadFile(filepath.Join(directory, journalName))
	if err != nil {
		t.Fatal(err)
	}
	reset := State{SchemaVersion: SchemaVersion, Plan: testPlan(), Phase: Planned}
	if err := store.Save(reset); err == nil {
		t.Fatal("persistent latch reset")
	}
	after, err := os.ReadFile(filepath.Join(directory, journalName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected transition changed persisted journal")
	}
}

func TestProductionGateRejectsPreJournalRuntimeFragments(t *testing.T) {
	for _, name := range []string{"native-runtime-installer", "native-runtime-intent.json", "native-prerequisites.json", "native-prerequisite-installer", "native-recovery-choice.json", "native-boot-build", "native-onboarding-source.json", "native-setup.json"} {
		for _, kind := range []string{"partial", "symlink", "directory"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				root, fd := rootTestDirectory(t)
				path := filepath.Join(root, name)
				var err error
				switch kind {
				case "partial":
					err = os.WriteFile(path, []byte("partial"), 0600)
				case "symlink":
					err = os.Symlink("missing", path)
				case "directory":
					err = os.Mkdir(path, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := checkInactiveAt(fd); !errors.Is(err, ErrNotQualified) {
					t.Fatal("orphan runtime treated as inactivity", err)
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 1 || entries[0].Name() != name {
					t.Fatal("gate modified evidence", entries, err)
				}
			})
		}
	}
}

func TestExistingLockDoesNotCreateOrIgnoreContention(t *testing.T) {
	directory, fd := rootTestDirectory(t)
	if _, err := lockStoreAtMode(fd, false); err == nil {
		t.Fatal("missing lock accepted")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("read-only inspection created files", err)
	}
	store := openTestStore(t, directory)
	if _, err := lockStoreAtMode(fd, false); err == nil {
		t.Fatal("busy lock accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Dup so the returned store owns a distinct descriptor from test cleanup.
	owned, err := unix.Dup(fd)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := lockStoreAtMode(owned, false)
	if err != nil {
		_ = unix.Close(owned)
		t.Fatal(err)
	}
	if err := existing.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileStoreRejectsUnsafeStateWithoutReplacingIt(t *testing.T) {
	planned := State{SchemaVersion: SchemaVersion, Plan: testPlan(), Phase: Planned}
	canonical, err := json.MarshalIndent(planned, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	for _, kind := range []string{"symlink", "hardlink", "fifo", "directory", "mode", "owner", "oversize", "truncated", "unknown-key", "duplicate-key", "case-alias", "trailing", "invalid-phase"} {
		t.Run(kind, func(t *testing.T) {
			directory, fd := rootTestDirectory(t)
			store := openTestStore(t, directory)
			path := filepath.Join(directory, journalName)
			content := string(canonical)
			switch kind {
			case "oversize":
				content = strings.Repeat("x", maximumStateBytes+1)
			case "truncated":
				content = content[:len(content)/2]
			case "unknown-key":
				content = strings.Replace(content, `"phase":`, `"unexpected": true, "phase":`, 1)
			case "duplicate-key":
				content = strings.Replace(content, `"phase":`, `"phase": "ready", "phase":`, 1)
			case "case-alias":
				content = strings.Replace(content, `"phase":`, `"Phase":`, 1)
			case "trailing":
				content += "{}"
			case "invalid-phase":
				content = strings.Replace(content, `"planned"`, `"anything"`, 1)
			}
			switch kind {
			case "symlink", "hardlink":
				target := filepath.Join(directory, "sentinel")
				if err := os.WriteFile(target, []byte("must survive"), 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					err = os.Symlink(target, path)
				} else {
					err = os.Link(target, path)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "mode" {
					if err := os.Chmod(path, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "owner" {
					if err := os.Chown(path, 65534, 65534); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Load(); err == nil {
				t.Fatal("unsafe state accepted")
			}
			if err := checkInactiveAt(fd); err == nil {
				t.Fatal("unsafe journal treated as absent")
			}
			if err := store.Save(planned); err == nil {
				t.Fatal("unsafe state overwritten")
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("rejected journal replaced")
			}
			if kind == "symlink" || kind == "hardlink" {
				data, err := os.ReadFile(filepath.Join(directory, "sentinel"))
				if err != nil || string(data) != "must survive" {
					t.Fatal("link target modified")
				}
			}
		})
	}
}

func TestStateDirectoryWalkIsReadOnlyAndRejectsUnsafeAncestors(t *testing.T) {
	for _, kind := range []string{"missing", "create", "symlink", "writable-parent", "wrong-mode", "foreign-owner"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := rootTestDirectory(t)
			lib := filepath.Join(root, "var", "lib")
			if err := os.MkdirAll(lib, 0o755); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(lib, "stackfort-installer")
			var err error
			switch kind {
			case "symlink":
				if err := os.Symlink(root, state); err != nil {
					t.Fatal(err)
				}
			case "writable-parent":
				if err := os.Chmod(lib, 0o777); err != nil {
					t.Fatal(err)
				}
			case "wrong-mode", "foreign-owner":
				if err := os.Mkdir(state, 0o700); err != nil {
					t.Fatal(err)
				}
				if kind == "wrong-mode" {
					err = os.Chmod(state, 0o755)
				} else {
					err = os.Chown(state, 65534, 65534)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			fd, err := openStateDirectoryAt(root, kind == "create")
			if err == nil {
				_ = unix.Close(fd)
			}
			switch kind {
			case "create":
				if err != nil {
					t.Fatal(err)
				}
			case "missing":
				if !errors.Is(err, unix.ENOENT) {
					t.Fatal(err)
				}
				if _, err := os.Lstat(state); !os.IsNotExist(err) {
					t.Fatal("inspection created state directory")
				}
			default:
				if err == nil {
					t.Fatal("unsafe directory accepted")
				}
			}
		})
	}
}

func TestUnsafeInstallationLockRejected(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "mode"} {
		t.Run(kind, func(t *testing.T) {
			directory, fd := rootTestDirectory(t)
			path := filepath.Join(directory, lockName)
			switch kind {
			case "symlink":
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				target := filepath.Join(directory, "other")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(target, path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if store, err := lockStoreAt(fd); err == nil {
				_ = store.Close()
				t.Fatal("unsafe lock accepted")
			}
		})
	}
}
