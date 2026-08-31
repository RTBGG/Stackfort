// SPDX-License-Identifier: AGPL-3.0-or-later

package hostcapabilities

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

func TestSupportedDistributionFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		fixture          string
		distribution     string
		version          string
		securityProvider string
		packageTool      string
		quotaStatus      agentprotocol.CapabilityStatus
		occupiedPort     int
	}{
		{"debian-13", "debian", "13", "apparmor", "/usr/bin/dpkg-query", agentprotocol.CapabilityAvailable, 80},
		{"ubuntu-26.04", "ubuntu", "26.04", "apparmor", "/usr/bin/dpkg-query", agentprotocol.CapabilityUnavailable, 0},
		{"rocky-10", "rocky", "10.0", "selinux", "/usr/bin/rpm", agentprotocol.CapabilityAvailable, 443},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			runner := &fixtureRunner{}
			inspector := fixtureInspector(filepath.Join("testdata", test.fixture), runner)
			report, err := inspector.Inspect(t.Context())
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if report.Platform.DistributionID != test.distribution || report.Platform.VersionID != test.version {
				t.Fatalf("platform = %#v", report.Platform)
			}
			if report.Platform.Support.Status != agentprotocol.CapabilityAvailable ||
				report.Systemd.Status != agentprotocol.CapabilityAvailable {
				t.Fatalf("platform support = %#v, systemd = %#v", report.Platform.Support, report.Systemd)
			}
			if report.Cgroup.Version != 2 || report.Cgroup.CPU.Status != agentprotocol.CapabilityAvailable ||
				report.Cgroup.Memory.Status != agentprotocol.CapabilityAvailable ||
				report.Cgroup.IO.Status != agentprotocol.CapabilityAvailable ||
				report.Cgroup.PIDs.Status != agentprotocol.CapabilityAvailable {
				t.Fatalf("cgroup = %#v", report.Cgroup)
			}
			if report.Filesystem.ProjectQuota.Status != test.quotaStatus {
				t.Fatalf("project quota = %#v", report.Filesystem.ProjectQuota)
			}
			if report.Security.Provider != test.securityProvider ||
				report.Security.Enforcement.Status != agentprotocol.CapabilityAvailable {
				t.Fatalf("security = %#v", report.Security)
			}
			assertPortFixture(t, report.Ports, test.occupiedPort)
			if len(report.Packages) != 6 || len(report.Services) != 8 {
				t.Fatalf("packages/services = %d/%d", len(report.Packages), len(report.Services))
			}
			if findPackage(t, report.Packages, "vinyl").Availability.Status != agentprotocol.CapabilityUnavailable {
				t.Fatal("missing Vinyl package was not represented as unavailable")
			}
			if findService(t, report.Services, "vinyl").Availability.Status != agentprotocol.CapabilityUnavailable {
				t.Fatal("missing Vinyl service was not represented as unavailable")
			}
			if !runner.calledExecutable(test.packageTool) || !runner.calledExecutable("/usr/bin/systemctl") {
				t.Fatalf("probe calls = %#v", runner.calls)
			}
		})
	}
}

func TestUnsupportedAndMalformedCapabilitiesRemainTyped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFixtureFile(t, root, "/etc/os-release", "ID=gentoo\nVERSION_ID=rolling\n")
	writeFixtureFile(t, root, "/proc/sys/kernel/osrelease", "6.99-test\n")
	writeFixtureFile(t, root, "/proc/1/comm", "openrc\n")
	writeFixtureFile(t, root, "/proc/self/mountinfo", "malformed\n")
	writeFixtureFile(t, root, "/proc/net/tcp", "malformed\n")
	writeFixtureFile(t, root, "/proc/net/tcp6", "malformed\n")
	if err := os.MkdirAll(filepath.Join(root, "srv", "hosting"), 0o750); err != nil {
		t.Fatalf("create hosting root: %v", err)
	}
	inspector := fixtureInspector(root, &fixtureRunner{})
	report, err := inspector.Inspect(t.Context())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if report.Platform.Support.Status != agentprotocol.CapabilityUnsupported ||
		report.Platform.Support.ReasonCode != "distribution-not-supported" {
		t.Fatalf("platform support = %#v", report.Platform.Support)
	}
	if report.Systemd.Status != agentprotocol.CapabilityUnavailable ||
		report.Filesystem.Inspection.Status != agentprotocol.CapabilityUnknown ||
		report.Security.Enforcement.Status != agentprotocol.CapabilityUnsupported {
		t.Fatalf("typed states = systemd %#v, filesystem %#v, security %#v",
			report.Systemd, report.Filesystem.Inspection, report.Security.Enforcement)
	}
	for _, item := range report.Packages {
		if item.Availability.Status != agentprotocol.CapabilityUnsupported {
			t.Fatalf("package %s = %#v", item.Key, item.Availability)
		}
	}
}

func TestParsersRejectAmbiguousInput(t *testing.T) {
	t.Parallel()

	if _, err := parseOSRelease("ID=debian\nBROKEN\n"); err == nil {
		t.Fatal("malformed os-release was accepted")
	}
	if _, err := findMount("1 2 3\n", agentprotocol.ManagedHostingRoot); err == nil {
		t.Fatal("malformed mountinfo was accepted")
	}
	if _, err := parseListeningTCPPorts("header\n1 short\n"); err == nil {
		t.Fatal("malformed TCP table was accepted")
	}
	if _, err := parseProperties("LoadState=loaded\nActiveState=active\n"); err == nil {
		t.Fatal("incomplete systemd properties were accepted")
	}
}

func TestDpkgResidualConfigurationIsNotInstalled(t *testing.T) {
	t.Parallel()

	inspector := fixtureInspector("testdata/debian-13", staticRunner{
		result: commandResult{Output: "rc \t1.2.3-1\n", ExitCode: 0},
	})
	version, installed, err := inspector.queryPackage(t.Context(), "debian", "nginx")
	if err != nil || installed || version != "" {
		t.Fatalf("version=%q installed=%t error=%v", version, installed, err)
	}
}

func TestOperatingSystemRunnerRejectsUnsafeAndPropagatesCancellation(t *testing.T) {
	if _, err := (operatingSystemRunner{}).Run(
		t.Context(), "/bin/sh", "-c", "id",
	); err == nil || !strings.Contains(err.Error(), "allowlisted") {
		t.Fatalf("unsafe invocation error = %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (operatingSystemRunner{}).Run(
		cancelled, "/usr/bin/rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}\\n", "nginx",
	); err == nil {
		t.Fatal("cancelled probe did not fail")
	}
}

func fixtureInspector(root string, runner commandRunner) *Inspector {
	return &Inspector{
		root: root, managedRoot: agentprotocol.ManagedHostingRoot, architecture: "amd64",
		now: func() time.Time { return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC) }, runner: runner,
	}
}

type fixtureCall struct {
	executable string
	arguments  []string
}

type fixtureRunner struct{ calls []fixtureCall }

type staticRunner struct {
	result commandResult
	err    error
}

func (runner staticRunner) Run(context.Context, string, ...string) (commandResult, error) {
	return runner.result, runner.err
}

func (runner *fixtureRunner) Run(_ context.Context, executable string, arguments ...string) (commandResult, error) {
	runner.calls = append(runner.calls, fixtureCall{executable: executable, arguments: append([]string(nil), arguments...)})
	if len(arguments) == 0 {
		return commandResult{}, errors.New("missing fixture arguments")
	}
	name := arguments[len(arguments)-1]
	switch executable {
	case "/usr/bin/dpkg-query":
		if name == "vinyl-cache" {
			return commandResult{ExitCode: 1}, nil
		}
		return commandResult{Output: "ii \t1.2.3-1\n", ExitCode: 0}, nil
	case "/usr/bin/rpm":
		if name == "vinyl-cache" {
			return commandResult{ExitCode: 1}, nil
		}
		return commandResult{Output: "1.2.3-1.el10\n", ExitCode: 0}, nil
	case "/usr/bin/systemctl":
		if name == "vinyl.service" {
			return commandResult{Output: "LoadState=not-found\nActiveState=inactive\nSubState=dead\nUnitFileState=\n"}, nil
		}
		return commandResult{Output: "LoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\n"}, nil
	default:
		return commandResult{}, errors.New("unexpected fixture executable")
	}
}

func (runner *fixtureRunner) calledExecutable(executable string) bool {
	for _, call := range runner.calls {
		if call.executable == executable {
			return true
		}
	}
	return false
}

func assertPortFixture(t *testing.T, ports []agentprotocol.PortCapability, occupied int) {
	t.Helper()
	for _, item := range ports {
		want := agentprotocol.CapabilityAvailable
		if item.Port == occupied {
			want = agentprotocol.CapabilityUnavailable
		}
		if item.Availability.Status != want {
			t.Fatalf("port %d = %#v, want %s", item.Port, item.Availability, want)
		}
	}
}

func findPackage(t *testing.T, packages []agentprotocol.PackageCapability, key string) agentprotocol.PackageCapability {
	t.Helper()
	for _, item := range packages {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("package %s not found", key)
	return agentprotocol.PackageCapability{}
}

func findService(t *testing.T, services []agentprotocol.ServiceCapability, key string) agentprotocol.ServiceCapability {
	t.Helper()
	for _, item := range services {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("service %s not found", key)
	return agentprotocol.ServiceCapability{}
}

func writeFixtureFile(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
