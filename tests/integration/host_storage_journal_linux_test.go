// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

// Exercise the actual fixed production paths without writing a journal into
// the VM's real /var/lib. A fresh private mount namespace hides it with a temp
// directory. Only this child sees the fixture; no daemon or disk conversion runs.
func TestDisposableNativeStorageJournalGate(t *testing.T) {
	requireNativeLab(t)
	if os.Getenv("STACKFORT_STORAGE_JOURNAL_CHILD") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private",
			executable, "-test.v", "-test.timeout=1m", "-test.run=^TestDisposableNativeStorageJournalGate$")
		command.Env = append(os.Environ(), "STACKFORT_STORAGE_JOURNAL_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("private namespace gate test: %v\n%s", err, output)
		}
		t.Logf("%s", output)
		return
	}
	// Prevent accidental invocation of the child in the host's mount namespace.
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == init {
		t.Fatal("journal fixture requires a separate mount namespace")
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount("/var/lib", 0)
	if err := storageprep.CheckInactive(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installapply.NewFileStore().Load(); err != nil {
		t.Fatalf("absent journal blocked old installer: %v", err)
	}
	if _, err := os.Lstat("/var/lib/stackfort-installer"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only gate created state")
	}
	store, err := storageprep.OpenFileStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if lock, err := installapply.NewFileStore().AcquireLock(); err == nil {
		_ = lock.Close()
		t.Fatal("production installer and storage preparation did not share the lock")
	}
	plan := storageprep.Plan{
		OperationID: "10000000-0000-4000-8000-000000000001", Version: "0.1.0-beta.3",
		SourceDigest: strings.Repeat("a", 64), Distribution: "debian",
		MachineID: "10000000-0000-4000-8000-000000000002", RootUUID: "10000000-0000-4000-8000-000000000003",
		PartitionUUID: "10000000-0000-4000-8000-000000000004", PreviousBootID: "10000000-0000-4000-8000-000000000005",
		Kernel: "6.12.107+deb13-cloud-amd64", ManifestDigest: strings.Repeat("b", 64),
	}
	state := storageprep.State{SchemaVersion: storageprep.SchemaVersion, Plan: plan, Phase: storageprep.Planned}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	// Each valid phase, including Ready, must stop current production entry points.
	for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.Ready, storageprep.RecoveryRequired} {
		state.Phase = phase
		if phase != storageprep.Planned {
			state.ArmAttempts = 1
		}
		if phase == storageprep.Verifying {
			state.ResumeBootID = "10000000-0000-4000-8000-000000000099"
		}
		if phase == storageprep.RecoveryRequired {
			state.FailureCode = "readiness-lost"
		}
		if err := store.Save(state); err != nil {
			t.Fatal(err)
		}
		if _, _, err := installapply.NewFileStore().Load(); !errors.Is(err, storageprep.ErrNotQualified) {
			t.Fatalf("installer accepted %s: %v", phase, err)
		}
		if _, err := installapply.NewLinuxRunner(io.Discard); !errors.Is(err, storageprep.ErrNotQualified) {
			t.Fatalf("runner accepted %s: %v", phase, err)
		}
		if _, err := installapply.NewLinuxUpdateRunner(io.Discard); !errors.Is(err, storageprep.ErrNotQualified) {
			t.Fatalf("updater accepted %s: %v", phase, err)
		}
	}
	// The bytes live only in the temporary mounted fixture, and use the same
	// canonical format as the production store. Malformed state also blocks.
	content, err := os.ReadFile(filepath.Join(fixture, "stackfort-installer", "storage-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted storageprep.State
	if err := json.Unmarshal(content, &persisted); err != nil || persisted != state {
		t.Fatal("fixture did not contain final journal")
	}
	if err := os.WriteFile(storageprep.JournalPath, []byte("{truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installapply.NewFileStore().Load(); err == nil {
		t.Fatal("corrupt journal allowed installation replay")
	}
	if _, err := installapply.NewLinuxUpdateRunner(io.Discard); err == nil {
		t.Fatal("corrupt journal allowed updater construction")
	}
}
