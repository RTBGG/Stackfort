// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

type NGINXBaselineRequest struct{}

type NGINXBaselineResponse struct {
	Changed                    bool       `json:"changed"`
	ConfigurationTested        bool       `json:"configurationTested"`
	ServiceActive              bool       `json:"serviceActive"`
	ServiceEnabled             bool       `json:"serviceEnabled"`
	ActivationPerformed        bool       `json:"activationPerformed"`
	ConfigurationRoot          string     `json:"configurationRoot"`
	MainConfiguration          string     `json:"mainConfiguration"`
	PanelIncludeDirectory      string     `json:"panelIncludeDirectory"`
	SitesIncludeDirectory      string     `json:"sitesIncludeDirectory"`
	HTTPDefaultRejectsUnknown  bool       `json:"httpDefaultRejectsUnknown"`
	HTTPSDefaultRejectsUnknown bool       `json:"httpsDefaultRejectsUnknown"`
	TrustedProxyHops           []string   `json:"trustedProxyHops"`
	Capability                 Capability `json:"capability"`
}

func validateNGINXBaselineResponse(response NGINXBaselineResponse, operation Operation) error {
	if operation != OperationReconcileNGINXBaseline || !response.ConfigurationTested ||
		!response.ServiceActive || !response.ServiceEnabled || !response.HTTPDefaultRejectsUnknown ||
		!response.HTTPSDefaultRejectsUnknown ||
		response.ConfigurationRoot != nginxbaseline.ManagedRoot ||
		response.MainConfiguration != nginxbaseline.MainConfiguration ||
		response.PanelIncludeDirectory != nginxbaseline.PanelDirectory ||
		response.SitesIncludeDirectory != nginxbaseline.SitesDirectory ||
		len(response.TrustedProxyHops) != 2 ||
		response.TrustedProxyHops[0] != nginxbaseline.LoopbackIPv4 ||
		response.TrustedProxyHops[1] != nginxbaseline.LoopbackIPv6 {
		return errors.New("agent NGINX baseline response is malformed")
	}
	if err := validateCapability(response.Capability); err != nil ||
		response.Capability.Status != CapabilityAvailable {
		return errors.New("agent NGINX baseline capability is malformed")
	}
	return nil
}
