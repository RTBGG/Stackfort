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
	UserDeleted       bool `json:"userDeleted,omitempty"`
	GroupDeleted      bool `json:"groupDeleted,omitempty"`
}

func validateHostingIdentityResponse(response HostingIdentityResponse, operation Operation) error {
	switch operation {
	case OperationReconcileIdentity:
		if response.UserDeleted || response.GroupDeleted || response.Changed !=
			(response.GroupCreated || response.UserCreated || response.UserRepaired ||
				response.DirectoryCreated || response.OwnershipRepaired) {
			return errors.New("agent hosting identity reconciliation response is malformed")
		}
	case OperationDeleteIdentity:
		if response.GroupCreated || response.UserCreated || response.UserRepaired ||
			response.DirectoryCreated || response.OwnershipRepaired ||
			response.Changed != (response.UserDeleted || response.GroupDeleted) {
			return errors.New("agent hosting identity deletion response is malformed")
		}
	default:
		return errors.New("agent hosting identity response operation is unknown")
	}
	return nil
}
