// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import "testing"

func TestNativeFirewallServiceAllowsOnlyInactiveUnmanagedState(t *testing.T) {
	for _, text := range []string{
		"LoadState=not-found\nActiveState=inactive\nUnitFileState=\n",
		"LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n",
		"LoadState=masked\nActiveState=inactive\nUnitFileState=masked\n",
	} {
		if err := nativeFirewallServiceState(text); err != nil {
			t.Fatal(err)
		}
	}
	for _, text := range []string{
		"", "LoadState=not-found\n", "LoadState=not-found\nActiveState=inactive\n",
		"LoadState=loaded\nActiveState=active\nUnitFileState=disabled\n",
		"LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled\n",
		"LoadState=loaded\nActiveState=inactive\nUnitFileState=enabled-runtime\n",
		"LoadState=loaded\nActiveState=inactive\nUnitFileState=static\n",
		"LoadState=not-found\nActiveState=inactive\nUnitFileState=\nUnitFileState=\n",
		"LoadState=not-found\nActiveState=inactive\nUnitFileState=\nUnexpected=value\n",
	} {
		if nativeFirewallServiceState(text) == nil {
			t.Fatal("unsafe service state accepted")
		}
	}
	for _, name := range []string{"ufw", "firewalld", "iptables-persistent", "netfilter-persistent:amd64"} {
		if !nativeConflictingPackage(name) {
			t.Fatal("foreign firewall package accepted")
		}
	}
}
