// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package agentexec

import "os/exec"

import "github.com/RTBGG/stackfort/internal/hostingidentity"

func configureProcess(*exec.Cmd, *hostingidentity.Spec) error {
	return newRunError(ErrUnsupportedPlatform, nil)
}

func terminateProcessTree(*exec.Cmd) error {
	return newRunError(ErrUnsupportedPlatform, nil)
}
