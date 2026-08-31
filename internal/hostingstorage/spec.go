// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostingstorage defines the complete project-quota intent shared by
// the control plane, protocol, process allowlist, and host reconciler.
package hostingstorage

import (
	"errors"
	"math"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const QuotaBlockBytes uint64 = 1024

var ErrInvalidSpec = errors.New("invalid hosting storage specification")

// Spec uses zero for an unlimited dimension. ProjectID is deliberately bound
// to the account UID so no second allocator or mutable identity exists.
type Spec struct {
	Identity   hostingidentity.Spec `json:"identity"`
	ProjectID  uint32               `json:"projectId"`
	ByteLimit  uint64               `json:"byteLimit"`
	InodeLimit uint64               `json:"inodeLimit"`
}

func Validate(spec Spec) error {
	if hostingidentity.Validate(spec.Identity) != nil || spec.ProjectID != spec.Identity.UID {
		return ErrInvalidSpec
	}
	if spec.ByteLimit > math.MaxInt64 || spec.InodeLimit > math.MaxInt64 {
		return ErrInvalidSpec
	}
	if spec.ByteLimit != 0 && (spec.ByteLimit < QuotaBlockBytes || spec.ByteLimit%QuotaBlockBytes != 0) {
		return ErrInvalidSpec
	}
	return nil
}
