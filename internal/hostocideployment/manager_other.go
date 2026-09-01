// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostocideployment

import (
	"context"

	"github.com/RTBGG/stackfort/internal/ocideployment"
)

type unsupportedManager struct{}

func NewManager() Manager { return unsupportedManager{} }

func (unsupportedManager) Reconcile(context.Context, string, ocideployment.Request) (ocideployment.LifecycleResult, error) {
	return ocideployment.LifecycleResult{}, ErrUnavailable
}

func (unsupportedManager) ReadLogs(context.Context, ocideployment.LogSpec) (ocideployment.LogResult, error) {
	return ocideployment.LogResult{}, ErrUnavailable
}
