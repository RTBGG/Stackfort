// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"fmt"
	"slices"
	"strings"
)

// The privileged broker creates shadow-utils lock/temporary files and atomically
// renames passwd/group/shadow/gshadow/subuid/subgid in /etc. Per-file bind mounts
// cannot preserve those semantics. It also owns fixed NGINX, PHP, systemd,
// container and (on Rocky) SELinux configuration below /etc. This is deliberate
// root provisioning authority, not a sandbox for untrusted tenant application
// code; the peer-UID RPC boundary and closed semantic command profiles remain.
const agentWritableConfigurationRoot = "/etc"

const agentWritableConfigurationExplanation = "# The root provisioning broker requires directory-level /etc writes for\n" +
	"# atomic shadow-utils databases and managed service/runtime configuration.\n" +
	"# ProtectSystem=yes keeps /usr and /boot read-only; full would also lock /etc.\n" +
	"# Rootless UID-map helpers need privilege transitions after the account UID\n" +
	"# drop. Quota/FUSE need host devices, and Podman needs netlink plus /run/user.\n" +
	"# Rootless pause namespaces must retain the writable delegated cgroup view;\n" +
	"# per-account ownership/delegation and resource limits remain authoritative.\n" +
	"# This is a privileged broker, NOT a sandbox for tenant application code.\n"

// Use the same exact properties for initial activation and completed native
// admission. A stale unit or an additional writable path must fail admission.
func requiredServiceSandboxes() map[string]map[string]string {
	return map[string]map[string]string{
		"vinyl.service": {
			"User": "vinyl", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict",
		},
		"stackfort-agent.service": {
			"User": "root", "Group": "root", "Slice": "stackfort-core.slice", "NoNewPrivileges": "no",
			"PrivateDevices": "no", "PrivateTmp": "yes", "ProtectSystem": "yes",
			"ProtectHome": "no", "InaccessiblePaths": "/home /root", "ReadWritePaths": agentWritableConfigurationRoot,
			"ProtectClock": "yes", "ProtectControlGroups": "no", "ProtectKernelLogs": "yes",
			"ProtectKernelModules": "yes", "ProtectKernelTunables": "yes", "LockPersonality": "yes",
			"RestrictRealtime": "yes", "RestrictAddressFamilies": "AF_UNIX AF_INET AF_INET6 AF_NETLINK",
		},
		"stackfort-api.service": {
			"User": "stackfort", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict",
		},
		phpMyAdminUnit: {
			"User": "stackfort-pma", "Slice": "stackfort-core.slice", "NoNewPrivileges": "yes",
			"PrivateDevices": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict",
		},
	}
}

func verifyServiceSandbox(unit string, observed map[string]string) error {
	required, exists := requiredServiceSandboxes()[unit]
	if !exists {
		return fmt.Errorf("service sandbox is not allowlisted: %s", unit)
	}
	for key, wanted := range required {
		actual, present := observed[key]
		equal := actual == wanted
		if key == "InaccessiblePaths" || key == "RestrictAddressFamilies" {
			// PID 1 may canonicalize list order, but no missing, additional,
			// negated, duplicate or differently spelled value is accepted.
			a, b := strings.Fields(actual), strings.Fields(wanted)
			slices.Sort(a)
			slices.Sort(b)
			equal = slices.Equal(a, b)
		}
		if !present || !equal {
			return fmt.Errorf("systemd sandbox mismatch for %s: %s=%q", unit, key, observed[key])
		}
	}
	return nil
}
