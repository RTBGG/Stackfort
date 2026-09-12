// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
)

func InspectCompletedNativeOnboarding(context.Context, NativeOnboardingSource) (NativeOnboardingPrepared, bool, error) {
	return NativeOnboardingPrepared{}, false, errors.New("native onboarding requires a qualified Linux host")
}
