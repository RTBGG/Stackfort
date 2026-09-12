// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

// No boolean from a caller or journal alone enables the normal OS lifecycle.
// Re-read the still-locked source stage on every check. Direct backend users
// without this verified stage retain the original strict conversion-era pins.
func (b nativeReadyBackend) postConversionReady(ctx context.Context) (bool, error) {
	if ctx == nil || ctx.Err() != nil {
		return false, errors.New("native readiness requires an active context")
	}
	if b.sourceStage == nil {
		return false, nil
	}
	stage := b.sourceStage
	if stage.check() != nil || stage.journal == nil {
		return false, errors.New("native readiness source stage is not locked")
	}
	state, exists, err := stage.journal.Load()
	if err != nil || !exists || state.Validate() != nil || state.Plan != b.plan {
		return false, errors.Join(err, errors.New("native readiness journal binding mismatch"))
	}
	if b.intent.Profile != NativeBootProfile || state.Phase != storageprep.Ready {
		return false, nil
	}
	data, exists, err := admissionReadAt(stage.dir, nativeReleaseManifestName)
	if err != nil || !exists {
		return false, errors.Join(err, errors.New("native ready manifest is missing"))
	}
	var manifest NativeReleaseManifest
	if err := nativeBootDecode(data, &manifest); err != nil {
		return false, err
	}
	plan, err := manifest.Plan()
	if err != nil || plan != b.plan || b.intent.Ready != b.spec {
		return false, errors.Join(err, errors.New("native ready manifest/specification mismatch"))
	}
	digest, err := b.intent.Digest()
	if err != nil || digest != manifest.BootSHA256 {
		return false, errors.Join(err, errors.New("native ready runtime intent mismatch"))
	}
	if err := equalOriginRecord(stage.dir, nativeRuntimeName, nativeBootJSON(b.intent)); err != nil {
		return false, err
	}
	if _, err := stage.VerifyManifest(ctx, manifest); err != nil {
		return false, err
	}
	if err := verifyNativePrerequisiteBinding(ctx, manifest, b.intent); err != nil {
		return false, err
	}
	if err := stage.verifyNativeSetupBinding(manifest, b.intent); err != nil {
		return false, err
	}
	data, exists, err = admissionReadAt(stage.dir, "native-boot-completed.json")
	if err != nil || !exists {
		return false, errors.Join(err, errors.New("native verified completion receipt is missing"))
	}
	return nativeReadyCompletionAccepted(state, plan, b.intent, data)
}

func nativeReadyCurrentArtifact(ctx context.Context, path string) error {
	if ctx == nil || ctx.Err() != nil {
		return errors.New("current native OS artifact inspection requires an active context")
	}
	dir, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	file, err := openResumeFile(dir, filepath.Base(path), 512<<20)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return errors.Join(err, errors.New("current native OS boot artifact is empty"))
	}
	return ctx.Err()
}
