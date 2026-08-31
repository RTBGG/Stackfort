// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package installpreflight

import (
	"runtime"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func InspectResources() ResourceReport {
	return ResourceReport{
		LogicalCPUs: runtime.NumCPU(), CPUInspection: available(),
		MemoryInspection:  unknown("linux-memory-inspection-required"),
		StorageTarget:     agentprotocol.ManagedHostingRoot,
		StorageInspection: unknown("linux-storage-inspection-required"),
	}
}
