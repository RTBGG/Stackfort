// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
)

// Interrupt only a revalidation of an already complete package journal; never
// kill apt/dpkg during first installation. No test callback runs in the service.
func TestDisposableNativeRuntimeInterrupt(t *testing.T) {
	requireNativeLab(t)
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	if manifest.Runtime == nil {
		t.Fatal("sealed real-runtime profile required")
	}
	journalWaitInstallation(t)
	before := imageRead(t, installapply.DefaultJournalPath)
	var old installapply.AdmissionState
	if err := json.Unmarshal(imageRead(t, "/var/lib/stackfort-installer/installation-admission.json"), &old); err != nil || old.Phase != "admitted" {
		t.Fatal("completed admission required", err)
	}
	imageCommand(t, "/usr/bin/systemctl", "stop", nativeInstallUnit)
	if err := journalAdmissionGate(t).VerifyClosed(t.Context()); err != nil {
		t.Fatal(err)
	}
	imageCommand(t, "/usr/bin/systemctl", "start", "--no-block", nativeInstallUnit)
	deadline := time.Now().Add(30 * time.Second)
	paused := false
	for time.Now().Before(deadline) {
		var current installapply.AdmissionState
		data, err := os.ReadFile("/var/lib/stackfort-installer/installation-admission.json")
		if err == nil && json.Unmarshal(data, &current) == nil && current.Phase == "checking" && current.Attempt == old.Attempt+1 {
			imageCommand(t, "/usr/bin/systemctl", "kill", "--signal=SIGSTOP", "--kill-whom=main", nativeInstallUnit)
			paused = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !paused {
		t.Fatal("did not observe real runtime checking; no process killed")
	}
	imageCommand(t, "/usr/bin/systemctl", "kill", "--signal=SIGKILL", "--kill-whom=main", nativeInstallUnit)
	deadline = time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if imageCommand(t, "/usr/bin/systemctl", "show", "--property=ActiveState", "--value", nativeInstallUnit) == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	journalCheckQuarantine(t)
	if !bytes.Equal(before, imageRead(t, installapply.DefaultJournalPath)) {
		t.Fatal("read-only revalidation changed package journal")
	}
	var final installapply.AdmissionState
	if err := json.Unmarshal(imageRead(t, "/var/lib/stackfort-installer/installation-admission.json"), &final); err != nil || final.Phase != "checking" || final.Attempt != old.Attempt+1 {
		t.Fatal("missing interrupted checking evidence", err)
	}
	TestDisposableNativeAdmissionInspect(t)
	t.Log("NATIVE_RUNTIME SIGKILL of real dispatcher: independent real CLI quarantine closed web and stopped consumers; complete package journal unchanged; explicit reviewed recovery required")
}
