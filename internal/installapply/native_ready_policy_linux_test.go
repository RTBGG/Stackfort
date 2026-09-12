// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

func TestNativeReadyPolicyCannotSelfAssertCompletion(t *testing.T) {
	state, intent, proof := nativeReadyPolicyFixture()
	b := nativeReadyBackend{plan: state.Plan, spec: intent.Ready, intent: intent}
	if allowed, err := b.postConversionReady(t.Context()); err != nil || allowed {
		t.Fatal("unbound backend asserted completed conversion", allowed, err)
	}
	if string(nativeBootJSON(proof)) != string(nativeBootJSON(nativeBootProof{Operation: proof.Operation, Manifest: proof.Manifest, BootID: proof.BootID, Status: proof.Status})) {
		t.Fatal("completion policy differs from real conversion receipt format")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if allowed, err := b.postConversionReady(ctx); err == nil || allowed {
			t.Fatal("inactive context accepted")
		}
	}
	b.sourceStage = &SourceStage{closed: true}
	if allowed, err := b.postConversionReady(t.Context()); err == nil || allowed {
		t.Fatal("closed source stage accepted")
	}
	b.sourceStage = &SourceStage{}
	if allowed, err := b.postConversionReady(t.Context()); err == nil || allowed {
		t.Fatal("unlocked source stage accepted")
	}
}

func TestDisposableNativeReadyPolicyBoundaries(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_NATIVE_READY_POLICY_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeReadyPolicyBoundaries$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_READY_POLICY_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	nativePackageTestNamespace(t)
	t.Run("journal-and-source-required", func(t *testing.T) {
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
		state, intent, proof := nativeReadyPolicyFixture()
		b := nativeReadyBackend{plan: state.Plan, spec: intent.Ready, intent: intent, sourceStage: stage}
		if allowed, err := b.postConversionReady(t.Context()); err == nil || allowed {
			t.Fatal("missing journal accepted")
		}
		if err := writeOriginRecord(stage.dir, "native-boot-completed.json", nativeBootJSON(proof)); err != nil {
			t.Fatal(err)
		}
		for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.Ready} {
			next := state
			next.Phase = phase
			if phase == storageprep.Planned {
				next.ArmAttempts = 0
			}
			if phase != storageprep.Verifying && phase != storageprep.Ready {
				next.ResumeBootID = ""
			}
			if err := stage.journal.Save(next); err != nil {
				t.Fatal(err)
			}
			allowed, err := b.postConversionReady(t.Context())
			if allowed || (phase == storageprep.Ready && err == nil) || (phase != storageprep.Ready && err != nil) {
				t.Fatal("receipt+ready flag bypassed immutable manifest/source authority", phase, allowed, err)
			}
		}
		b.plan.Kernel += "-other"
		if allowed, err := b.postConversionReady(t.Context()); err == nil || allowed {
			t.Fatal("backend plan differs from locked journal")
		}
		b.plan = state.Plan
		if err := stage.journal.Close(); err != nil {
			t.Fatal(err)
		}
		if allowed, err := b.postConversionReady(t.Context()); err == nil || allowed {
			t.Fatal("released journal lock retained completion authority")
		}
	})
	for _, scenario := range []string{"valid", "missing", "empty", "symlink", "hardlink", "fifo", "directory", "writable", "foreign-owner", "oversized", "cancelled-context"} {
		t.Run("current-artifact-"+scenario, func(t *testing.T) {
			fixture := t.TempDir()
			if err := unix.Mount(fixture, "/boot", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer unix.Unmount("/boot", 0)
			path := "/boot/current-artifact"
			var err error
			switch scenario {
			case "missing":
			case "symlink":
				err = os.Symlink("missing", path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			default:
				err = os.WriteFile(path, []byte("synthetic ordinary OS artifact, never executed"), 0644)
				if err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "empty":
					err = os.Truncate(path, 0)
				case "hardlink":
					err = os.Link(path, path+".link")
				case "writable":
					err = os.Chmod(path, 0664)
				case "foreign-owner":
					err = os.Chown(path, 65534, 65534)
				case "oversized":
					err = os.Truncate(path, (512<<20)+1)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if scenario == "cancelled-context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = nativeReadyCurrentArtifact(ctx, path)
			if (err == nil) != (scenario == "valid") {
				t.Fatal(scenario, err)
			}
		})
	}
}
