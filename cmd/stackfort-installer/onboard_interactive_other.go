// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package main

import (
	"errors"
	"io"
)

func newOnboardInteractiveController() onboardInteractiveController {
	return onboardInteractiveController{tty: func() (io.ReadWriteCloser, error) {
		return nil, errors.New("native onboarding requires a qualified Linux host")
	}}
}
