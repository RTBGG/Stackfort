// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostocideployment owns fixed rootless Quadlet lifecycle, readiness,
// and bounded application logs on the privileged local agent.
package hostocideployment

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/ocideployment"
)

var (
	ErrInvalid     = errors.New("invalid managed OCI deployment operation")
	ErrConflict    = errors.New("managed OCI deployment conflicts with host state")
	ErrUnavailable = errors.New("managed OCI deployment is unavailable")
	ErrMutation    = errors.New("managed OCI deployment mutation failed")
	ErrUnhealthy   = errors.New("managed OCI deployment failed its health check")
)

type CapabilityError struct{ Capability agentprotocol.Capability }

func (failure *CapabilityError) Error() string { return ErrUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrUnavailable }

type Manager interface {
	Reconcile(context.Context, string, ocideployment.Request) (ocideployment.LifecycleResult, error)
	ReadLogs(context.Context, ocideployment.LogSpec) (ocideployment.LogResult, error)
}

func firstUnavailableCapability(runtime agentprotocol.OCIRuntimeCapabilities) agentprotocol.Capability {
	for _, capability := range []agentprotocol.Capability{runtime.Rootless, runtime.Quadlet,
		runtime.Network, runtime.Storage, runtime.RootfulSocketIsolation} {
		if capability.Status != agentprotocol.CapabilityAvailable {
			return capability
		}
	}
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}
