// SPDX-License-Identifier: AGPL-3.0-or-later

// Package installpreflight turns bounded, read-only host observations into an
// actionable readiness decision and a deterministic installation plan.
package installpreflight

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

const (
	SchemaVersion      = 1
	MinimumLogicalCPUs = 2
	// A nominal 4 GiB VM exposes slightly less MemTotal after firmware and
	// kernel reservations. 3.5 GiB accepts all target distributions without
	// accepting a nominal 3 GiB guest.
	MinimumMemoryBytes      = 7 << 29
	MinimumFreeStorageBytes = 5 << 30
)

type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckWarn CheckStatus = "warning"
	CheckFail CheckStatus = "fail"
)

type ResourceReport struct {
	LogicalCPUs       int                      `json:"logicalCpus"`
	CPUInspection     agentprotocol.Capability `json:"cpuInspection"`
	MemoryTotalBytes  uint64                   `json:"memoryTotalBytes"`
	MemoryInspection  agentprotocol.Capability `json:"memoryInspection"`
	StorageTarget     string                   `json:"storageTarget"`
	StorageTotalBytes uint64                   `json:"storageTotalBytes"`
	StorageFreeBytes  uint64                   `json:"storageFreeBytes"`
	StorageInspection agentprotocol.Capability `json:"storageInspection"`
}

type Check struct {
	ID          string      `json:"id"`
	Status      CheckStatus `json:"status"`
	Summary     string      `json:"summary"`
	Detail      string      `json:"detail,omitempty"`
	ReasonCode  string      `json:"reasonCode,omitempty"`
	Remediation string      `json:"remediation,omitempty"`
}

type PackagePlan struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type FilePlan struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Owner  string `json:"owner"`
	Mode   string `json:"mode"`
}

type UserPlan struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Home   string `json:"home"`
	Shell  string `json:"shell"`
}

type ServicePlan struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

type PortPlan struct {
	Endpoint string `json:"endpoint"`
	Scope    string `json:"scope"`
	Purpose  string `json:"purpose"`
}

type SecurityPlan struct {
	Provider string `json:"provider"`
	Action   string `json:"action"`
}

type Plan struct {
	Packages []PackagePlan  `json:"packages"`
	Files    []FilePlan     `json:"files"`
	Users    []UserPlan     `json:"users"`
	Services []ServicePlan  `json:"services"`
	Ports    []PortPlan     `json:"ports"`
	Security []SecurityPlan `json:"securityModuleChanges"`
}

type Result struct {
	SchemaVersion int                            `json:"schemaVersion"`
	ReadOnly      bool                           `json:"readOnly"`
	Ready         bool                           `json:"ready"`
	Capabilities  agentprotocol.CapabilityReport `json:"capabilities"`
	Resources     ResourceReport                 `json:"resources"`
	Checks        []Check                        `json:"checks"`
	Plan          Plan                           `json:"plan"`
}

func Evaluate(capabilities agentprotocol.CapabilityReport, resources ResourceReport) Result {
	checks := []Check{
		capabilityCheck("platform.support", "Supported operating system", capabilities.Platform.Support,
			"Use a fresh Debian 13, Ubuntu 26.04 LTS, or Rocky Linux 10 host."),
		architectureCheck(capabilities.Platform.Architecture),
		capabilityCheck("init.systemd", "systemd is PID 1", capabilities.Systemd,
			"Boot the host with systemd as PID 1; containers and WSL are not supported installation targets."),
		resourceMinimumCheck("resources.cpu", "Logical CPU capacity", resources.CPUInspection,
			nonNegativeUint64(resources.LogicalCPUs), uint64(MinimumLogicalCPUs), "logical CPUs",
			"Assign at least 2 logical CPUs to the host."),
		memoryMinimumCheck(resources),
		capabilityCheck("cgroup.unified", "Unified cgroup v2 hierarchy", capabilities.Cgroup.Unified,
			"Enable the unified cgroup v2 hierarchy and reboot the host."),
		capabilityCheck("cgroup.cpu", "cgroup CPU controller", capabilities.Cgroup.CPU,
			"Make the cgroup v2 CPU controller available to systemd."),
		capabilityCheck("cgroup.memory", "cgroup memory controller", capabilities.Cgroup.Memory,
			"Make the cgroup v2 memory controller available to systemd."),
		capabilityCheck("cgroup.io", "cgroup I/O controller", capabilities.Cgroup.IO,
			"Make the cgroup v2 I/O controller available to systemd."),
		capabilityCheck("cgroup.pids", "cgroup process controller", capabilities.Cgroup.PIDs,
			"Make the cgroup v2 pids controller available to systemd."),
		capabilityCheck("storage.filesystem", "Managed hosting filesystem", capabilities.Filesystem.Inspection,
			"Create /srv/hosting as a real directory on its intended dedicated filesystem."),
		capabilityCheck("storage.project-quota", "Project quotas for hosting accounts", capabilities.Filesystem.ProjectQuota,
			"Mount /srv/hosting on ext4 with prjquota or XFS with prjquota/pquota, then rerun preflight."),
		storageMinimumCheck(resources),
		capabilityCheck("security.enforcement", "Mandatory access-control enforcement", capabilities.Security.Enforcement,
			"Enable AppArmor on Debian/Ubuntu or enforcing SELinux on Rocky Linux, reboot if required, and rerun preflight."),
	}
	for _, port := range capabilities.Ports {
		checks = append(checks, capabilityCheck(
			fmt.Sprintf("network.port-%d", port.Port),
			fmt.Sprintf("TCP port %d is available", port.Port), port.Availability,
			fmt.Sprintf("Stop or remove the process listening on TCP port %d, then rerun preflight.", port.Port),
		))
	}
	for _, service := range capabilities.Services {
		checks = append(checks, serviceCheck(service))
	}
	sort.SliceStable(checks, func(left, right int) bool { return checks[left].ID < checks[right].ID })

	ready := true
	for _, check := range checks {
		if check.Status == CheckFail {
			ready = false
			break
		}
	}
	return Result{
		SchemaVersion: SchemaVersion,
		ReadOnly:      true,
		Ready:         ready,
		Capabilities:  capabilities,
		Resources:     resources,
		Checks:        checks,
		Plan:          installationPlan(capabilities.Platform.DistributionID),
	}
}

func nonNegativeUint64(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func capabilityCheck(id, summary string, capability agentprotocol.Capability, remediation string) Check {
	if capability.Status == agentprotocol.CapabilityAvailable {
		return Check{ID: id, Status: CheckPass, Summary: summary}
	}
	return Check{
		ID: id, Status: CheckFail, Summary: summary, Detail: "Detected state: " + string(capability.Status) + ".",
		ReasonCode: capability.ReasonCode, Remediation: remediation,
	}
}

func architectureCheck(architecture string) Check {
	if architecture == "amd64" {
		return Check{ID: "platform.architecture", Status: CheckPass, Summary: "Supported amd64 architecture"}
	}
	return Check{
		ID: "platform.architecture", Status: CheckFail, Summary: "Supported amd64 architecture",
		Detail: "Detected architecture: " + architecture + ".", ReasonCode: "architecture-not-supported",
		Remediation: "Use an amd64 host; arm64 support is scheduled after the initial release gate.",
	}
}

func resourceMinimumCheck(
	id, summary string,
	inspection agentprotocol.Capability,
	value uint64,
	minimum uint64,
	unit, remediation string,
) Check {
	if inspection.Status != agentprotocol.CapabilityAvailable {
		return Check{
			ID: id, Status: CheckFail, Summary: summary, Detail: "The resource could not be measured safely.",
			ReasonCode: inspection.ReasonCode, Remediation: remediation,
		}
	}
	if value < minimum {
		return Check{
			ID: id, Status: CheckFail, Summary: summary,
			Detail:     fmt.Sprintf("Detected %d %s; at least %d are required.", value, unit, minimum),
			ReasonCode: "resource-below-minimum", Remediation: remediation,
		}
	}
	return Check{ID: id, Status: CheckPass, Summary: summary, Detail: fmt.Sprintf("Detected %d %s.", value, unit)}
}

func memoryMinimumCheck(resources ResourceReport) Check {
	const id = "resources.memory"
	if resources.MemoryInspection.Status != agentprotocol.CapabilityAvailable {
		return Check{
			ID: id, Status: CheckFail, Summary: "Physical memory capacity",
			Detail:      "Physical memory could not be measured safely.",
			ReasonCode:  resources.MemoryInspection.ReasonCode,
			Remediation: "Assign at least 4 GiB of physical memory to the host.",
		}
	}
	if resources.MemoryTotalBytes < MinimumMemoryBytes {
		return Check{
			ID: id, Status: CheckFail, Summary: "Physical memory capacity",
			Detail: fmt.Sprintf("Detected %s usable memory; at least %s is required.",
				formatBytes(resources.MemoryTotalBytes), formatBytes(MinimumMemoryBytes)),
			ReasonCode:  "resource-below-minimum",
			Remediation: "Assign at least 4 GiB of physical memory to the host.",
		}
	}
	return Check{
		ID: id, Status: CheckPass, Summary: "Physical memory capacity",
		Detail: fmt.Sprintf("Detected %s usable memory.", formatBytes(resources.MemoryTotalBytes)),
	}
}

func storageMinimumCheck(resources ResourceReport) Check {
	const id = "storage.capacity"
	if resources.StorageInspection.Status != agentprotocol.CapabilityAvailable {
		return Check{
			ID: id, Status: CheckFail, Summary: "Free hosting storage",
			Detail:      "Storage capacity for " + resources.StorageTarget + " could not be measured safely.",
			ReasonCode:  resources.StorageInspection.ReasonCode,
			Remediation: "Make /srv/hosting available on the intended local filesystem and rerun preflight.",
		}
	}
	if resources.StorageFreeBytes < MinimumFreeStorageBytes {
		return Check{
			ID: id, Status: CheckFail, Summary: "Free hosting storage",
			Detail: fmt.Sprintf("Detected %s free on %s; at least %s is required.",
				formatBytes(resources.StorageFreeBytes), resources.StorageTarget, formatBytes(MinimumFreeStorageBytes)),
			ReasonCode:  "storage-below-minimum",
			Remediation: "Free space or expand the filesystem containing /srv/hosting, then rerun preflight.",
		}
	}
	return Check{
		ID: id, Status: CheckPass, Summary: "Free hosting storage",
		Detail: fmt.Sprintf("%s free on %s.", formatBytes(resources.StorageFreeBytes), resources.StorageTarget),
	}
}

func serviceCheck(service agentprotocol.ServiceCapability) Check {
	id := "service." + service.Key
	summary := "No conflicting " + service.Unit
	if service.Key == "firewall" {
		summary = "Firewall service can be inspected"
		if service.Availability.Status == agentprotocol.CapabilityUnknown {
			return Check{
				ID: id, Status: CheckFail, Summary: summary, ReasonCode: service.Availability.ReasonCode,
				Remediation: "Restore systemd service inspection and rerun preflight before firewall rules are planned.",
			}
		}
		return Check{ID: id, Status: CheckPass, Summary: summary, Detail: "Existing firewall state will be preserved and reconciled."}
	}
	if service.Availability.Status == agentprotocol.CapabilityUnavailable && service.Availability.ReasonCode == "service-not-installed" {
		return Check{ID: id, Status: CheckPass, Summary: summary, Detail: "The unit is not installed."}
	}
	if service.Availability.Status != agentprotocol.CapabilityAvailable {
		return Check{
			ID: id, Status: CheckFail, Summary: summary, ReasonCode: service.Availability.ReasonCode,
			Detail:      "The service state could not be established safely.",
			Remediation: "Restore systemd service inspection and rerun preflight.",
		}
	}
	if service.Key == "stackfort-api" || service.Key == "stackfort-agent" {
		return Check{
			ID: id, Status: CheckFail, Summary: summary, ReasonCode: "existing-stackfort-installation",
			Detail:      service.Unit + " is already installed.",
			Remediation: "Use the future upgrade/repair workflow or remove the previous Stackfort installation before a fresh install.",
		}
	}
	if oneOf(service.ActiveState, "active", "activating", "reloading") {
		return Check{
			ID: id, Status: CheckFail, Summary: summary, ReasonCode: "conflicting-service-active",
			Detail:      fmt.Sprintf("%s is %s/%s.", service.Unit, service.ActiveState, service.SubState),
			Remediation: "Migrate or stop the existing service on this disposable host, then rerun preflight.",
		}
	}
	if !oneOf(service.ActiveState, "inactive", "failed", "deactivating") {
		return Check{
			ID: id, Status: CheckFail, Summary: summary, ReasonCode: "service-state-unknown",
			Detail:      "Detected active state: " + service.ActiveState + ".",
			Remediation: "Resolve the ambiguous systemd state and rerun preflight.",
		}
	}
	return Check{ID: id, Status: CheckPass, Summary: summary, Detail: service.Unit + " is not active."}
}

func installationPlan(distribution string) Plan {
	packages := distroPackages(distribution)
	plan := Plan{
		Packages: make([]PackagePlan, 0, len(packages)),
		Files: []FilePlan{
			{"/usr/local/bin/stackfort-api", "install verified release binary", "root:root", "0755"},
			{"/usr/local/sbin/stackfort-agent", "install verified release binary", "root:root", "0755"},
			{"/usr/share/stackfort/web", "install immutable web assets", "root:root", "0755"},
			{"/etc/stackfort", "create configuration directory", "root:stackfort", "0750"},
			{"/etc/stackfort/php", "store generated per-account PHP-FPM configurations", "root:root", "0750"},
			{"/etc/stackfort/panel-tls", "store the local bootstrap panel certificate", "root:root", "0700"},
			{"/etc/stackfort/stackfort.env", "write generated service environment", "root:stackfort", "0640"},
			{"/var/lib/stackfort", "create control-plane state directory", "stackfort:stackfort", "0750"},
			{"/var/lib/stackfort/stackfort.db", "initialize the SQLite control-plane state", "stackfort:stackfort", "0600"},
			{"/var/lib/stackfort-agent/acme-http01", "create ACME challenge directory", "root:root", "0755"},
			{"/run/stackfort", "create systemd-managed runtime directory", "root:stackfort", "0750"},
			{"/run/stackfort-php", "store per-account PHP-FPM sockets and PID files", "root:root", "0755"},
			{"/srv/hosting/accounts", "create hosting account root", "root:root", "0711"},
			{"/srv/hosting/backups", "create local backup root", "root:root", "0710"},
			{"/etc/systemd/system/stackfort-api.service", "install hardened systemd unit", "root:root", "0644"},
			{"/etc/systemd/system/stackfort-agent.service", "install hardened systemd unit", "root:root", "0644"},
			{"/etc/systemd/system/stackfort.slice", "install platform resource slice", "root:root", "0644"},
			{"/etc/systemd/system/stackfort-core.slice", "install control-plane resource slice", "root:root", "0644"},
			{"/etc/systemd/system/stackfort-accounts.slice", "install hosting resource slice", "root:root", "0644"},
			{"/etc/nginx/stackfort/nginx.conf", "install Stackfort-owned NGINX baseline", "root:root", "0640"},
			{"/etc/nginx/stackfort/coraza", "create fixed worker-readable WAF configuration root", "root:root", "0755"},
			{"/etc/nginx/stackfort/coraza/engine.conf", "install privacy-conservative Coraza engine policy", "root:root", "0644"},
			{"/etc/nginx/stackfort/coraza/profiles/detection-pl1.conf", "install fixed OWASP CRS PL1 detection profile", "root:root", "0644"},
			{"/etc/nginx/stackfort/coraza/profiles/blocking-pl1.conf", "install fixed OWASP CRS PL1 blocking profile", "root:root", "0644"},
			{"/etc/nginx/stackfort/panel-enabled/00-panel.conf", "publish the HTTPS management endpoint", "root:root", "0640"},
			{"/etc/systemd/system/nginx.service.d/20-stackfort.conf", "select the managed NGINX baseline", "root:root", "0644"},
		},
		Users: []UserPlan{
			{"stackfort", "create locked system service account", "/var/lib/stackfort", "/usr/sbin/nologin"},
		},
		Services: []ServicePlan{
			{"stackfort-agent.service", "enable and start as root with systemd hardening"},
			{"stackfort-api.service", "enable and start as the stackfort account"},
			{"nginx.service", "enable and start with the Stackfort-owned configuration"},
			{phpVendorUnit(distribution), "disable the distribution-wide pool; use isolated per-account PHP-FPM units"},
			{"podman.socket", "mask now as a system unit and mask in the global user configuration"},
			{"podman.service", "mask now as a system unit and mask in the global user configuration"},
		},
		Ports: []PortPlan{
			{"tcp/80", "public inbound", "HTTP, redirects, and ACME HTTP-01"},
			{"tcp/443", "public inbound", "HTTPS ingress"},
			{"tcp/8443", "public inbound", "initial HTTPS management endpoint"},
			{"127.0.0.1:8080", "loopback only", "control API behind the panel virtual host"},
			{"/run/stackfort/agent.sock", "local Unix socket", "authenticated API-to-agent RPC"},
		},
		Security: securityPlan(distribution),
	}
	switch distribution {
	case "debian", "ubuntu":
		plan.Files = append(plan.Files,
			FilePlan{"/etc/stackfort/firewall.nft", "install the dedicated Stackfort nftables table", "root:stackfort", "0640"},
			FilePlan{"/etc/apparmor.d/stackfort-api", "install the control API AppArmor profile", "root:root", "0644"},
		)
		plan.Services = append(plan.Services, ServicePlan{"stackfort-firewall.service", "enable Stackfort's dedicated nftables table"})
	case "rocky":
		plan.Files = append(plan.Files, FilePlan{
			"/etc/systemd/system/nginx.service.d/php-fpm.conf",
			"remove the exact RPM-owned coupling to the disabled distribution-wide PHP-FPM pool",
			"root:root", "absent",
		})
		plan.Services = append(plan.Services, ServicePlan{"firewalld.service", "enable and reconcile only Stackfort's firewall rules"})
	}
	for _, name := range packages {
		plan.Packages = append(plan.Packages, PackagePlan{Name: name, Action: "install or retain distribution package"})
	}
	return plan
}

func distroPackages(distribution string) []string {
	switch distribution {
	case "debian":
		return []string{"acl", "apparmor", "apparmor-utils", "ca-certificates", "curl", "nginx", "nftables", "php8.4-fpm", "quota",
			"aardvark-dns", "fuse-overlayfs", "netavark", "passt", "podman", "slirp4netns", "uidmap"}
	case "ubuntu":
		return []string{"acl", "apparmor", "apparmor-utils", "ca-certificates", "curl", "nginx", "nftables", "php8.5-fpm", "quota",
			"aardvark-dns", "fuse-overlayfs", "netavark", "passt", "podman", "slirp4netns", "uidmap"}
	case "rocky":
		return []string{"acl", "ca-certificates", "curl", "firewalld", "nginx", "php-fpm", "policycoreutils-python-utils", "quota",
			"aardvark-dns", "fuse-overlayfs", "netavark", "passt", "podman", "shadow-utils-subid", "slirp4netns"}
	default:
		return []string{}
	}
}

func phpVendorUnit(distribution string) string {
	switch distribution {
	case "debian":
		return "php8.4-fpm.service"
	case "ubuntu":
		return "php8.5-fpm.service"
	case "rocky":
		return "php-fpm.service"
	default:
		return "php-fpm.service"
	}
}

func securityPlan(distribution string) []SecurityPlan {
	switch distribution {
	case "debian", "ubuntu":
		return []SecurityPlan{
			{"AppArmor", "install and load Stackfort service profiles; never disable enforcement"},
			{"nftables", "allow inbound TCP 80/443 while preserving unrelated rules"},
		}
	case "rocky":
		return []SecurityPlan{
			{"SELinux", "install Stackfort policy and persistent narrow file contexts; keep enforcing mode"},
			{"firewalld", "allow inbound TCP 80/443 while preserving unrelated zones and rules"},
		}
	default:
		return []SecurityPlan{{"unresolved", "no security-module mutation can be planned for an unsupported distribution"}}
	}
}

func formatBytes(value uint64) string {
	const gibibyte = uint64(1 << 30)
	return fmt.Sprintf("%.1f GiB", float64(value)/float64(gibibyte))
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}
