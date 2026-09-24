// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func nativeCheckPartition(ctx context.Context, device, partitionUUID string) error {
	table, err := storageprep.PartitionTable(partitionUUID)
	if err != nil {
		return err
	}
	keys := []string{"TYPE", "PART_ENTRY_SCHEME", "PART_ENTRY_UUID"}
	firmware := ""
	if table == "dos" {
		keys = append(keys, "PART_ENTRY_TYPE", "PART_ENTRY_FLAGS", "PART_ENTRY_NUMBER")
		firmware, err = nativeHostSecureBoot()
		if err != nil {
			return err
		}
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key], err = nativeReadCommand(ctx, "/usr/sbin/blkid", "-p", "-s", key, "-o", "value", device)
		if err != nil {
			return err
		}
	}
	return nativeValidatePartition(partitionUUID, values, firmware)
}
