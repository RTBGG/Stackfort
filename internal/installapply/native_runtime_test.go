// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func testRuntimeIntent() NativeRuntimeIntent {
	d := strings.Repeat("a", 64)
	return NativeRuntimeIntent{SchemaVersion: 1, Profile: "debian-native-post-ready-qualification-v1", InstallerSHA256: d, BootIntentSHA256: d,
		Ready: NativeReadySpec{Features: "has_journal ext_attr extent needs_recovery", Blocks: "100000", BlockSize: "4096", InodeSize: "256", FstabSHA256: d, KernelSHA256: d, InitrdSHA256: d, GRUBSHA256: d}}
}

func TestNativeRuntimeIntentBindsEveryArtifact(t *testing.T) {
	good := testRuntimeIntent()
	digest, err := good.Digest()
	if err != nil || len(digest) != 64 {
		t.Fatal(digest, err)
	}
	for _, edit := range []func(*NativeRuntimeIntent){
		func(i *NativeRuntimeIntent) { i.InstallerSHA256 = strings.Repeat("b", 64) },
		func(i *NativeRuntimeIntent) { i.BootIntentSHA256 = strings.Repeat("b", 64) },
		func(i *NativeRuntimeIntent) { i.Ready.FstabSHA256 = strings.Repeat("b", 64) },
		func(i *NativeRuntimeIntent) { i.Ready.KernelSHA256 = strings.Repeat("b", 64) },
		func(i *NativeRuntimeIntent) { i.Ready.InitrdSHA256 = strings.Repeat("b", 64) },
		func(i *NativeRuntimeIntent) { i.Ready.GRUBSHA256 = strings.Repeat("b", 64) },
		func(i *NativeRuntimeIntent) { i.Ready.Blocks = "100001" },
		func(i *NativeRuntimeIntent) { i.Ready.BlockSize = "8192" },
		func(i *NativeRuntimeIntent) { i.Ready.InodeSize = "512" },
		func(i *NativeRuntimeIntent) { i.Ready.Features += " metadata_csum" },
	} {
		next := good
		edit(&next)
		got, err := next.Digest()
		if err != nil || got == digest {
			t.Fatal("unbound runtime field", got, err)
		}
	}
}

func TestNativeRuntimeRejectsInvalidProfilesAndGeometry(t *testing.T) {
	for _, edit := range []func(*NativeRuntimeIntent){
		func(i *NativeRuntimeIntent) { i.SchemaVersion++ },
		func(i *NativeRuntimeIntent) { i.Profile = "production" },
		func(i *NativeRuntimeIntent) { i.InstallerSHA256 = "" },
		func(i *NativeRuntimeIntent) { i.Ready.Blocks = "0" },
		func(i *NativeRuntimeIntent) { i.Ready.BlockSize = "04096" },
		func(i *NativeRuntimeIntent) { i.Ready.InodeSize = "256\n" },
		func(i *NativeRuntimeIntent) { i.Ready.Features = "extent extent" },
		func(i *NativeRuntimeIntent) { i.Ready.Features = " extent" },
	} {
		next := testRuntimeIntent()
		edit(&next)
		if _, err := next.Digest(); err == nil {
			t.Fatal("invalid runtime accepted", next)
		}
	}
}

func TestNativeRuntimeUnitsUseOnlyFixedRealDispatcher(t *testing.T) {
	operation := testSourcePin().OperationID
	units, err := NativeRuntimeUnits(operation)
	if err != nil || len(units) != 4 {
		t.Fatal(units, err)
	}
	for name, unit := range units {
		for _, forbidden := range []string{"probe.test", "-test.", "Environment=", "/bin/sh", "Restart=", "quotaon", "tune2fs"} {
			if strings.Contains(unit, forbidden) {
				t.Fatal(name, forbidden)
			}
		}
		if name != "srv-hosting.mount" && !strings.Contains(unit, "ExecStart="+NativeRuntimePath+" native-service ") {
			t.Fatal(name)
		}
	}
	if !strings.Contains(units[NativeRuntimeInstallUnit], "ExecStopPost="+NativeRuntimePath+" native-service quarantine") || strings.Contains(units[NativeRuntimeInstallUnit], "After=nginx") {
		t.Fatal("missing independent cleanup or consumer stop deadlock")
	}
	if !strings.Contains(units[NativeRuntimeGateUnit], "Before=network-pre.target") || !strings.Contains(units[NativeRuntimeVerifyUnit], "Before=srv-hosting.mount") {
		t.Fatal("missing early closed gate or storage ordering")
	}
	for _, value := range []string{"", operation + "\nExecStart=/bin/true", strings.ToUpper(operation), "00000000-0000-0000-0000-000000000000"} {
		if value == operation {
			continue
		}
		if _, err := NativeRuntimeUnits(value); err == nil {
			t.Fatal("invalid operation accepted")
		}
	}
}

func TestNativeRuntimeCannotAdvanceUnreadyOrForeignState(t *testing.T) {
	plan := pinPlan(testSourcePin())
	for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.Ready, storageprep.RecoveryRequired} {
		state := storageprep.State{Plan: plan, Phase: phase}
		if (requireNativeRuntimeReady(state, true, plan) == nil) != (phase == storageprep.Ready) {
			t.Fatal(phase)
		}
		if requireNativeRuntimeReady(state, false, plan) == nil {
			t.Fatal("absent state")
		}
		state.Plan.SourceDigest = strings.Repeat("f", 64)
		if requireNativeRuntimeReady(state, true, plan) == nil {
			t.Fatal("foreign state")
		}
	}
}

func TestNativeReadinessFeatureDelta(t *testing.T) {
	for _, sample := range []struct {
		before, after string
		valid         bool
	}{
		{"extent needs_recovery", "extent project quota", true},
		{"extent", "extent project quota orphan_present", true},
		{"extent", "extent project quota encrypt", false},
		{"extent metadata_csum", "extent project quota", false},
	} {
		if (nativeFeatureDelta(sample.before, sample.after) == nil) != sample.valid {
			t.Fatal(sample)
		}
	}
}

func TestNativeServiceRequestHasNoActivationOrRepairAction(t *testing.T) {
	for _, action := range []string{"close", "verify-storage", "admit", "quarantine", "open", "prepare", "arm", "resume", "force", "reset", ""} {
		err := (NativeServiceRequest{Action: action, OperationID: testSourcePin().OperationID}).Validate()
		valid := action == "close" || action == "verify-storage" || action == "admit" || action == "quarantine"
		if (err == nil) != valid {
			t.Fatal(action, err)
		}
	}
}
