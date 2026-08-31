// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostlogs creates and reads the root-owned web logs referenced by the
// managed NGINX configuration. Callers provide typed identities and normalized
// domains, never filesystem paths.
package hostlogs

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

var (
	ErrInvalid     = errors.New("hosting log request is invalid")
	ErrNotFound    = errors.New("hosting log was not found")
	ErrConflict    = errors.New("hosting log storage conflicts with managed state")
	ErrUnavailable = errors.New("hosting logs are unavailable")
)

type platformManager interface {
	Ensure(context.Context, hostingidentity.Spec, []core.NormalizedDomainName) error
	Read(context.Context, agentprotocol.HostingLogReadRequest) (agentprotocol.HostingLogReadResponse, error)
	ReadWAFEvents(context.Context, agentprotocol.WAFEventReadRequest) (agentprotocol.WAFEventReadResponse, error)
}

func (manager *Manager) ReadWAFEvents(
	ctx context.Context, request agentprotocol.WAFEventReadRequest,
) (agentprotocol.WAFEventReadResponse, error) {
	if manager == nil || manager.platform == nil || ctx == nil {
		return agentprotocol.WAFEventReadResponse{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	return manager.platform.ReadWAFEvents(ctx, request)
}

type Manager struct{ platform platformManager }

func NewManager() *Manager { return &Manager{platform: newPlatformManager()} }

func (manager *Manager) Ensure(
	ctx context.Context, identity hostingidentity.Spec, domains []core.NormalizedDomainName,
) error {
	if manager == nil || manager.platform == nil || ctx == nil || hostingidentity.Validate(identity) != nil ||
		len(domains) > 10_000 {
		return ErrInvalid
	}
	for _, domain := range domains {
		normalized, err := core.NormalizeDomainName(domain.ASCII)
		display, displayErr := core.NormalizeDomainName(domain.Display)
		if err != nil || displayErr != nil || normalized.ASCII != domain.ASCII || display != domain {
			return ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return manager.platform.Ensure(ctx, identity, domains)
}

func (manager *Manager) Read(
	ctx context.Context, request agentprotocol.HostingLogReadRequest,
) (agentprotocol.HostingLogReadResponse, error) {
	if manager == nil || manager.platform == nil || ctx == nil {
		return agentprotocol.HostingLogReadResponse{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	return manager.platform.Read(ctx, request)
}
