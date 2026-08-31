// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installapply

import (
	"context"
	"errors"
	"io"
)

type LinuxRunner struct{}

func NewLinuxRunner(io.Writer) (*LinuxRunner, error) {
	return nil, errors.New("Stackfort installation requires Linux")
}

func ValidateSourceTrust(Source) error    { return errors.New("Stackfort installation requires Linux") }
func (*LinuxRunner) Distribution() string { return "unsupported" }
func (*LinuxRunner) Preflight(context.Context) error {
	return errors.New("Stackfort installation requires Linux")
}
func (*LinuxRunner) Apply(context.Context, StageID, Source) (bool, error) {
	return false, errors.New("Stackfort installation requires Linux")
}
func (*LinuxRunner) Verify(context.Context, StageID, Source) error {
	return errors.New("Stackfort installation requires Linux")
}
func (*LinuxRunner) VerifyInstallation(context.Context, Source) error {
	return errors.New("Stackfort installation requires Linux")
}
