// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func completedNativeStatusFixture(t *testing.T) (NativeOperatorStatus, NativeReleaseManifest) {
	t.Helper()
	choice, manifest, _, _ := testRecoveryBoot(t)
	plan, err := manifest.Plan()
	if err != nil {
		t.Fatal(err)
	}
	report := NativeOperatorStatus{SchemaVersion: 1,
		Storage:        &storageprep.State{SchemaVersion: 1, Plan: plan, Phase: storageprep.Ready, ArmAttempts: 1, ResumeBootID: "20000000-0000-4000-8000-000000000001"},
		Admission:      &AdmissionInspection{Exists: true, PackageComplete: true, State: AdmissionState{SchemaVersion: 1, Plan: plan, BootID: "20000000-0000-4000-8000-000000000001", Attempt: 1, Phase: "admitted"}},
		RecoveryChoice: &NativeRecoveryChoiceStatus{OperationID: plan.OperationID, Mode: NativeRecoveryFreshDisposable, RecordSHA256: admissionDigest(nativeBootJSON(choice)), ReviewSHA256: choice.Decision.ReviewedSHA256},
		Prerequisites:  &NativePrerequisiteStatus{OperationID: plan.OperationID, Phase: "complete", RecordSHA256: strings.Repeat("a", 64)}}
	if err := validateNativeCompletedStatus(report, manifest); err != nil {
		t.Fatal(err)
	}
	return report, manifest
}

func TestNativeCompletedStatusRequiresExactReadyAdmittedCompletePublicState(t *testing.T) {
	for _, scenario := range []string{"valid", "planned", "recovery", "invalid-ready", "missing-storage", "missing-admission", "pending-admission", "no-packages", "foreign-plan", "missing-choice", "pending-prerequisites", "pending-approval", "lab-policy"} {
		t.Run(scenario, func(t *testing.T) {
			report, manifest := completedNativeStatusFixture(t)
			switch scenario {
			case "planned":
				report.Storage.Phase = storageprep.Planned
			case "recovery":
				report.Storage.Phase = storageprep.RecoveryRequired
			case "invalid-ready":
				report.Storage.ResumeBootID = ""
			case "missing-storage":
				report.Storage = nil
			case "missing-admission":
				report.Admission = nil
			case "pending-admission":
				report.Admission.State.Phase = "checking"
			case "no-packages":
				report.Admission.PackageComplete = false
			case "foreign-plan":
				report.Admission.State.Plan.SourceDigest = strings.Repeat("f", 64)
			case "missing-choice":
				report.RecoveryChoice = nil
			case "pending-prerequisites":
				report.Prerequisites.Phase = "applying"
			case "pending-approval":
				report.Approval = &RecoveryApproval{SchemaVersion: 1, ID: sourceOperation, Plan: report.Storage.Plan, Review: AdmissionRecovery{strings.Repeat("a", 64), strings.Repeat("b", 64)}, Status: "pending"}
			case "lab-policy":
				manifest.Release.Policy = OriginPolicy{"lab-candidate", "0.1.0-beta.3", "5282946bec1f865de7222128a6a5d0d8a656f34c"}
			}
			if err := validateNativeCompletedStatus(report, manifest); (err == nil) != (scenario == "valid") {
				t.Fatal(scenario, err)
			}
		})
	}
}

func completedNativeResultFixture(manifest NativeReleaseManifest) Result {
	result := Result{Version: manifest.Release.Policy.Version, SourceDigest: manifest.Release.Source.SourceDigest, Status: InstallComplete, AlreadyInstalled: true}
	for _, id := range orderedStages {
		result.Stages = append(result.Stages, StageState{ID: id, Status: StageComplete, Attempts: 1})
	}
	return result
}

func TestNativeCompletedResultRequiresUnchangedVerifiedAllStages(t *testing.T) {
	_, manifest := completedNativeStatusFixture(t)
	for _, scenario := range []string{"valid", "changed", "resumed", "new-install", "version", "source", "failed", "missing-stage", "wrong-stage", "pending-stage", "unattempted"} {
		result := completedNativeResultFixture(manifest)
		switch scenario {
		case "changed":
			result.Changed = true
		case "resumed":
			result.Resumed = true
		case "new-install":
			result.AlreadyInstalled = false
		case "version":
			result.Version = "0.1.0-beta.99"
		case "source":
			result.SourceDigest = strings.Repeat("f", 64)
		case "failed":
			result.Status = InstallFailed
		case "missing-stage":
			result.Stages = result.Stages[1:]
		case "wrong-stage":
			result.Stages[0].ID = StageServices
		case "pending-stage":
			result.Stages[0].Status = StagePending
		case "unattempted":
			result.Stages[0].Attempts = 0
		}
		if err := ValidateNativeCompletedResult(result, manifest); (err == nil) != (scenario == "valid") {
			t.Fatal(scenario, err)
		}
	}
}

func TestNativeCompletedSupervisorHasOnlyFixedBoundedCommands(t *testing.T) {
	arguments, err := NativeCompletedRecheckArguments(sourceOperation)
	if err != nil {
		t.Fatal(err)
	}
	unit, _ := NativeCompletedRecheckUnit(sourceOperation)
	for _, required := range []string{"--wait", "--pipe", "--collect", "--unit=" + unit, "--property=Restart=no", "--property=RuntimeMaxSec=600", "--property=KillMode=control-group", "--property=ExecStopPost=" + NativeRuntimePath + " native-service recheck-cleanup --operation-id=" + sourceOperation} {
		found := false
		for _, value := range arguments {
			found = found || value == required
		}
		if !found {
			t.Fatal("missing supervision fence", required)
		}
	}
	if !reflect.DeepEqual(arguments[len(arguments)-5:], []string{"--", NativeRuntimePath, "native-service", "recheck-completed", "--operation-id=" + sourceOperation}) {
		t.Fatal(arguments)
	}
	for _, invalid := range []string{"", "bad", sourceOperation + ";reboot", strings.ToUpper(sourceOperation + "a")} {
		if _, err := NativeCompletedRecheckArguments(invalid); err == nil {
			t.Fatal("invalid operation entered supervisor", invalid)
		}
	}
	for _, action := range []string{"recheck-completed", "recheck-cleanup"} {
		if err := (NativeServiceRequest{Action: action, OperationID: sourceOperation}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
