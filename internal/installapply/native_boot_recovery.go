// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import "github.com/RTBGG/stackfort/internal/storageprep"

func nativeBootGRUBEntry(plan storageprep.Plan, intent NativeRuntimeIntent) (string, error) {
	if intent.Offline != nil && intent.Offline.PowerLossGuard {
		return storageprep.RenderRecoveryGRUBEntry(plan)
	}
	return storageprep.RenderGRUBEntry(plan)
}
