// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
)

// ReviewNativeBootRecovery performs read-only inspection under the source lock.
// It neither records consent nor creates prerequisite/boot/recovery-choice files.
func (stage *SourceStage) ReviewNativeBootRecovery(ctx context.Context, binding ReleaseBinding, dispatcher string) (NativeRecoveryReview, error) {
	if ctx == nil || ctx.Err() != nil || stage.check() != nil || stage.journal == nil {
		return NativeRecoveryReview{}, errors.New("recovery review requires an active locked source stage")
	}
	if _, exists, err := stage.journal.Load(); err != nil || exists {
		return NativeRecoveryReview{}, errors.Join(err, errors.New("existing storage operation cannot acquire recovery consent"))
	}
	if err := checkNativeOrphans(stage.dir); err != nil {
		return NativeRecoveryReview{}, err
	}
	if _, err := stage.VerifyBinding(ctx, binding); err != nil {
		return NativeRecoveryReview{}, err
	}
	report, err := inspectNativeHost(ctx, true)
	if err != nil {
		return NativeRecoveryReview{}, err
	}
	if err := report.failure(); err != nil {
		return NativeRecoveryReview{}, err
	}
	if report.Snapshot == nil {
		return NativeRecoveryReview{}, errors.New("missing reviewed host snapshot")
	}
	digest, err := nativeTrustedHash(ctx, dispatcher)
	if err != nil {
		return NativeRecoveryReview{}, err
	}
	review := NativeRecoveryReview{SchemaVersion: 1, PolicyVersion: NativeRecoveryPolicyVersion, Release: binding, InstallerSHA256: digest, Snapshot: *report.Snapshot}
	if _, err := review.Digest(); err != nil {
		return NativeRecoveryReview{}, err
	}
	return review, ctx.Err()
}

func (stage *SourceStage) startNativeRecoveryChoice(ctx context.Context, binding ReleaseBinding, dispatcher string, decision NativeRecoveryDecision) (nativeRecoveryChoice, error) {
	if err := decision.Validate(); err != nil {
		return nativeRecoveryChoice{}, err
	}
	// Re-inspect; a supplied hash/previous report alone never admits this host.
	review, err := stage.ReviewNativeBootRecovery(ctx, binding, dispatcher)
	if err != nil {
		return nativeRecoveryChoice{}, err
	}
	choice := nativeRecoveryChoice{SchemaVersion: 1, Review: review, Decision: decision}
	if err := choice.validate(); err != nil {
		return nativeRecoveryChoice{}, err
	}
	if err := ctx.Err(); err != nil {
		return nativeRecoveryChoice{}, err
	}
	if err := stage.writeNativeRecoveryChoice(choice); err != nil {
		return nativeRecoveryChoice{}, err
	}
	return choice, nil
}

func (stage *SourceStage) writeNativeRecoveryChoice(choice nativeRecoveryChoice) error {
	if stage.check() != nil || stage.journal == nil {
		return errors.New("recovery choice requires shared lock")
	}
	if err := choice.validate(); err != nil {
		return err
	}
	if _, exists, err := stage.journal.Load(); err != nil || exists {
		return errors.Join(err, errors.New("cannot add consent to a storage operation"))
	}
	if err := checkNativeOrphans(stage.dir); err != nil {
		return err
	}
	// Exclusive create + file/parent fsync. Partial records are retained on error.
	return writeOriginRecord(stage.dir, nativeRecoveryChoiceName, nativeBootJSON(choice))
}

func (stage *SourceStage) readNativeRecoveryChoice() (nativeRecoveryChoice, bool, error) {
	var choice nativeRecoveryChoice
	data, exists, err := admissionReadAt(stage.dir, nativeRecoveryChoiceName)
	if err != nil || !exists {
		return choice, exists, err
	}
	if err := nativeBootDecode(data, &choice); err != nil {
		return choice, true, err
	}
	return choice, true, choice.validate()
}
