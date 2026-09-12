// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

func TestNativeHostKernelFileRejectsUnlistedPaths(t *testing.T) {
	for _, path := range []string{"", "/etc/shadow", "/proc/self/environ", "/proc/net/../sys/kernel/osrelease", t.TempDir(), "/sys/firmware/efi/efivars/SecureBoot-foreign"} {
		if data, err := nativeHostKernelFile(path); err == nil || data != nil {
			t.Fatal("unlisted host input accepted")
		}
	}
}

func TestDisposableNativeHostBoundaries(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires explicit disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_NATIVE_HOST_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--net", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeHostBoundaries$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_HOST_CHILD=1")
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
	if nativeHostEnvironment(t.Context()) == nil {
		t.Fatal("isolated namespace accepted as boot target")
	}
	listener, err := net.ListenPacket("udp4", "0.0.0.0:8443")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	data, err := nativeHostKernelFile("/proc/net/udp")
	if err != nil || nativeSocketConflicts(string(data), false) == nil {
		t.Fatal("actual UDP listener not detected", err)
	}
	for _, final := range []string{"complete", "recovery-required"} {
		t.Run(final, func(t *testing.T) {
			fixture := t.TempDir()
			if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer unix.Unmount("/var/lib", 0)
			_, _ = InspectNativeHost(t.Context())
			if _, err := os.Lstat(DefaultJournalDirectory); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("inspection created installer state")
			}
			stage, err := OpenSourceStage()
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			record := testPrerequisiteRecord()
			if err := stage.saveNativePrerequisites(record); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(storageprep.CheckInactive(), storageprep.ErrNotQualified) {
				t.Fatal("early prerequisite state failed to block public installer")
			}
			record.Phase = "applying"
			record.Planned = map[string]string{"quota": "4.09-1+b1"}
			if err := stage.saveNativePrerequisites(record); err != nil {
				t.Fatal(err)
			}
			changed := record
			changed.Planned = map[string]string{"quota": "4.10"}
			if stage.saveNativePrerequisites(changed) == nil {
				t.Fatal("changed transaction accepted")
			}
			record.Phase = final
			if final == "complete" {
				after := record.Before
				record.After = &after
			}
			if err := stage.saveNativePrerequisites(record); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(DefaultJournalDirectory + "/" + nativePrerequisiteName)
			if err != nil {
				t.Fatal(err)
			}
			record.Phase = "checking"
			record.After = nil
			if stage.saveNativePrerequisites(record) == nil {
				t.Fatal("terminal prerequisite state reset")
			}
			after, err := os.ReadFile(DefaultJournalDirectory + "/" + nativePrerequisiteName)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected reset changed receipt", err)
			}
			status, _, err := stage.recordedNative()
			if err != nil || status.Storage != nil || status.Prerequisites == nil || status.Prerequisites.Phase != final || status.LiveReadinessVerified || status.PublicResumeEnabled {
				t.Fatal("incorrect prerequisite-only status", status, err)
			}
		})
	}
	t.Log("NATIVE_HOST isolated environment/UDP rejection, read-only absence, durable prerequisite transitions, public gate and no-reset policy passed")
}

func TestNativePrerequisiteReceiptReadAndBootBinding(t *testing.T) {
	root := stageTestDirectory(t)
	stage := testSourceStage(t, root)
	record := testPrerequisiteRecord()
	manifest := NativeReleaseManifest{Release: ReleaseBinding{Source: testSourcePin()}, Host: record.Before.Host}
	record.ReleaseSHA256 = admissionDigest(nativeBootJSON(manifest.Release))
	after := record.Before
	record.After = &after
	record.Phase = "complete"
	intent := testBootIntent()
	intent.InstallerSHA256 = record.InstallerSHA256
	intent.Offline.PrerequisitesSHA256 = admissionDigest(nativeBootJSON(record))
	if err := checkNativePrerequisiteData(nativeBootJSON(record), manifest, intent); err != nil {
		t.Fatal(err)
	}
	writeStageFixture(t, root, nativePrerequisiteName, nativeBootJSON(record), 0600)
	if _, exists, err := stage.readNativePrerequisites(); err != nil || !exists {
		t.Fatal(exists, err)
	}
	if stage.saveNativePrerequisites(record) == nil {
		t.Fatal("unlocked receipt update")
	}
	for _, edit := range []func(*nativePrerequisiteRecord){
		func(r *nativePrerequisiteRecord) { r.Phase = "recovery-required"; r.After = nil },
		func(r *nativePrerequisiteRecord) { r.OperationID = "30000000-0000-4000-8000-000000000077" },
		func(r *nativePrerequisiteRecord) { r.InstallerSHA256 = strings.Repeat("e", 64) },
		func(r *nativePrerequisiteRecord) { r.Before.Host.Kernel = "other" },
	} {
		next := record
		edit(&next)
		if checkNativePrerequisiteData(nativeBootJSON(next), manifest, intent) == nil {
			t.Fatal("receipt drift accepted")
		}
	}
	if checkNativePrerequisiteData(append(nativeBootJSON(record), ' '), manifest, intent) == nil {
		t.Fatal("noncanonical receipt")
	}
	if err := os.Chmod(filepath.Join(root, nativePrerequisiteName), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := stage.readNativePrerequisites(); err == nil {
		t.Fatal("unsafe receipt metadata")
	}
}

func TestNativeHostContainerTreeAndOrphans(t *testing.T) {
	root := stageTestDirectory(t)
	if err := os.Mkdir(filepath.Join(root, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := nativeHostEmptyTree(root, 3); err != nil {
		t.Fatal(err)
	}
	writeStageFixture(t, root, "tmp/state", []byte("workload"), 0600)
	if nativeHostEmptyTree(root, 3) == nil {
		t.Fatal("container files accepted")
	}
	for _, name := range []string{nativePrerequisiteName, filepath.Base(NativePrerequisiteInstaller)} {
		root := stageTestDirectory(t)
		stage := testSourceStage(t, root)
		if err := os.Symlink("missing", filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
		if checkNativeOrphans(stage.dir) == nil {
			t.Fatal("orphan hidden")
		}
	}
	if CheckNativePrerequisiteTransaction(t.Context(), testSourcePin().OperationID, strings.NewReader("VERSION 2\n\n")) == nil {
		t.Fatal("ordinary executable acted as APT guard")
	}
}
