// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostidentity

import (
	"context"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

type unsupportedRuntimeManager struct{}

func newRuntimeManager() runtimeManager { return unsupportedRuntimeManager{} }

func (unsupportedRuntimeManager) EnsureRuntime(context.Context, hostingidentity.Spec) (RuntimeResult, error) {
	return RuntimeResult{}, ErrRuntimeUnavailable
}

func (unsupportedRuntimeManager) RemoveRuntime(context.Context, hostingidentity.Spec) (RuntimeRemovalResult, error) {
	return RuntimeRemovalResult{}, ErrRuntimeUnavailable
}
