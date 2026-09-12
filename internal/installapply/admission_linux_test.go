// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdmissionStoreRejectsMalformedLinkedAndForeignState(t *testing.T) {
	for _, scenario := range []string{"valid", "truncated", "duplicate", "mode", "hardlink", "symlink", "fifo", "foreign-plan", "unbound-package"} {
		t.Run(scenario, func(t *testing.T) {
			root := stageTestDirectory(t)
			stage := testSourceStage(t, root)
			store := nativeAdmissionStore{stage}
			state := AdmissionState{SchemaVersion: 1, Plan: pinPlan(testSourcePin()), BootID: "30000000-0000-4000-8000-000000000099", Attempt: 1, Phase: "checking"}
			path := filepath.Join(root, admissionName)
			if scenario == "unbound-package" {
				writeStageFixture(t, root, "install-state.json", []byte("{}\n"), 0600)
				if store.Save(state) == nil {
					t.Fatal("adopted package journal")
				}
				return
			}
			if err := store.Save(state); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "truncated":
				writeStageFixture(t, root, admissionName, []byte("{"), 0600)
			case "duplicate":
				writeStageFixture(t, root, admissionName, append([]byte("{\"schemaVersion\":1,"), data[1:]...), 0600)
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "symlink", "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if scenario == "symlink" {
					if err := os.Symlink("missing", path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := exec.Command("/usr/bin/mkfifo", path).Run(); err != nil {
						t.Fatal(err)
					}
				}
			case "foreign-plan":
				state.Plan.OperationID = "30000000-0000-4000-8000-000000000098"
			}
			state.Phase = "admitted"
			err = store.Save(state)
			if (scenario == "valid") != (err == nil) {
				t.Fatal(scenario, err)
			}
			if scenario == "valid" {
				inspection, err := store.Load()
				if err != nil || inspection.State.Phase != "admitted" {
					t.Fatal(inspection, err)
				}
			} else if scenario != "fifo" && scenario != "symlink" {
				// A second read after rejected Save must return the same unsafe bytes.
				before, _ := os.ReadFile(path)
				_ = store.Save(state)
				after, _ := os.ReadFile(path)
				if !bytes.Equal(before, after) {
					t.Fatal("unsafe state repaired")
				}
			}
		})
	}
}

func TestAdmissionStoreRecoverySnapshotsBindExactPackageBytes(t *testing.T) {
	root := stageTestDirectory(t)
	stage := testSourceStage(t, root)
	store := nativeAdmissionStore{stage}
	state := AdmissionState{SchemaVersion: 1, Plan: pinPlan(testSourcePin()), BootID: "30000000-0000-4000-8000-000000000099", Attempt: 1, Phase: "checking"}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	missing, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	memory := &memoryStore{}
	engine, _ := NewEngine(memory, newFakeRunner())
	if _, err := engine.Install(t.Context(), Source{Root: "/test", Version: state.Plan.Version, Digest: state.Plan.SourceDigest}); err != nil {
		t.Fatal(err)
	}
	journal, _, _ := memory.Load()
	data, _ := json.MarshalIndent(journal, "", "  ")
	writeStageFixture(t, root, "install-state.json", append(data, '\n'), 0600)
	complete, err := store.Load()
	if err != nil || !complete.PackageComplete || complete.Review.PackageSHA256 == missing.Review.PackageSHA256 {
		t.Fatal("package snapshot", complete, err)
	}
	journal.Status = InstallApplying
	data, _ = json.MarshalIndent(journal, "", "  ")
	writeStageFixture(t, root, "install-state.json", append(data, '\n'), 0600)
	partial, err := store.Load()
	if err != nil || partial.PackageComplete || partial.Review.PackageSHA256 == complete.Review.PackageSHA256 {
		t.Fatal("partial snapshot", partial, err)
	}
	state.Phase = "recovery-required"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	state.Attempt++
	state.Phase = "checking"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("closed stage accepted")
	}
}

func TestAdmissionNFTPolicyRejectsEveryNonWhitespaceChange(t *testing.T) {
	gate := &LinuxAdmissionGate{operation: "30000000-0000-4000-8000-000000000001"}
	closed := gate.listing(true)
	if !sameNFT(closed, strings.ReplaceAll(closed, "\n", "  \t")) {
		t.Fatal("whitespace rejected")
	}
	for _, modified := range []string{gate.listing(false), strings.ReplaceAll(closed, "drop", "accept"), strings.ReplaceAll(closed, "-150", "0"), strings.ReplaceAll(closed, "8443", "22"), strings.ReplaceAll(closed, "!=", "=="), closed + "# extra"} {
		if sameNFT(closed, modified) {
			t.Fatal("policy modification accepted")
		}
	}
}

func TestDisposableAdmissionNFTNamespace(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires explicit disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	namespace, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	parent := os.Getenv("STACKFORT_ADMISSION_PARENT_NETNS")
	if parent == "" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--net", self, "-test.v", "-test.run=^TestDisposableAdmissionNFTNamespace$")
		command.Env = append(os.Environ(), "STACKFORT_ADMISSION_PARENT_NETNS="+namespace)
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if parent == namespace || !strings.HasPrefix(parent, "net:[") {
		t.Fatal("not isolated from parent network namespace")
	}
	gate, err := NewLinuxAdmissionGate("30000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for index := 0; index < 2; index++ {
		if err := gate.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if err := gate.Close(ctx); err != nil {
			t.Fatal("idempotent close", err)
		}
		if err := gate.VerifyClosed(ctx); err != nil {
			t.Fatal(err)
		}
		if err := gate.Open(ctx); err != nil {
			t.Fatal(err)
		}
		if err := gate.VerifyOpen(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := gate.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// Replay exactly the dedicated-table service start/reload/stop operations.
	// None may remove or weaken the independently owned closed admission gate.
	for iteration := 0; iteration < 2; iteration++ {
		if _, err := runAdmissionCommand(ctx, nftablesFile(), "/usr/sbin/nft", "-f", "-"); err != nil {
			t.Fatal("managed firewall start", err)
		}
		if err := gate.VerifyClosed(ctx); err != nil {
			t.Fatal("managed firewall start changed admission", err)
		}
		if _, err := runAdmissionCommand(ctx, "", "/usr/sbin/nft", "delete", "table", "inet", "stackfort"); err != nil {
			t.Fatal("managed firewall stop/reload", err)
		}
		if err := gate.VerifyClosed(ctx); err != nil {
			t.Fatal("managed firewall stop/reload changed admission", err)
		}
	}
	if _, err := runAdmissionCommand(ctx, "add rule inet "+admissionTable+" ingress counter\n", "/usr/sbin/nft", "-f", "-"); err != nil {
		t.Fatal(err)
	}
	before, err := runAdmissionCommand(ctx, "", "/usr/sbin/nft", "-s", "-n", "-y", "list", "table", "inet", admissionTable)
	if err != nil {
		t.Fatal(err)
	}
	if gate.Close(ctx) == nil || gate.Open(ctx) == nil || gate.VerifyClosed(ctx) == nil {
		t.Fatal("conflicting table accepted")
	}
	after, err := runAdmissionCommand(context.Background(), "", "/usr/sbin/nft", "-s", "-n", "-y", "list", "table", "inet", admissionTable)
	if err != nil || before != after {
		t.Fatal("foreign policy overwritten", err)
	}
	t.Log("ADMISSION_NFT real isolated create/close/open/idempotence/conflict tests pass")
}
