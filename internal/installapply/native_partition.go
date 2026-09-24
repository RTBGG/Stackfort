// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

// All values come from read-only low-level blkid probes, never caller input.
// MBR support intentionally excludes logical, extended, inactive, non-Linux
// and UEFI layouts. GPT keeps its existing validation and journal encoding.
func nativeValidatePartition(partitionUUID string, values map[string]string, firmware string) error {
	table, err := storageprep.PartitionTable(partitionUUID)
	if err != nil {
		return err
	}
	for key, expected := range map[string]string{"TYPE": "ext4", "PART_ENTRY_SCHEME": table, "PART_ENTRY_UUID": partitionUUID} {
		if values[key] != expected {
			return fmt.Errorf("native partition mismatch: %s (requires ext4 on GPT or a supported primary MBR partition)", key)
		}
	}
	if table == "dos" {
		if firmware != "bios" {
			return errors.New("native MBR preparation requires BIOS/GRUB; UEFI MBR is not qualified")
		}
		if values["PART_ENTRY_TYPE"] != "0x83" || values["PART_ENTRY_FLAGS"] != "0x80" ||
			values["PART_ENTRY_NUMBER"] != strings.TrimPrefix(partitionUUID[9:], "0") {
			return errors.New("native MBR root must be an active primary Linux partition (01-04, type 0x83)")
		}
	}
	return nil
}
