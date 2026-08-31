// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
)

type HostingResourcesRequest struct {
	Resources hostingresources.Spec `json:"resources"`
}

type HostingResourcesResponse struct {
	UID           uint32     `json:"uid"`
	UnitName      string     `json:"unitName"`
	ControlGroup  string     `json:"controlGroup"`
	UnitsChanged  bool       `json:"unitsChanged"`
	LimitsApplied bool       `json:"limitsApplied"`
	Capability    Capability `json:"capability"`
}

func validateHostingResourcesResponse(response HostingResourcesResponse, operation Operation) error {
	if operation != OperationReconcileResources || response.UID < hostingidentity.MinimumID ||
		response.UID > hostingidentity.MaximumID || !response.LimitsApplied ||
		response.Capability.Status != CapabilityAvailable || response.Capability.ReasonCode != "" {
		return errors.New("agent hosting resources response is malformed")
	}
	unit, err := hostingresources.AccountSliceName(response.UID)
	if err != nil || response.UnitName != unit ||
		response.ControlGroup != "/stackfort.slice/stackfort-accounts.slice/"+unit {
		return errors.New("agent hosting resources response boundary is malformed")
	}
	return nil
}
