// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// InspectCompletedNativeOnboarding is read-only except acquiring the existing
// shared lock. New download paths are permitted only for byte-identical release
// inputs. False means genuinely empty state, never a partial native attempt.
// A true result is only a closed-lock handoff to the sealed live supervisor.
func InspectCompletedNativeOnboarding(ctx context.Context, selection NativeOnboardingSource) (NativeOnboardingPrepared, bool, error) {
	if err := nativeOnboardingEntry(ctx, selection); err != nil {
		return NativeOnboardingPrepared{}, false, err
	}
	return newNativeCompletedCoordinator().inspect(ctx, selection)
}

type nativeCompletedStage interface {
	completedNativeManifest(context.Context) (NativeReleaseManifest, bool, error)
	Close() error
}

type nativeCompletedCoordinator struct {
	existing func() (nativeCompletedStage, bool, error)
	self     func(context.Context, OriginPolicy) (string, error)
	inputs   func(context.Context, NativeOnboardingSource, ReleaseBinding) error
}

func newNativeCompletedCoordinator() nativeCompletedCoordinator {
	return nativeCompletedCoordinator{existing: func() (nativeCompletedStage, bool, error) {
		stage, exists, err := openExistingSourceStage()
		if err != nil || !exists {
			return nil, exists, err
		}
		return stage, true, nil
	}, self: nativeOnboardingSelf, inputs: verifyNativeOnboardingInputs}
}

func (coordinator nativeCompletedCoordinator) inspect(ctx context.Context, selection NativeOnboardingSource) (result NativeOnboardingPrepared, complete bool, err error) {
	if ctx == nil || ctx.Err() != nil || selection.Validate() != nil {
		return result, false, errors.New("invalid completed native inspection")
	}
	running, err := coordinator.self(ctx, selection.Origin)
	if err != nil {
		return result, false, err
	}
	stage, exists, err := coordinator.existing()
	if err != nil || !exists {
		return result, false, err
	}
	var manifest NativeReleaseManifest
	defer func() {
		err = errors.Join(err, stage.Close())
		if err != nil {
			result, complete = NativeOnboardingPrepared{}, false
			return
		}
		if complete {
			result = NativeOnboardingPrepared{OperationID: manifest.Release.Source.OperationID, RuntimePath: NativeRuntimePath,
				InstallerSHA256: manifest.Release.Source.InstallerSHA256, Manifest: manifest}
		}
	}()
	manifest, complete, err = stage.completedNativeManifest(ctx)
	if err != nil || !complete {
		return result, complete, err
	}
	if _, err := manifest.Plan(); err != nil || manifest.Release.Policy != selection.Origin || running != manifest.Release.Source.InstallerSHA256 {
		return result, false, errors.New("native rerun build differs from the completed authenticated installation")
	}
	if err := coordinator.inputs(ctx, selection, manifest.Release); err != nil {
		return result, false, err
	}
	return result, true, ctx.Err()
}

func (stage *SourceStage) completedNativeManifest(ctx context.Context) (NativeReleaseManifest, bool, error) {
	var empty NativeReleaseManifest
	if ctx == nil || ctx.Err() != nil || stage.check() != nil || stage.journal == nil {
		return empty, false, errors.New("completed native inspection requires existing locked state")
	}
	_, exists, err := stage.journal.Load()
	if err != nil {
		return empty, false, err
	}
	if !exists {
		// Even an unknown incomplete file is evidence, not a fresh-server signal.
		fd, err := unix.Openat(stage.dir, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return empty, false, err
		}
		directory := os.NewFile(uintptr(fd), "native-state")
		names, readErr := directory.Readdirnames(128)
		closeErr := directory.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return empty, false, readErr
		}
		if closeErr != nil {
			return empty, false, closeErr
		}
		for _, name := range names {
			if name != "install.lock" {
				return empty, false, errors.New("existing installer evidence has no complete native journal; preserve it for inspection")
			}
		}
		return empty, false, nil
	}
	report, manifest, err := stage.recordedNative()
	if err != nil {
		return empty, false, err
	}
	if err := validateNativeCompletedStatus(report, manifest); err != nil {
		return empty, false, err
	}
	if _, err := stage.VerifyManifest(ctx, manifest); err != nil {
		return empty, false, err
	}
	data, found, err := admissionReadAt(stage.dir, nativeOnboardingSourceName)
	if err != nil || !found {
		return empty, false, errors.Join(err, errors.New("completed native onboarding selection is missing"))
	}
	var selection nativeOnboardingSourceRecord
	if nativeBootDecode(data, &selection) != nil || selection.validate() != nil || selection.Release != manifest.Release {
		return empty, false, errors.New("completed onboarding selection differs from release")
	}
	data, found, err = admissionReadAt(stage.dir, nativeRuntimeName)
	if err != nil || !found {
		return empty, false, errors.Join(err, errors.New("completed runtime intent missing"))
	}
	var intent NativeRuntimeIntent
	if err := nativeBootDecode(data, &intent); err != nil {
		return empty, false, err
	}
	digest, err := intent.Digest()
	if err != nil || digest != manifest.BootSHA256 || intent.Profile != NativeBootProfile || intent.InstallerSHA256 != manifest.Release.Source.InstallerSHA256 ||
		intent.Offline == nil || !intent.Offline.PowerLossGuard || intent.SetupSHA256 == "" {
		return empty, false, errors.New("completed runtime is not a bound public onboarding runtime")
	}
	if err := stage.verifyNativeDispatcher(ctx, intent, false); err != nil {
		return empty, false, err
	}
	if err := verifyNativePrerequisiteBinding(ctx, manifest, intent); err != nil {
		return empty, false, err
	}
	if err := stage.verifyNativeSetupBinding(manifest, intent); err != nil {
		return empty, false, err
	}
	data, found, err = admissionReadAt(stage.dir, nativeSetupRegistrationName)
	if err != nil || !found {
		return empty, false, errors.Join(err, errors.New("completed setup registration missing; rerun cannot issue or register a new code"))
	}
	var registration nativeSetupRegistration
	if nativeBootDecode(data, &registration) != nil || registration.validate(manifest.Release.Source.OperationID, intent.SetupSHA256) != nil {
		return empty, false, errors.New("completed setup registration differs from sealed commitment")
	}
	return manifest, true, ctx.Err()
}

// Successful cleanup bypass is available only to the fixed systemd unit's
// ExecStopPost process after a normal zero exit, not success-classified signals.
// Missing/foreign context always quarantines; it never grants public admission.
func nativeCompletedCleanupSucceeded(operation, cgroup, result, code, status string) bool {
	unit, err := NativeCompletedRecheckUnit(operation)
	return err == nil && strings.TrimSuffix(cgroup, "\n") == "0::/system.slice/"+unit && result == "success" && code == "exited" && status == "0"
}
