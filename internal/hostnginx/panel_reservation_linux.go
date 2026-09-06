// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"errors"
	"os"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/panelconfig"
)

// Called under the same lock as panel configuration. Even an operation queued
// before the hostname was reserved cannot subsequently shadow its virtual host.
func (workspace *linuxActivationWorkspace) checkPanelReservation(candidate []byte) error {
	manager := &panelManager{root: workspace.store.root}
	if _, err := manager.read(panelconfig.JournalPath, true); !errors.Is(err, os.ErrNotExist) {
		return errors.New("recover the panel hostname transaction before changing hosted domains")
	}
	content, err := manager.read(panelconfig.ConfigurationPath, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, distribution := range []string{"debian", "rocky"} {
		spec, _ := nginxbaseline.ForDistribution(distribution)
		config, err := panelconfig.Parse(spec, content)
		if err == nil {
			if panelconfig.Conflicts(candidate, config.Hostname) {
				return errors.New("hosting domain conflicts with the reserved panel hostname")
			}
			return nil
		}
	}
	return ErrConflict
}
