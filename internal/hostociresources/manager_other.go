// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostociresources

import (
	"context"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

type unsupportedManager struct{}

func NewManager() Manager { return unsupportedManager{} }

func (unsupportedManager) Reconcile(context.Context, string, ociresources.Spec) (ociresources.Result, error) {
	return ociresources.Result{}, &CapabilityError{Capability: agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnsupported, ReasonCode: "oci-private-resources-platform-unsupported",
	}}
}
