// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import "errors"

const nativeMinimumFreeBytes uint64 = 8 << 30
const nativeMinimumFreeInodes uint64 = 100_000

// This is installation headroom, NOT a durable OS reserve or an aggregate
// hosting quota policy. It is rechecked by each live host inspection.
func nativeCheckInitialCapacity(blockSize int64, availableBlocks, freeInodes uint64) error {
	if blockSize <= 0 || uint64(blockSize) > 1<<20 {
		return errors.New("invalid native filesystem block size")
	}
	// Divide the fixed minimum with rounding up; never multiply untrusted block
	// counts (which may overflow) or round the required capacity down.
	requiredBlocks := (nativeMinimumFreeBytes + uint64(blockSize) - 1) / uint64(blockSize)
	if availableBlocks < requiredBlocks || freeInodes < nativeMinimumFreeInodes {
		return errors.New("native installation requires at least 8 GiB free root space and 100000 free inodes")
	}
	return nil
}
