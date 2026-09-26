// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func initrdRequiredFixture() string { return strings.Join(nativeInitrdRequired[:], "\n") + "\n" }

// Synthetic paths, matching only the operator-reported dimensions. No customer
// initrd, filesystem names, sealed records or setup credentials are test inputs.
func initrdReportedSizeFixture(t *testing.T) string {
	t.Helper()
	const size, entries = 93120, 1578
	var fixture strings.Builder
	fixture.WriteString(initrdRequiredFixture())
	remainingEntries := entries - len(nativeInitrdRequired)
	remainingBytes := size - fixture.Len()
	for i := 0; i < remainingEntries; i++ {
		prefix := fmt.Sprintf("usr/lib/modules/synthetic/driver-%04d-", i)
		lineSize := remainingBytes / (remainingEntries - i)
		fixture.WriteString(prefix + strings.Repeat("x", lineSize-len(prefix)-4) + ".ko\n")
		remainingBytes -= lineSize
	}
	result := fixture.String()
	if len(result) != size || strings.Count(result, "\n") != entries {
		t.Fatal("regression fixture does not reproduce reported listing dimensions")
	}
	return result
}

func TestNativeInitrdInventoryReportedLargeListing(t *testing.T) {
	listing := initrdReportedSizeFixture(t)
	for _, chunk := range []int{1, 7, 4096, 65536, len(listing)} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			var inventory nativeInitrdInventory
			for start := 0; start < len(listing); start += chunk {
				part := listing[start:min(start+chunk, len(listing))]
				if n, err := inventory.Write([]byte(part)); err != nil || n != len(part) {
					t.Fatalf("large legitimate listing rejected: n=%d err=%v", n, err)
				}
			}
			if err := inventory.finish(); err != nil || inventory.bytes != 93120 || inventory.entries != 1578 {
				t.Fatal("reported-size inventory rejected", err)
			}
		})
	}
	var tail nativeInitrdInventory
	listing = strings.TrimPrefix(listing, initrdRequiredFixture()) + initrdRequiredFixture()
	if _, err := io.Copy(&tail, strings.NewReader(listing)); err != nil || tail.finish() != nil {
		t.Fatal("required members beyond the old output limit were not recognized", err)
	}
}

func TestNativeInitrdInventoryRequiredAndForbiddenPaths(t *testing.T) {
	for _, missing := range nativeInitrdRequired {
		t.Run(missing, func(t *testing.T) {
			listing := strings.ReplaceAll(initrdRequiredFixture(), missing+"\n", "prefix/"+missing+"\n"+missing+".bak\n")
			var inventory nativeInitrdInventory
			if _, err := io.WriteString(&inventory, listing); err != nil {
				t.Fatal(err)
			}
			if err := inventory.finish(); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatal("missing exact member accepted", err)
			}
		})
	}
	for _, listing := range []string{
		initrdRequiredFixture() + "usr/bin/stackfort-native-quota.test\n",
		initrdReportedSizeFixture(t) + "hidden/stackfort-native-quota.test.extra\n",
		strings.TrimSuffix(initrdRequiredFixture(), "\n"),
		initrdRequiredFixture() + "partial-tail",
		"",
	} {
		var inventory nativeInitrdInventory
		_, writeErr := io.Copy(&inventory, strings.NewReader(listing))
		if writeErr == nil && inventory.finish() == nil {
			t.Fatal("forbidden, missing or truncated inventory accepted")
		}
	}
}

func initrdByteBoundaryFixture(size int) string {
	var listing strings.Builder
	listing.WriteString(initrdRequiredFixture())
	for listing.Len() < size {
		length := min(1024, size-listing.Len())
		listing.WriteString(strings.Repeat("x", length-1) + "\n")
	}
	return listing.String()
}

func TestNativeInitrdInventoryLimitsRemainFailClosed(t *testing.T) {
	for _, test := range []struct {
		name, listing string
		valid         bool
	}{
		{"line-exact", initrdRequiredFixture() + strings.Repeat("x", nativeInitrdMaximumLine) + "\n", true},
		{"line-over", initrdRequiredFixture() + strings.Repeat("x", nativeInitrdMaximumLine+1) + "\n", false},
		{"entries-exact", initrdRequiredFixture() + strings.Repeat("x\n", nativeInitrdMaximumEntries-len(nativeInitrdRequired)), true},
		{"entries-over", initrdRequiredFixture() + strings.Repeat("x\n", nativeInitrdMaximumEntries-len(nativeInitrdRequired)+1), false},
		{"bytes-exact", initrdByteBoundaryFixture(nativeInitrdMaximumBytes), true},
		{"bytes-over", initrdByteBoundaryFixture(nativeInitrdMaximumBytes + 1), false},
		{"nul", initrdRequiredFixture() + "bad\x00name\n", false},
		{"cr", initrdRequiredFixture() + "bad\rname\n", false},
		{"ansi", initrdRequiredFixture() + "bad\x1b[31m\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var inventory nativeInitrdInventory
			_, writeErr := io.Copy(&inventory, strings.NewReader(test.listing))
			err := inventory.finish()
			if (writeErr == nil && err == nil) != test.valid {
				t.Fatal("unexpected boundary result", writeErr, err)
			}
			if !test.valid {
				if _, retryErr := inventory.Write([]byte(initrdRequiredFixture())); retryErr == nil || inventory.finish() == nil {
					t.Fatal("failed inventory was reusable")
				}
			}
		})
	}
}

func TestNativeInitrdInventoryCannotResumeAfterFinish(t *testing.T) {
	var inventory nativeInitrdInventory
	if _, err := io.WriteString(&inventory, initrdRequiredFixture()); err != nil || inventory.finish() != nil {
		t.Fatal(err)
	}
	if _, err := inventory.Write([]byte("late\n")); err == nil || inventory.finish() == nil {
		t.Fatal("completed inventory reused")
	}
}
