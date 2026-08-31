// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostociimage prepares immutable rootless Podman images through the
// fixed agent execution profiles and records a root-owned replay manifest.
package hostociimage

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

type CapabilityError struct{ Capability agentprotocol.Capability }

func (failure *CapabilityError) Error() string { return ErrUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrUnavailable }

var (
	ErrInvalid      = errors.New("invalid managed OCI image operation")
	ErrConflict     = errors.New("managed OCI image operation conflicts with host state")
	ErrUnavailable  = errors.New("managed OCI image preparation is unavailable")
	ErrMutation     = errors.New("managed OCI image preparation failed")
	ErrScanRejected = ociimage.ErrScanRejected
)

type Manager interface {
	Prepare(context.Context, string, ociimage.PrepareSpec) (ociimage.Result, error)
}

func firstUnavailableCapability(runtime agentprotocol.OCIRuntimeCapabilities) agentprotocol.Capability {
	for _, capability := range []agentprotocol.Capability{
		runtime.Rootless, runtime.Storage, runtime.RootfulSocketIsolation,
		runtime.ImagePreparation, runtime.ImageScanning,
	} {
		if capability.Status != agentprotocol.CapabilityAvailable {
			return capability
		}
	}
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}
