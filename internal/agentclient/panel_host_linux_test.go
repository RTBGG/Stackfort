// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package agentclient

import (
	"os"
	"testing"
)

// This opt-in probe is read-only and must use a separate qualification agent,
// never the installed release's socket. Run it as the real API service identity.
func TestPanelCompletedNativeAgentSocket(t *testing.T) {
	if os.Getenv("STACKFORT_TEST_PANEL_AGENT") != "1" {
		t.Skip("completed native host with an isolated qualification agent required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("run as the unprivileged API service identity")
	}
	client, err := New("/run/stackfort-panel-qualification/agent.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	status, err := client.InspectPanel(t.Context(), "qualification-panel-status")
	if err != nil {
		t.Fatal(err)
	}
	if status.RecoveryRequired {
		t.Fatal("qualification host has an interrupted panel transaction")
	}
	t.Logf("Real service-identity RPC accepted; panel enabled=%t, autoRenew=%t", status.Enabled, status.AutoRenew)
}
