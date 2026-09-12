// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func TestNativeDispatcherStagingHasSeparateBinaryBoundAndNeverOverwrites(t *testing.T) {
	root := stageTestDirectory(t)
	stage := testSourceStage(t, root)
	content := bytes.Repeat([]byte{1}, 128<<10)
	if err := writeNativeDispatcher(stage.dir, content); err != nil {
		t.Fatal(err)
	}
	intent := testRuntimeIntent()
	intent.InstallerSHA256 = admissionDigest(content)
	if err := stage.verifyNativeDispatcher(t.Context(), intent, false); err != nil {
		t.Fatal(err)
	}
	if writeNativeDispatcher(stage.dir, []byte("replacement")) == nil {
		t.Fatal("existing binary replaced")
	}
	if err := stage.verifyNativeDispatcher(t.Context(), intent, false); err != nil {
		t.Fatal(err)
	}
	if writeNativeDispatcher(stage.dir, nil) == nil {
		t.Fatal("empty binary accepted")
	}
}

func TestNativeReadyBackendNeverMutatesPreparation(t *testing.T) {
	b := nativeReadyBackend{}
	p := pinPlan(testSourcePin())
	for _, err := range []error{b.Arm(t.Context(), p), b.VerifyBoot(t.Context(), p, p.PreviousBootID), b.Resume(t.Context(), p, p.PreviousBootID)} {
		if !errors.Is(err, storageprep.ErrNotQualified) {
			t.Fatal("offline operation allowed", err)
		}
	}
	if b.VerifyReady(t.Context(), p) == nil {
		t.Fatal("unbound readiness allowed")
	}
}

func TestNativeDispatcherRejectsDriftAndUnsafeFiles(t *testing.T) {
	for _, scenario := range []string{"valid", "changed", "mode", "symlink", "hardlink", "running-path"} {
		t.Run(scenario, func(t *testing.T) {
			root := stageTestDirectory(t)
			stage := testSourceStage(t, root)
			content := []byte("synthetic, never executed")
			intent := testRuntimeIntent()
			intent.InstallerSHA256 = admissionDigest(content)
			path := filepath.Join(root, filepath.Base(NativeRuntimePath))
			if scenario == "symlink" {
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			} else {
				if scenario == "changed" {
					content = []byte("changed")
				}
				writeStageFixture(t, root, filepath.Base(path), content, 0500)
				if scenario == "mode" {
					if err := os.Chmod(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "hardlink" {
					if err := os.Link(path, path+".link"); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := stage.verifyNativeDispatcher(t.Context(), intent, scenario == "running-path")
			if (err == nil) != (scenario == "valid") {
				t.Fatal(scenario, err)
			}
		})
	}
}

func TestNativeRuntimeSealRequiresSharedJournalLock(t *testing.T) {
	stage := testSourceStage(t, stageTestDirectory(t))
	if stage.SealNativeRuntime(t.Context(), NativeReleaseManifest{}, testRuntimeIntent(), "/tmp/untrusted") == nil {
		t.Fatal("unlocked runtime staged")
	}
}

func TestNativeOperatorRejectsOrphanRuntimeWithoutReadingBinaryAsJSON(t *testing.T) {
	for _, name := range []string{nativeRuntimeName, filepath.Base(NativeRuntimePath)} {
		root := stageTestDirectory(t)
		stage := testSourceStage(t, root)
		if err := checkNativeOrphans(stage.dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("missing", filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
		if checkNativeOrphans(stage.dir) == nil {
			t.Fatal("orphan runtime hidden from operator")
		}
	}
}
