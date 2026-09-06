// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package hostnginx

import (
	"context"
	"errors"
)

func managePanel(context.Context, PanelRequest) (PanelStatus, error) {
	return PanelStatus{}, errors.New("panel hostname configuration requires a supported Linux host and root")
}
