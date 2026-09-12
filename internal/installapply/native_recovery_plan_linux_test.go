// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDisposableNativeRecoveryInspection(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires explicit disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_NATIVE_RECOVERY_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeRecoveryInspection$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_RECOVERY_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == parent {
		t.Fatal("mount isolation required")
	}
	for _, scenario := range []string{"absent", "empty", "busy", "missing-lock", "unsafe-lock", "checking", "complete", "malformed", "duplicate", "unknown", "symlink", "hardlink", "fifo", "orphan-runtime"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := t.TempDir()
			if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer unix.Unmount("/var/lib", 0)
			if scenario != "absent" {
				stage, err := OpenSourceStage()
				if err != nil {
					t.Fatal(err)
				}
				defer stage.Close()
				if scenario != "busy" {
					if err := stage.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			path := filepath.Join(DefaultJournalDirectory, nativePrerequisiteName)
			record := testPrerequisiteRecord()
			switch scenario {
			case "missing-lock":
				if err := os.Remove(filepath.Join(DefaultJournalDirectory, "install.lock")); err != nil {
					t.Fatal(err)
				}
			case "unsafe-lock":
				if err := os.Chmod(filepath.Join(DefaultJournalDirectory, "install.lock"), 0666); err != nil {
					t.Fatal(err)
				}
			case "checking", "complete", "malformed", "duplicate", "unknown", "hardlink":
				if scenario == "complete" {
					record.Phase = "complete"
					after := record.Before
					record.After = &after
				}
				data := nativeBootJSON(record)
				if scenario == "malformed" {
					data = []byte("{\"secret\":")
				}
				if scenario == "duplicate" {
					data = append([]byte("{\"schemaVersion\":1,"), data[1:]...)
				}
				if scenario == "unknown" {
					data = append([]byte("{\"secret\":true,"), data[1:]...)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				if scenario == "hardlink" {
					if err := os.Link(path, path+".link"); err != nil {
						t.Fatal(err)
					}
				}
			case "symlink":
				if err := os.Symlink("secret-missing-target", path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "orphan-runtime":
				if err := os.WriteFile(filepath.Join(DefaultJournalDirectory, nativeRuntimeName), []byte("secret-partial"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := recoveryFixtureSnapshot(t, fixture)
			status, inspectErr := ManageNativeInstallation(t.Context(), NativeOperatorRequest{Action: "status"})
			plan := AssessNativeRecovery(status, inspectErr)
			want := "inspection-unavailable"
			switch scenario {
			case "absent", "empty":
				want = "no-native-state"
			case "checking":
				want = "prerequisite-review"
			case "complete":
				want = "preparation-incomplete"
			}
			if plan.Classification != want || (inspectErr == nil) != plan.InspectionComplete {
				t.Fatal(scenario, plan, inspectErr)
			}
			if !bytes.Equal(before, recoveryFixtureSnapshot(t, fixture)) {
				t.Fatal("inspection modified evidence or created state")
			}
			if strings.Contains(string(nativeBootJSON(plan)), "secret") {
				t.Fatal("untrusted content exported")
			}
			if scenario == "absent" {
				if _, err := os.Lstat(DefaultJournalDirectory); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("inspection created journal directory")
				}
			}
		})
	}
	t.Log("NATIVE_RECOVERY locked inspection: absence, contention, permissions, incomplete receipts and unsafe records preserved; no recovery actions")
}

// No opening links/FIFOs. Snapshot ownership, mode, identity and regular bytes;
// reading may affect atime, but no evidence files or contents may change.
func recoveryFixtureSnapshot(t *testing.T, root string) []byte {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil {
			return err
		}
		value := fmt.Sprintf("%d:%d:%d:%d:%d:%d", stat.Ino, stat.Mode, stat.Uid, stat.Gid, stat.Size, stat.Nlink)
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += ":" + admissionDigest(data)
		} else if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + target
		}
		snapshot[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return nativeBootJSON(snapshot)
}
