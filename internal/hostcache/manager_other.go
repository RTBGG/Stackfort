// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostcache

import (
	"context"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

type unavailableManager struct{}

func newPlatformManager() platformManager { return unavailableManager{} }

func (unavailableManager) Metrics(context.Context, agentprotocol.CacheMetricsRequest) (agentprotocol.CacheMetricsResponse, error) {
	return agentprotocol.CacheMetricsResponse{}, ErrUnavailable
}

func (unavailableManager) Purge(context.Context, agentprotocol.CachePurgeRequest, *agentexec.Runner) (agentprotocol.CachePurgeResponse, error) {
	return agentprotocol.CachePurgeResponse{}, ErrUnavailable
}
