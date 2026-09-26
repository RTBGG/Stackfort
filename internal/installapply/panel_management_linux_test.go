// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package installapply

import (
	"errors"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"os"
	"testing"
)

// Read-only test against an explicitly selected completed native fixture.
func TestPanelManagementCompletedNativeHost(t *testing.T) {
	if os.Getenv("STACKFORT_TEST_COMPLETED_PANEL_HOST") != "1" {
		t.Skip("completed native host opt-in required")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root required")
	}
	guard, err := AcquirePanelManagement(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if _, err := AcquirePanelManagement(t.Context()); err == nil {
		t.Fatal("shared installer lock not retained")
	}
	if _, _, err := NewFileStore().Load(); !errors.Is(err, storageprep.ErrNotQualified) {
		t.Fatalf("ordinary installer bypassed native guard: %v", err)
	}
	t.Log("Completed native panel management allowed; generic installer remains blocked; shared lock retained")
}
