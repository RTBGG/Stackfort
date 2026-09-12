// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Flow doubles never claim signature verification or execute release fixtures.
// The public entry points cannot receive these private dependencies.
type onboardingStageDouble struct {
	t        *testing.T
	request  NativeOnboardingRequest
	review   NativeOnboardingReview
	events   []string
	fail     string
	drift    string
	closed   bool
	prepared bool
	selfRuns int
}

func (stage *onboardingStageDouble) event(name string) error {
	stage.events = append(stage.events, name)
	if stage.closed {
		stage.t.Fatal("stage used after closing", name)
	}
	if stage.fail == name {
		return errors.New("injected " + name)
	}
	return nil
}

func (stage *onboardingStageDouble) Prepare(_ context.Context, root, operation string) (SourcePin, error) {
	if root != stage.request.Source.SourceDirectory || operation != sourceOperation {
		stage.t.Fatal("source preparation rebound inputs")
	}
	return stage.review.Recovery.Release.Source, stage.event("stage-source")
}

func (stage *onboardingStageDouble) BindOrigin(_ context.Context, pin SourcePin, policy OriginPolicy, archive, bundle string) (ReleaseBinding, error) {
	if pin != stage.review.Recovery.Release.Source || policy != stage.request.Source.Origin || archive != stage.request.Source.ArchivePath || bundle != stage.request.Source.AttestationPath {
		stage.t.Fatal("origin verification rebound inputs")
	}
	return stage.review.Recovery.Release, stage.event("authenticate")
}

func (stage *onboardingStageDouble) VerifyBinding(_ context.Context, binding ReleaseBinding) (Source, error) {
	if binding != stage.review.Recovery.Release {
		stage.t.Fatal("retained source rebound")
	}
	return Source{Root: "/var/lib/stackfort-installer/resume-source/source"}, stage.event("verify-retained")
}

func (stage *onboardingStageDouble) ReviewNativeBootRecovery(_ context.Context, binding ReleaseBinding, dispatcher string) (NativeRecoveryReview, error) {
	if binding != stage.review.Recovery.Release || dispatcher != "/var/lib/stackfort-installer/resume-source/source/bin/stackfort-installer" {
		stage.t.Fatal("review used an arbitrary dispatcher or source")
	}
	review := stage.review.Recovery
	if stage.drift == "host-review" {
		review.Snapshot.PackagesSHA256 = strings.Repeat("f", 64)
	}
	return review, stage.event("review-host")
}

func (stage *onboardingStageDouble) PrepareNativeBoot(_ context.Context, binding ReleaseBinding, dispatcher string, decision NativeRecoveryDecision) (NativeReleaseManifest, error) {
	if binding != stage.review.Recovery.Release || dispatcher != "/var/lib/stackfort-installer/resume-source/source/bin/stackfort-installer" || decision != stage.request.Decision {
		stage.t.Fatal("preparation did not receive exact authenticated consent")
	}
	stage.prepared = true
	manifest := NativeReleaseManifest{SchemaVersion: 1, Release: binding, Host: stage.review.Recovery.Snapshot.Host, BootSHA256: strings.Repeat("e", 64)}
	if stage.drift == "sealed-manifest" {
		manifest.Release.Source.OperationID = "11111111-1111-4111-8111-111111111111"
	}
	return manifest, stage.event("prepare-boot")
}

func (stage *onboardingStageDouble) saveNativeOnboardingSource(_ context.Context, source NativeOnboardingSource, binding ReleaseBinding) error {
	if source != stage.request.Source || binding != stage.review.Recovery.Release {
		stage.t.Fatal("saved selection differs")
	}
	return stage.event("save-selection")
}

func (stage *onboardingStageDouble) verifyNativeOnboardingSource(source NativeOnboardingSource, binding ReleaseBinding) error {
	if source != stage.request.Source || binding != stage.review.Recovery.Release {
		stage.t.Fatal("verified selection differs")
	}
	return stage.event("verify-selection")
}

func (stage *onboardingStageDouble) saveNativeSetup(_ context.Context, binding ReleaseBinding, commitment NativeSetupCommitment) error {
	if binding != stage.review.Recovery.Release || commitment.Validate(stage.review) != nil {
		stage.t.Fatal("setup commitment rebound")
	}
	return stage.event("save-setup")
}

func (stage *onboardingStageDouble) Close() error {
	err := stage.event("close")
	stage.closed = true
	return err
}

func onboardingFlowFixture(t *testing.T) (nativeOnboardingCoordinator, *onboardingStageDouble) {
	t.Helper()
	request, review := testNativeOnboarding(t)
	stage := &onboardingStageDouble{t: t, request: request, review: review}
	coordinator := nativeOnboardingCoordinator{
		inspect: func(context.Context) (NativeHostReport, error) {
			return NativeHostReport{Eligible: stage.drift != "ineligible-host", Snapshot: &review.Recovery.Snapshot}, stage.event("inspect-fresh")
		},
		open: func() (nativeOnboardingStage, error) { return stage, stage.event("open-new") },
		existing: func() (nativeOnboardingStage, bool, error) {
			return stage, stage.drift != "missing-stage", stage.event("open-existing")
		},
		self: func(_ context.Context, policy OriginPolicy) (string, error) {
			if policy != request.Source.Origin {
				t.Fatal("self check used another build identity")
			}
			stage.selfRuns++
			name := "self-before"
			if stage.selfRuns > 1 {
				name = "self-before-mutation"
			}
			digest := review.Recovery.Release.Source.InstallerSHA256
			if stage.drift == "self" || (stage.drift == "self-later" && stage.selfRuns > 1) {
				digest = strings.Repeat("f", 64)
			}
			return digest, stage.event(name)
		},
		inputs: func(_ context.Context, source NativeOnboardingSource, binding ReleaseBinding) error {
			if source != request.Source || binding != review.Recovery.Release {
				t.Fatal("original source verification rebound")
			}
			return stage.event("verify-originals")
		},
		operation: func() string { return sourceOperation },
	}
	return coordinator, stage
}

func TestNativeOnboardingReviewStagesOnlyAuthenticatedSource(t *testing.T) {
	sequence := []string{"self-before", "inspect-fresh", "open-new", "stage-source", "authenticate", "verify-retained", "verify-originals", "save-selection", "review-host", "close"}
	for _, failure := range append([]string{""}, sequence...) {
		t.Run(failure, func(t *testing.T) {
			coordinator, stage := onboardingFlowFixture(t)
			stage.fail = failure
			review, err := coordinator.review(t.Context(), stage.request.Source)
			if stage.prepared {
				t.Fatal("review prepared packages or boot")
			}
			if failure == "" {
				if err != nil || !reflect.DeepEqual(review, stage.review) || !stage.closed || !reflect.DeepEqual(sequence, stage.events) {
					t.Fatal(review, err, stage.events)
				}
			} else if err == nil || !reflect.DeepEqual(review, NativeOnboardingReview{}) {
				t.Fatal("failed review returned usable result", review, err)
			}
			if len(stage.events) > 3 && !stage.closed {
				t.Fatal("source lock not closed after failure", stage.events)
			}
		})
	}
	for _, drift := range []string{"ineligible-host", "self"} {
		coordinator, stage := onboardingFlowFixture(t)
		stage.drift = drift
		if _, err := coordinator.review(t.Context(), stage.request.Source); err == nil || stage.prepared {
			t.Fatal("invalid review admitted", drift, err)
		}
	}
}

func TestNativeOnboardingPreparationRevalidatesBeforeMutationAndClosesBeforeResult(t *testing.T) {
	sequence := []string{"self-before", "open-existing", "verify-selection", "verify-retained", "verify-originals", "review-host", "self-before-mutation", "prepare-boot", "close"}
	for _, failure := range append([]string{""}, sequence...) {
		t.Run(failure, func(t *testing.T) {
			coordinator, stage := onboardingFlowFixture(t)
			stage.fail = failure
			result, err := coordinator.prepare(t.Context(), stage.request, stage.review)
			if failure == "" {
				if err != nil || result.OperationID != sourceOperation || result.RuntimePath != NativeRuntimePath || result.InstallerSHA256 != stage.review.Recovery.Release.Source.InstallerSHA256 || !stage.closed || !reflect.DeepEqual(sequence, stage.events) {
					t.Fatal(result, err, stage.events)
				}
			} else if err == nil || result != (NativeOnboardingPrepared{}) {
				t.Fatal("failed preparation returned sealed result", result, err)
			}
			if failure != "" && failure != "prepare-boot" && failure != "close" && stage.prepared {
				t.Fatal("host mutation preceded validation", stage.events)
			}
			if len(stage.events) > 2 && !stage.closed {
				t.Fatal("source lock left open", stage.events)
			}
		})
	}
	for _, drift := range []string{"self", "self-later", "missing-stage", "host-review", "sealed-manifest"} {
		t.Run(drift, func(t *testing.T) {
			coordinator, stage := onboardingFlowFixture(t)
			stage.drift = drift
			result, err := coordinator.prepare(t.Context(), stage.request, stage.review)
			if err == nil || result != (NativeOnboardingPrepared{}) || (drift != "sealed-manifest" && stage.prepared) {
				t.Fatal("drift admitted", result, err, stage.events)
			}
		})
	}
}

func TestNativeOnboardingRejectsInvalidRequestBeforeOpeningState(t *testing.T) {
	for _, scenario := range []string{"nil-context", "cancelled", "no-consent", "no-reboot", "lab-policy", "stale-review"} {
		t.Run(scenario, func(t *testing.T) {
			coordinator, stage := onboardingFlowFixture(t)
			ctx := t.Context()
			switch scenario {
			case "nil-context":
				ctx = nil
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "no-consent":
				stage.request.Decision.NoDataToRetain = false
			case "no-reboot":
				stage.request.AcceptReboot = false
			case "lab-policy":
				stage.request.Source.Origin.Class = "lab-candidate"
			case "stale-review":
				stage.request.Decision.ReviewedSHA256 = strings.Repeat("f", 64)
			}
			if result, err := coordinator.prepare(ctx, stage.request, stage.review); err == nil || result != (NativeOnboardingPrepared{}) || len(stage.events) != 0 {
				t.Fatal("invalid request reached state", result, err, stage.events)
			}
			if _, err := PrepareNativeOnboarding(ctx, stage.request, stage.review); err == nil {
				t.Fatal("public library entry accepted invalid request")
			}
		})
	}
	if _, err := ReviewNativeOnboarding(t.Context(), NativeOnboardingSource{}); err == nil {
		t.Fatal("invalid source accepted")
	}
	if _, err := nativeOnboardingSelf(t.Context(), OriginPolicy{Class: "lab-candidate", Version: "0.1.0-beta.3", Commit: "5282946bec1f865de7222128a6a5d0d8a656f34c"}); err == nil {
		t.Fatal("public running-inode check accepted lab policy")
	}
}

func TestNativeOnboardingSetupIsSavedAfterRevalidationBeforePreparation(t *testing.T) {
	sequence := []string{"self-before", "open-existing", "verify-selection", "verify-retained", "verify-originals", "review-host", "self-before-mutation", "save-setup", "prepare-boot", "close"}
	for _, failure := range append([]string{""}, sequence...) {
		t.Run(failure, func(t *testing.T) {
			coordinator, stage := onboardingFlowFixture(t)
			stage.fail = failure
			_, setup, err := IssueNativeSetup(stage.review)
			if err != nil {
				t.Fatal(err)
			}
			result, err := coordinator.prepareWithSetup(t.Context(), stage.request, stage.review, &setup)
			if failure == "" {
				if err != nil || !stage.closed || !stage.prepared || result.OperationID != sourceOperation || !reflect.DeepEqual(sequence, stage.events) {
					t.Fatal(result, err, stage.events)
				}
			} else if err == nil || result != (NativeOnboardingPrepared{}) {
				t.Fatal("failed setup preparation returned usable runtime", result, err)
			}
			if failure != "" && failure != "prepare-boot" && failure != "close" && stage.prepared {
				t.Fatal("preparation preceded setup validation", stage.events)
			}
		})
	}
	for _, scenario := range []string{"invalid-setup", "wrong-operation", "host-drift", "self-drift"} {
		coordinator, stage := onboardingFlowFixture(t)
		_, setup, err := IssueNativeSetup(stage.review)
		if err != nil {
			t.Fatal(err)
		}
		switch scenario {
		case "invalid-setup":
			setup.TokenSHA256 = "invalid"
		case "wrong-operation":
			setup.OperationID = "11111111-1111-4111-8111-111111111111"
		case "host-drift":
			stage.drift = "host-review"
		case "self-drift":
			stage.drift = "self-later"
		}
		result, err := coordinator.prepareWithSetup(t.Context(), stage.request, stage.review, &setup)
		if err == nil || result != (NativeOnboardingPrepared{}) || stage.prepared || strings.Contains(strings.Join(stage.events, ","), "save-setup") {
			t.Fatal("invalid setup or stale review persisted", scenario, result, err, stage.events)
		}
	}
}

func TestNativeOnboardingOriginalInputsMustRemainExact(t *testing.T) {
	for _, scenario := range []string{"valid", "source", "source-mode", "archive", "attestations", "archive-link", "attestation-missing", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root := resumeSourceFixture(t)
			_, pin, err := inspectResumeSource(t.Context(), root, sourceOperation)
			if err != nil {
				t.Fatal(err)
			}
			evidence := t.TempDir()
			archive, bundle := []byte("synthetic archive data only"), []byte("synthetic bundle data only")
			selection := NativeOnboardingSource{SourceDirectory: root, ArchivePath: filepath.Join(evidence, "archive.tar.gz"),
				AttestationPath: filepath.Join(evidence, "attestations.jsonl"), Origin: OriginPolicy{"tag-release", pin.Version, strings.Repeat("a", 40)}}
			for path, data := range map[string][]byte{selection.ArchivePath: archive, selection.AttestationPath: bundle} {
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			binding := ReleaseBinding{1, pin, selection.Origin, admissionDigest(archive), admissionDigest(bundle), originVerifierSHA256}
			ctx := t.Context()
			switch scenario {
			case "source":
				err = os.WriteFile(filepath.Join(root, "README.md"), []byte("changed"), 0o644)
			case "source-mode":
				err = os.Chmod(filepath.Join(root, "README.md"), 0o600)
			case "archive":
				err = os.WriteFile(selection.ArchivePath, []byte("changed"), 0o600)
			case "attestations":
				err = os.WriteFile(selection.AttestationPath, []byte("changed"), 0o600)
			case "archive-link":
				if err := os.Remove(selection.ArchivePath); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(selection.AttestationPath, selection.ArchivePath)
			case "attestation-missing":
				err = os.Remove(selection.AttestationPath)
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if err != nil {
				t.Fatal(err)
			}
			err = verifyNativeOnboardingInputs(ctx, selection, binding)
			if (err == nil) != (scenario == "valid") {
				t.Fatal("original input validation", scenario, err)
			}
		})
	}
}

func TestNativeOnboardingSelectionRecordRejectsDifferentOriginAndUnlockedStage(t *testing.T) {
	request, review := testNativeOnboarding(t)
	record := nativeOnboardingSourceRecord{1, request.Source, review.Recovery.Release}
	if err := record.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*nativeOnboardingSourceRecord){
		func(r *nativeOnboardingSourceRecord) { r.SchemaVersion = 2 },
		func(r *nativeOnboardingSourceRecord) { r.Source.Origin.Commit = strings.Repeat("f", 40) },
		func(r *nativeOnboardingSourceRecord) { r.Release.ArchiveSHA256 = "invalid" },
		func(r *nativeOnboardingSourceRecord) { r.Release.Policy.Class = "lab-candidate" },
	} {
		changed := record
		mutate(&changed)
		if err := changed.validate(); err == nil {
			t.Fatal("invalid selection record accepted")
		}
	}
	for _, stage := range []*SourceStage{nil, {}, {closed: true}} {
		if err := stage.saveNativeOnboardingSource(t.Context(), request.Source, review.Recovery.Release); err == nil {
			t.Fatal("selection writer accepted unlocked stage")
		}
		if err := stage.verifyNativeOnboardingSource(request.Source, review.Recovery.Release); err == nil {
			t.Fatal("selection reader accepted unlocked stage")
		}
	}
}
