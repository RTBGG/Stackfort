// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installpreflight

import (
	"io"
	"os"
	"runtime"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"golang.org/x/sys/unix"
)

const resourceProbeLimit = 1 << 20

func InspectResources() ResourceReport {
	report := ResourceReport{
		LogicalCPUs: runtime.NumCPU(), CPUInspection: available(),
		StorageTarget:     agentprotocol.ManagedHostingRoot,
		MemoryInspection:  unknown("memory-inspection-failed"),
		StorageInspection: unknown("storage-inspection-failed"),
	}
	if report.LogicalCPUs < 1 {
		report.CPUInspection = unknown("cpu-inspection-failed")
	}

	if file, err := os.Open("/proc/meminfo"); err == nil {
		content, readErr := io.ReadAll(io.LimitReader(file, resourceProbeLimit+1))
		closeErr := file.Close()
		if readErr == nil && closeErr == nil && len(content) <= resourceProbeLimit {
			if memory, parseErr := parseMemoryTotal(string(content)); parseErr == nil {
				report.MemoryTotalBytes = memory
				report.MemoryInspection = available()
			}
		}
	}

	var filesystem unix.Statfs_t
	if err := unix.Statfs(agentprotocol.ManagedHostingRoot, &filesystem); err == nil && filesystem.Bsize > 0 {
		report.StorageTotalBytes = multiplySaturated(filesystem.Blocks, uint64(filesystem.Bsize))
		report.StorageFreeBytes = multiplySaturated(filesystem.Bavail, uint64(filesystem.Bsize))
		report.StorageInspection = available()
	}
	return report
}

func multiplySaturated(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}
