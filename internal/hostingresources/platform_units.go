// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingresources

import "strconv"

const platformUnitHeader = "# Managed by Stackfort. Do not edit.\n"

// CoreSliceUnit is shared by installation and live account reconciliation.
func CoreSliceUnit() string {
	return platformUnitHeader + `[Unit]
Description=Stackfort platform and control-plane services

[Slice]
CPUWeight=10000
IOWeight=10000
MemoryLow=` + strconv.FormatUint(100-DefaultAccountCapacityPercent, 10) + `%
`
}

// AccountsSliceUnit requires a trusted or already-validated host CPU count.
// Keep the shared parent byte-identical across installer and agent renders.
func AccountsSliceUnit(processorCount uint64) string {
	return platformUnitHeader + `[Unit]
Description=Stackfort hosting account workloads

[Slice]
CPUQuota=` + strconv.FormatUint(processorCount*DefaultAccountCapacityPercent, 10) + `%
CPUQuotaPeriodSec=100ms
CPUWeight=100
IOWeight=100
MemoryHigh=75%
MemoryMax=` + strconv.FormatUint(DefaultAccountCapacityPercent, 10) + `%
`
}
