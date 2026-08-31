// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"strings"
	"testing"
	"time"
)

func TestCapabilityRequestTaggedUnionIsStrict(t *testing.T) {
	t.Parallel()

	request := Request{
		ProtocolVersion: WireVersion, RequestID: "cap-request-1", IdempotencyKey: "cap-key-1",
		Operation: OperationInspectCapabilities, InspectCapabilities: &InspectCapabilitiesRequest{},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid capability request: %v", err)
	}
	request.Handshake = validHandshakeRequest().Handshake
	if err := ValidateRequest(request); err == nil {
		t.Fatal("request with two operation payloads was accepted")
	}
	unknown := `{"protocolVersion":1,"requestId":"cap-request-2","idempotencyKey":"cap-key-2","operation":"host.capabilities.inspect","inspectCapabilities":{"path":"/etc/shadow"}}`
	if _, err := DecodeRequest(strings.NewReader(unknown)); err == nil {
		t.Fatal("unknown capability input was accepted")
	}
}

func TestCapabilityResponseValidationIsBoundedAndCorrelated(t *testing.T) {
	t.Parallel()

	valid := Response{
		ProtocolVersion: WireVersion, RequestID: "cap-request-1", Capabilities: validCapabilityReport(),
	}
	if err := ValidateResponse(valid, valid.RequestID, OperationInspectCapabilities); err != nil {
		t.Fatalf("valid capability response: %v", err)
	}
	if err := ValidateResponse(valid, valid.RequestID, OperationHandshake); err == nil {
		t.Fatal("capability response was accepted for handshake operation")
	}

	tests := []struct {
		name   string
		mutate func(*CapabilityReport)
	}{
		{"bad status", func(report *CapabilityReport) { report.Systemd.Status = "maybe" }},
		{"available with reason", func(report *CapabilityReport) { report.Systemd.ReasonCode = "unexpected" }},
		{"unknown OCI provider", func(report *CapabilityReport) { report.OCI.Provider = "docker" }},
		{"rootless OCI without version", func(report *CapabilityReport) { report.OCI.Version = "" }},
		{"missing port", func(report *CapabilityReport) { report.Ports = report.Ports[:1] }},
		{"duplicate package", func(report *CapabilityReport) { report.Packages[1].Key = report.Packages[0].Key }},
		{"unbounded version", func(report *CapabilityReport) { report.Packages[0].Version = strings.Repeat("x", 129) }},
		{"arbitrary unit", func(report *CapabilityReport) { report.Services[0].Unit = "../../etc/shadow" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := cloneCapabilityReport(*valid.Capabilities)
			test.mutate(&report)
			response := valid
			response.Capabilities = &report
			if err := ValidateResponse(response, response.RequestID, OperationInspectCapabilities); err == nil {
				t.Fatal("invalid capability response was accepted")
			}
		})
	}
}

func validCapabilityReport() *CapabilityReport {
	return &CapabilityReport{
		InspectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Platform: PlatformCapabilities{
			DistributionID: "debian", VersionID: "13", Architecture: "amd64",
			KernelRelease: "6.12.0", Support: Capability{Status: CapabilityAvailable},
		},
		Systemd: Capability{Status: CapabilityAvailable},
		Cgroup: CgroupCapabilities{
			Version: 2,
			Unified: Capability{Status: CapabilityAvailable}, CPU: Capability{Status: CapabilityAvailable},
			Memory: Capability{Status: CapabilityAvailable}, IO: Capability{Status: CapabilityAvailable},
			PIDs: Capability{Status: CapabilityAvailable},
		},
		Filesystem: FilesystemCapabilities{
			Target: ManagedHostingRoot, MountPoint: "/srv/hosting", Type: "ext4",
			Inspection:   Capability{Status: CapabilityAvailable},
			ProjectQuota: Capability{Status: CapabilityAvailable},
		},
		Security: SecurityCapabilities{
			Provider: "apparmor", Mode: "enabled", Enforcement: Capability{Status: CapabilityAvailable},
		},
		OCI: OCIRuntimeCapabilities{
			Provider: "podman", Version: "5.5.2", ScannerProvider: "trivy", ScannerVersion: "0.74.0",
			Rootless: Capability{Status: CapabilityAvailable},
			Quadlet:  Capability{Status: CapabilityAvailable}, Network: Capability{Status: CapabilityAvailable},
			Storage: Capability{Status: CapabilityAvailable}, RootfulSocketIsolation: Capability{Status: CapabilityAvailable},
			ImagePreparation: Capability{Status: CapabilityAvailable}, ImageScanning: Capability{Status: CapabilityAvailable},
		},
		Ports: []PortCapability{
			{Port: 80, Network: "tcp", Availability: Capability{Status: CapabilityAvailable}},
			{Port: 443, Network: "tcp", Availability: Capability{Status: CapabilityAvailable}},
			{Port: 8443, Network: "tcp", Availability: Capability{Status: CapabilityAvailable}},
		},
		Packages: []PackageCapability{
			{Key: "nginx", PackageName: "nginx", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "php-fpm", PackageName: "php-fpm", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "mariadb", PackageName: "mariadb-server", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "vinyl", PackageName: "vinyl-cache", Availability: Capability{Status: CapabilityUnavailable, ReasonCode: "package-not-installed"}},
			{Key: "podman", PackageName: "podman", Version: "5.5.2", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "netavark", PackageName: "netavark", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "aardvark-dns", PackageName: "aardvark-dns", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "passt", PackageName: "passt", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "slirp4netns", PackageName: "slirp4netns", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "fuse-overlayfs", PackageName: "fuse-overlayfs", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "uidmap", PackageName: "uidmap", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
			{Key: "coraza", PackageName: "stackfort-waf", Version: "1", Availability: Capability{Status: CapabilityAvailable}},
		},
		Services: []ServiceCapability{
			serviceCapability("nginx", "nginx.service"),
			serviceCapability("php-fpm", "php8.4-fpm.service"),
			serviceCapability("mariadb", "mariadb.service"),
			serviceCapability("vinyl", "vinyl.service"),
			serviceCapability("podman", "podman.socket"),
			serviceCapability("firewall", "nftables.service"),
			serviceCapability("stackfort-api", "stackfort-api.service"),
			serviceCapability("stackfort-agent", "stackfort-agent.service"),
		},
	}
}

func serviceCapability(key, unit string) ServiceCapability {
	return ServiceCapability{
		Key: key, Unit: unit, LoadState: "loaded", ActiveState: "active",
		SubState: "running", UnitFileState: "enabled", Availability: Capability{Status: CapabilityAvailable},
	}
}

func cloneCapabilityReport(report CapabilityReport) CapabilityReport {
	report.Ports = append([]PortCapability(nil), report.Ports...)
	report.Packages = append([]PackageCapability(nil), report.Packages...)
	report.Services = append([]ServiceCapability(nil), report.Services...)
	return report
}
