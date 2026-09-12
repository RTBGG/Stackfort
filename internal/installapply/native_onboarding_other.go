// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
)

func ReviewNativeOnboarding(context.Context, NativeOnboardingSource) (NativeOnboardingReview, error) {
	return NativeOnboardingReview{}, errors.New("native onboarding requires a qualified Linux host")
}

func PrepareNativeOnboarding(context.Context, NativeOnboardingRequest, NativeOnboardingReview) (NativeOnboardingPrepared, error) {
	return NativeOnboardingPrepared{}, errors.New("native onboarding requires a qualified Linux host")
}

func PrepareNativeOnboardingWithSetup(context.Context, NativeOnboardingRequest, NativeOnboardingReview, NativeSetupCommitment) (NativeOnboardingPrepared, error) {
	return NativeOnboardingPrepared{}, errors.New("native onboarding requires a qualified Linux host")
}
