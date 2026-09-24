// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

import (
	"errors"
	"regexp"
	"strings"
)

var primaryMBRUUID = regexp.MustCompile(`^[0-9a-f]{8}-0[1-4]$`)

// PartitionTable derives the table type from a canonical kernel PARTUUID.
// GPT uses a nonzero UUID; DOS uses the nonzero disk signature and primary
// partition number. Logical partitions, aliases and PARTNROFF are not accepted.
// The actual table, partition type and identity must also be inspected at each
// host/offline boundary. The short MBR signature is never sufficient authority
// by itself: plans additionally bind the filesystem UUID, machine and geometry.
func PartitionTable(partitionUUID string) (string, error) {
	if canonicalUUID(partitionUUID) {
		return "gpt", nil
	}
	if primaryMBRUUID.MatchString(partitionUUID) && !strings.HasPrefix(partitionUUID, "00000000-") {
		return "dos", nil
	}
	return "", errors.New("invalid native PARTUUID: expected canonical GPT UUID or nonzero MBR signature with primary partition 01-04")
}

func grubPartitionModule(plan Plan) string {
	table, _ := PartitionTable(plan.PartitionUUID) // Callers first validate the plan.
	if table == "dos" {
		return "part_msdos"
	}
	return "part_gpt"
}
