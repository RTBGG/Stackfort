// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostociresources prepares an account-private rootless network and
// descriptor-verified managed volume roots. It never starts a workload or
// accepts a host path, public port, device, namespace, or capability option.
package hostociresources

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

type CapabilityError struct{ Capability agentprotocol.Capability }

func (failure *CapabilityError) Error() string { return ErrUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrUnavailable }

var (
	ErrInvalid     = errors.New("invalid managed OCI private-resource operation")
	ErrConflict    = errors.New("managed OCI private resources conflict with host state")
	ErrUnavailable = errors.New("managed OCI private resources are unavailable")
	ErrMutation    = errors.New("managed OCI private resources could not be reconciled")
)

type Manager interface {
	Reconcile(context.Context, string, ociresources.Spec) (ociresources.Result, error)
}

func firstUnavailableCapability(runtime agentprotocol.OCIRuntimeCapabilities) agentprotocol.Capability {
	for _, capability := range []agentprotocol.Capability{
		runtime.Rootless, runtime.Network, runtime.Storage, runtime.RootfulSocketIsolation,
	} {
		if capability.Status != agentprotocol.CapabilityAvailable {
			return capability
		}
	}
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}
