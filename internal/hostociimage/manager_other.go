// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostociimage

import (
	"context"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

type unsupportedManager struct{}

func NewManager() Manager { return unsupportedManager{} }

func (unsupportedManager) Prepare(context.Context, string, ociimage.PrepareSpec) (ociimage.Result, error) {
	return ociimage.Result{}, &CapabilityError{Capability: agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnsupported, ReasonCode: "oci-image-platform-unsupported",
	}}
}
