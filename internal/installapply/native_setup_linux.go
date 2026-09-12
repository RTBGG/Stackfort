// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"errors"
)

func (stage *SourceStage) saveNativeSetup(ctx context.Context, binding ReleaseBinding, commitment NativeSetupCommitment) error {
	if ctx == nil || ctx.Err() != nil || stage.check() != nil || stage.journal == nil || commitment.validateBinding(binding) != nil {
		return errors.New("setup commitment requires the exact locked authenticated installation")
	}
	if _, exists, err := stage.journal.Load(); err != nil || exists {
		return errors.Join(err, errors.New("setup commitment cannot be added to an existing storage operation"))
	}
	if _, err := stage.VerifyBinding(ctx, binding); err != nil {
		return err
	}
	data, exists, err := admissionReadAt(stage.dir, nativeOnboardingSourceName)
	if err != nil || !exists {
		return errors.Join(err, errors.New("setup requires authenticated onboarding selection"))
	}
	var selection nativeOnboardingSourceRecord
	if err := nativeBootDecode(data, &selection); err != nil {
		return err
	}
	if selection.validate() != nil || selection.Release != binding {
		return errors.New("setup source selection differs")
	}
	// No overwrite/reissue after an uncertain attempt, even for identical input.
	return writeOriginRecord(stage.dir, nativeSetupName, nativeBootJSON(commitment))
}

func decodeNativeSetup(data []byte, binding ReleaseBinding) (NativeSetupCommitment, error) {
	var commitment NativeSetupCommitment
	if err := nativeBootDecode(data, &commitment); err != nil {
		return commitment, err
	}
	if !bytes.Equal(data, nativeBootJSON(commitment)) {
		return NativeSetupCommitment{}, errors.New("noncanonical setup commitment")
	}
	if err := commitment.validateBinding(binding); err != nil {
		return NativeSetupCommitment{}, err
	}
	return commitment, nil
}

func (stage *SourceStage) nativeSetupDigest(binding ReleaseBinding) (string, error) {
	if stage.check() != nil || stage.journal == nil {
		return "", errors.New("setup inspection requires locked source")
	}
	if _, _, err := stage.journal.Load(); err != nil {
		return "", err
	}
	data, exists, err := admissionReadAt(stage.dir, nativeSetupName)
	if err != nil || !exists {
		return "", err
	}
	if _, err := decodeNativeSetup(data, binding); err != nil {
		return "", err
	}
	return admissionDigest(data), nil
}

func (stage *SourceStage) verifyNativeSetupBinding(manifest NativeReleaseManifest, intent NativeRuntimeIntent) error {
	if manifest.Release.Policy.Class == "tag-release" && intent.SetupSHA256 == "" {
		return errors.New("tagged native runtime requires a sealed setup commitment")
	}
	digest, err := stage.nativeSetupDigest(manifest.Release)
	if err != nil || digest != intent.SetupSHA256 {
		return errors.Join(err, errors.New("sealed setup commitment changed"))
	}
	return nil
}
