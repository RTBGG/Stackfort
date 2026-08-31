// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"encoding/hex"
	"errors"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
)

// NGINXActivationRequest carries only typed account/domain intent. The agent
// independently renders it; configuration text and caller-selected paths are
// intentionally absent.
type NGINXActivationRequest struct {
	Identity               hostingidentity.Spec     `json:"identity"`
	DesiredStateRevisionID string                   `json:"desiredStateRevisionId"`
	Domains                []nginxconfig.DomainSpec `json:"domains"`
	Options                nginxconfig.Options      `json:"options"`
}

type NGINXActivationResponse struct {
	Changed                bool   `json:"changed"`
	ConfigurationTested    bool   `json:"configurationTested"`
	ReloadPerformed        bool   `json:"reloadPerformed"`
	HealthChecked          bool   `json:"healthChecked"`
	RecoveryPerformed      bool   `json:"recoveryPerformed"`
	ActiveRevisionID       string `json:"activeRevisionId"`
	PreviousRevisionID     string `json:"previousRevisionId,omitempty"`
	DesiredStateRevisionID string `json:"desiredStateRevisionId"`
	ConfigDigest           string `json:"configDigest"`
	RenderedDomains        int    `json:"renderedDomains"`
}

func validateNGINXActivationResponse(response NGINXActivationResponse, operation Operation) error {
	if operation != OperationActivateNGINXSites || !response.ConfigurationTested ||
		!response.HealthChecked || !validCanonicalUUIDv7(response.ActiveRevisionID) ||
		!validCanonicalUUIDv7(response.DesiredStateRevisionID) ||
		response.RenderedDomains < 0 || response.RenderedDomains > nginxconfig.MaximumDomains {
		return errors.New("agent NGINX activation response is malformed")
	}
	if response.PreviousRevisionID != "" && !validCanonicalUUIDv7(response.PreviousRevisionID) {
		return errors.New("agent NGINX previous revision is malformed")
	}
	digest, err := hex.DecodeString(response.ConfigDigest)
	if err != nil || len(digest) != 32 || response.ConfigDigest != hex.EncodeToString(digest) {
		return errors.New("agent NGINX configuration digest is malformed")
	}
	if response.Changed && !response.ReloadPerformed {
		return errors.New("changed NGINX activation omitted its reload")
	}
	return nil
}
