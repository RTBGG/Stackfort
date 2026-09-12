// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func TestPendingRecoveryOnlyAcceptsExactRecoverableSnapshots(t *testing.T) {
	for _, scenario := range []string{"pending", "none", "consumed", "cancelled", "state-drift", "package-drift", "plan-drift", "storage-not-ready", "already-admitted", "missing-admission", "bad-status"} {
		t.Run(scenario, func(t *testing.T) {
			plan := pinPlan(testSourcePin())
			review := AdmissionRecovery{strings.Repeat("a", 64), strings.Repeat("b", 64)}
			report := NativeOperatorStatus{Storage: &storageprep.State{Plan: plan, Phase: storageprep.Ready},
				Admission: &AdmissionInspection{Exists: true, State: AdmissionState{Plan: plan, Phase: "checking"}, Review: review},
				Approval:  &RecoveryApproval{SchemaVersion: 1, ID: "30000000-0000-4000-8000-000000000099", Plan: plan, Review: review, Status: "pending"}}
			switch scenario {
			case "none":
				report.Approval = nil
			case "consumed", "cancelled":
				report.Approval.Status = scenario
			case "state-drift":
				report.Admission.Review.StateSHA256 = strings.Repeat("c", 64)
			case "package-drift":
				report.Admission.Review.PackageSHA256 = strings.Repeat("c", 64)
			case "plan-drift":
				report.Storage.Plan.OperationID = "30000000-0000-4000-8000-000000000098"
			case "storage-not-ready":
				report.Storage.Phase = storageprep.RecoveryRequired
			case "already-admitted":
				report.Admission.State.Phase = "admitted"
				report.Admission.PackageComplete = true
			case "missing-admission":
				report.Admission = nil
			case "bad-status":
				report.Approval.Status = "unexpected"
			}
			got, err := pendingRecovery(report)
			switch scenario {
			case "pending":
				if err != nil || got == nil || *got != review {
					t.Fatal(got, err)
				}
			case "none", "consumed", "cancelled":
				if err != nil || got != nil {
					t.Fatal("replayed approval", got, err)
				}
			default:
				if err == nil || got != nil {
					t.Fatal("stale approval accepted", got, err)
				}
			}
		})
	}
}
