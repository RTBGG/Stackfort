// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"github.com/google/uuid"
)

// The fixture only authenticates input and invokes the regular preparation API.
// No testing executable, test backend or fault switch enters the boot path.
func TestDisposableNativeBootPrepare(t *testing.T) {
	requireNativeLab(t)
	if os.Getenv("STACKFORT_NATIVE_DISPOSABLE_RECOVERY_ACCEPTED") != "1" {
		t.Fatal("preparation requires explicit disposable-fresh-server recovery-risk acceptance")
	}
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	pin, err := stage.Prepare(t.Context(), "/var/tmp/stackfort-origin-lab/stackfort-0.1.0-beta.3-linux-amd64", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	binding, err := stage.BindOrigin(t.Context(), pin, installapply.OriginPolicy{Class: "lab-candidate", Version: "0.1.0-beta.3", Commit: "5282946bec1f865de7222128a6a5d0d8a656f34c"}, "/var/tmp/stackfort-origin-lab/archive.tar.gz", "/var/tmp/stackfort-origin-lab/attestations.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	review, err := stage.ReviewNativeBootRecovery(t.Context(), binding, "/var/lib/stackfort-boot-qualification/native-installer")
	if err != nil {
		t.Fatal(err)
	}
	reviewDigest, err := review.Digest()
	if err != nil {
		t.Fatal(err)
	}
	decision := installapply.NativeRecoveryDecision{ReviewedSHA256: reviewDigest, Mode: installapply.NativeRecoveryFreshDisposable,
		NoDataToRetain: true, AcceptProviderReinstallationRisk: true}
	manifest, err := stage.PrepareNativeBoot(t.Context(), binding, "/var/lib/stackfort-boot-qualification/native-installer", decision)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := manifest.Plan()
	if err != nil {
		t.Fatal(err)
	}
	var prerequisites struct {
		Phase   string            `json:"phase"`
		Planned map[string]string `json:"planned"`
	}
	if err := json.Unmarshal(imageRead(t, installapply.DefaultJournalDirectory+"/native-prerequisites.json"), &prerequisites); err != nil || prerequisites.Phase != "complete" {
		t.Fatal("missing completed prerequisite receipt", err)
	}
	var intent installapply.NativeRuntimeIntent
	if err := json.Unmarshal(imageRead(t, installapply.DefaultJournalDirectory+"/native-runtime-intent.json"), &intent); err != nil || intent.Offline == nil || len(intent.Offline.PrerequisitesSHA256) != 64 || len(intent.Offline.RecoveryChoiceSHA256) != 64 {
		t.Fatal("unbound prerequisites", err)
	}
	choiceBytes := imageRead(t, installapply.DefaultJournalDirectory+"/native-recovery-choice.json")
	if got := fmt.Sprintf("%x", sha256.Sum256(choiceBytes)); got != intent.Offline.RecoveryChoiceSHA256 {
		t.Fatal("unbound recovery decision")
	}
	t.Log("NATIVE_RECOVERY explicit disposable-host decision bound to reviewed host, prerequisites and boot intent")
	if _, err := os.Lstat("/usr/sbin/policy-rc.d"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prerequisite policy leaked", err)
	}
	t.Logf("NATIVE_HOST prerequisite receipt complete, exact new packages: %v; bound into offline intent", prerequisites.Planned)
	if _, err := os.Lstat("/var/tmp/stackfort-origin-retired"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("existing retired fixture")
	}
	if err := os.Rename("/var/tmp/stackfort-origin-lab", "/var/tmp/stackfort-origin-retired"); err != nil {
		t.Fatal(err)
	}
	t.Log("NATIVE_BOOT prepared by installer API; original input retired; operation=" + plan.OperationID)
}

func bootManifest(t *testing.T) (installapply.NativeReleaseManifest, storageprep.Plan) {
	t.Helper()
	var manifest installapply.NativeReleaseManifest
	if err := json.Unmarshal(imageRead(t, installapply.NativeReleaseManifestPath), &manifest); err != nil {
		t.Fatal(err)
	}
	plan, err := manifest.Plan()
	if err != nil {
		t.Fatal(err)
	}
	return manifest, plan
}

func TestDisposableNativeBootArm(t *testing.T) {
	requireNativeLab(t)
	_, plan := bootManifest(t)
	t.Log(imageCommand(t, installapply.NativeRuntimePath, "native-boot", "arm", "--operation-id="+plan.OperationID))
	state, err := storageprep.DecodeState(imageRead(t, storageprep.JournalPath))
	if err != nil || state.Phase != storageprep.AwaitingReboot {
		t.Fatal(state, err)
	}
	before := imageRead(t, storageprep.JournalPath)
	t.Log(imageCommand(t, installapply.NativeRuntimePath, "native-boot", "arm", "--operation-id="+plan.OperationID))
	if !bytes.Equal(before, imageRead(t, storageprep.JournalPath)) {
		t.Fatal("repeated arm mutated journal")
	}
	id, _ := storageprep.GRUBEntryID(plan)
	listing := imageCommand(t, "/usr/bin/lsinitramfs", "/boot/"+id+".img")
	if strings.Contains(listing, ".test") || !strings.Contains(listing, "stackfort-native-installer") {
		t.Fatal("boot path uses test binary or lacks installer")
	}
	t.Log("NATIVE_BOOT armed real installer only; repeated request made no journal transition")
}

func TestDisposableNativeBootValidate(t *testing.T) {
	requireNativeLab(t)
	manifest, plan := bootManifest(t)
	deadline := time.Now().Add(8 * time.Minute)
	for {
		properties := imageCommand(t, "/usr/bin/systemctl", "show", "--property=ActiveState", "--property=Job", installapply.NativeRuntimeInstallUnit)
		state, waiting := nativeBootPendingUnit(properties)
		if state == "active" {
			break
		}
		if !waiting || time.Now().After(deadline) {
			t.Fatal("installer did not complete", state)
		}
		t.Log("NATIVE_BOOT waiting for actual installer package/service stages")
		time.Sleep(5 * time.Second)
	}
	state, err := storageprep.DecodeState(imageRead(t, storageprep.JournalPath))
	if err != nil || state.Phase != storageprep.Ready || state.Plan != plan {
		t.Fatal(state, err)
	}
	var journal installapply.Journal
	if err := json.Unmarshal(imageRead(t, installapply.DefaultJournalPath), &journal); err != nil {
		t.Fatal(err)
	}
	if journal.Status != installapply.InstallComplete || journal.Version != plan.Version || journal.SourceDigest != plan.SourceDigest || len(journal.Stages) != 9 {
		t.Fatal("invalid package completion")
	}
	for _, stage := range journal.Stages {
		if stage.Status != installapply.StageComplete {
			t.Fatal(stage)
		}
	}
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	_, verifyErr := stage.VerifyManifest(t.Context(), manifest)
	_ = stage.Close()
	if verifyErr != nil {
		t.Fatal(verifyErr)
	}
	gate, err := installapply.NewLinuxAdmissionGate(plan.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.VerifyOpen(t.Context()); err != nil {
		t.Fatal(err)
	}
	units, err := installapply.NativeBootUnits(plan.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range units {
		if string(imageRead(t, "/etc/systemd/system/"+name)) != expected {
			t.Fatal("boot unit drift", name)
		}
		imageCommand(t, "/usr/bin/systemctl", "is-active", name)
	}
	log := imageCommand(t, "/usr/bin/journalctl", "-b", "-u", installapply.NativeBootFinalizeUnit, "-u", installapply.NativeRuntimeVerifyUnit, "-u", installapply.NativeRuntimeInstallUnit, "--no-pager", "-o", "cat")
	if strings.Contains(log, "=== RUN") || strings.Contains(log, ".test") {
		t.Fatal("test program executed during boot")
	}
	var result installapply.Result
	for _, line := range strings.Split(log, "\n") {
		if data, ok := strings.CutPrefix(line, installapply.NativeRuntimeResultPrefix); ok {
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				t.Fatal(err)
			}
		}
	}
	if result.Status != installapply.InstallComplete {
		t.Fatal("missing runtime result")
	}
	normal := os.Getenv("STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT") == "1"
	if normal && (!result.AlreadyInstalled || result.Changed) {
		t.Fatal("normal boot reapplied installation")
	}
	if normal {
		if strings.Contains(string(imageRead(t, "/proc/cmdline")), "stackfort.native-quota=") {
			t.Fatal("normal boot reused conversion entry")
		}
		if _, err := os.Stat("/run/initramfs/stackfort-native-boot-proof.json"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("offline helper ran on normal boot")
		}
	} else if !strings.Contains(string(imageRead(t, "/run/initramfs/stackfort-native-quota.log")), "NATIVE_BOOT converted") {
		t.Fatal("real offline conversion missing")
	}
	for _, path := range []string{nativeLab, nativeHook, nativePremount, "/boot/grub/custom.cfg", "/etc/systemd/system/" + nativeResume} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("legacy preparation artifact present", path)
		}
	}
	for _, test := range []struct {
		name string
		fn   func(*testing.T)
	}{{"QuotasAndIsolation", TestDisposableHostProjectQuotaAndAccountIsolation}, {"OCIPrivateResources", TestDisposableHostOCIPrivateResources}, {"OCILifecycle", TestDisposableHostOCIDeploymentLifecycle}, {"ContainerSubUIDQuota", testContainerProjectQuota}} {
		t.Run(test.name, test.fn)
	}
	t.Logf("NATIVE_BOOT all-real preparation/finalization/admission passed; normal=%t alreadyInstalled=%t changed=%t", normal, result.AlreadyInstalled, result.Changed)
}

// SSH can be ready while this unit's start job waits on finalization/mount.
// Inactive without a queued job is still a failure, never assumed progress.
func nativeBootPendingUnit(properties string) (string, bool) {
	values := map[string]string{}
	for _, line := range strings.Split(properties, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	state := values["ActiveState"]
	if state == "activating" {
		return state, true
	}
	job := strings.Fields(values["Job"])
	if state == "inactive" && len(job) > 0 {
		id, err := strconv.ParseUint(job[0], 10, 32)
		return state, err == nil && id > 0
	}
	return state, false
}

func TestNativeBootPendingUnitPolicy(t *testing.T) {
	for _, row := range []struct {
		text    string
		waiting bool
	}{
		{"ActiveState=inactive\nJob=42", true}, {"ActiveState=activating\nJob=42", true},
		{"ActiveState=inactive\nJob=", false}, {"ActiveState=inactive\nJob=0", false},
		{"ActiveState=inactive\nJob=invalid", false}, {"ActiveState=failed\nJob=42", false},
		{"ActiveState=active\nJob=", false}, {"", false},
	} {
		_, waiting := nativeBootPendingUnit(row.text)
		if waiting != row.waiting {
			t.Fatal(row)
		}
	}
}

func TestDisposableNativeBootRejected(t *testing.T) {
	requireNativeLab(t)
	_, plan := bootManifest(t)
	state, err := storageprep.DecodeState(imageRead(t, storageprep.JournalPath))
	if err != nil || state.Phase != storageprep.RecoveryRequired || state.FailureCode != "boot-evidence-invalid" {
		t.Fatal(state, err)
	}
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	features := nativeSuperblock(t, device)["Filesystem features"]
	if strings.Contains(features, "project") || strings.Contains(features, "quota") {
		t.Fatal("pre-write rejection changed quota features")
	}
	gate, err := installapply.NewLinuxAdmissionGate(plan.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.VerifyClosed(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{installapply.DefaultJournalPath, "/run/initramfs/stackfort-native-quota-mutating", "/run/initramfs/stackfort-native-boot-proof.json"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unexpected write or install", path)
		}
	}
	imageCommand(t, "/usr/bin/findmnt", "-nro", "TARGET", "-T", "/srv/hosting")
	if imageCommand(t, "/usr/bin/systemctl", "show", "--property=ActiveState", "--value", "srv-hosting.mount") == "active" {
		t.Fatal("hosting mounted after rejected boot")
	}
	t.Log("NATIVE_BOOT rejected before filesystem write; recovery required; hosting and web gate remain blocked")
}
