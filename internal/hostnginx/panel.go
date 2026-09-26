// SPDX-License-Identifier: AGPL-3.0-or-later

package hostnginx

import (
	"context"
	"time"
)

type PanelRequest struct {
	Action          string
	Hostname        string
	CertificatePath string
	PrivateKeyPath  string
	Email           string
	AcceptTerms     bool
}

type PanelStatus struct {
	Enabled              bool      `json:"enabled"`
	Hostname             string    `json:"hostname,omitempty"`
	URL                  string    `json:"url,omitempty"`
	CertificateExpiresAt time.Time `json:"certificateExpiresAt,omitempty"`
	RecoveryRequired     bool      `json:"recoveryRequired"`
	AutoRenew            bool      `json:"autoRenew"`
}

// ManagePanel is the root management boundary. Browser requests are constrained
// by the agent to status/ACME issuance only; private-key paths are console-only.
func ManagePanel(ctx context.Context, request PanelRequest) (PanelStatus, error) {
	return managePanel(ctx, request)
}
