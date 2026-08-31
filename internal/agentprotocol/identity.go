// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

type HostingIdentityRequest struct {
	Identity hostingidentity.Spec `json:"identity"`
}

type HostingIdentityResponse struct {
	Changed           bool `json:"changed"`
	GroupCreated      bool `json:"groupCreated,omitempty"`
	UserCreated       bool `json:"userCreated,omitempty"`
	UserRepaired      bool `json:"userRepaired,omitempty"`
	DirectoryCreated  bool `json:"directoryCreated,omitempty"`
	OwnershipRepaired bool `json:"ownershipRepaired,omitempty"`
	SubUIDsConfigured bool `json:"subUidsConfigured,omitempty"`
	SubGIDsConfigured bool `json:"subGidsConfigured,omitempty"`
	LingerEnabled     bool `json:"lingerEnabled,omitempty"`
	RuntimePrepared   bool `json:"runtimePrepared,omitempty"`
	UserDeleted       bool `json:"userDeleted,omitempty"`
	GroupDeleted      bool `json:"groupDeleted,omitempty"`
	RuntimeRemoved    bool `json:"runtimeRemoved,omitempty"`
	SubUIDsRemoved    bool `json:"subUidsRemoved,omitempty"`
	SubGIDsRemoved    bool `json:"subGidsRemoved,omitempty"`
	LingerDisabled    bool `json:"lingerDisabled,omitempty"`
}

func validateHostingIdentityResponse(response HostingIdentityResponse, operation Operation) error {
	switch operation {
	case OperationReconcileIdentity:
		if response.UserDeleted || response.GroupDeleted || response.RuntimeRemoved ||
			response.SubUIDsRemoved || response.SubGIDsRemoved || response.LingerDisabled || response.Changed !=
			(response.GroupCreated || response.UserCreated || response.UserRepaired ||
				response.DirectoryCreated || response.OwnershipRepaired || response.SubUIDsConfigured ||
				response.SubGIDsConfigured || response.LingerEnabled || response.RuntimePrepared) {
			return errors.New("agent hosting identity reconciliation response is malformed")
		}
	case OperationDeleteIdentity:
		if response.GroupCreated || response.UserCreated || response.UserRepaired ||
			response.DirectoryCreated || response.OwnershipRepaired || response.SubUIDsConfigured ||
			response.SubGIDsConfigured || response.LingerEnabled || response.RuntimePrepared ||
			response.Changed != (response.UserDeleted || response.GroupDeleted || response.RuntimeRemoved ||
				response.SubUIDsRemoved || response.SubGIDsRemoved || response.LingerDisabled) {
			return errors.New("agent hosting identity deletion response is malformed")
		}
	default:
		return errors.New("agent hosting identity response operation is unknown")
	}
	return nil
}
