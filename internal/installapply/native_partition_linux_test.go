// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"os"
	"strings"
	"testing"
)

// Read-only live block-device regression, opt-in and pinned to the disposable
// MBR fixture. No partition table, filesystem or boot configuration is changed.
func TestNativeMBRLivePartitionProbe(t *testing.T) {
	if os.Getenv("STACKFORT_NATIVE_MBR_PROBE") != "1" {
		t.Skip("dedicated MBR lab only")
	}
	dmi, err := os.ReadFile("/sys/class/dmi/id/product_uuid")
	host, hostErr := os.Hostname()
	if err != nil || hostErr != nil || os.Geteuid() != 0 || host != "stackfort-native-mbr-debian-13" ||
		strings.TrimSpace(string(dmi)) != "91d127c9-40e6-ab48-9528-5085e8888729" {
		t.Fatal("wrong disposable MBR host")
	}
	device, _, err := nativeRootDevice(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := nativeCheckPartition(t.Context(), device, "7c92ab10-01"); err != nil {
		t.Fatal(err)
	}
	for _, foreign := range []string{"7c92ab11-01", "7c92ab10-02", "7c92ab10-05", testPrerequisiteRecord().Before.Host.PartitionUUID} {
		if nativeCheckPartition(t.Context(), device, foreign) == nil {
			t.Fatal("actual MBR device accepted under foreign partition/disk/table identity", foreign)
		}
	}
	before, err := nativeBootSuper(t.Context(), device)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := (nativeReadyBackend{}).Observe(t.Context())
	if err != nil || observed.PartitionUUID != "7c92ab10-01" {
		t.Fatal(observed, err)
	}
	t.Log("Actual blkid MBR metadata accepted; disk-signature, partition-number, logical-partition and GPT substitution rejected read-only")
	after, err := nativeBootSuper(t.Context(), device)
	if err != nil || before["Filesystem features"] != after["Filesystem features"] || before["Filesystem UUID"] != after["Filesystem UUID"] {
		t.Fatal("read-only probe changed filesystem", err)
	}
}
