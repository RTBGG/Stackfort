// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
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

const originLab = "/var/tmp/stackfort-origin-lab"

func TestDisposableResumeOriginBinding(t *testing.T) {
	requireNativeLab(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("STACKFORT_ORIGIN_CHILD") != "1" {
		for _, scenario := range []string{"valid", "wrong-ref", "bad-signature", "wrong-extraction", "forged-verifier", "source-drift", "manifest-drift", "missing-evidence"} {
			t.Run(scenario, func(t *testing.T) {
				command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", executable, "-test.v", "-test.timeout=2m", "-test.run=^TestDisposableResumeOriginBinding$")
				command.Env = append(os.Environ(), "STACKFORT_ORIGIN_CHILD=1", "STACKFORT_ORIGIN_SCENARIO="+scenario)
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("origin namespace: %v\n%s", err, output)
				}
				t.Logf("%s", output)
			})
		}
		return
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == init {
		t.Fatal("origin fixture requires private mount namespace")
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount("/var/lib", 0)
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	scenario := os.Getenv("STACKFORT_ORIGIN_SCENARIO")
	sourceRoot := originLab + "/stackfort-0.1.0-beta.3-linux-amd64"
	if scenario == "wrong-extraction" || scenario == "forged-verifier" {
		copyRoot := t.TempDir()
		command := exec.CommandContext(t.Context(), "/usr/bin/cp", "-a", sourceRoot+"/.", copyRoot)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("copy test extraction: %v %s", err, output)
		}
		sourceRoot = copyRoot
		if scenario == "wrong-extraction" {
			data, err := os.ReadFile(filepath.Join(copyRoot, "README.md"))
			if err != nil || len(data) == 0 {
				t.Fatal("read nonempty extraction fixture", err)
			}
			// Keep the size unchanged so this exercises the content-hash gate.
			data[0] ^= 1
			if err := os.WriteFile(filepath.Join(copyRoot, "README.md"), data, 0600); err != nil {
				t.Fatal(err)
			}
		} else {
			data, err := os.ReadFile(filepath.Join(copyRoot, "bin/stackfort-installer"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(copyRoot, "bin/stackfort-gh"), data, 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	pin, err := stage.Prepare(t.Context(), sourceRoot, "30000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	policy := installapply.OriginPolicy{Class: "lab-candidate", Version: "0.1.0-beta.3", Commit: "5282946bec1f865de7222128a6a5d0d8a656f34c"}
	if scenario == "wrong-ref" {
		policy.Class = "tag-release"
	}
	bundle := originLab + "/attestations.jsonl"
	if scenario == "bad-signature" {
		data, err := os.ReadFile(bundle)
		if err != nil {
			t.Fatal(err)
		}
		// Change the signed DSSE payload while retaining a parseable bundle.
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		envelope := value["dsseEnvelope"].(map[string]any)
		payload := envelope["payload"].(string)
		envelope["payload"] = "A" + payload[1:]
		data, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		bundle = filepath.Join(t.TempDir(), "altered.jsonl")
		if err := os.WriteFile(bundle, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	binding, err := stage.BindOrigin(t.Context(), pin, policy, originLab+"/archive.tar.gz", bundle)
	if scenario == "wrong-ref" || scenario == "bad-signature" || scenario == "wrong-extraction" || scenario == "forged-verifier" {
		if err == nil {
			t.Fatal("invalid origin accepted")
		}
		if scenario == "forged-verifier" && !strings.Contains(err.Error(), "independent upstream pin") {
			t.Fatal("forged verifier did not reach the hash gate", err)
		}
		if scenario == "wrong-extraction" && !strings.Contains(err.Error(), "differs from attested archive") {
			t.Fatal("wrong extraction did not reach archive binding", err)
		}
		if (scenario == "wrong-ref" || scenario == "bad-signature") && (!strings.Contains(err.Error(), "release attestation rejected") || strings.Contains(err.Error(), "flags in the group")) {
			t.Fatal("signature rejection did not reach verification", err)
		}
		if _, err := os.Lstat(storageprep.JournalPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("origin rejection created storage journal")
		}
		if _, err := os.Lstat("/var/lib/stackfort-installer/resume-origin/receipt.json"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("origin rejection published success receipt")
		}
		t.Log("ORIGIN_BINDING rejected", scenario, "before journal/boot authorization")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	host := storageprep.Observation{MachineID: "30000000-0000-4000-8000-000000000002", RootUUID: "30000000-0000-4000-8000-000000000003", PartitionUUID: "30000000-0000-4000-8000-000000000004", BootID: "30000000-0000-4000-8000-000000000005", Kernel: "6.12.107+deb13-cloud-amd64"}
	manifest := installapply.NativeReleaseManifest{SchemaVersion: 1, Release: binding, Host: host, BootSHA256: strings.Repeat("a", 64)}
	plan, err := stage.SealManifest(t.Context(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := stage.SealManifest(t.Context(), manifest); err != nil || again != plan {
		t.Fatalf("manifest reseal: %v", err)
	}
	backend := &originProtocolBackend{host: host}
	if decision, err := stage.AdvanceManifest(t.Context(), manifest, backend); err != nil || !decision.Waiting || backend.arms != 1 {
		t.Fatalf("advance: %+v %v", decision, err)
	}
	if _, err := stage.SealManifest(t.Context(), manifest); err == nil {
		t.Fatal("armed journal reset by sealing")
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	stage, err = installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	switch scenario {
	case "source-drift":
		err = os.WriteFile("/var/lib/stackfort-installer/resume-source/source/README.md", []byte("changed after arming\n"), 0644)
	case "manifest-drift":
		err = os.WriteFile(installapply.NativeReleaseManifestPath, []byte("{"), 0600)
	case "missing-evidence":
		err = os.Remove("/var/lib/stackfort-installer/resume-origin/attestations.jsonl")
	}
	if err != nil {
		t.Fatal(err)
	}
	backend.host.BootID = "30000000-0000-4000-8000-000000000099"
	decision, err := stage.AdvanceManifest(t.Context(), manifest, backend)
	if scenario == "valid" {
		if err != nil || !decision.Ready || backend.resumes != 1 {
			t.Fatalf("resume: %+v %v", decision, err)
		}
		if _, err := installapply.NewLinuxRunner(io.Discard); !errors.Is(err, storageprep.ErrNotQualified) {
			t.Fatal("release binding bypassed production storage guard", err)
		}
		t.Logf("ORIGIN_BINDING signature/source/manifest pass; plan=%+v", plan)
	} else {
		if !errors.Is(err, storageprep.ErrRecoveryRequired) || decision.State.FailureCode != "release-invalid" || backend.resumes != 0 {
			t.Fatalf("unsafe resume: %+v %v", decision, err)
		}
		if _, err := stage.AdvanceManifest(t.Context(), manifest, backend); !errors.Is(err, storageprep.ErrRecoveryRequired) {
			t.Fatal("recovery retried")
		}
		t.Log("ORIGIN_BINDING terminal recovery before backend resume:", scenario)
	}
}

// Protocol-only seam; the separate boot test uses the actual Debian backend.
type originProtocolBackend struct {
	host          storageprep.Observation
	arms, resumes int
}

func (b *originProtocolBackend) Observe(context.Context) (storageprep.Observation, error) {
	return b.host, nil
}
func (b *originProtocolBackend) Arm(context.Context, storageprep.Plan) error { b.arms++; return nil }
func (b *originProtocolBackend) VerifyBoot(context.Context, storageprep.Plan, string) error {
	return nil
}
func (b *originProtocolBackend) Resume(context.Context, storageprep.Plan, string) error {
	b.resumes++
	return nil
}
func (b *originProtocolBackend) VerifyReady(context.Context, storageprep.Plan) error { return nil }
