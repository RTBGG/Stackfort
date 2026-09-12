// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingresources

import (
	"strconv"
	"strings"
	"testing"
)

func TestPlatformSlicesPreserveReserveAndAvoidRemovedSwitches(t *testing.T) {
	t.Parallel()
	for _, processors := range []uint64{1, 2, 4, 64, 16384} {
		accounts := AccountsSliceUnit(processors)
		if !strings.Contains(accounts, "CPUQuota="+strconv.FormatUint(processors*80, 10)+"%\n") ||
			!strings.Contains(accounts, "MemoryMax=80%\n") || !strings.Contains(CoreSliceUnit(), "MemoryLow=20%\n") {
			t.Fatal("platform capacity reserve changed")
		}
		for _, removed := range []string{"CPUAccounting=", "IOAccounting=", "MemoryAccounting=", "TasksAccounting="} {
			if strings.Contains(accounts, removed) || strings.Contains(CoreSliceUnit(), removed) {
				t.Fatal("shared platform unit contains removed switch", removed)
			}
		}
	}
}
