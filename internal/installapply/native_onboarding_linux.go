// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const nativeOnboardingSourceName = "native-onboarding-source.json"

type nativeOnboardingSourceRecord struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Source        NativeOnboardingSource `json:"source"`
	Release       ReleaseBinding         `json:"release"`
}

func (record nativeOnboardingSourceRecord) validate() error {
	if record.SchemaVersion != 1 || record.Source.Validate() != nil || record.Release.Validate() != nil || record.Source.Origin != record.Release.Policy {
		return errors.New("invalid authenticated onboarding source selection")
	}
	return nil
}

// ReviewNativeOnboarding starts one fresh, authenticated staging attempt. It may
// retain source/evidence and the exact input selection, never consent, packages,
// boot artifacts, services or a reboot. Partial attempts are preserved, not reset.
// The caller must obtain explicit consent to the returned recovery digest.
func ReviewNativeOnboarding(ctx context.Context, source NativeOnboardingSource) (NativeOnboardingReview, error) {
	if err := nativeOnboardingEntry(ctx, source); err != nil {
		return NativeOnboardingReview{}, err
	}
	return newNativeOnboardingCoordinator().review(ctx, source)
}

// PrepareNativeOnboarding reopens only the exact authenticated staging attempt.
// The supplied review is an expectation, not authority: disk receipts, original
// inputs, running inode and live host review are all checked again. This does
// not arm or reboot. A successful return guarantees the source lock is closed.
func PrepareNativeOnboarding(ctx context.Context, request NativeOnboardingRequest, expected NativeOnboardingReview) (NativeOnboardingPrepared, error) {
	if err := request.ValidateReview(expected); err != nil {
		return NativeOnboardingPrepared{}, err
	}
	if err := nativeOnboardingEntry(ctx, request.Source); err != nil {
		return NativeOnboardingPrepared{}, err
	}
	return newNativeOnboardingCoordinator().prepare(ctx, request, expected)
}

// PrepareNativeOnboardingWithSetup binds the acknowledged setup-code hash to
// this exact reviewed operation before preparation. The raw code is never an
// input to this API. Like PrepareNativeOnboarding, it returns after lock closure.
func PrepareNativeOnboardingWithSetup(ctx context.Context, request NativeOnboardingRequest, expected NativeOnboardingReview, setup NativeSetupCommitment) (NativeOnboardingPrepared, error) {
	if err := request.ValidateReview(expected); err != nil {
		return NativeOnboardingPrepared{}, err
	}
	if err := setup.Validate(expected); err != nil {
		return NativeOnboardingPrepared{}, err
	}
	if err := nativeOnboardingEntry(ctx, request.Source); err != nil {
		return NativeOnboardingPrepared{}, err
	}
	return newNativeOnboardingCoordinator().prepareWithSetup(ctx, request, expected, &setup)
}

func nativeOnboardingEntry(ctx context.Context, source NativeOnboardingSource) error {
	if err := source.Validate(); err != nil {
		return err
	}
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 0 {
		return errors.New("native onboarding requires root and an active context")
	}
	return nil
}

// These private dependencies permit control-flow tests without turning a test
// executable or synthetic source into an authenticated installer. Production
// entry points always construct the fixed implementation below, with no flags,
// environment variables, mutable globals or exported injection seam.
type nativeOnboardingStage interface {
	Prepare(context.Context, string, string) (SourcePin, error)
	BindOrigin(context.Context, SourcePin, OriginPolicy, string, string) (ReleaseBinding, error)
	VerifyBinding(context.Context, ReleaseBinding) (Source, error)
	ReviewNativeBootRecovery(context.Context, ReleaseBinding, string) (NativeRecoveryReview, error)
	PrepareNativeBoot(context.Context, ReleaseBinding, string, NativeRecoveryDecision) (NativeReleaseManifest, error)
	saveNativeOnboardingSource(context.Context, NativeOnboardingSource, ReleaseBinding) error
	verifyNativeOnboardingSource(NativeOnboardingSource, ReleaseBinding) error
	saveNativeSetup(context.Context, ReleaseBinding, NativeSetupCommitment) error
	Close() error
}

type nativeOnboardingCoordinator struct {
	inspect   func(context.Context) (NativeHostReport, error)
	open      func() (nativeOnboardingStage, error)
	existing  func() (nativeOnboardingStage, bool, error)
	self      func(context.Context, OriginPolicy) (string, error)
	inputs    func(context.Context, NativeOnboardingSource, ReleaseBinding) error
	operation func() string
}

func newNativeOnboardingCoordinator() nativeOnboardingCoordinator {
	return nativeOnboardingCoordinator{
		inspect: InspectNativeHost,
		open: func() (nativeOnboardingStage, error) {
			return OpenSourceStage()
		},
		existing: func() (nativeOnboardingStage, bool, error) {
			stage, exists, err := openExistingSourceStage()
			if err != nil || !exists {
				return nil, exists, err
			}
			return stage, true, nil
		},
		self: nativeOnboardingSelf, inputs: verifyNativeOnboardingInputs, operation: uuid.NewString,
	}
}

func (coordinator nativeOnboardingCoordinator) review(ctx context.Context, source NativeOnboardingSource) (result NativeOnboardingReview, err error) {
	if source.Validate() != nil || ctx == nil || ctx.Err() != nil {
		return result, errors.New("invalid native onboarding review request")
	}
	running, err := coordinator.self(ctx, source.Origin)
	if err != nil {
		return result, err
	}
	report, err := coordinator.inspect(ctx)
	if err != nil || !report.Eligible || report.Snapshot == nil {
		return result, errors.Join(err, report.failure(), errors.New("native onboarding requires an eligible fresh host"))
	}
	stage, err := coordinator.open()
	if err != nil {
		return result, err
	}
	defer func() {
		err = errors.Join(err, stage.Close())
		if err != nil {
			result = NativeOnboardingReview{}
		}
	}()
	pin, err := stage.Prepare(ctx, source.SourceDirectory, coordinator.operation())
	if err != nil {
		return result, err
	}
	if pin.InstallerSHA256 != running {
		return result, errors.New("running installer differs from staged release")
	}
	binding, err := stage.BindOrigin(ctx, pin, source.Origin, source.ArchivePath, source.AttestationPath)
	if err != nil {
		return result, err
	}
	retained, err := stage.VerifyBinding(ctx, binding)
	if err != nil {
		return result, err
	}
	if err := coordinator.inputs(ctx, source, binding); err != nil {
		return result, err
	}
	if err := stage.saveNativeOnboardingSource(ctx, source, binding); err != nil {
		return result, err
	}
	recovery, err := stage.ReviewNativeBootRecovery(ctx, binding, filepath.Join(retained.Root, "bin", "stackfort-installer"))
	if err != nil {
		return result, err
	}
	result = NativeOnboardingReview{SchemaVersion: 1, Source: source, Recovery: recovery}
	if err := result.Validate(); err != nil {
		return NativeOnboardingReview{}, err
	}
	return result, ctx.Err()
}

func (coordinator nativeOnboardingCoordinator) prepare(ctx context.Context, request NativeOnboardingRequest, expected NativeOnboardingReview) (result NativeOnboardingPrepared, err error) {
	return coordinator.prepareWithSetup(ctx, request, expected, nil)
}

func (coordinator nativeOnboardingCoordinator) prepareWithSetup(ctx context.Context, request NativeOnboardingRequest, expected NativeOnboardingReview, setup *NativeSetupCommitment) (result NativeOnboardingPrepared, err error) {
	if err := request.ValidateReview(expected); err != nil {
		return result, err
	}
	if setup != nil {
		if err := setup.Validate(expected); err != nil {
			return result, err
		}
	}
	if ctx == nil || ctx.Err() != nil {
		return result, errors.New("native onboarding requires an active context")
	}
	running, err := coordinator.self(ctx, request.Source.Origin)
	if err != nil || running != expected.Recovery.Release.Source.InstallerSHA256 {
		return result, errors.Join(err, errors.New("running installer differs from authenticated release"))
	}
	stage, exists, err := coordinator.existing()
	if err != nil || !exists {
		return result, errors.Join(err, errors.New("no matching authenticated onboarding source exists"))
	}
	var manifest NativeReleaseManifest
	defer func() {
		err = errors.Join(err, stage.Close())
		if err == nil {
			result = NativeOnboardingPrepared{OperationID: manifest.Release.Source.OperationID,
				RuntimePath: NativeRuntimePath, InstallerSHA256: manifest.Release.Source.InstallerSHA256, Manifest: manifest}
		} else {
			result = NativeOnboardingPrepared{}
		}
	}()
	binding := expected.Recovery.Release
	if err := stage.verifyNativeOnboardingSource(request.Source, binding); err != nil {
		return result, err
	}
	retained, err := stage.VerifyBinding(ctx, binding)
	if err != nil {
		return result, err
	}
	if err := coordinator.inputs(ctx, request.Source, binding); err != nil {
		return result, err
	}
	dispatcher := filepath.Join(retained.Root, "bin", "stackfort-installer")
	recovery, err := stage.ReviewNativeBootRecovery(ctx, binding, dispatcher)
	if err != nil {
		return result, err
	}
	fresh := NativeOnboardingReview{SchemaVersion: 1, Source: request.Source, Recovery: recovery}
	if err := request.ValidateReview(fresh); err != nil {
		return result, err
	}
	// Verify the running inode again immediately before prerequisite/boot writes.
	running, err = coordinator.self(ctx, request.Source.Origin)
	if err != nil || running != binding.Source.InstallerSHA256 {
		return result, errors.Join(err, errors.New("running installer changed before native preparation"))
	}
	if setup != nil {
		if err := setup.Validate(fresh); err != nil {
			return result, err
		}
		if err := stage.saveNativeSetup(ctx, binding, *setup); err != nil {
			return result, err
		}
	}
	manifest, err = stage.PrepareNativeBoot(ctx, binding, dispatcher, request.Decision)
	if err != nil {
		return result, err
	}
	if _, err := manifest.Plan(); err != nil || manifest.Release != binding || manifest.Host != recovery.Snapshot.Host {
		return result, errors.Join(err, errors.New("prepared runtime differs from authenticated onboarding review"))
	}
	return result, ctx.Err()
}

func nativeOnboardingSelf(ctx context.Context, policy OriginPolicy) (string, error) {
	build := buildinfo.Current()
	if ctx == nil || ctx.Err() != nil || policy.Class != "tag-release" || policy.Validate() != nil || build.Version != policy.Version || build.Commit != policy.Commit {
		return "", errors.New("native onboarding requires the exact tagged installer build")
	}
	// The magic link selects the already-running executable inode, not an input
	// path which could have been replaced after exec. It is never executed here.
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", err
	}
	defer running.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(running.Fd()), &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7022 != 0 || stat.Mode&0o100 == 0 || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
		return "", errors.New("running native installer has unsafe metadata")
	}
	digest, err := originFileDigest(running, 64<<20)
	if err != nil {
		return "", err
	}
	return digest, ctx.Err()
}

func verifyNativeOnboardingInputs(ctx context.Context, selection NativeOnboardingSource, binding ReleaseBinding) error {
	if ctx == nil || ctx.Err() != nil || selection.Validate() != nil || binding.Validate() != nil || selection.Origin != binding.Policy {
		return errors.New("invalid native onboarding input binding")
	}
	_, current, err := inspectResumeSource(ctx, selection.SourceDirectory, binding.Source.OperationID)
	if err != nil || current != binding.Source {
		return errors.Join(err, errors.New("original onboarding source changed"))
	}
	for _, input := range []struct {
		path, digest string
		maximum      int64
	}{{selection.ArchivePath, binding.ArchiveSHA256, maximumSourceBytes}, {selection.AttestationPath, binding.BundleSHA256, maximumOriginBundle}} {
		dir, err := openSourceDirectory(filepath.Dir(input.path), true)
		if err != nil {
			return err
		}
		file, err := openResumeFile(dir, filepath.Base(input.path), input.maximum)
		_ = unix.Close(dir)
		if err != nil {
			return err
		}
		digest, readErr := originFileDigest(file, input.maximum)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || digest != input.digest {
			return errors.Join(readErr, closeErr, errors.New("original onboarding evidence changed"))
		}
	}
	return ctx.Err()
}

func (stage *SourceStage) saveNativeOnboardingSource(ctx context.Context, selection NativeOnboardingSource, binding ReleaseBinding) error {
	record := nativeOnboardingSourceRecord{SchemaVersion: 1, Source: selection, Release: binding}
	if stage.check() != nil || stage.journal == nil || record.validate() != nil || ctx == nil || ctx.Err() != nil {
		return errors.New("onboarding source selection requires an active authenticated stage")
	}
	if _, exists, err := stage.journal.Load(); err != nil || exists {
		return errors.Join(err, errors.New("onboarding selection must precede storage preparation"))
	}
	if err := checkNativeOrphans(stage.dir); err != nil {
		return err
	}
	if _, err := stage.VerifyBinding(ctx, binding); err != nil {
		return err
	}
	return writeOriginRecord(stage.dir, nativeOnboardingSourceName, nativeBootJSON(record))
}

func (stage *SourceStage) verifyNativeOnboardingSource(selection NativeOnboardingSource, binding ReleaseBinding) error {
	record := nativeOnboardingSourceRecord{SchemaVersion: 1, Source: selection, Release: binding}
	if stage.check() != nil || stage.journal == nil || record.validate() != nil {
		return errors.New("invalid authenticated onboarding source selection")
	}
	return equalOriginRecord(stage.dir, nativeOnboardingSourceName, nativeBootJSON(record))
}
