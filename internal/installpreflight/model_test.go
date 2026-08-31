// SPDX-License-Identifier: AGPL-3.0-or-later

package installpreflight

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func TestEvaluateReadyFreshHost(t *testing.T) {
	t.Parallel()

	result := Evaluate(readyCapabilities(), readyResources())
	if !result.Ready || !result.ReadOnly || result.SchemaVersion != SchemaVersion {
		t.Fatalf("result = %#v", result)
	}
	for _, check := range result.Checks {
		if check.Status == CheckFail {
			t.Fatalf("unexpected failed check: %#v", check)
		}
	}
	if len(result.Plan.Packages) == 0 || len(result.Plan.Files) == 0 || len(result.Plan.Users) == 0 ||
		len(result.Plan.Services) == 0 || len(result.Plan.Ports) == 0 || len(result.Plan.Security) == 0 {
		t.Fatalf("incomplete plan = %#v", result.Plan)
	}
	if result.Plan.Packages[0].Name != "acl" || result.Plan.Security[0].Provider != "AppArmor" {
		t.Fatalf("unexpected Debian plan = %#v", result.Plan)
	}
	if !hasService(result.Plan.Services, "podman.socket") || !hasService(result.Plan.Services, "podman.service") {
		t.Fatalf("Podman API units are absent from the installation plan: %#v", result.Plan.Services)
	}
}

func TestEvaluateActionableBlockers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		check  string
		mutate func(*agentprotocol.CapabilityReport, *ResourceReport)
	}{
		{"unsupported OS", "platform.support", func(report *agentprotocol.CapabilityReport, _ *ResourceReport) {
			report.Platform.Support = unavailable("distribution-not-supported")
		}},
		{"unsupported architecture", "platform.architecture", func(report *agentprotocol.CapabilityReport, _ *ResourceReport) {
			report.Platform.Architecture = "arm64"
		}},
		{"occupied port", "network.port-80", func(report *agentprotocol.CapabilityReport, _ *ResourceReport) {
			report.Ports[0].Availability = unavailable("port-in-use")
		}},
		{"insufficient storage", "storage.capacity", func(_ *agentprotocol.CapabilityReport, resources *ResourceReport) {
			resources.StorageFreeBytes = MinimumFreeStorageBytes - 1
		}},
		{"missing cgroup", "cgroup.io", func(report *agentprotocol.CapabilityReport, _ *ResourceReport) {
			report.Cgroup.IO = unavailable("cgroup-controller-unavailable")
		}},
		{"missing quota", "storage.project-quota", func(report *agentprotocol.CapabilityReport, _ *ResourceReport) {
			report.Filesystem.ProjectQuota = unavailable("project-quota-not-mounted")
		}},
		{"active conflicting service", "service.nginx", func(report *agentprotocol.CapabilityReport, _ *ResourceReport) {
			report.Services[0].Availability = available()
			report.Services[0].LoadState = "loaded"
			report.Services[0].ActiveState = "active"
			report.Services[0].SubState = "running"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capabilities := readyCapabilities()
			resources := readyResources()
			test.mutate(&capabilities, &resources)
			result := Evaluate(capabilities, resources)
			if result.Ready {
				t.Fatal("blocked host was reported ready")
			}
			check := findCheck(t, result.Checks, test.check)
			if check.Status != CheckFail || check.ReasonCode == "" || check.Remediation == "" {
				t.Fatalf("check is not actionable: %#v", check)
			}
		})
	}
}

func TestPlansAreDistributionSpecific(t *testing.T) {
	t.Parallel()

	rocky := installationPlan("rocky")
	if !hasPackage(rocky.Packages, "firewalld") || !hasPackage(rocky.Packages, "php-fpm") ||
		hasPackage(rocky.Packages, "nftables") ||
		rocky.Security[0].Provider != "SELinux" {
		t.Fatalf("Rocky plan = %#v", rocky)
	}
	debian := installationPlan("debian")
	ubuntu := installationPlan("ubuntu")
	if !hasPackage(debian.Packages, "php8.4-fpm") || !hasPackage(ubuntu.Packages, "php8.5-fpm") {
		t.Fatalf("native PHP plans = Debian %#v / Ubuntu %#v", debian.Packages, ubuntu.Packages)
	}
	unsupported := installationPlan("gentoo")
	if len(unsupported.Packages) != 0 || unsupported.Security[0].Provider != "unresolved" {
		t.Fatalf("unsupported plan = %#v", unsupported)
	}
}

func TestWriteTextStatesReadOnlyVerdictAndCompletePlan(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := WriteText(&output, Evaluate(readyCapabilities(), readyResources())); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	text := output.String()
	for _, expected := range []string{
		"preflight (read-only)", "Result: READY", "Installation plan (no changes have been applied)",
		"Packages:", "Files and directories:", "Users:", "Services:",
		"Ports and local endpoints:", "Security-module changes:",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("text report lacks %q:\n%s", expected, text)
		}
	}
}

func TestMemoryThresholdAllowsFourGiBGuestReservations(t *testing.T) {
	t.Parallel()

	capabilities := readyCapabilities()
	resources := readyResources()
	resources.MemoryTotalBytes = MinimumMemoryBytes
	if result := Evaluate(capabilities, resources); !result.Ready {
		t.Fatalf("minimum usable memory was blocked: %#v", findCheck(t, result.Checks, "resources.memory"))
	}
	resources.MemoryTotalBytes--
	result := Evaluate(capabilities, resources)
	if result.Ready || findCheck(t, result.Checks, "resources.memory").Status != CheckFail {
		t.Fatal("below-minimum usable memory was accepted")
	}
}

func TestParseMemoryTotal(t *testing.T) {
	t.Parallel()

	value, err := parseMemoryTotal("MemFree: 12 kB\nMemTotal:       4194304 kB\n")
	if err != nil || value != 4<<30 {
		t.Fatalf("value=%d error=%v", value, err)
	}
	if _, err := parseMemoryTotal("MemFree: 12 kB\n"); err == nil {
		t.Fatal("missing MemTotal was accepted")
	}
}

func readyCapabilities() agentprotocol.CapabilityReport {
	missing := unavailable("service-not-installed")
	services := []agentprotocol.ServiceCapability{
		{Key: "nginx", Unit: "nginx.service"},
		{Key: "php-fpm", Unit: "php8.4-fpm.service"},
		{Key: "mariadb", Unit: "mariadb.service"},
		{Key: "vinyl", Unit: "vinyl.service"},
		{Key: "podman", Unit: "podman.socket"},
		{Key: "firewall", Unit: "nftables.service"},
		{Key: "stackfort-api", Unit: "stackfort-api.service"},
		{Key: "stackfort-agent", Unit: "stackfort-agent.service"},
	}
	for index := range services {
		services[index].LoadState = "not-found"
		services[index].ActiveState = "inactive"
		services[index].SubState = "dead"
		services[index].UnitFileState = "unknown"
		services[index].Availability = missing
	}
	packages := []agentprotocol.PackageCapability{
		{Key: "nginx", PackageName: "nginx"},
		{Key: "php-fpm", PackageName: "php-fpm"},
		{Key: "mariadb", PackageName: "mariadb-server"},
		{Key: "vinyl", PackageName: "vinyl-cache"},
		{Key: "podman", PackageName: "podman"},
		{Key: "coraza", PackageName: "stackfort-waf"},
	}
	for index := range packages {
		packages[index].Availability = unavailable("package-not-installed")
	}
	return agentprotocol.CapabilityReport{
		InspectedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Platform: agentprotocol.PlatformCapabilities{
			DistributionID: "debian", VersionID: "13", Architecture: "amd64", KernelRelease: "6.12.0",
			Support: available(),
		},
		Systemd: available(),
		Cgroup: agentprotocol.CgroupCapabilities{
			Version: 2, Unified: available(), CPU: available(), Memory: available(), IO: available(), PIDs: available(),
		},
		Filesystem: agentprotocol.FilesystemCapabilities{
			Target: "/srv/hosting", MountPoint: "/srv/hosting", Type: "ext4",
			Inspection: available(), ProjectQuota: available(),
		},
		Security: agentprotocol.SecurityCapabilities{
			Provider: "apparmor", Mode: "enabled", Enforcement: available(),
		},
		Ports: []agentprotocol.PortCapability{
			{Port: 80, Network: "tcp", Availability: available()},
			{Port: 443, Network: "tcp", Availability: available()},
			{Port: 8443, Network: "tcp", Availability: available()},
		},
		Packages: packages,
		Services: services,
	}
}

func readyResources() ResourceReport {
	return ResourceReport{
		LogicalCPUs: 2, CPUInspection: available(),
		MemoryTotalBytes: 4 << 30, MemoryInspection: available(),
		StorageTarget: "/srv/hosting", StorageTotalBytes: 8 << 30,
		StorageFreeBytes: 7 << 30, StorageInspection: available(),
	}
}

func unavailable(reason string) agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityUnavailable, ReasonCode: reason}
}

func findCheck(t *testing.T, checks []Check, id string) Check {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %s not found", id)
	return Check{}
}

func hasPackage(packages []PackagePlan, name string) bool {
	for _, item := range packages {
		if item.Name == name {
			return true
		}
	}
	return false
}

func hasService(services []ServicePlan, name string) bool {
	for _, item := range services {
		if item.Name == name {
			return true
		}
	}
	return false
}
