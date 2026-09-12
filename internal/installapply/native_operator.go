// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

// NativeOperatorRequest never selects a source, path, unit, command or backend.
type NativeOperatorRequest struct {
	Action         string
	Review         AdmissionRecovery
	ApprovalSHA256 string
}

func (request NativeOperatorRequest) Validate() error {
	switch request.Action {
	case "status":
		if request.Review == (AdmissionRecovery{}) && request.ApprovalSHA256 == "" {
			return nil
		}
	case "approve-recovery":
		if pinDigestPattern.MatchString(request.Review.StateSHA256) &&
			pinDigestPattern.MatchString(request.Review.PackageSHA256) && request.ApprovalSHA256 == "" {
			return nil
		}
	case "cancel-recovery":
		if request.Review == (AdmissionRecovery{}) && pinDigestPattern.MatchString(request.ApprovalSHA256) {
			return nil
		}
	}
	return errors.New("invalid native operator request")
}

type RecoveryApproval struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Plan          storageprep.Plan  `json:"plan"`
	Review        AdmissionRecovery `json:"review"`
	Status        string            `json:"status"`
}

func (approval RecoveryApproval) validate() error {
	if err := approval.Plan.Validate(); err != nil {
		return err
	}
	if approval.SchemaVersion != 1 || !validSourceOperation(approval.ID) ||
		!pinDigestPattern.MatchString(approval.Review.StateSHA256) || !pinDigestPattern.MatchString(approval.Review.PackageSHA256) ||
		(approval.Status != "pending" && approval.Status != "consumed" && approval.Status != "cancelled") {
		return errors.New("invalid native recovery approval")
	}
	return nil
}

// This is recorded state, NOT a live network/host readiness assertion.
type NativeOperatorStatus struct {
	RecoveryChoice        *NativeRecoveryChoiceStatus `json:"recoveryChoice,omitempty"`
	Prerequisites         *NativePrerequisiteStatus   `json:"prerequisites,omitempty"`
	SchemaVersion         int                         `json:"schemaVersion"`
	PublicResumeEnabled   bool                        `json:"publicResumeEnabled"`
	LiveReadinessVerified bool                        `json:"liveReadinessVerified"`
	Storage               *storageprep.State          `json:"storage,omitempty"`
	Admission             *AdmissionInspection        `json:"admission,omitempty"`
	Approval              *RecoveryApproval           `json:"approval,omitempty"`
	ApprovalSHA256        string                      `json:"approvalSHA256,omitempty"`
}

type NativePrerequisiteStatus struct {
	OperationID  string            `json:"operationId"`
	Phase        string            `json:"phase"`
	RecordSHA256 string            `json:"recordSHA256"`
	Planned      map[string]string `json:"planned"`
}

func needsAdmissionRecovery(inspection AdmissionInspection) bool {
	return inspection.Exists && (inspection.State.Phase != "admitted" || !inspection.PackageComplete)
}

func pendingRecovery(report NativeOperatorStatus) (*AdmissionRecovery, error) {
	if report.Approval == nil || report.Approval.Status == "consumed" || report.Approval.Status == "cancelled" {
		return nil, nil
	}
	if report.Approval.validate() != nil || report.Storage == nil || report.Storage.Phase != storageprep.Ready ||
		report.Approval.Plan != report.Storage.Plan || report.Admission == nil ||
		report.Admission.State.Plan != report.Storage.Plan || !needsAdmissionRecovery(*report.Admission) ||
		report.Approval.Review != report.Admission.Review {
		return nil, errors.New("pending recovery approval is stale; preserve and review it")
	}
	review := report.Approval.Review
	return &review, nil
}
