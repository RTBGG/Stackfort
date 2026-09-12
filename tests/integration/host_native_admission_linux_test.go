// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func journalAdmissionGate(t *testing.T) *installapply.LinuxAdmissionGate {
	t.Helper()
	n := nativeState(t, nativeLab+"/intent.json")
	gate, err := installapply.NewLinuxAdmissionGate(n.Operation)
	if err != nil {
		t.Fatal(err)
	}
	return gate
}

func TestDisposableNativeAdmissionClose(t *testing.T) {
	requireNativeLab(t)
	gate := journalAdmissionGate(t)
	if err := gate.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := gate.VerifyClosed(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Log("NATIVE_ADMISSION public IPv4/IPv6 TCP/UDP web gate closed; loopback and SSH unaffected")
}

func TestDisposableNativeAdmissionQuarantine(t *testing.T) {
	requireNativeLab(t)
	gate := journalAdmissionGate(t)
	// Independent service supervisor cleanup, including a killed coordinator.
	err := errors.Join(gate.Close(t.Context()), gate.VerifyClosed(t.Context()), gate.StopConsumers(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	t.Log("NATIVE_ADMISSION supervisor quarantine complete")
}

type admissionLabGate struct {
	*installapply.LinuxAdmissionGate
	t          *testing.T
	fault      string
	recovering bool
}

func (gate admissionLabGate) VerifyClosed(ctx context.Context) error {
	if err := gate.LinuxAdmissionGate.VerifyClosed(ctx); err != nil {
		return err
	}
	if gate.fault != "pause-services-once" || gate.recovering {
		return nil
	}
	data, err := os.ReadFile("/var/lib/stackfort-installer/installation-admission.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state installapply.AdmissionState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Attempt != 1 || state.Phase != "checking" {
		return nil
	}
	data, err = os.ReadFile(installapply.DefaultJournalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal installapply.Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return err
	}
	for _, stage := range journal.Stages {
		if stage.ID == installapply.StageServices && stage.Status == installapply.StageApplying {
			imageWrite(gate.t, nativeLab+"/admission-paused", []byte("pause before real services stage; package manager has finished\n"), 0600)
			gate.t.Log("NATIVE_ADMISSION paused before services; waiting for deliberate supervisor kill, not inside package manager or filesystem conversion")
			<-ctx.Done()
			return ctx.Err()
		}
	}
	return nil
}

func TestDisposableNativeAdmissionInterrupt(t *testing.T) {
	requireNativeLab(t)
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	if manifest.InstallFault != "pause-services-once" {
		t.Fatal("interruption requires the sealed fault mode")
	}
	deadline := time.Now().Add(6 * time.Minute)
	for {
		if _, err := os.Stat(nativeLab + "/admission-paused"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("installation did not reach safe interruption point")
		}
		t.Log("NATIVE_ADMISSION waiting for package transactions to complete before interruption")
		time.Sleep(5 * time.Second)
	}
	if err := journalAdmissionGate(t).VerifyClosed(t.Context()); err != nil {
		t.Fatal(err)
	}
	imageCommand(t, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=KILL", nativeInstallUnit)
	deadline = time.Now().Add(90 * time.Second)
	for imageCommand(t, "/usr/bin/systemctl", "show", "--property=ActiveState", "--value", nativeInstallUnit) != "failed" {
		if time.Now().After(deadline) {
			t.Fatal("supervisor cleanup did not finish")
		}
		time.Sleep(time.Second)
	}
	journalCheckQuarantine(t)
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	inspection, err := stage.InspectAdmission()
	if err != nil || inspection.State.Phase != "checking" || inspection.State.Attempt != 1 || inspection.PackageComplete {
		t.Fatal("interruption state", inspection, err)
	}
	data, _ := json.Marshal(inspection)
	t.Log("ADMISSION_REVIEW " + string(data))
	t.Log("NATIVE_ADMISSION deliberate process loss quarantined by ExecStopPost; journal preserved for explicit recovery")
}

func TestDisposableNativeAdmissionInspect(t *testing.T) {
	requireNativeLab(t)
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	inspection, err := stage.InspectAdmission()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(inspection)
	t.Log("ADMISSION_REVIEW " + string(data))
}

func TestDisposableNativeAdmissionRecover(t *testing.T) {
	requireNativeLab(t)
	wanted := installapply.AdmissionRecovery{StateSHA256: os.Getenv("STACKFORT_ADMISSION_STATE_SHA256"), PackageSHA256: os.Getenv("STACKFORT_ADMISSION_PACKAGE_SHA256")}
	if len(wanted.StateSHA256) != 64 || len(wanted.PackageSHA256) != 64 {
		t.Fatal("explicit operator review required")
	}
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := stage.InspectAdmission()
	_ = stage.Close()
	if err != nil || !inspection.Exists || wanted != inspection.Review || (inspection.State.Phase == "admitted" && inspection.PackageComplete) {
		t.Fatal("stale or inapplicable operator review", err)
	}
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	operator := nativeLab + "/operator"
	if len(manifest.OperatorDigest) != 64 || nativeHash(imageRead(t, operator)) != manifest.OperatorDigest {
		t.Fatal("operator CLI differs from sealed artifact")
	}
	readStatus := func() installapply.NativeOperatorStatus {
		t.Helper()
		var status installapply.NativeOperatorStatus
		if err := json.Unmarshal([]byte(imageCommand(t, operator, "native", "status", "--format=json")), &status); err != nil {
			t.Fatal(err)
		}
		if status.PublicResumeEnabled || status.LiveReadinessVerified {
			t.Fatal("recorded state claims runtime readiness")
		}
		return status
	}
	approve := func() {
		imageCommand(t, operator, "native", "approve-recovery", "--yes", "--state-sha256="+wanted.StateSHA256, "--package-sha256="+wanted.PackageSHA256)
	}
	status := readStatus()
	if status.Admission == nil || status.Admission.Review != wanted {
		t.Fatal("CLI and locked inspection differ")
	}
	approve()
	first := readStatus()
	if first.Approval == nil || first.Approval.Status != "pending" {
		t.Fatal("approval not queued")
	}
	approve()
	if readStatus().ApprovalSHA256 != first.ApprovalSHA256 {
		t.Fatal("same approval not idempotent")
	}
	if err := exec.Command(operator, "native", "cancel-recovery", "--yes", "--approval-sha256="+strings.Repeat("0", 64)).Run(); err == nil {
		t.Fatal("wrong cancellation accepted")
	}
	if readStatus().ApprovalSHA256 != first.ApprovalSHA256 {
		t.Fatal("wrong cancellation changed approval")
	}
	imageCommand(t, operator, "native", "cancel-recovery", "--yes", "--approval-sha256="+first.ApprovalSHA256)
	if readStatus().Approval.Status != "cancelled" {
		t.Fatal("approval not cancelled")
	}
	approve()
	pending := readStatus()
	if pending.Approval.ID == first.Approval.ID {
		t.Fatal("new authorization reused consumed identity")
	}
	if err := journalAdmissionGate(t).VerifyClosed(t.Context()); err != nil {
		t.Fatal("operator command opened gate", err)
	}
	imageCommand(t, "/usr/bin/systemctl", "reset-failed", nativeInstallUnit)
	imageCommand(t, "/usr/bin/systemctl", "start", nativeInstallUnit)
	journalWaitInstallation(t)
	done := readStatus()
	if done.Approval == nil || done.Approval.Status != "consumed" || done.Approval.ID != pending.Approval.ID {
		t.Fatal("supervisor did not consume exact approval")
	}
	if err := exec.Command(operator, "native", "approve-recovery", "--yes", "--state-sha256="+wanted.StateSHA256, "--package-sha256="+wanted.PackageSHA256).Run(); err == nil {
		t.Fatal("completed review replayed")
	}
	if readStatus().ApprovalSHA256 != done.ApprovalSHA256 {
		t.Fatal("replay changed consumed receipt")
	}
	TestDisposableNativeAdmissionInspect(t)
	t.Log("NATIVE_OPERATOR real CLI status/approve/idempotence/cancel/reapprove pass; supervisor consumes once; stale replay rejected; source and original package journal retained")
}

func TestDisposableNativeAdmissionWebProbe(t *testing.T) {
	requireNativeLab(t)
	gate := journalAdmissionGate(t)
	if err := gate.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Keep NGINX running to distinguish network quarantine from a dead listener.
	imageCommand(t, "/usr/bin/systemctl", "is-active", "nginx.service")
	output, err := exec.Command("/usr/bin/curl", "--noproxy", "*", "-kfsS", "--max-time", "5", "https://127.0.0.1:8443/api/v1/health").CombinedOutput()
	if err != nil || !strings.Contains(string(output), `"status":"ok"`) {
		t.Fatal("loopback health failed behind closed gate", err, string(output))
	}
	t.Log("NATIVE_ADMISSION gate closed with live NGINX and loopback health=ok; probe externally before next reboot")
}
