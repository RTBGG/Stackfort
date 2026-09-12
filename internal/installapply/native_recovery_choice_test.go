// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"strings"
	"testing"
)

func testRecoveryChoice() nativeRecoveryChoice {
	prerequisite := testPrerequisiteRecord()
	pin := testSourcePin()
	review := NativeRecoveryReview{SchemaVersion: 1, PolicyVersion: NativeRecoveryPolicyVersion,
		Release:         ReleaseBinding{1, pin, OriginPolicy{"tag-release", pin.Version, strings.Repeat("a", 40)}, strings.Repeat("b", 64), strings.Repeat("c", 64), originVerifierSHA256},
		InstallerSHA256: prerequisite.InstallerSHA256, Snapshot: prerequisite.Before}
	digest, _ := review.Digest()
	return nativeRecoveryChoice{SchemaVersion: 1, Review: review, Decision: NativeRecoveryDecision{
		ReviewedSHA256: digest, Mode: NativeRecoveryFreshDisposable, NoDataToRetain: true, AcceptProviderReinstallationRisk: true}}
}

func testRecoveryBoot(t *testing.T) (nativeRecoveryChoice, NativeReleaseManifest, NativeRuntimeIntent, nativePrerequisiteRecord) {
	t.Helper()
	choice := testRecoveryChoice()
	intent := testBootIntent()
	snapshot := &choice.Review.Snapshot
	snapshot.Features, snapshot.Blocks, snapshot.BlockSize, snapshot.InodeSize = intent.Ready.Features, intent.Ready.Blocks, intent.Ready.BlockSize, intent.Ready.InodeSize
	snapshot.BootArtifacts["/etc/fstab"] = admissionDigest([]byte(intent.Offline.FstabBefore))
	snapshot.BootArtifacts["/boot/grub/grub.cfg"] = intent.Ready.GRUBSHA256
	snapshot.BootArtifacts["/boot/vmlinuz-"+snapshot.Host.Kernel] = intent.Ready.KernelSHA256
	snapshot.BootArtifacts["/boot/initrd.img-"+snapshot.Host.Kernel] = intent.Ready.InitrdSHA256
	choice.Decision.ReviewedSHA256, _ = choice.Review.Digest()
	intent.InstallerSHA256 = choice.Review.InstallerSHA256
	intent.Offline.PowerLossGuard = true
	intent.Offline.RecoveryChoiceSHA256 = admissionDigest(nativeBootJSON(choice))
	record := nativePrerequisiteRecord{SchemaVersion: 1, RecoveryChoiceSHA256: intent.Offline.RecoveryChoiceSHA256,
		OperationID: choice.Review.Release.Source.OperationID, ReleaseSHA256: admissionDigest(nativeBootJSON(choice.Review.Release)),
		InstallerSHA256: choice.Review.InstallerSHA256, Phase: "complete", Before: *snapshot, Planned: map[string]string{}, After: snapshot}
	intent.Offline.PrerequisitesSHA256 = admissionDigest(nativeBootJSON(record))
	intent.BootIntentSHA256, _ = intent.Offline.Digest()
	digest, err := intent.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest := NativeReleaseManifest{SchemaVersion: 1, Release: choice.Review.Release, Host: snapshot.Host, BootSHA256: digest}
	return choice, manifest, intent, record
}

func TestNativeRecoveryDecisionRequiresBothExplicitAssertionsAndExactReview(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "no-data-unconfirmed", "risk-unconfirmed", "backup", "yes-only", "stale", "uppercase", "wrong-version"} {
		t.Run(scenario, func(t *testing.T) {
			choice := testRecoveryChoice()
			switch scenario {
			case "missing":
				choice.Decision = NativeRecoveryDecision{}
			case "no-data-unconfirmed":
				choice.Decision.NoDataToRetain = false
			case "risk-unconfirmed":
				choice.Decision.AcceptProviderReinstallationRisk = false
			case "backup":
				choice.Decision.Mode = "external-backup"
			case "yes-only":
				choice.Decision = NativeRecoveryDecision{AcceptProviderReinstallationRisk: true}
			case "stale":
				choice.Decision.ReviewedSHA256 = strings.Repeat("e", 64)
			case "uppercase":
				choice.Decision.ReviewedSHA256 = strings.ToUpper(choice.Decision.ReviewedSHA256)
			case "wrong-version":
				choice.SchemaVersion = 2
			}
			if (choice.validate() == nil) != (scenario == "valid") {
				t.Fatal("incorrect consent validation", scenario)
			}
		})
	}
}

func TestNativeRecoveryReviewBindsHostReleasePackagesBootAndPolicy(t *testing.T) {
	for _, edit := range []func(*NativeRecoveryReview){
		func(r *NativeRecoveryReview) { r.Release.Source.OperationID = "30000000-0000-4000-8000-000000000077" },
		func(r *NativeRecoveryReview) { r.Release.ArchiveSHA256 = strings.Repeat("e", 64) },
		func(r *NativeRecoveryReview) { r.InstallerSHA256 = strings.Repeat("e", 64) },
		func(r *NativeRecoveryReview) { r.Snapshot.Host.MachineID = "30000000-0000-4000-8000-000000000077" },
		func(r *NativeRecoveryReview) { r.Snapshot.Host.RootUUID = "30000000-0000-4000-8000-000000000077" },
		func(r *NativeRecoveryReview) { r.Snapshot.Host.PartitionUUID = "30000000-0000-4000-8000-000000000077" },
		func(r *NativeRecoveryReview) { r.Snapshot.Host.BootID = "30000000-0000-4000-8000-000000000077" },
		func(r *NativeRecoveryReview) { r.Snapshot.SecureBoot = "disabled" },
		func(r *NativeRecoveryReview) { r.Snapshot.PackagesSHA256 = strings.Repeat("e", 64) },
		func(r *NativeRecoveryReview) { r.Snapshot.Blocks = "9000000" },
		func(r *NativeRecoveryReview) { r.Snapshot.BootArtifacts["/etc/fstab"] = strings.Repeat("e", 64) },
		func(r *NativeRecoveryReview) {
			r.Snapshot.BootArtifacts["/boot/grub/grub.cfg"] = strings.Repeat("e", 64)
		},
		func(r *NativeRecoveryReview) { r.SchemaVersion = 2 },
		func(r *NativeRecoveryReview) { r.PolicyVersion = "future-policy" },
		func(r *NativeRecoveryReview) { r.Snapshot.Host.Kernel = "unsafe\n" },
		func(r *NativeRecoveryReview) {
			old := r.Snapshot.Host.Kernel
			r.Snapshot.Host.Kernel = "invalid-kernel"
			for _, prefix := range []string{"/boot/vmlinuz-", "/boot/initrd.img-"} {
				r.Snapshot.BootArtifacts[prefix+r.Snapshot.Host.Kernel] = r.Snapshot.BootArtifacts[prefix+old]
				delete(r.Snapshot.BootArtifacts, prefix+old)
			}
		},
		func(r *NativeRecoveryReview) { r.Snapshot.BlockSize = "bad" },
		func(r *NativeRecoveryReview) { r.Snapshot.Host.RootUUID = "bad" },
		func(r *NativeRecoveryReview) { r.Snapshot.BootArtifacts = nil },
	} {
		choice := testRecoveryChoice()
		edit(&choice.Review)
		if choice.validate() == nil {
			t.Fatal("changed review retained consent")
		}
	}
}

func TestNativeRecoveryChoiceSealedThroughPrerequisitesAndBoot(t *testing.T) {
	choice, manifest, intent, record := testRecoveryBoot(t)
	data := nativeBootJSON(choice)
	if checkNativeRecoveryChoiceData(data, manifest, intent) != nil || checkNativeRecoveryPrerequisites(data, record) != nil {
		t.Fatal("valid binding rejected")
	}
	plan, err := manifest.Plan()
	if err != nil || !recoveryChoiceMatchesPlan(choice, plan) {
		t.Fatal("valid plan rejected", err)
	}
	plan.RootUUID = "30000000-0000-4000-8000-000000000077"
	if recoveryChoiceMatchesPlan(choice, plan) {
		t.Fatal("foreign root accepted")
	}
	for _, edit := range []func(*NativeRuntimeIntent){
		func(i *NativeRuntimeIntent) { i.Offline.RecoveryChoiceSHA256 = "" },
		func(i *NativeRuntimeIntent) { i.Offline.RecoveryChoiceSHA256 = strings.Repeat("e", 64) },
		func(i *NativeRuntimeIntent) { i.Offline.PrerequisitesSHA256 = "" },
		func(i *NativeRuntimeIntent) { i.Offline.PowerLossGuard = false },
	} {
		_, _, altered, _ := testRecoveryBoot(t)
		edit(&altered)
		if _, err := altered.Digest(); err == nil {
			t.Fatal("unbound/downgraded intent accepted")
		}
	}
	for _, field := range []string{"manifest", "host", "installer", "features", "geometry", "fstab", "kernel", "missing", "malformed"} {
		_, alteredManifest, alteredIntent, _ := testRecoveryBoot(t)
		input := data
		switch field {
		case "manifest":
			alteredManifest.Release.BundleSHA256 = strings.Repeat("e", 64)
		case "host":
			alteredManifest.Host.RootUUID = "30000000-0000-4000-8000-000000000077"
		case "installer":
			alteredIntent.InstallerSHA256 = strings.Repeat("e", 64)
		case "features":
			alteredIntent.Ready.Features += " quota"
		case "geometry":
			alteredIntent.Ready.Blocks = "9000000"
		case "fstab":
			alteredIntent.Offline.FstabBefore += "# drift\n"
		case "kernel":
			alteredIntent.Ready.KernelSHA256 = strings.Repeat("e", 64)
		case "missing":
			input = nil
		case "malformed":
			input = []byte("{")
		}
		if checkNativeRecoveryChoiceData(input, alteredManifest, alteredIntent) == nil {
			t.Fatal("changed boot accepted", field)
		}
	}
	for _, edit := range []func(*nativePrerequisiteRecord){
		func(r *nativePrerequisiteRecord) { r.RecoveryChoiceSHA256 = "" },
		func(r *nativePrerequisiteRecord) { r.RecoveryChoiceSHA256 = strings.Repeat("e", 64) },
		func(r *nativePrerequisiteRecord) { r.OperationID = "30000000-0000-4000-8000-000000000077" },
		func(r *nativePrerequisiteRecord) { r.ReleaseSHA256 = strings.Repeat("e", 64) },
		func(r *nativePrerequisiteRecord) { r.InstallerSHA256 = strings.Repeat("e", 64) },
		func(r *nativePrerequisiteRecord) { r.Before.PackagesSHA256 = strings.Repeat("e", 64) },
	} {
		altered := record
		edit(&altered)
		if checkNativeRecoveryPrerequisites(data, altered) == nil {
			t.Fatal("foreign prerequisite accepted")
		}
	}
	legacy := testBootIntent()
	before := nativeBootJSON(legacy)
	if _, err := legacy.Digest(); err != nil || !bytes.Equal(before, nativeBootJSON(legacy)) || bytes.Contains(before, []byte("recoveryChoice")) {
		t.Fatal("legacy intent silently changed", err)
	}
}

func TestNativeRecoveryChoiceAdviceCannotGrantRecoveryAuthority(t *testing.T) {
	choice := testRecoveryChoice()
	for _, scenario := range []string{"valid", "invalid-mode", "invalid-hash", "foreign-operation"} {
		status := recoverySnapshot()
		status.RecoveryChoice = &NativeRecoveryChoiceStatus{OperationID: status.Storage.Plan.OperationID, Mode: choice.Decision.Mode,
			RecordSHA256: admissionDigest(nativeBootJSON(choice)), ReviewSHA256: choice.Decision.ReviewedSHA256}
		switch scenario {
		case "invalid-mode":
			status.RecoveryChoice.Mode = "restore"
		case "invalid-hash":
			status.RecoveryChoice.RecordSHA256 = "invalid"
		case "foreign-operation":
			status.RecoveryChoice.OperationID = "30000000-0000-4000-8000-000000000077"
		}
		plan := AssessNativeRecovery(status, nil)
		if plan.InspectionComplete != (scenario == "valid") || plan.DestructiveActionsAuthorized || plan.BackupVerified || plan.AutomaticRecoveryEnabled {
			t.Fatal(scenario, plan)
		}
		if scenario == "valid" && (plan.Evidence == nil || plan.Evidence.RecoveryMode != NativeRecoveryFreshDisposable || plan.Evidence.RecoveryChoiceSHA256 != status.RecoveryChoice.RecordSHA256) {
			t.Fatal("missing bounded choice evidence")
		}
	}
}
