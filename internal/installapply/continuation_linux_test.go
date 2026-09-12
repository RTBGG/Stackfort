// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeInstallBindingRejectsAdoptionAndPartialState(t *testing.T) {
	for _, scenario := range []string{"valid", "unbound-journal", "partial-binding", "different-plan", "symlink", "hardlink", "unsafe-mode"} {
		t.Run(scenario, func(t *testing.T) {
			root := stageTestDirectory(t)
			stage := testSourceStage(t, root)
			plan := pinPlan(testSourcePin())
			path := filepath.Join(root, nativeInstallBindingName)
			switch scenario {
			case "unbound-journal":
				writeStageFixture(t, root, "install-state.json", []byte("{}"), 0600)
			case "partial-binding":
				writeStageFixture(t, root, nativeInstallBindingName, []byte("{"), 0600)
			case "symlink":
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			default:
				if err := sealInstallBinding(stage.dir, plan); err != nil {
					t.Fatal(err)
				}
				if scenario == "different-plan" {
					plan.OperationID = "30000000-0000-4000-8000-000000000098"
				}
				if scenario == "hardlink" {
					if err := os.Link(path, path+".link"); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "unsafe-mode" {
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, _ := os.ReadFile(path)
			err := sealInstallBinding(stage.dir, plan)
			if (scenario == "valid") != (err == nil) {
				t.Fatal(scenario, err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("existing binding rewritten")
			}
		})
	}
}

func TestNativeInstallStoreIsBoundCanonicalAndReopenable(t *testing.T) {
	root := stageTestDirectory(t)
	stage := testSourceStage(t, root)
	plan := pinPlan(testSourcePin())
	if err := sealInstallBinding(stage.dir, plan); err != nil {
		t.Fatal(err)
	}
	store := nativeInstallStore{stage: stage, files: &FileStore{directory: root, path: filepath.Join(root, "install-state.json")}, plan: plan}
	runner := newFakeRunner()
	engine, _ := NewEngine(store, runner)
	source := Source{Root: "/retained", Version: plan.Version, Digest: plan.SourceDigest}
	if _, err := engine.Install(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if result, err := engine.Install(t.Context(), source); err != nil || !result.AlreadyInstalled {
		t.Fatal(result, err)
	}
	journal, exists, err := store.Load()
	if err != nil || !exists {
		t.Fatal(err)
	}
	foreign := journal
	foreign.SourceDigest = "foreign"
	if store.Save(foreign) == nil {
		t.Fatal("foreign journal accepted")
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("closed stage accepted")
	}
	store.stage = testSourceStage(t, root)
	if _, exists, err := store.Load(); err != nil || !exists {
		t.Fatal("reopen", err)
	}
	data, _ := json.Marshal(journal)
	if err := os.WriteFile(store.files.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("noncanonical journal accepted")
	}
}
