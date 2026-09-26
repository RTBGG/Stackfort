// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNativeInitrdInventoryProcessHelper(t *testing.T) {
	// Private test-process protocol; never compiled into the production binary.
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--initrd-inventory-helper" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "large" {
		_, _ = io.WriteString(os.Stdout, initrdReportedSizeFixture(t))
	} else {
		_, _ = io.WriteString(os.Stdout, initrdRequiredFixture())
	}
	switch mode {
	case "failure":
		_, _ = fmt.Fprintln(os.Stderr, "synthetic decompressor failure")
		os.Exit(3)
	case "stderr-overflow":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", (64<<10)+1))
	case "forbidden-tail":
		_, _ = io.WriteString(os.Stdout, "stackfort-native-quota.test\n")
	case "timeout":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func TestNativeInitrdInventoryProcessBoundary(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"large", "failure", "stderr-overflow", "forbidden-tail", "timeout", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if mode == "timeout" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 250*time.Millisecond)
				defer stop()
			} else if mode == "cancelled" {
				cancel()
			}
			command := exec.CommandContext(ctx, self, "-test.run=^TestNativeInitrdInventoryProcessHelper$", "--", "--initrd-inventory-helper", mode)
			err := runNativeInitrdInventory(ctx, command)
			if (err == nil) != (mode == "large") {
				t.Fatal(mode, err)
			}
			if mode == "failure" && !strings.Contains(err.Error(), "synthetic decompressor failure") {
				t.Fatal("command failure was hidden", err)
			}
		})
	}
	// The short-output route must not silently reintroduce the old listing cap.
	if _, err := nativeBootCommand(t.Context(), "/usr/bin/lsinitramfs", "/not-used"); err == nil || err.Error() != "invalid boot command" {
		t.Fatal("inventory still reachable through short-output runner", err)
	}
}

func TestNativeInitrdInventoryReproducesLegacyCap(t *testing.T) {
	var oldOutput nativeBootOutput
	if _, err := io.Copy(&oldOutput, strings.NewReader(initrdReportedSizeFixture(t))); err == nil || !oldOutput.overflow {
		t.Fatal("reported-size fixture did not reproduce the old 64 KiB rejection")
	}
	var inventory nativeInitrdInventory
	if _, err := io.Copy(&inventory, strings.NewReader(initrdReportedSizeFixture(t))); err != nil || inventory.finish() != nil {
		t.Fatal("same fixture not accepted by bounded streaming verification", err)
	}
}
