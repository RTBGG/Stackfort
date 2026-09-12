// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

func TestDisposableNativeRecoveryChoiceBoundaries(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_NATIVE_CHOICE_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeRecoveryChoiceBoundaries$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_CHOICE_CHILD=1")
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
	for _, scenario := range []string{"valid", "existing-storage", "existing-prerequisites", "missing-consent", "backup-consent", "symlink", "hardlink", "fifo", "directory", "mode", "truncated", "duplicate", "unknown", "cancelled-context"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := t.TempDir()
			if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer unix.Unmount("/var/lib", 0)
			stage, err := OpenSourceStage()
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			choice := testRecoveryChoice()
			path := filepath.Join(DefaultJournalDirectory, nativeRecoveryChoiceName)
			switch scenario {
			case "existing-storage":
				state := storageprep.State{SchemaVersion: 1, Plan: pinPlan(testSourcePin()), Phase: storageprep.Planned}
				if err := stage.journal.Save(state); err != nil {
					t.Fatal(err)
				}
			case "existing-prerequisites":
				if err := stage.saveNativePrerequisites(testPrerequisiteRecord()); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "hardlink", "mode", "truncated", "duplicate", "unknown":
				data := nativeBootJSON(choice)
				if scenario == "truncated" {
					data = []byte("{")
				}
				if scenario == "duplicate" {
					data = append([]byte("{\"schemaVersion\":1,"), data[1:]...)
				}
				if scenario == "unknown" {
					data = append([]byte("{\"force\":true,"), data[1:]...)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				if scenario == "hardlink" {
					if err := os.Link(path, path+".link"); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "mode" {
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := recoveryFixtureSnapshot(t, fixture)
			if scenario == "missing-consent" || scenario == "backup-consent" || scenario == "cancelled-context" {
				decision := NativeRecoveryDecision{}
				if scenario == "backup-consent" {
					decision = choice.Decision
					decision.Mode = "external-backup"
				}
				ctx := t.Context()
				if scenario == "cancelled-context" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
					decision = choice.Decision
				}
				if _, err := stage.PrepareNativeBoot(ctx, choice.Review.Release, "/does-not-exist", decision); err == nil {
					t.Fatal("unconfirmed preparation admitted")
				}
			} else {
				err := stage.writeNativeRecoveryChoice(choice)
				if (err == nil) != (scenario == "valid") {
					t.Fatal(scenario, err)
				}
			}
			if scenario != "valid" {
				if !bytes.Equal(before, recoveryFixtureSnapshot(t, fixture)) {
					t.Fatal("rejected choice changed evidence")
				}
				if scenario != "missing-consent" && scenario != "backup-consent" && scenario != "cancelled-context" && scenario != "existing-storage" && scenario != "existing-prerequisites" {
					if _, _, err := stage.readNativeRecoveryChoice(); err == nil {
						t.Fatal("unsafe receipt read")
					}
				}
				return
			}
			if !errors.Is(storageprep.CheckInactive(), storageprep.ErrNotQualified) {
				t.Fatal("choice failed to block public installation")
			}
			before = recoveryFixtureSnapshot(t, fixture)
			if stage.writeNativeRecoveryChoice(choice) == nil {
				t.Fatal("choice replay accepted")
			}
			if _, err := stage.PrepareNativeBoot(t.Context(), choice.Review.Release, "/does-not-exist", choice.Decision); err == nil {
				t.Fatal("preparation replay accepted")
			}
			if _, err := stage.ReviewNativeBootRecovery(t.Context(), choice.Review.Release, "/does-not-exist"); err == nil {
				t.Fatal("existing choice re-reviewed")
			}
			status, _, err := stage.recordedNative()
			if err != nil || status.RecoveryChoice == nil {
				t.Fatal("receipt-only handoff unavailable", err)
			}
			plan := AssessNativeRecovery(status, nil)
			if !plan.InspectionComplete || plan.Classification != "preparation-incomplete" || plan.BackupVerified || plan.DestructiveActionsAuthorized {
				t.Fatal(plan)
			}
			if !bytes.Equal(before, recoveryFixtureSnapshot(t, fixture)) {
				t.Fatal("replay/status mutated receipt")
			}
		})
	}
	t.Log("NATIVE_RECOVERY_CHOICE explicit decision gate, exclusive durable receipt, no replay, public block and safe handoff passed")
}

func TestNativeRecoveryChoiceRejectsPrerequisiteDriftAndDowngrade(t *testing.T) {
	choice, manifest, intent, record := testRecoveryBoot(t)
	if err := checkNativePrerequisiteData(nativeBootJSON(record), manifest, intent); err != nil {
		t.Fatal(err)
	}
	record.RecoveryChoiceSHA256 = ""
	intent.Offline.PrerequisitesSHA256 = admissionDigest(nativeBootJSON(record))
	if checkNativePrerequisiteData(nativeBootJSON(record), manifest, intent) == nil {
		t.Fatal("prerequisite downgrade accepted")
	}
	stage := testSourceStage(t, stageTestDirectory(t))
	if stage.writeNativeRecoveryChoice(choice) == nil {
		t.Fatal("unlocked receipt created")
	}
	if _, err := stage.ReviewNativeBootRecovery(t.Context(), choice.Review.Release, "/tmp/dispatcher"); err == nil {
		t.Fatal("unlocked review allowed")
	}
}
