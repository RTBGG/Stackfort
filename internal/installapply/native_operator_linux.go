// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const recoveryApprovalName = "native-recovery-approval.json"

// ManageNativeInstallation does not start services, open a firewall, arm a
// conversion or reboot. A pending approval is NOT a successful recovery.
func ManageNativeInstallation(ctx context.Context, request NativeOperatorRequest) (NativeOperatorStatus, error) {
	if err := request.Validate(); err != nil {
		return NativeOperatorStatus{}, err
	}
	if ctx == nil || ctx.Err() != nil {
		return NativeOperatorStatus{}, errors.New("native management requires an active context")
	}
	stage, exists, err := openExistingSourceStage()
	if err != nil {
		return NativeOperatorStatus{}, err
	}
	if !exists {
		if request.Action != "status" {
			return NativeOperatorStatus{}, errors.New("no native installation to recover")
		}
		return NativeOperatorStatus{SchemaVersion: 1}, nil
	}
	defer stage.Close()
	return stage.manageNative(ctx, request)
}

func (stage *SourceStage) recordedNative() (NativeOperatorStatus, NativeReleaseManifest, error) {
	report := NativeOperatorStatus{SchemaVersion: 1}
	if err := stage.check(); err != nil {
		return report, NativeReleaseManifest{}, err
	}
	if stage.journal == nil {
		return report, NativeReleaseManifest{}, errors.New("native operator requires the shared lock")
	}
	choice, choiceExists, err := stage.readNativeRecoveryChoice()
	if err != nil {
		return report, NativeReleaseManifest{}, err
	}
	if choiceExists {
		report.RecoveryChoice = &NativeRecoveryChoiceStatus{OperationID: choice.Review.Release.Source.OperationID, Mode: choice.Decision.Mode,
			RecordSHA256: admissionDigest(nativeBootJSON(choice)), ReviewSHA256: choice.Decision.ReviewedSHA256}
	}
	prerequisites, found, prerequisiteErr := stage.readNativePrerequisites()
	if prerequisiteErr != nil {
		return report, NativeReleaseManifest{}, prerequisiteErr
	}
	if found {
		if choiceExists || prerequisites.RecoveryChoiceSHA256 != "" {
			if !choiceExists {
				return report, NativeReleaseManifest{}, errors.New("missing prerequisite recovery choice")
			}
			if err := checkNativeRecoveryPrerequisites(nativeBootJSON(choice), prerequisites); err != nil {
				return report, NativeReleaseManifest{}, err
			}
		}
		report.Prerequisites = &NativePrerequisiteStatus{OperationID: prerequisites.OperationID, Phase: prerequisites.Phase, RecordSHA256: admissionDigest(nativeBootJSON(prerequisites)), Planned: prerequisites.Planned}
	}
	state, exists, err := stage.journal.Load()
	if err != nil {
		return report, NativeReleaseManifest{}, err
	}
	if !exists {
		if report.Prerequisites != nil || report.RecoveryChoice != nil {
			if report.Prerequisites == nil {
				if err := nativeOrphansAt(stage.dir, []string{"native-prerequisite-installer"}); err != nil {
					return report, NativeReleaseManifest{}, err
				}
			}
			return report, NativeReleaseManifest{}, checkNativeBootOrphans(stage.dir)
		}
		if err := nativeOrphansAt(stage.dir, []string{nativeSetupName, nativeOnboardingSourceName}); err != nil {
			return report, NativeReleaseManifest{}, err
		}
		return report, NativeReleaseManifest{}, checkNativeOrphans(stage.dir)
	}
	report.Storage = &state
	data, found, err := admissionReadAt(stage.dir, nativeReleaseManifestName)
	if err != nil || !found {
		return report, NativeReleaseManifest{}, errors.Join(err, errors.New("missing native release manifest"))
	}
	var manifest NativeReleaseManifest
	if json.Unmarshal(data, &manifest) != nil {
		return report, manifest, errors.New("invalid native manifest")
	}
	canonical, _ := json.MarshalIndent(manifest, "", "  ")
	plan, err := manifest.Plan()
	if err != nil || plan != state.Plan || !bytes.Equal(data, append(canonical, '\n')) {
		return report, manifest, errors.New("native manifest differs from recorded plan")
	}
	if choiceExists {
		if !recoveryChoiceMatchesPlan(choice, plan) || report.Prerequisites == nil {
			return report, manifest, errors.New("recovery choice differs from storage operation")
		}
		data, found, err := admissionReadAt(stage.dir, nativeRuntimeName)
		if err != nil || !found {
			return report, manifest, errors.New("missing recovery-bound runtime")
		}
		var intent NativeRuntimeIntent
		if err := nativeBootDecode(data, &intent); err != nil {
			return report, manifest, err
		}
		digest, err := intent.Digest()
		if err != nil || digest != manifest.BootSHA256 {
			return report, manifest, errors.New("recovery-bound runtime mismatch")
		}
		if err := checkNativeRecoveryChoiceData(nativeBootJSON(choice), manifest, intent); err != nil {
			return report, manifest, err
		}
	} else {
		// A removed receipt must not look like a legacy operation. Do not demand
		// new fields of historical profiles, but recognize a sealed new binding.
		data, found, err := admissionReadAt(stage.dir, nativeRuntimeName)
		if err != nil {
			return report, manifest, err
		}
		if found {
			var intent NativeRuntimeIntent
			if err := nativeBootDecode(data, &intent); err != nil {
				return report, manifest, err
			}
			if intent.Offline != nil && intent.Offline.RecoveryChoiceSHA256 != "" {
				return report, manifest, errors.New("sealed recovery choice is missing")
			}
		}
	}
	inspection, err := stage.InspectAdmission()
	if err != nil {
		return report, manifest, err
	}
	if inspection.Exists && inspection.State.Plan != plan {
		return report, manifest, errors.New("admission differs from native storage")
	}
	if _, packageExists, err := admissionReadAt(stage.dir, "install-state.json"); err != nil {
		return report, manifest, err
	} else if packageExists {
		if _, _, err := (nativeInstallStore{stage: stage, files: NewFileStore(), plan: plan}).Load(); err != nil {
			return report, manifest, err
		}
	}
	report.Admission = &inspection
	approval, digest, err := stage.readRecoveryApproval()
	if err != nil {
		return report, manifest, err
	}
	if approval != nil && approval.Plan != plan {
		return report, manifest, errors.New("recovery approval differs from native plan")
	}
	report.Approval, report.ApprovalSHA256 = approval, digest
	return report, manifest, nil
}

func checkNativeOrphans(dir int) error {
	if err := checkNativeBootOrphans(dir); err != nil {
		return err
	}
	return nativeOrphansAt(dir, []string{nativePrerequisiteName, "native-prerequisite-installer", nativeRecoveryChoiceName})
}

func checkNativeBootOrphans(dir int) error {
	return nativeOrphansAt(dir, []string{admissionName, nativeReleaseManifestName, recoveryApprovalName, nativeRuntimeName, "native-runtime-installer", nativeBootArtifactsName, nativeBootBuildName, nativeSetupRegistrationName})
}

func nativeOrphansAt(dir int, names []string) error {
	for _, name := range names {
		var stat unix.Stat_t
		err := unix.Fstatat(dir, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if !errors.Is(err, unix.ENOENT) {
			return errors.Join(err, errors.New("orphan native state; preserve evidence"))
		}
	}
	return nil
}

func (stage *SourceStage) manageNative(ctx context.Context, request NativeOperatorRequest) (NativeOperatorStatus, error) {
	if err := request.Validate(); err != nil {
		return NativeOperatorStatus{}, err
	}
	report, manifest, err := stage.recordedNative()
	if err != nil || request.Action == "status" {
		return report, err
	}
	if report.Storage == nil || report.Admission == nil {
		return report, errors.New("no native installation to recover")
	}
	var next RecoveryApproval
	switch request.Action {
	case "approve-recovery":
		if report.Storage.Phase != storageprep.Ready || !needsAdmissionRecovery(*report.Admission) ||
			request.Review != report.Admission.Review {
			return report, ErrAdmissionRecovery
		}
		if report.Approval != nil && report.Approval.Status == "pending" && report.Approval.Review != request.Review {
			return report, errors.New("cancel the reviewed pending approval before replacing it")
		}
		// Authentic retained source must still verify; approval cannot repair or
		// rebind source/storage. Full live checks happen again at consumption.
		if _, err := stage.VerifyManifest(ctx, manifest); err != nil {
			return report, err
		}
		if report.Approval != nil && report.Approval.Status == "pending" {
			return report, nil // Same request is idempotent, without a second token.
		}
		next = RecoveryApproval{SchemaVersion: 1, ID: uuid.NewString(), Plan: report.Storage.Plan, Review: request.Review, Status: "pending"}
	case "cancel-recovery":
		if report.Approval == nil || report.Approval.Status != "pending" || request.ApprovalSHA256 != report.ApprovalSHA256 {
			return report, errors.New("cancel requires the exact pending approval digest")
		}
		next = *report.Approval
		next.Status = "cancelled"
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := stage.saveRecoveryApproval(next, report.ApprovalSHA256); err != nil {
		return report, err
	}
	report, _, err = stage.recordedNative()
	return report, err
}

func (stage *SourceStage) readRecoveryApproval() (*RecoveryApproval, string, error) {
	if err := stage.check(); err != nil {
		return nil, "", err
	}
	data, exists, err := admissionReadAt(stage.dir, recoveryApprovalName)
	if err != nil || !exists {
		return nil, "", err
	}
	var record RecoveryApproval
	if json.Unmarshal(data, &record) != nil || record.validate() != nil {
		return nil, "", errors.New("invalid recovery approval")
	}
	canonical, _ := json.MarshalIndent(record, "", "  ")
	if !bytes.Equal(data, append(canonical, '\n')) {
		return nil, "", errors.New("noncanonical recovery approval")
	}
	return &record, admissionDigest(data), nil
}

func (stage *SourceStage) saveRecoveryApproval(next RecoveryApproval, previousDigest string) error {
	if err := next.validate(); err != nil {
		return err
	}
	previous, digest, err := stage.readRecoveryApproval()
	if err != nil || digest != previousDigest {
		return errors.Join(err, errors.New("recovery approval changed"))
	}
	if previous == nil {
		if next.Status != "pending" {
			return errors.New("approval must begin pending")
		}
	} else if next.Plan != previous.Plan {
		return errors.New("cannot rebind recovery approval")
	} else if previous.Status == "pending" {
		expected := *previous
		expected.Status = next.Status
		if (next.Status != "consumed" && next.Status != "cancelled") || expected != next {
			return errors.New("invalid approval transition")
		}
	} else if next.Status != "pending" || next.ID == previous.ID {
		return errors.New("a new explicit approval needs a new identity")
	}
	data, _ := json.MarshalIndent(next, "", "  ")
	temporary := ".native-approval-" + uuid.NewString()
	if err := writeOriginRecord(stage.dir, temporary, append(data, '\n')); err != nil {
		return err
	}
	defer unix.Unlinkat(stage.dir, temporary, 0)
	if err := unix.Renameat(stage.dir, temporary, stage.dir, recoveryApprovalName); err != nil {
		return err
	}
	return unix.Fsync(stage.dir)
}

// takeRecoveryApproval must remain private: the caller holds the same lock
// through admission. Persist consumed BEFORE invoking any installation action.
func (stage *SourceStage) takeRecoveryApproval(ctx context.Context) (*AdmissionRecovery, error) {
	report, _, err := stage.recordedNative()
	if err != nil {
		return nil, err
	}
	review, err := pendingRecovery(report)
	if err != nil || review == nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	consumed := *report.Approval
	consumed.Status = "consumed"
	if err := stage.saveRecoveryApproval(consumed, report.ApprovalSHA256); err != nil {
		return nil, err
	}
	return review, nil
}

// AdmitPendingInstallation is for a qualified boot supervisor, not a public
// backend bypass. Caller must arrange early gate and process-loss quarantine.
func (stage *SourceStage) AdmitPendingInstallation(ctx context.Context, manifest NativeReleaseManifest, backend storageprep.Backend, gate InstallationGate, output io.Writer) (Result, error) {
	if ctx == nil || gate == nil || backend == nil {
		return Result{}, errors.New("invalid pending admission invocation")
	}
	if err := gate.VerifyClosed(ctx); err != nil {
		return Result{}, err
	}
	recovery, err := stage.takeRecoveryApproval(ctx)
	if err != nil {
		return Result{}, err
	}
	return stage.AdmitInstallation(ctx, manifest, backend, gate, recovery, output)
}
