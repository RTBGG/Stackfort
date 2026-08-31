// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostlogs

import (
	"context"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

type unavailableManager struct{}

func newPlatformManager() platformManager { return unavailableManager{} }

func (unavailableManager) Ensure(context.Context, hostingidentity.Spec, []core.NormalizedDomainName) error {
	return ErrUnavailable
}

func (unavailableManager) Read(context.Context, agentprotocol.HostingLogReadRequest) (agentprotocol.HostingLogReadResponse, error) {
	return agentprotocol.HostingLogReadResponse{}, ErrUnavailable
}

func (unavailableManager) ReadWAFEvents(context.Context, agentprotocol.WAFEventReadRequest) (agentprotocol.WAFEventReadResponse, error) {
	return agentprotocol.WAFEventReadResponse{}, ErrUnavailable
}
