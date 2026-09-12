// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func recoverySnapshot() NativeOperatorStatus {
	plan := pinPlan(testSourcePin())
	return NativeOperatorStatus{SchemaVersion: 1,
		Storage: &storageprep.State{SchemaVersion: 1, Plan: plan, Phase: storageprep.Ready, ArmAttempts: 1, ResumeBootID: "30000000-0000-4000-8000-000000000075"},
		Admission: &AdmissionInspection{Exists: true, PackageComplete: true,
			State:  AdmissionState{SchemaVersion: 1, Plan: plan, BootID: "30000000-0000-4000-8000-000000000075", Attempt: 1, Phase: "admitted"},
			Review: AdmissionRecovery{strings.Repeat("a", 64), strings.Repeat("b", 64)}},
	}
}

func recoveryPrerequisite(status NativeOperatorStatus, phase string) *NativePrerequisiteStatus {
	return &NativePrerequisiteStatus{OperationID: status.Storage.Plan.OperationID, Phase: phase, RecordSHA256: strings.Repeat("c", 64)}
}

func recoveryPending(status *NativeOperatorStatus) {
	status.Approval = &RecoveryApproval{SchemaVersion: 1, ID: "30000000-0000-4000-8000-000000000099", Plan: status.Storage.Plan, Review: status.Admission.Review, Status: "pending"}
	status.ApprovalSHA256 = strings.Repeat("d", 64)
}

func TestNativeRecoveryPlanClassificationAndNoAuthority(t *testing.T) {
	cases := []struct {
		name, class string
		change      func(*NativeOperatorStatus)
	}{
		{"absent", "no-native-state", func(s *NativeOperatorStatus) { *s = NativeOperatorStatus{SchemaVersion: 1} }},
		{"complete", "admission-recorded", func(s *NativeOperatorStatus) {}},
		{"complete-prerequisites", "admission-recorded", func(s *NativeOperatorStatus) { s.Prerequisites = recoveryPrerequisite(*s, "complete") }},
		{"no-admission", "storage-ready-unadmitted", func(s *NativeOperatorStatus) { s.Admission = nil }},
		{"absent-admission-file", "storage-ready-unadmitted", func(s *NativeOperatorStatus) {
			s.Admission = &AdmissionInspection{Review: AdmissionRecovery{PackageSHA256: admissionDigest([]byte("absent package journal\n"))}}
		}},
		{"checking", "admission-review", func(s *NativeOperatorStatus) { s.Admission.State.Phase = "checking" }},
		{"failed-admission", "admission-review", func(s *NativeOperatorStatus) { s.Admission.State.Phase = "recovery-required" }},
		{"incomplete-packages", "admission-review", func(s *NativeOperatorStatus) { s.Admission.PackageComplete = false }},
		{"pending", "admission-approval-pending", func(s *NativeOperatorStatus) { s.Admission.PackageComplete = false; recoveryPending(s) }},
		{"consumed", "admission-review", func(s *NativeOperatorStatus) {
			s.Admission.PackageComplete = false
			recoveryPending(s)
			s.Approval.Status = "consumed"
		}},
		{"cancelled", "admission-review", func(s *NativeOperatorStatus) {
			s.Admission.PackageComplete = false
			recoveryPending(s)
			s.Approval.Status = "cancelled"
		}},
		{"pending-after-completion", "approval-review", recoveryPending},
		{"stale-state", "approval-review", func(s *NativeOperatorStatus) {
			s.Admission.PackageComplete = false
			recoveryPending(s)
			s.Approval.Review.StateSHA256 = strings.Repeat("e", 64)
		}},
		{"stale-packages", "approval-review", func(s *NativeOperatorStatus) {
			s.Admission.PackageComplete = false
			recoveryPending(s)
			s.Approval.Review.PackageSHA256 = strings.Repeat("e", 64)
		}},
		{"readiness-lost-with-approval", "storage-recovery-required", func(s *NativeOperatorStatus) {
			recoveryPending(s)
			s.Storage.Phase = storageprep.RecoveryRequired
			s.Storage.FailureCode = "readiness-lost"
		}},
	}
	for _, phase := range []string{"checking", "applying", "recovery-required", "complete"} {
		class := "prerequisite-review"
		if phase == "complete" {
			class = "preparation-incomplete"
		}
		cases = append(cases, struct {
			name, class string
			change      func(*NativeOperatorStatus)
		}{"prerequisites-" + phase, class, func(s *NativeOperatorStatus) {
			s.Prerequisites = recoveryPrerequisite(*s, phase)
			s.Storage, s.Admission = nil, nil
		}})
	}
	for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.RecoveryRequired} {
		class := "storage-unverified"
		if phase == storageprep.Planned {
			class = "preparation-incomplete"
		}
		if phase == storageprep.RecoveryRequired {
			class = "storage-recovery-required"
		}
		cases = append(cases, struct {
			name, class string
			change      func(*NativeOperatorStatus)
		}{string(phase), class, func(s *NativeOperatorStatus) {
			s.Admission = nil
			s.Storage.Phase = phase
			if phase == storageprep.Planned {
				s.Storage.ArmAttempts = 0
			}
			if phase != storageprep.Verifying {
				s.Storage.ResumeBootID = ""
			}
			if phase == storageprep.RecoveryRequired {
				s.Storage.FailureCode = "interrupted-arm"
			}
		}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status := recoverySnapshot()
			test.change(&status)
			before := nativeBootJSON(status)
			plan := AssessNativeRecovery(status, nil)
			if !plan.InspectionComplete || plan.Classification != test.class ||
				plan.NeedsReview != (test.class != "admission-recorded" && test.class != "no-native-state") {
				t.Fatalf("unexpected assessment: %+v", plan)
			}
			if plan.BackupVerified || plan.AutomaticRecoveryEnabled || plan.DestructiveActionsAuthorized || plan.PublicResumeEnabled || plan.LiveReadinessVerified || !plan.RecordedStateOnly {
				t.Fatal("advice granted authority", plan)
			}
			if !bytes.Equal(before, nativeBootJSON(status)) {
				t.Fatal("assessment changed recorded state")
			}
			if test.class != "no-native-state" && (plan.Evidence == nil || !validSourceOperation(plan.Evidence.OperationID)) {
				t.Fatal("missing bounded evidence")
			}
		})
	}
}

func TestNativeRecoveryPlanRejectsPartialUnsafeOrContradictoryState(t *testing.T) {
	for _, change := range []func(*NativeOperatorStatus){
		func(s *NativeOperatorStatus) { s.SchemaVersion = 0 },
		func(s *NativeOperatorStatus) { s.PublicResumeEnabled = true },
		func(s *NativeOperatorStatus) { s.LiveReadinessVerified = true },
		func(s *NativeOperatorStatus) { s.Storage = nil },
		func(s *NativeOperatorStatus) { s.Storage.Phase = "secret-invalid-phase" },
		func(s *NativeOperatorStatus) { s.Storage.Plan.OperationID = "secret-invalid-id" },
		func(s *NativeOperatorStatus) {
			s.Admission.State.Plan.OperationID = "30000000-0000-4000-8000-000000000087"
		},
		func(s *NativeOperatorStatus) { s.Admission.State.Phase = "secret-invalid-phase" },
		func(s *NativeOperatorStatus) { s.Admission.Review.StateSHA256 = "secret-invalid-hash" },
		func(s *NativeOperatorStatus) { s.Admission.Review.PackageSHA256 = "secret-invalid-hash" },
		func(s *NativeOperatorStatus) { s.Admission.Exists = false },
		func(s *NativeOperatorStatus) { s.Prerequisites = recoveryPrerequisite(*s, "secret-invalid-phase") },
		func(s *NativeOperatorStatus) {
			s.Prerequisites = recoveryPrerequisite(*s, "complete")
			s.Prerequisites.RecordSHA256 = "secret-invalid-hash"
		},
		func(s *NativeOperatorStatus) {
			s.Prerequisites = recoveryPrerequisite(*s, "complete")
			s.Prerequisites.OperationID = "30000000-0000-4000-8000-000000000087"
		},
		func(s *NativeOperatorStatus) { s.Prerequisites = recoveryPrerequisite(*s, "applying") },
		func(s *NativeOperatorStatus) {
			recoveryPending(s)
			s.Approval.Plan.OperationID = "30000000-0000-4000-8000-000000000087"
		},
		func(s *NativeOperatorStatus) { recoveryPending(s); s.Approval.Status = "secret-invalid-status" },
		func(s *NativeOperatorStatus) { recoveryPending(s); s.ApprovalSHA256 = "secret-invalid-hash" },
		func(s *NativeOperatorStatus) { s.ApprovalSHA256 = strings.Repeat("d", 64) },
		func(s *NativeOperatorStatus) { recoveryPending(s); s.Admission = nil },
	} {
		status := recoverySnapshot()
		change(&status)
		plan := AssessNativeRecovery(status, nil)
		if plan.Classification != "inspection-unavailable" || plan.InspectionComplete || !plan.NeedsReview || plan.Evidence != nil || strings.Contains(string(nativeBootJSON(plan)), "secret-") {
			t.Fatal("unsafe/partial data promoted or exported", plan)
		}
	}
	for _, cause := range []error{errors.New("secret-error"), ErrAdmissionRecovery} {
		plan := AssessNativeRecovery(recoverySnapshot(), cause)
		if plan.InspectionComplete || plan.Evidence != nil || strings.Contains(string(nativeBootJSON(plan)), "secret-") {
			t.Fatal("read error ignored or exported", plan)
		}
	}
}
