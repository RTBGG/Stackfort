// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"strings"
)

// The fresh native profile does not adopt an active firewall manager. In
// particular Debian's nftables.service can flush the entire ruleset on stop,
// unlike Stackfort's dedicated-table service. Never disable it automatically.
func nativeFirewallServiceState(text string) error {
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || (key != "LoadState" && key != "ActiveState" && key != "UnitFileState") {
			return errors.New("unknown firewall service state")
		}
		if _, duplicate := values[key]; duplicate {
			return errors.New("duplicate firewall service state")
		}
		values[key] = value
	}
	if len(values) != 3 || values["ActiveState"] != "inactive" {
		return errors.New("firewall manager is active or its state is incomplete")
	}
	load, enabled := values["LoadState"], values["UnitFileState"]
	if load == "not-found" && enabled == "" || load == "loaded" && enabled == "disabled" || load == "masked" && enabled == "masked" {
		return nil
	}
	return errors.New("firewall manager is enabled or has an unsupported unit policy")
}
