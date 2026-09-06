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

// ManagePanel is a root-console boundary, intentionally not an agent RPC or
// browser endpoint that accepts private-key paths from a remote caller.
func ManagePanel(ctx context.Context, request PanelRequest) (PanelStatus, error) {
	return managePanel(ctx, request)
}
