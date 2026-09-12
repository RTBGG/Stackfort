// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"maps"
	"strings"
	"testing"
)

func TestAgentConfigurationWritesPreserveRemainingSystemSandbox(t *testing.T) {
	t.Parallel()
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		unit := serviceUnits(distribution)["stackfort-agent.service"]
		if strings.Count(unit, "ReadWritePaths=") != 1 || !strings.Contains(unit, "\nReadWritePaths=/etc\n") {
			t.Fatalf("%s agent must permit atomic /etc directory operations, not per-file binds or unrelated writable roots", distribution)
		}
		for _, remaining := range []string{
			"ProtectSystem=yes", "ProtectHome=no", "InaccessiblePaths=/home /root", "PrivateTmp=yes", "PrivateDevices=no",
			"NoNewPrivileges=no", "ProtectControlGroups=yes", "ProtectKernelModules=yes",
			"ProtectKernelTunables=yes", "ProtectKernelLogs=yes", "ProtectClock=yes",
			"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK",
		} {
			if !strings.Contains(unit, "\n"+remaining+"\n") {
				t.Fatalf("%s agent lost unrelated protection %s", distribution, remaining)
			}
		}
		controlUnit := serviceUnits(distribution)["stackfort-api.service"]
		if strings.Contains(controlUnit, "ReadWritePaths=/etc") || !strings.Contains(controlUnit, "ProtectSystem=strict\n") ||
			!strings.Contains(controlUnit, "NoNewPrivileges=yes\n") || !strings.Contains(controlUnit, "PrivateDevices=yes\n") ||
			!strings.Contains(controlUnit, "ProtectHome=yes\n") {
			t.Fatal("privileged broker exception leaked into the control API")
		}
	}
}

func TestServiceAdmissionRejectsAgentWritablePathDrift(t *testing.T) {
	t.Parallel()
	const unit = "stackfort-agent.service"
	good := requiredServiceSandboxes()[unit]
	if good["ReadWritePaths"] != "/etc" || good["ProtectSystem"] != "yes" || verifyServiceSandbox(unit, good) != nil {
		t.Fatal("current provisioning sandbox is not the exact admission contract")
	}
	for _, paths := range []string{"", "/etc/passwd /etc/group /etc/shadow /etc/gshadow", "/etc /usr", "/", "/etc /srv", "-/etc"} {
		observed := maps.Clone(good)
		observed["ReadWritePaths"] = paths
		if verifyServiceSandbox(unit, observed) == nil {
			t.Fatalf("admission accepted stale or expanded writable paths %q", paths)
		}
	}
	for _, property := range []string{"User", "ProtectSystem", "PrivateDevices", "NoNewPrivileges"} {
		observed := maps.Clone(good)
		delete(observed, property)
		if verifyServiceSandbox(unit, observed) == nil {
			t.Fatalf("admission accepted missing %s", property)
		}
	}
	for property, changed := range map[string]string{
		"ProtectSystem": "no", "ProtectHome": "yes", "InaccessiblePaths": "/home",
		"NoNewPrivileges": "yes", "PrivateDevices": "yes", "ProtectControlGroups": "no",
		"RestrictAddressFamilies": "AF_UNIX AF_INET AF_INET6", "ProtectKernelModules": "no",
	} {
		observed := maps.Clone(good)
		observed[property] = changed
		if verifyServiceSandbox(unit, observed) == nil {
			t.Fatalf("admission accepted incompatible or broadened %s=%q", property, changed)
		}
	}
	for _, changed := range []string{"/home /root /run/user", "-/home /root", "/home /home /root"} {
		observed := maps.Clone(good)
		observed["InaccessiblePaths"] = changed
		if verifyServiceSandbox(unit, observed) == nil {
			t.Fatalf("admission accepted inaccessible-path drift %q", changed)
		}
	}
	ordered := maps.Clone(good)
	ordered["RestrictAddressFamilies"] = "AF_NETLINK AF_INET6 AF_INET AF_UNIX"
	ordered["InaccessiblePaths"] = "/root /home"
	if verifyServiceSandbox(unit, ordered) != nil {
		t.Fatal("systemd list ordering changed semantic admission")
	}
	if verifyServiceSandbox("unrelated.service", good) == nil {
		t.Fatal("unknown service admitted")
	}
}
