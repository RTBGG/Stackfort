// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"errors"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

const nativeRecoveryChoiceName = "native-recovery-choice.json"
const NativeRecoveryPolicyVersion = "native-recovery-v1"
const NativeRecoveryFreshDisposable = "fresh-disposable"

// A review is a snapshot, NOT consent. It is constructed by the locked host
// inspector and re-created immediately before accepting an explicit decision.
type NativeRecoveryReview struct {
	SchemaVersion   int                `json:"schemaVersion"`
	PolicyVersion   string             `json:"policyVersion"`
	Release         ReleaseBinding     `json:"release"`
	InstallerSHA256 string             `json:"installerSHA256"`
	Snapshot        NativeHostSnapshot `json:"snapshot"`
}

func (review NativeRecoveryReview) Digest() (string, error) {
	if review.SchemaVersion != 1 || review.PolicyVersion != NativeRecoveryPolicyVersion || review.Release.Validate() != nil {
		return "", errors.New("invalid native recovery review")
	}
	// Apply the same bounded identity/boot-pin validation as prerequisite state.
	baseline := nativePrerequisiteRecord{SchemaVersion: 1, OperationID: review.Release.Source.OperationID,
		ReleaseSHA256: admissionDigest(nativeBootJSON(review.Release)), InstallerSHA256: review.InstallerSHA256,
		Phase: "checking", Before: review.Snapshot, Planned: map[string]string{}}
	if err := baseline.validate(); err != nil {
		return "", err
	}
	manifest := NativeReleaseManifest{SchemaVersion: 1, Release: review.Release, Host: review.Snapshot.Host, BootSHA256: review.InstallerSHA256}
	if _, err := manifest.Plan(); err != nil {
		return "", err
	}
	ready := NativeReadySpec{Features: review.Snapshot.Features, Blocks: review.Snapshot.Blocks,
		BlockSize: review.Snapshot.BlockSize, InodeSize: review.Snapshot.InodeSize,
		FstabSHA256: review.Snapshot.BootArtifacts["/etc/fstab"], GRUBSHA256: review.Snapshot.BootArtifacts["/boot/grub/grub.cfg"],
		KernelSHA256: review.Snapshot.BootArtifacts["/boot/vmlinuz-"+review.Snapshot.Host.Kernel],
		InitrdSHA256: review.Snapshot.BootArtifacts["/boot/initrd.img-"+review.Snapshot.Host.Kernel]}
	if _, err := (NativeRuntimeIntent{SchemaVersion: 1, Profile: "debian-native-post-ready-qualification-v1",
		InstallerSHA256: review.InstallerSHA256, BootIntentSHA256: review.InstallerSHA256, Ready: ready}).Digest(); err != nil {
		return "", err
	}
	return admissionDigest(nativeBootJSON(review)), nil
}

// Neither a generic --yes nor a backup path can substitute for these two
// assertions about the reviewed fresh host. No destructive recovery is granted.
type NativeRecoveryDecision struct {
	ReviewedSHA256                   string `json:"reviewedSHA256"`
	Mode                             string `json:"mode"`
	NoDataToRetain                   bool   `json:"noDataToRetain"`
	AcceptProviderReinstallationRisk bool   `json:"acceptProviderReinstallationRisk"`
}

func (decision NativeRecoveryDecision) Validate() error {
	if !pinDigestPattern.MatchString(decision.ReviewedSHA256) || decision.Mode != NativeRecoveryFreshDisposable ||
		!decision.NoDataToRetain || !decision.AcceptProviderReinstallationRisk {
		return errors.New("native preparation requires an explicit reviewed fresh-disposable decision; backup recovery is not qualified")
	}
	return nil
}

// Created exclusively before prerequisites. Presence means preparation was
// started; it is not a queued authorization and cannot be reused or replaced.
type nativeRecoveryChoice struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Review        NativeRecoveryReview   `json:"review"`
	Decision      NativeRecoveryDecision `json:"decision"`
}

func (choice nativeRecoveryChoice) validate() error {
	digest, err := choice.Review.Digest()
	if err != nil || choice.SchemaVersion != 1 || choice.Decision.Validate() != nil || choice.Decision.ReviewedSHA256 != digest {
		return errors.New("invalid or stale native recovery choice")
	}
	return nil
}

func checkNativeRecoveryPrerequisites(data []byte, record nativePrerequisiteRecord) error {
	var choice nativeRecoveryChoice
	if nativeBootDecode(data, &choice) != nil || choice.validate() != nil || record.validate() != nil ||
		record.RecoveryChoiceSHA256 == "" || admissionDigest(data) != record.RecoveryChoiceSHA256 ||
		choice.Review.Release.Source.OperationID != record.OperationID ||
		admissionDigest(nativeBootJSON(choice.Review.Release)) != record.ReleaseSHA256 ||
		choice.Review.InstallerSHA256 != record.InstallerSHA256 ||
		!bytes.Equal(nativeBootJSON(choice.Review.Snapshot), nativeBootJSON(record.Before)) {
		return errors.New("recovery choice differs from prerequisite transaction")
	}
	return nil
}

func checkNativeRecoveryChoiceData(data []byte, manifest NativeReleaseManifest, intent NativeRuntimeIntent) error {
	var choice nativeRecoveryChoice
	if nativeBootDecode(data, &choice) != nil || choice.validate() != nil || intent.Offline == nil ||
		intent.Offline.RecoveryChoiceSHA256 == "" || admissionDigest(data) != intent.Offline.RecoveryChoiceSHA256 ||
		!bytes.Equal(nativeBootJSON(choice.Review.Release), nativeBootJSON(manifest.Release)) ||
		choice.Review.Snapshot.Host != manifest.Host || choice.Review.InstallerSHA256 != intent.InstallerSHA256 {
		return errors.New("recovery choice differs from sealed boot")
	}
	snapshot := choice.Review.Snapshot
	if snapshot.Features != intent.Ready.Features || snapshot.Blocks != intent.Ready.Blocks ||
		snapshot.BlockSize != intent.Ready.BlockSize || snapshot.InodeSize != intent.Ready.InodeSize ||
		snapshot.BootArtifacts["/etc/fstab"] != admissionDigest([]byte(intent.Offline.FstabBefore)) ||
		snapshot.BootArtifacts["/boot/grub/grub.cfg"] != intent.Ready.GRUBSHA256 ||
		snapshot.BootArtifacts["/boot/vmlinuz-"+manifest.Host.Kernel] != intent.Ready.KernelSHA256 ||
		snapshot.BootArtifacts["/boot/initrd.img-"+manifest.Host.Kernel] != intent.Ready.InitrdSHA256 {
		return errors.New("reviewed boot baseline differs from sealed intent")
	}
	return nil
}

type NativeRecoveryChoiceStatus struct {
	OperationID  string `json:"operationId"`
	Mode         string `json:"mode"`
	RecordSHA256 string `json:"recordSHA256"`
	ReviewSHA256 string `json:"reviewSHA256"`
}

func (status NativeRecoveryChoiceStatus) valid() bool {
	return validSourceOperation(status.OperationID) && status.Mode == NativeRecoveryFreshDisposable &&
		pinDigestPattern.MatchString(status.RecordSHA256) && pinDigestPattern.MatchString(status.ReviewSHA256)
}

func recoveryChoiceMatchesPlan(choice nativeRecoveryChoice, plan storageprep.Plan) bool {
	return choice.Review.Release.Source.OperationID == plan.OperationID && choice.Review.Snapshot.Host.MachineID == plan.MachineID &&
		choice.Review.Snapshot.Host.RootUUID == plan.RootUUID && choice.Review.Snapshot.Host.PartitionUUID == plan.PartitionUUID &&
		choice.Review.Snapshot.Host.BootID == plan.PreviousBootID && choice.Review.Snapshot.Host.Kernel == plan.Kernel
}
