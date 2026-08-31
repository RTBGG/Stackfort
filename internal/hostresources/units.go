// SPDX-License-Identifier: AGPL-3.0-or-later

package hostresources

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingresources"
)

const managedUnitHeader = "# Managed by Stackfort. Do not edit.\n"

type renderedUnit struct {
	name    string
	content string
}

func renderUnits(spec hostingresources.Spec, processorCount int) ([]renderedUnit, error) {
	if err := hostingresources.Validate(spec); err != nil || processorCount < 1 || processorCount > 16_384 {
		return nil, ErrMutationFailed
	}
	accountUnit, err := hostingresources.AccountSliceName(spec.Identity.UID)
	if err != nil {
		return nil, ErrMutationFailed
	}
	accountCapacity := uint64(processorCount) * hostingresources.DefaultAccountCapacityPercent
	reservedCapacity := uint64(100) - hostingresources.DefaultAccountCapacityPercent
	properties, err := hostingresources.SystemdProperties(spec)
	if err != nil {
		return nil, ErrMutationFailed
	}

	core := managedUnitHeader + `[Unit]
Description=Stackfort platform and control-plane services

[Slice]
CPUAccounting=yes
CPUWeight=10000
IOAccounting=yes
IOWeight=10000
MemoryAccounting=yes
MemoryLow=` + strconv.FormatUint(reservedCapacity, 10) + `%
TasksAccounting=yes
`
	accounts := managedUnitHeader + `[Unit]
Description=Stackfort hosting account workloads

[Slice]
CPUAccounting=yes
CPUQuota=` + strconv.FormatUint(accountCapacity, 10) + `%
CPUQuotaPeriodSec=100ms
CPUWeight=100
IOAccounting=yes
IOWeight=100
MemoryAccounting=yes
MemoryHigh=75%
MemoryMax=` + strconv.FormatUint(hostingresources.DefaultAccountCapacityPercent, 10) + `%
TasksAccounting=yes
`
	var account strings.Builder
	account.WriteString(managedUnitHeader)
	account.WriteString("[Unit]\nDescription=Stackfort hosting account ")
	account.WriteString(strconv.FormatUint(uint64(spec.Identity.UID), 10))
	account.WriteString(" resource boundary\nRequires=stackfort-accounts.slice\nAfter=stackfort-accounts.slice\n\n[Slice]\n")
	for _, property := range properties {
		account.WriteString(property)
		account.WriteByte('\n')
	}
	if strings.Contains(account.String(), "\x00") {
		return nil, fmt.Errorf("%w: rendered unit contains NUL", ErrMutationFailed)
	}
	return []renderedUnit{
		{name: "stackfort-core.slice", content: core},
		{name: "stackfort-accounts.slice", content: accounts},
		{name: accountUnit, content: account.String()},
	}, nil
}
