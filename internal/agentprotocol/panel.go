// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"github.com/RTBGG/stackfort/internal/panelconfig"
	"time"
)

const MaximumPanelIssueDuration = 5 * time.Minute

type PanelInspectRequest struct{}

// PanelIssueRequest cannot select commands, key paths, a CA, or upstream URLs.
type PanelIssueRequest struct {
	Hostname            string `json:"hostname"`
	Email               string `json:"email"`
	AcceptTerms         bool   `json:"acceptTerms"`
	ConfirmOriginChange bool   `json:"confirmOriginChange"`
}

func ValidatePanelIssue(input PanelIssueRequest) error {
	if panelconfig.Hostname(input.Hostname) != nil || panelconfig.Email(input.Email) != nil || !input.AcceptTerms || !input.ConfirmOriginChange {
		return ErrInvalidRequest
	}
	return nil
}

// PanelStatusResponse contains public endpoint metadata only.
type PanelStatusResponse struct {
	Enabled              bool      `json:"enabled"`
	Hostname             string    `json:"hostname,omitempty"`
	URL                  string    `json:"url,omitempty"`
	CertificateExpiresAt time.Time `json:"certificateExpiresAt,omitempty"`
	RecoveryRequired     bool      `json:"recoveryRequired"`
	AutoRenew            bool      `json:"autoRenew"`
}

func validatePanelStatus(status PanelStatusResponse) error {
	if status.Hostname != "" && panelconfig.Hostname(status.Hostname) != nil {
		return errors.New("invalid panel hostname")
	}
	if status.Enabled {
		if status.Hostname == "" || status.URL != "https://"+status.Hostname+"/" || status.CertificateExpiresAt.IsZero() {
			return errors.New("invalid active panel endpoint")
		}
	} else if status.URL != "" || !status.CertificateExpiresAt.IsZero() || status.AutoRenew {
		return errors.New("invalid disabled panel endpoint")
	}
	return nil
}
