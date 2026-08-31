// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostfiles

import (
	"context"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

type unsupportedBrowser struct{}

func newPlatformBrowser() platformBrowser { return unsupportedBrowser{} }

func (unsupportedBrowser) List(context.Context, agentprotocol.FileListRequest) (agentprotocol.FileListResponse, error) {
	return agentprotocol.FileListResponse{}, ErrUnavailable
}
