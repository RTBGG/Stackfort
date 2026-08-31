// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostcapabilities performs bounded, read-only inspection of the Linux
// host. Individual probe failures become typed capability states.
package hostcapabilities

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/ociimage"
)

const (
	maximumProbeFileBytes   = 1 << 20
	maximumInspectionPeriod = 8 * time.Second
)

type commandResult struct {
	Output   string
	ExitCode int
}

type commandRunner interface {
	Run(context.Context, string, ...string) (commandResult, error)
}

type Inspector struct {
	root         string
	managedRoot  string
	architecture string
	now          func() time.Time
	runner       commandRunner
}

func NewInspector() *Inspector {
	return &Inspector{
		root: "/", managedRoot: agentprotocol.ManagedHostingRoot,
		architecture: runtime.GOARCH, now: time.Now, runner: newOperatingSystemRunner(),
	}
}

func (inspector *Inspector) Inspect(ctx context.Context) (agentprotocol.CapabilityReport, error) {
	probeContext, cancel := context.WithTimeout(ctx, maximumInspectionPeriod)
	defer cancel()
	report := agentprotocol.CapabilityReport{
		InspectedAt: inspector.now().UTC().Format(time.RFC3339Nano),
		Platform: agentprotocol.PlatformCapabilities{
			DistributionID: "unknown", VersionID: "unknown", Architecture: inspector.architecture,
			KernelRelease: "unknown", Support: unknown("os-release-unavailable"),
		},
		Systemd: unknown("init-inspection-failed"),
		Filesystem: agentprotocol.FilesystemCapabilities{
			Target: inspector.managedRoot, MountPoint: "unknown", Type: "unknown",
			Inspection: unknown("mount-inspection-failed"), ProjectQuota: unknown("mount-inspection-failed"),
		},
		Security: agentprotocol.SecurityCapabilities{
			Provider: "unknown", Mode: "unknown", Enforcement: unknown("security-inspection-failed"),
		},
		OCI: unavailableOCIRuntime("runtime-inspection-failed"),
	}

	distribution := inspector.inspectPlatform(&report)
	inspector.inspectSystemd(&report)
	inspector.inspectCgroup(&report)
	inspector.inspectFilesystem(&report)
	inspector.inspectSecurity(&report, distribution)
	inspector.inspectPorts(&report)
	inspector.inspectPackages(probeContext, &report, distribution)
	inspector.inspectServices(probeContext, &report, distribution)
	inspector.inspectOCIRuntime(&report)

	if err := agentprotocol.ValidateCapabilityReport(report); err != nil {
		return agentprotocol.CapabilityReport{}, fmt.Errorf("validate detected capabilities: %w", err)
	}
	return report, nil
}

// InspectOCIRuntime performs the bounded global readiness checks used before
// provisioning a rootless account runtime. It never opens an engine socket or
// starts a Podman service.
func (inspector *Inspector) InspectOCIRuntime(ctx context.Context) (agentprotocol.OCIRuntimeCapabilities, error) {
	report, err := inspector.Inspect(ctx)
	if err != nil {
		return agentprotocol.OCIRuntimeCapabilities{}, err
	}
	return report.OCI, nil
}

// InspectManagedFilesystem performs only the bounded mount inspection needed
// immediately before a quota mutation. It avoids unrelated package and service
// probes while preserving the same typed capability semantics as Inspect.
func (inspector *Inspector) InspectManagedFilesystem() agentprotocol.FilesystemCapabilities {
	report := agentprotocol.CapabilityReport{Filesystem: agentprotocol.FilesystemCapabilities{
		Target: inspector.managedRoot, MountPoint: "unknown", Type: "unknown",
		Inspection: unknown("mount-inspection-failed"), ProjectQuota: unknown("mount-inspection-failed"),
	}}
	inspector.inspectFilesystem(&report)
	return report.Filesystem
}

// InspectResourceControl performs only the systemd and cgroup-v2 probes needed
// immediately before a resource-control mutation.
func (inspector *Inspector) InspectResourceControl() (agentprotocol.Capability, agentprotocol.CgroupCapabilities) {
	report := agentprotocol.CapabilityReport{Systemd: unknown("init-inspection-failed")}
	inspector.inspectSystemd(&report)
	inspector.inspectCgroup(&report)
	return report.Systemd, report.Cgroup
}

// InspectPlatform performs only the bounded platform probes needed immediately
// before a distribution-specific host mutation.
func (inspector *Inspector) InspectPlatform() agentprotocol.PlatformCapabilities {
	platform := agentprotocol.PlatformCapabilities{
		DistributionID: "unknown", VersionID: "unknown", Architecture: inspector.architecture,
		KernelRelease: "unknown", Support: unknown("os-release-unavailable"),
	}
	report := agentprotocol.CapabilityReport{Platform: platform}
	inspector.inspectPlatform(&report)
	return report.Platform
}

func (inspector *Inspector) inspectPlatform(report *agentprotocol.CapabilityReport) string {
	content, err := inspector.readFile("/etc/os-release")
	if err == nil {
		values, parseErr := parseOSRelease(string(content))
		if parseErr == nil {
			distribution := strings.ToLower(values["ID"])
			version := values["VERSION_ID"]
			if distribution != "" {
				report.Platform.DistributionID = distribution
			}
			if version != "" {
				report.Platform.VersionID = version
			}
			report.Platform.Support = distributionSupport(distribution, version)
		} else {
			report.Platform.Support = unknown("os-release-malformed")
		}
	}
	if content, err := inspector.readFile("/proc/sys/kernel/osrelease"); err == nil {
		if value := boundedLine(string(content), 128); value != "" {
			report.Platform.KernelRelease = value
		}
	}
	return report.Platform.DistributionID
}

func (inspector *Inspector) inspectSystemd(report *agentprotocol.CapabilityReport) {
	content, err := inspector.readFile("/proc/1/comm")
	if err != nil {
		return
	}
	if strings.TrimSpace(string(content)) != "systemd" {
		report.Systemd = unavailable("systemd-not-pid1")
		return
	}
	info, err := os.Stat(inspector.rooted("/run/systemd/system"))
	if err != nil || !info.IsDir() {
		report.Systemd = unavailable("systemd-runtime-unavailable")
		return
	}
	report.Systemd = available()
}

func (inspector *Inspector) inspectCgroup(report *agentprotocol.CapabilityReport) {
	missing := unavailable("cgroup-v2-unavailable")
	report.Cgroup = agentprotocol.CgroupCapabilities{
		Version: 0, Unified: missing, CPU: missing, Memory: missing, IO: missing, PIDs: missing,
	}
	content, err := inspector.readFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return
	}
	report.Cgroup.Version = 2
	report.Cgroup.Unified = available()
	controllers := make(map[string]struct{})
	for _, controller := range strings.Fields(string(content)) {
		controllers[controller] = struct{}{}
	}
	report.Cgroup.CPU = controllerCapability(controllers, "cpu")
	report.Cgroup.Memory = controllerCapability(controllers, "memory")
	report.Cgroup.IO = controllerCapability(controllers, "io")
	report.Cgroup.PIDs = controllerCapability(controllers, "pids")
}

func (inspector *Inspector) inspectFilesystem(report *agentprotocol.CapabilityReport) {
	info, err := os.Stat(inspector.rooted(inspector.managedRoot))
	if err != nil || !info.IsDir() {
		report.Filesystem.Inspection = unavailable("managed-root-missing")
		report.Filesystem.ProjectQuota = unavailable("managed-root-missing")
		return
	}
	content, err := inspector.readFile("/proc/self/mountinfo")
	if err != nil {
		return
	}
	mount, err := findMount(string(content), inspector.managedRoot)
	if err != nil {
		report.Filesystem.Inspection = unknown("mountinfo-malformed")
		report.Filesystem.ProjectQuota = unknown("mountinfo-malformed")
		return
	}
	report.Filesystem.MountPoint = mount.point
	report.Filesystem.Type = mount.filesystem
	report.Filesystem.Inspection = available()
	options := mount.options
	switch mount.filesystem {
	case "xfs":
		if options["prjquota"] || options["pquota"] {
			report.Filesystem.ProjectQuota = available()
		} else {
			report.Filesystem.ProjectQuota = unavailable("project-quota-not-mounted")
		}
	case "ext4":
		if options["prjquota"] {
			report.Filesystem.ProjectQuota = available()
		} else {
			report.Filesystem.ProjectQuota = unavailable("project-quota-not-mounted")
		}
	default:
		report.Filesystem.ProjectQuota = unsupported("filesystem-project-quota-unsupported")
	}
}

func (inspector *Inspector) inspectSecurity(report *agentprotocol.CapabilityReport, distribution string) {
	switch distribution {
	case "debian", "ubuntu":
		report.Security.Provider = "apparmor"
		content, err := inspector.readFile("/sys/module/apparmor/parameters/enabled")
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(content)), "Y") {
			report.Security.Mode = "enabled"
			report.Security.Enforcement = available()
		} else {
			report.Security.Mode = "disabled"
			report.Security.Enforcement = unavailable("apparmor-disabled")
		}
	case "rocky":
		report.Security.Provider = "selinux"
		content, err := inspector.readFile("/sys/fs/selinux/enforce")
		if err != nil {
			report.Security.Mode = "disabled"
			report.Security.Enforcement = unavailable("selinux-disabled")
			return
		}
		switch strings.TrimSpace(string(content)) {
		case "1":
			report.Security.Mode = "enforcing"
			report.Security.Enforcement = available()
		case "0":
			report.Security.Mode = "permissive"
			report.Security.Enforcement = unavailable("selinux-permissive")
		default:
			report.Security.Mode = "unknown"
			report.Security.Enforcement = unknown("selinux-state-malformed")
		}
	default:
		report.Security.Provider = "none"
		report.Security.Mode = "unknown"
		report.Security.Enforcement = unsupported("distribution-security-unsupported")
	}
}

func (inspector *Inspector) inspectPorts(report *agentprotocol.CapabilityReport) {
	listeners := make(map[int]struct{})
	readable := false
	malformed := false
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		content, err := inspector.readFile(path)
		if err != nil {
			continue
		}
		readable = true
		ports, parseErr := parseListeningTCPPorts(string(content))
		if parseErr != nil {
			malformed = true
			continue
		}
		for port := range ports {
			listeners[port] = struct{}{}
		}
	}
	for _, port := range []int{80, 443, 8443} {
		state := available()
		if !readable || malformed {
			state = unknown("port-inspection-failed")
		} else if _, occupied := listeners[port]; occupied {
			state = unavailable("port-in-use")
		}
		report.Ports = append(report.Ports, agentprotocol.PortCapability{
			Port: port, Network: "tcp", Availability: state,
		})
	}
}

func (inspector *Inspector) inspectPackages(
	ctx context.Context,
	report *agentprotocol.CapabilityReport,
	distribution string,
) {
	definitions, supported := packageDefinitions(distribution)
	for _, definition := range definitions {
		item := agentprotocol.PackageCapability{Key: definition.key, PackageName: definition.name}
		if !supported {
			item.Availability = unsupported("distribution-package-query-unsupported")
			report.Packages = append(report.Packages, item)
			continue
		}
		version, installed, err := inspector.queryPackage(ctx, distribution, definition.name)
		switch {
		case err != nil:
			item.Availability = unknown("package-query-failed")
		case !installed:
			item.Availability = unavailable("package-not-installed")
		default:
			item.Version = version
			item.Availability = available()
		}
		report.Packages = append(report.Packages, item)
	}
}

func (inspector *Inspector) inspectServices(
	ctx context.Context,
	report *agentprotocol.CapabilityReport,
	distribution string,
) {
	definitions := serviceDefinitions(distribution)
	for _, definition := range definitions {
		item := agentprotocol.ServiceCapability{
			Key: definition.key, Unit: definition.unit, LoadState: "unknown",
			ActiveState: "unknown", SubState: "unknown", UnitFileState: "unknown",
		}
		if report.Systemd.Status != agentprotocol.CapabilityAvailable {
			item.Availability = unavailable("systemd-unavailable")
			report.Services = append(report.Services, item)
			continue
		}
		result, err := inspector.runner.Run(ctx, "/usr/bin/systemctl", "show", "--no-pager",
			"--property=LoadState", "--property=ActiveState", "--property=SubState",
			"--property=UnitFileState", definition.unit)
		if err != nil || result.ExitCode != 0 {
			item.Availability = unknown("service-query-failed")
			report.Services = append(report.Services, item)
			continue
		}
		properties, parseErr := parseProperties(result.Output)
		if parseErr != nil {
			item.Availability = unknown("service-state-malformed")
			report.Services = append(report.Services, item)
			continue
		}
		item.LoadState = properties["LoadState"]
		item.ActiveState = properties["ActiveState"]
		item.SubState = properties["SubState"]
		item.UnitFileState = properties["UnitFileState"]
		if item.LoadState == "not-found" {
			item.Availability = unavailable("service-not-installed")
		} else {
			item.Availability = available()
		}
		report.Services = append(report.Services, item)
	}
}

func (inspector *Inspector) inspectOCIRuntime(report *agentprotocol.CapabilityReport) {
	report.OCI = unavailableOCIRuntime("runtime-prerequisite-unavailable")
	podman := packageByKey(report.Packages, "podman")
	if podman.Availability.Status != agentprotocol.CapabilityAvailable {
		report.OCI.Rootless = podman.Availability
		return
	}
	report.OCI.Version = podman.Version
	report.OCI.Rootless = available()
	if report.Systemd.Status != agentprotocol.CapabilityAvailable ||
		report.Cgroup.Unified.Status != agentprotocol.CapabilityAvailable {
		report.OCI.Quadlet = unavailable("quadlet-systemd-cgroup-v2-required")
	} else if !podmanVersionAtLeast(podman.Version, 4, 4) {
		report.OCI.Quadlet = unsupported("podman-version-lacks-quadlet")
	} else {
		report.OCI.Quadlet = available()
	}
	if packagesAvailable(report.Packages, "netavark", "aardvark-dns", "passt", "slirp4netns") {
		report.OCI.Network = available()
	} else {
		report.OCI.Network = unavailable("rootless-network-dependency-missing")
	}
	if packagesAvailable(report.Packages, "fuse-overlayfs") {
		report.OCI.Storage = available()
	} else {
		report.OCI.Storage = unavailable("rootless-storage-dependency-missing")
	}
	if !packagesAvailable(report.Packages, "uidmap") {
		report.OCI.Rootless = unavailable("subordinate-id-helper-missing")
	}
	report.OCI.RootfulSocketIsolation = available()
	service := serviceByKey(report.Services, "podman")
	if service.ActiveState == "active" || service.ActiveState == "activating" ||
		service.UnitFileState == "enabled" || service.UnitFileState == "enabled-runtime" {
		report.OCI.RootfulSocketIsolation = unavailable("rootful-podman-socket-enabled")
	} else if service.UnitFileState != "masked" {
		report.OCI.RootfulSocketIsolation = unavailable("rootful-podman-socket-not-masked")
	}
	if _, err := os.Lstat(inspector.rooted("/run/podman/podman.sock")); err == nil {
		report.OCI.RootfulSocketIsolation = unavailable("rootful-podman-socket-present")
	} else if !errors.Is(err, os.ErrNotExist) {
		report.OCI.RootfulSocketIsolation = unknown("rootful-podman-socket-inspection-failed")
	}
	if report.OCI.Rootless.Status == agentprotocol.CapabilityAvailable &&
		report.OCI.Storage.Status == agentprotocol.CapabilityAvailable &&
		report.OCI.RootfulSocketIsolation.Status == agentprotocol.CapabilityAvailable {
		report.OCI.ImagePreparation = available()
	}
	report.OCI.ImageScanning = unavailable("image-scanner-not-installed")
	scannerPath := inspector.rooted("/usr/local/libexec/stackfort-trivy")
	trusted, present, err := trustedScannerExecutable(scannerPath)
	if err != nil {
		report.OCI.ImageScanning = unknown("image-scanner-inspection-failed")
	} else if present && !trusted {
		report.OCI.ImageScanning = unavailable("image-scanner-metadata-untrusted")
	} else if trusted {
		report.OCI.ScannerVersion = ociimage.ScannerVersion
		report.OCI.ImageScanning = available()
	}
}

func unavailableOCIRuntime(reason string) agentprotocol.OCIRuntimeCapabilities {
	state := unavailable(reason)
	return agentprotocol.OCIRuntimeCapabilities{
		Provider: "podman", ScannerProvider: "trivy", Rootless: state, Quadlet: state, Network: state,
		Storage: state, RootfulSocketIsolation: state, ImagePreparation: state, ImageScanning: state,
	}
}

func packageByKey(packages []agentprotocol.PackageCapability, key string) agentprotocol.PackageCapability {
	for _, item := range packages {
		if item.Key == key {
			return item
		}
	}
	return agentprotocol.PackageCapability{Availability: unknown("package-capability-missing")}
}

func serviceByKey(services []agentprotocol.ServiceCapability, key string) agentprotocol.ServiceCapability {
	for _, item := range services {
		if item.Key == key {
			return item
		}
	}
	return agentprotocol.ServiceCapability{ActiveState: "unknown", UnitFileState: "unknown"}
}

func packagesAvailable(packages []agentprotocol.PackageCapability, keys ...string) bool {
	for _, key := range keys {
		if packageByKey(packages, key).Availability.Status != agentprotocol.CapabilityAvailable {
			return false
		}
	}
	return true
}

func podmanVersionAtLeast(value string, wantMajor, wantMinor uint64) bool {
	value = strings.TrimSpace(value)
	if _, rest, found := strings.Cut(value, ":"); found {
		value = rest
	}
	parts := strings.SplitN(value, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return false
	}
	minorText := parts[1]
	for index, character := range minorText {
		if character < '0' || character > '9' {
			minorText = minorText[:index]
			break
		}
	}
	minor, err := strconv.ParseUint(minorText, 10, 32)
	return err == nil && (major > wantMajor || major == wantMajor && minor >= wantMinor)
}

func (inspector *Inspector) queryPackage(
	ctx context.Context,
	distribution string,
	name string,
) (string, bool, error) {
	var executable string
	var arguments []string
	switch distribution {
	case "debian", "ubuntu":
		executable = "/usr/bin/dpkg-query"
		arguments = []string{"-W", "-f=${db:Status-Abbrev}\\t${Version}\\n", name}
	case "rocky":
		executable = "/usr/bin/rpm"
		arguments = []string{"-q", "--qf", "%{VERSION}-%{RELEASE}\\n", name}
	default:
		return "", false, errors.New("unsupported package database")
	}
	result, err := inspector.runner.Run(ctx, executable, arguments...)
	if err != nil {
		return "", false, err
	}
	if result.ExitCode != 0 {
		return "", false, nil
	}
	line := boundedLine(result.Output, 256)
	if distribution == "rocky" {
		if line == "" {
			return "", false, errors.New("empty RPM result")
		}
		return line, true, nil
	}
	fields := strings.SplitN(line, "\t", 2)
	if len(fields) != 2 || fields[1] == "" {
		return "", false, errors.New("malformed dpkg result")
	}
	if !strings.HasPrefix(fields[0], "ii") {
		return "", false, nil
	}
	return fields[1], true, nil
}

func (inspector *Inspector) readFile(path string) ([]byte, error) {
	file, err := os.Open(inspector.rooted(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximumProbeFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maximumProbeFileBytes {
		return nil, errors.New("capability source exceeds size limit")
	}
	return content, nil
}

func (inspector *Inspector) rooted(path string) string {
	clean := filepath.Clean(filepath.FromSlash(path))
	volume := filepath.VolumeName(clean)
	clean = strings.TrimPrefix(clean, volume)
	clean = strings.TrimLeft(clean, string(filepath.Separator))
	return filepath.Join(inspector.root, clean)
}

type packageDefinition struct{ key, name string }
type serviceDefinition struct{ key, unit string }

func packageDefinitions(distribution string) ([]packageDefinition, bool) {
	definitions := []packageDefinition{
		{"nginx", "nginx"}, {"php-fpm", "php-fpm"}, {"mariadb", "mariadb-server"},
		{"vinyl", "vinyl-cache"}, {"podman", "podman"}, {"netavark", "netavark"},
		{"aardvark-dns", "aardvark-dns"}, {"passt", "passt"}, {"slirp4netns", "slirp4netns"},
		{"fuse-overlayfs", "fuse-overlayfs"}, {"uidmap", "uidmap"}, {"coraza", "stackfort-waf"},
	}
	switch distribution {
	case "debian", "ubuntu":
		return definitions, true
	case "rocky":
		for index := range definitions {
			if definitions[index].key == "uidmap" {
				definitions[index].name = "shadow-utils-subid"
			}
		}
		return definitions, true
	default:
		return definitions, false
	}
}

func serviceDefinitions(distribution string) []serviceDefinition {
	phpUnit := "php-fpm.service"
	firewallUnit := "firewalld.service"
	switch distribution {
	case "debian":
		phpUnit, firewallUnit = "php8.4-fpm.service", "nftables.service"
	case "ubuntu":
		phpUnit, firewallUnit = "php8.5-fpm.service", "nftables.service"
	}
	return []serviceDefinition{
		{"nginx", "nginx.service"}, {"php-fpm", phpUnit}, {"mariadb", "mariadb.service"},
		{"vinyl", "vinyl.service"}, {"podman", "podman.socket"}, {"firewall", firewallUnit},
		{"stackfort-api", "stackfort-api.service"}, {"stackfort-agent", "stackfort-agent.service"},
	}
}

func parseOSRelease(content string) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, found := strings.Cut(line, "=")
		if !found || key == "" {
			return nil, errors.New("malformed os-release assignment")
		}
		for _, character := range key {
			if !(character == '_' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
				return nil, errors.New("malformed os-release key")
			}
		}
		value, err := unquoteOSRelease(raw)
		if err != nil {
			return nil, err
		}
		if len(value) > 4_096 {
			return nil, errors.New("os-release value exceeds size limit")
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func unquoteOSRelease(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	quote := byte(0)
	if raw[0] == '\'' || raw[0] == '"' {
		quote = raw[0]
		if len(raw) < 2 || raw[len(raw)-1] != quote {
			return "", errors.New("unterminated os-release quote")
		}
		raw = raw[1 : len(raw)-1]
	}
	var result strings.Builder
	escaped := false
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		if escaped {
			if !strings.ContainsRune("$\\\"'`", rune(character)) {
				result.WriteByte('\\')
			}
			result.WriteByte(character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if quote == 0 && (character == '\'' || character == '"' || character == '`') {
			return "", errors.New("unquoted os-release special character")
		}
		result.WriteByte(character)
	}
	if escaped {
		return "", errors.New("unterminated os-release escape")
	}
	return result.String(), nil
}

type mountRecord struct {
	point      string
	filesystem string
	options    map[string]bool
}

func findMount(content, target string) (mountRecord, error) {
	best := mountRecord{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if len(fields) < 7 || separator < 6 || separator+3 >= len(fields) {
			return mountRecord{}, errors.New("malformed mountinfo record")
		}
		point, err := unescapeMountField(fields[4])
		if err != nil {
			return mountRecord{}, err
		}
		if !pathContains(point, target) || len(point) <= len(best.point) {
			continue
		}
		options := make(map[string]bool)
		for _, source := range []string{fields[5], fields[separator+3]} {
			for _, option := range strings.Split(source, ",") {
				options[option] = true
			}
		}
		best = mountRecord{point: point, filesystem: fields[separator+1], options: options}
	}
	if err := scanner.Err(); err != nil {
		return mountRecord{}, err
	}
	if best.point == "" {
		return mountRecord{}, errors.New("target mount not found")
	}
	return best, nil
}

func unescapeMountField(value string) (string, error) {
	replacer := strings.NewReplacer("\\040", " ", "\\011", "\t", "\\012", "\n", "\\134", "\\")
	decoded := replacer.Replace(value)
	if strings.Contains(decoded, "\\") {
		return "", errors.New("unsupported mountinfo escape")
	}
	return decoded, nil
}

func pathContains(mountPoint, target string) bool {
	cleanMount := filepath.ToSlash(filepath.Clean(filepath.FromSlash(mountPoint)))
	cleanTarget := filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	return cleanMount == "/" || cleanTarget == cleanMount || strings.HasPrefix(cleanTarget, strings.TrimSuffix(cleanMount, "/")+"/")
}

func parseListeningTCPPorts(content string) (map[int]struct{}, error) {
	ports := make(map[int]struct{})
	scanner := bufio.NewScanner(strings.NewReader(content))
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if first {
			first = false
			if strings.Contains(line, "local_address") {
				continue
			}
		}
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return nil, errors.New("malformed proc TCP row")
		}
		if fields[3] != "0A" {
			continue
		}
		_, portHex, found := strings.Cut(fields[1], ":")
		if !found {
			return nil, errors.New("malformed proc TCP address")
		}
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil {
			return nil, errors.New("malformed proc TCP port")
		}
		ports[int(port)] = struct{}{}
	}
	return ports, scanner.Err()
}

func parseProperties(content string) (map[string]string, error) {
	required := map[string]bool{
		"LoadState": false, "ActiveState": false, "SubState": false, "UnitFileState": false,
	}
	values := make(map[string]string, len(required))
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		key, raw, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			return nil, errors.New("malformed systemd property")
		}
		if _, expected := required[key]; !expected {
			return nil, errors.New("unexpected systemd property")
		}
		if raw == "" && key == "UnitFileState" {
			raw = "unknown"
		}
		value := boundedLine(raw, 64)
		if value == "" {
			return nil, errors.New("invalid systemd property")
		}
		values[key] = value
		required[key] = true
	}
	for _, present := range required {
		if !present {
			return nil, errors.New("missing systemd property")
		}
	}
	return values, nil
}

func distributionSupport(distribution, version string) agentprotocol.Capability {
	supported := map[string]string{"debian": "13", "ubuntu": "26.04", "rocky": "10"}
	prefix, exists := supported[distribution]
	if !exists {
		return unsupported("distribution-not-supported")
	}
	if version != prefix && !strings.HasPrefix(version, prefix+".") {
		return unsupported("distribution-version-not-supported")
	}
	return available()
}

func controllerCapability(controllers map[string]struct{}, name string) agentprotocol.Capability {
	if _, exists := controllers[name]; exists {
		return available()
	}
	return unavailable("cgroup-controller-unavailable")
}

func boundedLine(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func available() agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}

func unavailable(reason string) agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityUnavailable, ReasonCode: reason}
}

func unsupported(reason string) agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityUnsupported, ReasonCode: reason}
}

func unknown(reason string) agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: reason}
}
