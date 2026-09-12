// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package main

import (
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func newOnboardPublicController() onboardPublicController {
	return onboardPublicController{inspect: installapply.InspectCompletedNativeOnboarding,
		fresh: newOnboardInteractiveController().run, check: checkOnboardCompletedRuntime}
}

func checkOnboardCompletedRuntime(ctx context.Context, prepared installapply.NativeOnboardingPrepared) error {
	if err := verifyOnboardSealedRuntime(ctx, prepared); err != nil {
		return err
	}
	arguments, err := installapply.NativeCompletedRecheckArguments(prepared.OperationID)
	if err != nil {
		return err
	}
	// systemd owns the child after submission, including process-loss cleanup.
	// Its own shorter deadline still quarantines if this client is interrupted.
	bounded, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	// #nosec G204 -- Fixed systemd-run executable; the closed argument builder accepts only a canonical operation UUID and emits fixed runtime/supervision commands, without a shell.
	command := exec.CommandContext(bounded, "/usr/bin/systemd-run", arguments...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.WaitDelay = 5 * time.Second
	var output, diagnostic onboardCompletedOutput
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err != nil || output.overflow || diagnostic.overflow || bounded.Err() != nil {
		return errors.New("supervised native completion recheck failed or remains uncertain; preserve state and inspect the native service journal before further action")
	}
	return validateOnboardCompletedOutput(output.String(), prepared.Manifest)
}
