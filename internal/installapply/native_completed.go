// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

// Historical completion is only eligibility for a new live check, never proof
// that storage or public services are currently safe. No recovery is inferred.
func validateNativeCompletedStatus(report NativeOperatorStatus, manifest NativeReleaseManifest) error {
	plan, err := manifest.Plan()
	if err != nil || manifest.Release.Policy.Class != "tag-release" || report.SchemaVersion != 1 ||
		report.Storage == nil || report.Storage.Validate() != nil || report.Storage.Phase != storageprep.Ready || report.Storage.Plan != plan ||
		report.Admission == nil || !report.Admission.Exists || !report.Admission.PackageComplete ||
		report.Admission.State.Phase != "admitted" || report.Admission.State.Plan != plan || report.Admission.State.validate() != nil ||
		report.RecoveryChoice == nil || !report.RecoveryChoice.valid() || report.RecoveryChoice.OperationID != plan.OperationID ||
		report.Prerequisites == nil || report.Prerequisites.Phase != "complete" || report.Prerequisites.OperationID != plan.OperationID {
		return errors.New("native rerun requires the exact completed, admitted tagged installation with Ready storage; partial or recovery state is not resumed")
	}
	if report.Approval != nil && (report.Approval.validate() != nil || report.Approval.Plan != plan || report.Approval.Status == "pending") {
		return errors.New("native rerun cannot consume a recovery approval")
	}
	return nil
}

const NativeCompletedResultPrefix = "NATIVE_COMPLETED_RESULT="

func ValidateNativeCompletedResult(result Result, manifest NativeReleaseManifest) error {
	if _, err := manifest.Plan(); err != nil {
		return err
	}
	if manifest.Release.Policy.Class != "tag-release" || result.Status != InstallComplete || !result.AlreadyInstalled || result.Changed || result.Resumed ||
		result.Version != manifest.Release.Policy.Version || result.SourceDigest != manifest.Release.Source.SourceDigest || len(result.Stages) != len(orderedStages) {
		return errors.New("native recheck did not return the exact unchanged completed installation")
	}
	for index, stage := range result.Stages {
		if stage.ID != orderedStages[index] || stage.Status != StageComplete || stage.Attempts < 1 {
			return errors.New("native recheck returned an incomplete installed stage")
		}
	}
	return nil
}

func NativeCompletedRecheckUnit(operation string) (string, error) {
	if !validSourceOperation(operation) {
		return "", errors.New("invalid completed native operation")
	}
	return "stackfort-native-recheck-" + operation + ".service", nil
}

// The command and supervisor are fixed. No caller-supplied unit, environment,
// command, restart policy, recovery approval or setup secret is accepted.
func NativeCompletedRecheckArguments(operation string) ([]string, error) {
	unit, err := NativeCompletedRecheckUnit(operation)
	if err != nil {
		return nil, err
	}
	return []string{"--wait", "--pipe", "--quiet", "--collect", "--service-type=exec", "--unit=" + unit,
		"--property=Restart=no", "--property=RuntimeMaxSec=600", "--property=TimeoutStopSec=90", "--property=KillMode=control-group",
		"--property=User=root", "--property=Group=root", "--property=WorkingDirectory=/", "--property=UMask=0077",
		"--property=Environment=PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C LC_ALL=C",
		"--property=ExecStopPost=" + NativeRuntimePath + " native-service recheck-cleanup --operation-id=" + operation,
		"--", NativeRuntimePath, "native-service", "recheck-completed", "--operation-id=" + operation}, nil
}
