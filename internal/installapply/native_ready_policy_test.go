// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

type testNativeReadyCompletion struct {
	Operation string `json:"operation"`
	Manifest  string `json:"manifest"`
	BootID    string `json:"bootID"`
	Status    string `json:"status"`
}

func nativeReadyPolicyFixture() (storageprep.State, NativeRuntimeIntent, testNativeReadyCompletion) {
	plan := pinPlan(testSourcePin())
	state := storageprep.State{SchemaVersion: 1, Plan: plan, Phase: storageprep.Ready, ArmAttempts: 1, ResumeBootID: "22222222-2222-4222-8222-222222222222"}
	return state, testBootIntent(), testNativeReadyCompletion{plan.OperationID, plan.ManifestDigest, state.ResumeBootID, "converted"}
}

func TestNativeReadyCompletionRequiresExactPastSuccess(t *testing.T) {
	state, intent, proof := nativeReadyPolicyFixture()
	good := nativeBootJSON(proof)
	if allowed, err := nativeReadyCompletionAccepted(state, state.Plan, intent, good); err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.RecoveryRequired} {
		next := state
		next.Phase = phase
		if phase == storageprep.Planned {
			next.ArmAttempts = 0
		}
		if phase != storageprep.Verifying {
			next.ResumeBootID = ""
		}
		if phase == storageprep.RecoveryRequired {
			next.FailureCode = "interrupted-arm"
		}
		if allowed, err := nativeReadyCompletionAccepted(next, next.Plan, intent, good); err != nil || allowed {
			t.Fatal("receipt relaxed pre-ready or recovery state", phase, allowed, err)
		}
	}
	for _, scenario := range []string{"missing", "operation", "manifest", "boot", "status", "unknown", "duplicate", "noncanonical", "state-plan", "schema", "invalid-ready", "invalid-intent"} {
		t.Run(scenario, func(t *testing.T) {
			next, runtime, receipt := nativeReadyPolicyFixture()
			plan := next.Plan
			switch scenario {
			case "operation":
				receipt.Operation = next.ResumeBootID
			case "manifest":
				receipt.Manifest = strings.Repeat("f", 64)
			case "boot":
				receipt.BootID = plan.PreviousBootID
			case "status":
				receipt.Status = "ready"
			case "state-plan":
				plan.Kernel += "-other"
			case "schema":
				next.SchemaVersion++
			case "invalid-ready":
				next.ArmAttempts = 0
			case "invalid-intent":
				runtime.InstallerSHA256 = "bad"
			}
			data := nativeBootJSON(receipt)
			switch scenario {
			case "missing":
				data = nil
			case "unknown":
				data = []byte(strings.Replace(string(data), "{\n", "{\n  \"force\": true,\n", 1))
			case "duplicate":
				data = []byte(strings.Replace(string(data), "{\n", "{\n  \"status\": \"converted\",\n", 1))
			case "noncanonical":
				data = append(data, '\n')
			}
			if allowed, err := nativeReadyCompletionAccepted(next, plan, runtime, data); err == nil || allowed {
				t.Fatal("invalid completion authority accepted", allowed, err)
			}
		})
	}
	if allowed, err := nativeReadyCompletionAccepted(state, state.Plan, testRuntimeIntent(), good); err != nil || allowed {
		t.Fatal("historical post-ready-only fixture gained conversion authority", allowed, err)
	}
}

func TestNativeReadyArtifactPolicyDoesNotFreezeLegitimateOSUpdates(t *testing.T) {
	state, intent, _ := nativeReadyPolicyFixture()
	plan := state.Plan
	for _, completed := range []bool{false, true} {
		hashes, err := nativeReadyArtifactHashes(plan, intent.Ready, plan.Kernel, completed)
		if err != nil || len(hashes) != 4 || hashes["/etc/fstab"] != intent.Ready.FstabSHA256 {
			t.Fatal(hashes, err)
		}
		for _, path := range []string{"/boot/vmlinuz-" + plan.Kernel, "/boot/initrd.img-" + plan.Kernel, "/boot/grub/grub.cfg"} {
			if (hashes[path] == "") != completed {
				t.Fatal("conversion-era pins were relaxed at the wrong lifecycle stage", path)
			}
		}
		updated := "6.12.999+deb13-cloud-amd64"
		hashes, err = nativeReadyArtifactHashes(plan, intent.Ready, updated, completed)
		if (err == nil) != completed {
			t.Fatal("kernel update policy", completed, err)
		}
		if completed {
			if _, exists := hashes["/boot/vmlinuz-"+updated]; !exists {
				t.Fatal("current kernel not inspected")
			}
			if _, exists := hashes["/boot/vmlinuz-"+plan.Kernel]; exists {
				t.Fatal("removed historical kernel remains required")
			}
		}
		for _, bad := range []string{"", "../vmlinuz", "6.12/a", "6.12;echo", "6.12\nother", "6.12 other", strings.Repeat("6", 129)} {
			if _, err := nativeReadyArtifactHashes(plan, intent.Ready, bad, completed); err == nil {
				t.Fatal("unsafe current kernel accepted", bad)
			}
		}
	}
	bad := intent.Ready
	bad.FstabSHA256 = ""
	if _, err := nativeReadyArtifactHashes(plan, bad, plan.Kernel, true); err == nil {
		t.Fatal("post-ready fstab integrity dropped")
	}
}
