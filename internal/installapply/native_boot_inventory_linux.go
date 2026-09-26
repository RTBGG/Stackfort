// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

func verifyNativeBootInitrd(ctx context.Context, imagePath string) error {
	if ctx == nil || ctx.Err() != nil {
		return errors.New("inactive initrd inventory context")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- Fixed read-only executable; imagePath is the operation-bound
	// /boot path derived internally by Arm, never a public command override.
	command := exec.CommandContext(bounded, "/usr/bin/lsinitramfs", imagePath)
	return runNativeInitrdInventory(bounded, command)
}

// Private command seam for process-failure tests. Production constructs only
// the fixed lsinitramfs invocation above; no environment/CLI test override.
func runNativeInitrdInventory(ctx context.Context, command *exec.Cmd) error {
	var inventory nativeInitrdInventory
	var stderr nativeBootOutput
	containNativeCommand(command)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdout, command.Stderr = &inventory, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("initrd inventory command interrupted: %w", ctx.Err())
	}
	if inventory.err != nil {
		return inventory.err
	}
	if stderr.overflow {
		return errors.New("initrd inventory stderr exceeds 64 KiB limit")
	}
	if err != nil {
		return fmt.Errorf("native boot lsinitramfs: %w: %s", err, stderr.String())
	}
	return inventory.finish()
}
