// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
)

func requireNativeBootLab(t *testing.T) {
	t.Helper()
	host, err := os.Hostname()
	if err == nil && host == "stackfort-native-mbr-debian-13" {
		if os.Getenv(disposableHostOptIn) != "1" || os.Getenv("STACKFORT_NATIVE_QUOTA_PROTOTYPE") != "1" {
			t.Skip("dedicated native MBR lab only")
		}
		dmi, err := os.ReadFile("/sys/class/dmi/id/product_uuid")
		if err != nil || os.Geteuid() != 0 || strings.TrimSpace(string(dmi)) != "91d127c9-40e6-ab48-9528-5085e8888729" {
			t.Fatal("wrong disposable BIOS/MBR VM identity")
		}
		if _, err := os.Stat("/sys/firmware/efi"); !os.IsNotExist(err) {
			t.Fatal("MBR fixture must boot using BIOS")
		}
		return
	}
	requireNativeLab(t)
}

// A separate storage/failure-containment test, NOT successful installation.
// The retained beta.3 WAF package requires an older NGINX than today's APT
// repository. Never downgrade NGINX or relax that dependency to pass this lab.
func TestDisposableNativeMBRStorageAfterPackageFailure(t *testing.T) {
	requireNativeBootLab(t)
	host, _ := os.Hostname()
	if host != "stackfort-native-mbr-debian-13" {
		t.Fatal("MBR failure fixture only")
	}
	_, plan := bootManifest(t)
	if plan.PartitionUUID != "7c92ab10-01" {
		t.Fatal("wrong MBR partition")
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		properties := imageCommand(t, "/usr/bin/systemctl", "show", "--property=ActiveState", "--property=Job", installapply.NativeRuntimeInstallUnit)
		state, waiting := nativeBootPendingUnit(properties)
		if !waiting {
			if state != "failed" {
				t.Fatal("expected preserved package failure", state)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("install failure did not settle")
		}
		time.Sleep(time.Second)
	}
	state, err := storageprep.DecodeState(imageRead(t, storageprep.JournalPath))
	if err != nil || state.Phase != storageprep.Ready || state.Plan != plan || state.ArmAttempts != 1 {
		t.Fatal("storage not ready from exactly one conversion", state, err)
	}
	var admission installapply.AdmissionState
	if err := json.Unmarshal(imageRead(t, installapply.DefaultJournalDirectory+"/installation-admission.json"), &admission); err != nil ||
		admission.Phase != "recovery-required" || admission.Plan != plan || admission.Attempt != 1 || admission.BootID != state.ResumeBootID {
		t.Fatal("failed installation was replayed or admitted", admission, err)
	}
	var journal installapply.Journal
	if err := json.Unmarshal(imageRead(t, installapply.DefaultJournalPath), &journal); err != nil || journal.Status != installapply.InstallFailed {
		t.Fatal("missing failed installation journal", err)
	}
	failedWAF := false
	for _, stage := range journal.Stages {
		if stage.ID == installapply.StageWAFPackage {
			failedWAF = stage.Status == installapply.StageFailed && stage.Attempts == 1
		}
	}
	if !failedWAF {
		t.Fatal("expected the original WAF dependency failure without retry")
	}
	gate, err := installapply.NewLinuxAdmissionGate(plan.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.VerifyClosed(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{installapply.NativeBootFinalizeUnit, installapply.NativeRuntimeVerifyUnit, "srv-hosting.mount"} {
		imageCommand(t, "/usr/bin/systemctl", "is-active", unit)
	}
	normal := os.Getenv("STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT") == "1"
	if normal {
		if strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/random/boot_id"))) == state.ResumeBootID ||
			strings.Contains(string(imageRead(t, "/proc/cmdline")), "stackfort.native-quota=") {
			t.Fatal("normal boot identity or conversion token")
		}
		if _, err := os.Stat("/run/initramfs/stackfort-native-boot-proof.json"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("normal boot reran conversion helper", err)
		}
	} else if !strings.Contains(string(imageRead(t, "/run/initramfs/stackfort-native-quota.log")), "NATIVE_BOOT converted") {
		t.Fatal("real offline conversion missing")
	}
	t.Run("QuotasAndIsolation", TestDisposableHostProjectQuotaAndAccountIsolation)
	if err := gate.VerifyClosed(t.Context()); err != nil {
		t.Fatal("quota probes opened web admission", err)
	}
	t.Logf("MBR_STORAGE ready=true quota-isolation=true normal=%t admission=recovery-required package-install=FAILED (not release qualification)", normal)
}
