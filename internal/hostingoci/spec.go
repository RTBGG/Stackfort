// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostingoci derives the closed, account-owned rootless Podman
// identity and path contract. No caller-controlled path or numeric mapping is
// accepted at the privileged boundary.
package hostingoci

import (
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const (
	SubordinateIDBase  uint32 = 1_000_000
	SubordinateIDCount uint32 = 65_536
	MaximumRuntimeUID  uint32 = hostingidentity.MaximumID
	QuadletUsersRoot          = "/etc/containers/systemd/users"
)

var ErrInvalidSpec = errors.New("invalid hosting OCI runtime specification")

type Spec struct {
	Identity       hostingidentity.Spec `json:"identity"`
	SubUIDStart    uint32               `json:"subUidStart"`
	SubGIDStart    uint32               `json:"subGidStart"`
	SubordinateIDs uint32               `json:"subordinateIds"`
	StorageRoot    string               `json:"storageRoot"`
	RuntimeRoot    string               `json:"runtimeRoot"`
	QuadletRoot    string               `json:"quadletRoot"`
}

func ForIdentity(identity hostingidentity.Spec) (Spec, error) {
	if err := hostingidentity.Validate(identity); err != nil || identity.UID > MaximumRuntimeUID {
		return Spec{}, ErrInvalidSpec
	}
	offset := uint64(identity.UID-hostingidentity.MinimumID) * uint64(SubordinateIDCount)
	start := uint64(SubordinateIDBase) + offset
	end := start + uint64(SubordinateIDCount) - 1
	if end > math.MaxUint32 {
		return Spec{}, ErrInvalidSpec
	}
	uid := strconv.FormatUint(uint64(identity.UID), 10)
	result := Spec{
		Identity: identity, SubUIDStart: uint32(start), SubGIDStart: uint32(start),
		SubordinateIDs: SubordinateIDCount,
		StorageRoot:    path.Join(identity.HomeDirectory, ".local/share/containers"),
		RuntimeRoot:    "/run/user/" + uid,
		QuadletRoot:    path.Join(QuadletUsersRoot, uid),
	}
	return result, Validate(result)
}

func Validate(spec Spec) error {
	if err := hostingidentity.Validate(spec.Identity); err != nil || spec.Identity.UID > MaximumRuntimeUID {
		return fmt.Errorf("%w: identity", ErrInvalidSpec)
	}
	expected, err := ForIdentityUnchecked(spec.Identity)
	if err != nil || spec != expected {
		return fmt.Errorf("%w: derived values", ErrInvalidSpec)
	}
	return nil
}

func ForIdentityUnchecked(identity hostingidentity.Spec) (Spec, error) {
	if err := hostingidentity.Validate(identity); err != nil || identity.UID > MaximumRuntimeUID {
		return Spec{}, ErrInvalidSpec
	}
	offset := uint64(identity.UID-hostingidentity.MinimumID) * uint64(SubordinateIDCount)
	start := uint64(SubordinateIDBase) + offset
	if start+uint64(SubordinateIDCount)-1 > math.MaxUint32 {
		return Spec{}, ErrInvalidSpec
	}
	uid := strconv.FormatUint(uint64(identity.UID), 10)
	return Spec{
		Identity: identity, SubUIDStart: uint32(start), SubGIDStart: uint32(start),
		SubordinateIDs: SubordinateIDCount,
		StorageRoot:    path.Join(identity.HomeDirectory, ".local/share/containers"),
		RuntimeRoot:    "/run/user/" + uid,
		QuadletRoot:    path.Join(QuadletUsersRoot, uid),
	}, nil
}

func (spec Spec) SubUIDEnd() uint32 { return spec.SubUIDStart + spec.SubordinateIDs - 1 }

func (spec Spec) SubGIDEnd() uint32 { return spec.SubGIDStart + spec.SubordinateIDs - 1 }
