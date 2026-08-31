// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostfiles

import (
	"context"
	"io"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

type unsupportedWriter struct{}

func platformWriteExecutable() string         { return "/proc/self/exe" }
func newPlatformWriter(string) platformWriter { return unsupportedWriter{} }
func (unsupportedWriter) Execute(context.Context, agentprotocol.FileWriteRequest, io.Reader) (agentprotocol.FileWriteResult, error) {
	return agentprotocol.FileWriteResult{}, ErrUnavailable
}
func runPlatformWriteHelper(context.Context, io.Reader, io.Writer) error { return ErrUnavailable }
