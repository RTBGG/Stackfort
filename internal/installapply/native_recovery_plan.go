// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"slices"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

// NativeRecoveryPlan is advice, never an input to boot, admission or restore.
// No caller-supplied backup, device, command or authorization is accepted.
type NativeRecoveryPlan struct {
	SchemaVersion                int                     `json:"schemaVersion"`
	PolicyVersion                string                  `json:"policyVersion"`
	Classification               string                  `json:"classification"`
	InspectionComplete           bool                    `json:"inspectionComplete"`
	NeedsReview                  bool                    `json:"needsReview"`
	RecordedStateOnly            bool                    `json:"recordedStateOnly"`
	LiveReadinessVerified        bool                    `json:"liveReadinessVerified"`
	PublicResumeEnabled          bool                    `json:"publicResumeEnabled"`
	BackupVerified               bool                    `json:"backupVerified"`
	DestructiveActionsAuthorized bool                    `json:"destructiveActionsAuthorized"`
	AutomaticRecoveryEnabled     bool                    `json:"automaticRecoveryEnabled"`
	Summary                      string                  `json:"summary"`
	NextSteps                    []string                `json:"nextSteps"`
	Evidence                     *NativeRecoveryEvidence `json:"evidence,omitempty"`
}

// An allowlist, not a support archive: no fstab, keys, tokens, package inventory,
// network/host identifiers or raw errors. Digests do not authenticate a backup.
type NativeRecoveryEvidence struct {
	RecoveryMode         string            `json:"recoveryMode,omitempty"`
	RecoveryChoiceSHA256 string            `json:"recoveryChoiceSHA256,omitempty"`
	OperationID          string            `json:"operationId"`
	PrerequisitePhase    string            `json:"prerequisitePhase,omitempty"`
	PrerequisiteSHA256   string            `json:"prerequisiteSHA256,omitempty"`
	StoragePhase         storageprep.Phase `json:"storagePhase,omitempty"`
	FailureCode          string            `json:"failureCode,omitempty"`
	AdmissionPhase       string            `json:"admissionPhase,omitempty"`
	PackageComplete      bool              `json:"packageComplete"`
	Review               AdmissionRecovery `json:"admissionReview"`
	ApprovalStatus       string            `json:"approvalStatus,omitempty"`
	ApprovalSHA256       string            `json:"approvalSHA256,omitempty"`
}

// AssessNativeRecovery requires the complete result AND error of locked native
// status inspection. Any read/validation failure discards even partial evidence.
// It performs no I/O and must never serve as a readiness or retry decision.
func AssessNativeRecovery(status NativeOperatorStatus, inspectionErr error) NativeRecoveryPlan {
	result := NativeRecoveryPlan{
		SchemaVersion: 1, PolicyVersion: NativeRecoveryPolicyVersion,
		Classification: "inspection-unavailable", NeedsReview: true, RecordedStateOnly: true,
		Summary: "Recorded state could not be inspected completely; no recovery path is authorized.",
		NextSteps: []string{
			"Preserve installer journals, retained release files and boot artifacts; do not reset or delete them.",
			"Do not blindly retry installation, approve recovery, repair filesystems or reinstall the server.",
			"Have an operator inspect the lock, permissions and original records through a trusted console; this report omits untrusted partial data.",
		},
	}
	if inspectionErr != nil || !validRecoverySnapshot(status) {
		return result
	}
	result.InspectionComplete = true
	result.NextSteps = []string{
		"Preserve installer journals, retained release files and boot artifacts; do not reset or delete them.",
		"This report does not verify a backup or live storage/firewall health and does not authorize repair, restore or provider reinstallation.",
	}
	set := func(class, summary string, steps ...string) {
		result.Classification, result.Summary = class, summary
		result.NextSteps = append(result.NextSteps, steps...)
	}
	if status.Storage == nil && status.Prerequisites == nil && status.RecoveryChoice == nil {
		result.NeedsReview = false
		set("no-native-state", "No recorded native operation was found; this is not a fresh-host eligibility or installation-readiness check.",
			"Use the separate native-host inspection to assess eligibility. Public native activation remains disabled.")
		return result
	}
	evidence := &NativeRecoveryEvidence{}
	result.Evidence = evidence
	if choice := status.RecoveryChoice; choice != nil {
		evidence.OperationID, evidence.RecoveryMode, evidence.RecoveryChoiceSHA256 = choice.OperationID, choice.Mode, choice.RecordSHA256
	}
	if prerequisite := status.Prerequisites; prerequisite != nil {
		evidence.OperationID = prerequisite.OperationID
		evidence.PrerequisitePhase, evidence.PrerequisiteSHA256 = prerequisite.Phase, prerequisite.RecordSHA256
	}
	if status.Storage == nil {
		if status.Prerequisites == nil {
			set("preparation-incomplete", "The recovery decision was consumed for preparation, but prerequisites were not recorded.",
				"Preserve the receipt and inspect the interruption. It is not a pending approval and cannot authorize another preparation attempt.")
			return result
		}
		if status.Prerequisites.Phase != "complete" {
			set("prerequisite-review", "Prerequisite preparation is incomplete; package changes may have occurred.",
				"Review the exact prerequisite transaction and any retained service-start policy. There is no automatic APT repair, cleanup or retry.")
		} else {
			set("preparation-incomplete", "Prerequisites are recorded complete, but no storage operation was recorded.",
				"Review the preparation interruption. A missing storage journal does not authorize restarting or adopting retained artifacts.")
		}
		return result
	}
	evidence.OperationID, evidence.StoragePhase, evidence.FailureCode = status.Storage.Plan.OperationID, status.Storage.Phase, status.Storage.FailureCode
	if admission := status.Admission; admission != nil && admission.Exists {
		evidence.AdmissionPhase, evidence.PackageComplete, evidence.Review = admission.State.Phase, admission.PackageComplete, admission.Review
	}
	if approval := status.Approval; approval != nil {
		evidence.ApprovalStatus, evidence.ApprovalSHA256 = approval.Status, status.ApprovalSHA256
	}
	switch status.Storage.Phase {
	case storageprep.Planned:
		set("preparation-incomplete", "Storage preparation is planned, not verified ready.",
			"Review the sealed preparation and boot artifacts; do not reset the journal or manually select conversion entries.")
	case storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying:
		set("storage-unverified", "Storage preparation is in progress or interrupted; recorded state cannot distinguish these cases.",
			"Inspect the trusted console and sealed boot evidence before any action. Do not blindly reboot, replay conversion or mount an uncertain root.")
	case storageprep.RecoveryRequired:
		set("storage-recovery-required", "Storage recovery is required; installation-admission approval cannot repair it.",
			"If stopped in native recovery/initramfs, leave the root unmounted and use an independent rescue environment for operator-led diagnosis.",
			"Preserve the affected disk and obtain an explicit owner decision: verified external whole-system backup recovery, or provider reinstallation only if all affected data can be discarded.",
			"Neither option is executed by Stackfort. Successful filesystem checks do not authorize clearing journals or reusing a consumed conversion.")
	case storageprep.Ready:
		if _, err := pendingRecovery(status); err != nil {
			set("approval-review", "The pending installation-admission approval no longer matches recoverable recorded state.",
				"Review the changed journals and approval. Do not consume or automatically replace a stale approval; cancellation requires its exact reviewed digest.")
		} else if status.Admission == nil || !status.Admission.Exists {
			set("storage-ready-unadmitted", "Storage is recorded ready, but installation admission has not been recorded.",
				"Inspect the qualified supervisor and live checks. Do not manually start hosting services or bypass quarantine.")
		} else if !needsAdmissionRecovery(*status.Admission) {
			result.NeedsReview = false
			set("admission-recorded", "Complete installation admission is recorded; current host health is not verified.",
				"Use live service and health diagnostics for current availability; do not restore or reinstall solely on the basis of this report.")
		} else if status.Approval != nil && status.Approval.Status == "pending" {
			set("admission-approval-pending", "An exact installation-admission approval is pending, not completed recovery.",
				"Only the qualified supervisor may consume it once after all live checks. It does not approve disk repair, restore or provider reinstallation.")
		} else {
			set("admission-review", "Installation admission needs explicit review; recorded storage readiness alone is insufficient.",
				"Diagnose the cause, inspect current state again, and only then consider approve-recovery with both exact reviewed admission digests.",
				"Consumed or cancelled approvals cannot be replayed. Any new approval is for installation admission only, never filesystem repair or restore.")
		}
	}
	return result
}

func validRecoverySnapshot(status NativeOperatorStatus) bool {
	if status.SchemaVersion != 1 || status.PublicResumeEnabled || status.LiveReadinessVerified {
		return false
	}
	if choice := status.RecoveryChoice; choice != nil {
		if !choice.valid() || (status.Storage != nil && choice.OperationID != status.Storage.Plan.OperationID) ||
			(status.Prerequisites != nil && choice.OperationID != status.Prerequisites.OperationID) {
			return false
		}
	}
	if prerequisite := status.Prerequisites; prerequisite != nil {
		if !validSourceOperation(prerequisite.OperationID) || !pinDigestPattern.MatchString(prerequisite.RecordSHA256) ||
			!slices.Contains([]string{"checking", "applying", "complete", "recovery-required"}, prerequisite.Phase) {
			return false
		}
		if status.Storage != nil && (prerequisite.OperationID != status.Storage.Plan.OperationID || prerequisite.Phase != "complete") {
			return false
		}
	}
	if status.Storage == nil {
		return status.Admission == nil && status.Approval == nil && status.ApprovalSHA256 == ""
	}
	if status.Storage.Validate() != nil {
		return false
	}
	if admission := status.Admission; admission != nil {
		if !admission.Exists {
			if admission.State != (AdmissionState{}) || admission.PackageComplete || admission.Review.StateSHA256 != "" ||
				admission.Review.PackageSHA256 != admissionDigest([]byte("absent package journal\n")) {
				return false
			}
		} else if (status.Storage.Phase != storageprep.Ready && status.Storage.Phase != storageprep.RecoveryRequired) || admission.State.validate() != nil || admission.State.Plan != status.Storage.Plan ||
			!pinDigestPattern.MatchString(admission.Review.StateSHA256) || !pinDigestPattern.MatchString(admission.Review.PackageSHA256) {
			return false
		}
	}
	if approval := status.Approval; approval != nil {
		if (status.Storage.Phase != storageprep.Ready && status.Storage.Phase != storageprep.RecoveryRequired) || status.Admission == nil || !status.Admission.Exists ||
			approval.validate() != nil || approval.Plan != status.Storage.Plan || !pinDigestPattern.MatchString(status.ApprovalSHA256) {
			return false
		}
	} else if status.ApprovalSHA256 != "" {
		return false
	}
	return true
}
