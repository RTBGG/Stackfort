// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostcache exposes only per-domain log-derived cache counters and a
// scoped Vinyl ban. It never accepts a filesystem path, VCL, or command text.
package hostcache

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

var (
	ErrInvalid     = errors.New("invalid cache request")
	ErrConflict    = errors.New("cache host state conflicts with policy")
	ErrUnavailable = errors.New("cache service is unavailable")
)

type platformManager interface {
	Metrics(context.Context, agentprotocol.CacheMetricsRequest) (agentprotocol.CacheMetricsResponse, error)
	Purge(context.Context, agentprotocol.CachePurgeRequest, *agentexec.Runner) (agentprotocol.CachePurgeResponse, error)
}

type Manager struct {
	platform platformManager
	runner   *agentexec.Runner
}

func NewManager(runner *agentexec.Runner) *Manager {
	if runner == nil {
		runner = agentexec.NewRunner()
	}
	return &Manager{platform: newPlatformManager(), runner: runner}
}

func (manager *Manager) Metrics(ctx context.Context, request agentprotocol.CacheMetricsRequest) (agentprotocol.CacheMetricsResponse, error) {
	return manager.platform.Metrics(ctx, request)
}

func (manager *Manager) Purge(ctx context.Context, request agentprotocol.CachePurgeRequest) (agentprotocol.CachePurgeResponse, error) {
	return manager.platform.Purge(ctx, request, manager.runner)
}
