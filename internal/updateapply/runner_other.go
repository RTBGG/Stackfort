// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package updateapply

import (
	"context"
	"errors"
	"io"

	"github.com/RTBGG/stackfort/internal/installapply"
)

type LinuxRunner struct{}

func NewLinuxRunner(io.Writer, installapply.Source) (*LinuxRunner, error) {
	return nil, errors.New("Stackfort updates require Linux")
}

func (*LinuxRunner) Preflight(context.Context, installapply.Source, installapply.Source) error {
	return errors.New("Stackfort updates require Linux")
}
func (*LinuxRunner) Apply(context.Context, StageID, installapply.Source, installapply.Source) error {
	return errors.New("Stackfort updates require Linux")
}
func (*LinuxRunner) Verify(context.Context, StageID, installapply.Source, installapply.Source) error {
	return errors.New("Stackfort updates require Linux")
}
func (*LinuxRunner) Rollback(context.Context, installapply.Source, installapply.Source, Journal) error {
	return errors.New("Stackfort updates require Linux")
}
