// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"fmt"
	"strings"
)

// lsinitramfs can legitimately list far more than 64 KiB on a generic kernel.
// Keep only one bounded line and five membership bits, not the complete output.
// These limits are independent of the short command/attestation diagnostics.
const nativeInitrdMaximumBytes = 8 << 20
const nativeInitrdMaximumLine = 4096
const nativeInitrdMaximumEntries = 65536

var nativeInitrdRequired = [...]string{
	"stackfort-native-installer",
	"etc/stackfort-native/release.json",
	"etc/stackfort-native/runtime.json",
	"scripts/local-premount/stackfort-native-quota",
	"usr/sbin/debugfs",
}

type nativeInitrdInventory struct {
	line    [nativeInitrdMaximumLine]byte
	length  int
	bytes   int
	entries int
	seen    [len(nativeInitrdRequired)]bool
	err     error
	closed  bool
}

func (inventory *nativeInitrdInventory) Write(data []byte) (int, error) {
	if inventory.err != nil {
		return 0, inventory.err
	}
	if inventory.closed {
		inventory.err = errors.New("initrd inventory already finalized")
		return 0, inventory.err
	}
	for i, value := range data {
		if inventory.bytes >= nativeInitrdMaximumBytes {
			inventory.err = errors.New("initrd inventory exceeds 8 MiB byte limit")
			return i, inventory.err
		}
		inventory.bytes++
		if value == '\n' {
			if err := inventory.acceptLine(); err != nil {
				inventory.err = err
				return i + 1, err
			}
			continue
		}
		if value < 0x20 || value == 0x7f {
			inventory.err = errors.New("initrd inventory contains a control character")
			return i + 1, inventory.err
		}
		if inventory.length == len(inventory.line) {
			inventory.err = errors.New("initrd inventory line exceeds 4096-byte limit")
			return i + 1, inventory.err
		}
		inventory.line[inventory.length] = value
		inventory.length++
	}
	return len(data), nil
}

func (inventory *nativeInitrdInventory) acceptLine() error {
	if inventory.entries == nativeInitrdMaximumEntries {
		return errors.New("initrd inventory exceeds 65536-entry limit")
	}
	inventory.entries++
	line := string(inventory.line[:inventory.length])
	inventory.length = 0
	if strings.Contains(line, "stackfort-native-quota.test") {
		return errors.New("test executable in initrd")
	}
	for index, required := range nativeInitrdRequired {
		if line == required {
			inventory.seen[index] = true
		}
	}
	return nil
}

// Only call after the process completed successfully. Membership alone cannot
// turn a failed, cancelled, truncated or over-limit listing into a valid image.
func (inventory *nativeInitrdInventory) finish() error {
	if inventory.err != nil {
		return inventory.err
	}
	if inventory.closed {
		return errors.New("initrd inventory already finalized")
	}
	inventory.closed = true
	if inventory.length != 0 {
		inventory.err = errors.New("initrd inventory has an unterminated line")
		return inventory.err
	}
	for index, required := range nativeInitrdRequired {
		if !inventory.seen[index] {
			inventory.err = fmt.Errorf("incomplete one-shot initrd: %s", required)
			return inventory.err
		}
	}
	return nil
}
