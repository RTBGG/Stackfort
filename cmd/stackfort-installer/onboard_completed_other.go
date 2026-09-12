// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package main

import "github.com/RTBGG/stackfort/internal/installapply"

func newOnboardPublicController() onboardPublicController {
	return onboardPublicController{inspect: installapply.InspectCompletedNativeOnboarding, fresh: newOnboardInteractiveController().run}
}
