// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"os"
	"strings"
)

func nativeHostFirewall(ctx context.Context, packages map[string]string) error {
	for _, unit := range []string{"nftables.service", "ufw.service", "firewalld.service", "netfilter-persistent.service"} {
		text, code, err := nativeHostCommand(ctx, "/usr/bin/systemctl", "show", "--property=LoadState", "--property=ActiveState", "--property=UnitFileState", unit)
		if err != nil || code != 0 || nativeFirewallServiceState(text) != nil {
			return errors.New("conflicting or uninspectable firewall manager: " + unit)
		}
	}
	// A missing nft prerequisite is handled by the reviewed package plan. This
	// check repeats after its installation, before native boot preparation.
	if nativeInstalled(packages, "nftables") != "" {
		text, code, err := nativeHostCommand(ctx, "/usr/sbin/nft", "list", "tables")
		if err != nil || code != 0 || strings.TrimSpace(text) != "" {
			return errors.New("existing or uninspectable nftables rules; no policy will be replaced")
		}
	}
	for _, path := range []string{"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names"} {
		data, err := nativeHostKernelFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || strings.TrimSpace(string(data)) != "" {
			return errors.New("existing or uninspectable legacy iptables tables")
		}
	}
	return nil
}
