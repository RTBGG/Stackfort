// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"math"
	"testing"
)

func TestNativeInitialCapacityHeadroom(t *testing.T) {
	for _, row := range []struct {
		name           string
		size           int64
		blocks, inodes uint64
		valid          bool
	}{
		{"exact", 4096, nativeMinimumFreeBytes / 4096, nativeMinimumFreeInodes, true},
		{"one-block-short", 4096, nativeMinimumFreeBytes/4096 - 1, nativeMinimumFreeInodes, false},
		{"one-inode-short", 4096, nativeMinimumFreeBytes / 4096, nativeMinimumFreeInodes - 1, false},
		{"zero-inodes", 4096, math.MaxUint64, 0, false},
		{"large-count-no-overflow", 4096, math.MaxUint64, math.MaxUint64, true},
		{"rounding-up", 3000, nativeMinimumFreeBytes / 3000, nativeMinimumFreeInodes, false},
		{"rounding-up-enough", 3000, nativeMinimumFreeBytes/3000 + 1, nativeMinimumFreeInodes, true},
		{"zero-block-size", 0, math.MaxUint64, math.MaxUint64, false},
		{"negative-block-size", -1, math.MaxUint64, math.MaxUint64, false},
		{"excessive-block-size", math.MaxInt64, math.MaxUint64, math.MaxUint64, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if err := nativeCheckInitialCapacity(row.size, row.blocks, row.inodes); (err == nil) != row.valid {
				t.Fatal(err)
			}
		})
	}
}
