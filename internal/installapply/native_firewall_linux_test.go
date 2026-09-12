// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNativeFirewallReadOnlyCommandIsAllowedWithoutBroadeningMutation(t *testing.T) {
	// A cancelled context proves dispatch validation without ever running nft,
	// needing NET_ADMIN, inspecting real policy, or altering a host's rules.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := nativeHostCommand(ctx, "/usr/sbin/nft", "list", "tables"); !errors.Is(err, context.Canceled) {
		t.Fatal("fixed read-only nft inspection was rejected before dispatch", err)
	}
	for _, arguments := range [][]string{{"flush", "ruleset"}, {"-f", "/tmp/policy"}, {"list", "ruleset"}, {"list", "tables", "extra"}, {}} {
		if _, _, err := nativeHostCommand(ctx, "/usr/sbin/nft", arguments...); err == nil || !strings.Contains(err.Error(), "invalid host inspection command") {
			t.Fatal("nft allowlist broadened beyond fixed read-only call", arguments, err)
		}
	}
}

func TestNativeHostKernelFileAllowlistIncludesFirewallButRejectsForeignInputs(t *testing.T) {
	for _, path := range []string{"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names"} {
		if _, err := nativeHostKernelFile(path); err != nil && strings.Contains(err.Error(), "unsupported native host inspection file") {
			t.Fatal("kernel firewall file omitted", path, err)
		}
	}
	for _, path := range []string{"/etc/shadow", "/proc/self/environ", "/proc/net/../sys/kernel/osrelease", "/sys/firmware/efi/efivars/SecureBoot-unknown", "/proc/net/ip_tables_names/", "relative", ""} {
		if _, err := nativeHostKernelFile(path); err == nil || !strings.Contains(err.Error(), "unsupported native host inspection file") {
			t.Fatal("foreign kernel inspection input accepted", path, err)
		}
	}
}
