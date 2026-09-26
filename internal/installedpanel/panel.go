// SPDX-License-Identifier: AGPL-3.0-or-later

// Package installedpanel serializes panel management with installation and updates.
package installedpanel

import (
	"context"
	"errors"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/updateapply"
	"os"
)

func Manage(ctx context.Context, request hostnginx.PanelRequest) (hostnginx.PanelStatus, error) {
	if os.Geteuid() != 0 {
		return hostnginx.PanelStatus{}, errors.New("panel management requires root")
	}
	// Serialize even timer-driven renewals with active install/update commands.
	// Nonblocking locks make contention a retryable operator/timer failure.
	updates := updateapply.NewFileStore()
	updateLock, err := updates.AcquireLock()
	if err != nil {
		return hostnginx.PanelStatus{}, err
	}
	defer updateLock.Close()
	installLock, err := installapply.AcquirePanelManagement(ctx)
	if err != nil {
		return hostnginx.PanelStatus{}, err
	}
	defer installLock.Close()
	update, exists, err := updates.Load()
	if err != nil {
		return hostnginx.PanelStatus{}, err
	}
	if exists && update.Status != updateapply.StatusComplete && update.Status != updateapply.StatusRolledBack {
		return hostnginx.PanelStatus{}, errors.New("recover the interrupted platform update before managing the panel hostname")
	}
	return hostnginx.ManagePanel(ctx, request)
}
