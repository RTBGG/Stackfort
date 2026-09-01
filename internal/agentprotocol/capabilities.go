// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const ManagedHostingRoot = "/srv/hosting"

type CapabilityStatus string

const (
	CapabilityAvailable   CapabilityStatus = "available"
	CapabilityUnavailable CapabilityStatus = "unavailable"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityUnknown     CapabilityStatus = "unknown"
)

type Capability struct {
	Status     CapabilityStatus `json:"status"`
	ReasonCode string           `json:"reasonCode,omitempty"`
}

type InspectCapabilitiesRequest struct{}

type CapabilityReport struct {
	InspectedAt string                 `json:"inspectedAt"`
	Platform    PlatformCapabilities   `json:"platform"`
	Systemd     Capability             `json:"systemd"`
	Cgroup      CgroupCapabilities     `json:"cgroup"`
	Filesystem  FilesystemCapabilities `json:"filesystem"`
	Security    SecurityCapabilities   `json:"security"`
	OCI         OCIRuntimeCapabilities `json:"oci"`
	Ports       []PortCapability       `json:"ports"`
	Packages    []PackageCapability    `json:"packages"`
	Services    []ServiceCapability    `json:"services"`
}

type OCIRuntimeCapabilities struct {
	Provider               string     `json:"provider"`
	Version                string     `json:"version,omitempty"`
	ScannerProvider        string     `json:"scannerProvider"`
	ScannerVersion         string     `json:"scannerVersion,omitempty"`
	Rootless               Capability `json:"rootless"`
	Quadlet                Capability `json:"quadlet"`
	Network                Capability `json:"network"`
	Storage                Capability `json:"storage"`
	RootfulSocketIsolation Capability `json:"rootfulSocketIsolation"`
	ImagePreparation       Capability `json:"imagePreparation"`
	ImageScanning          Capability `json:"imageScanning"`
}

type PlatformCapabilities struct {
	DistributionID string     `json:"distributionId"`
	VersionID      string     `json:"versionId"`
	Architecture   string     `json:"architecture"`
	KernelRelease  string     `json:"kernelRelease"`
	Support        Capability `json:"support"`
}

type CgroupCapabilities struct {
	Version int        `json:"version"`
	Unified Capability `json:"unified"`
	CPU     Capability `json:"cpu"`
	Memory  Capability `json:"memory"`
	IO      Capability `json:"io"`
	PIDs    Capability `json:"pids"`
}

type FilesystemCapabilities struct {
	Target       string     `json:"target"`
	MountPoint   string     `json:"mountPoint"`
	Type         string     `json:"type"`
	Inspection   Capability `json:"inspection"`
	ProjectQuota Capability `json:"projectQuota"`
}

type SecurityCapabilities struct {
	Provider    string     `json:"provider"`
	Mode        string     `json:"mode"`
	Enforcement Capability `json:"enforcement"`
}

type PortCapability struct {
	Port         int        `json:"port"`
	Network      string     `json:"network"`
	Availability Capability `json:"availability"`
}

type PackageCapability struct {
	Key          string     `json:"key"`
	PackageName  string     `json:"packageName"`
	Version      string     `json:"version,omitempty"`
	Availability Capability `json:"availability"`
}

type ServiceCapability struct {
	Key           string     `json:"key"`
	Unit          string     `json:"unit"`
	LoadState     string     `json:"loadState"`
	ActiveState   string     `json:"activeState"`
	SubState      string     `json:"subState"`
	UnitFileState string     `json:"unitFileState"`
	Availability  Capability `json:"availability"`
}

func ValidateCapabilityReport(report CapabilityReport) error {
	if _, err := time.Parse(time.RFC3339Nano, report.InspectedAt); err != nil {
		return errors.New("agent capability timestamp is malformed")
	}
	if !boundedText(report.Platform.DistributionID, 64) ||
		!boundedText(report.Platform.VersionID, 64) ||
		!boundedText(report.Platform.KernelRelease, 128) ||
		(report.Platform.Architecture != "amd64" && report.Platform.Architecture != "arm64") {
		return errors.New("agent platform capabilities are malformed")
	}
	if err := validateCapability(report.Platform.Support); err != nil {
		return err
	}
	if err := validateCapability(report.Systemd); err != nil {
		return err
	}
	if report.Cgroup.Version < 0 || report.Cgroup.Version > 2 {
		return errors.New("agent cgroup version is malformed")
	}
	for _, capability := range []Capability{
		report.Cgroup.Unified, report.Cgroup.CPU, report.Cgroup.Memory,
		report.Cgroup.IO, report.Cgroup.PIDs, report.Filesystem.Inspection,
		report.Filesystem.ProjectQuota, report.Security.Enforcement,
		report.OCI.Rootless, report.OCI.Quadlet, report.OCI.Network,
		report.OCI.Storage, report.OCI.RootfulSocketIsolation,
		report.OCI.ImagePreparation, report.OCI.ImageScanning,
	} {
		if err := validateCapability(capability); err != nil {
			return err
		}
	}
	if report.OCI.Provider != "podman" || report.OCI.ScannerProvider != "trivy" ||
		(report.OCI.Version != "" && !boundedText(report.OCI.Version, 128)) ||
		(report.OCI.ScannerVersion != "" && !boundedText(report.OCI.ScannerVersion, 64)) ||
		(report.OCI.Rootless.Status == CapabilityAvailable && report.OCI.Version == "") ||
		(report.OCI.ImageScanning.Status == CapabilityAvailable && report.OCI.ScannerVersion == "") {
		return errors.New("agent OCI runtime capabilities are malformed")
	}
	if report.Filesystem.Target != ManagedHostingRoot ||
		!boundedText(report.Filesystem.MountPoint, 4_096) ||
		!boundedText(report.Filesystem.Type, 64) {
		return errors.New("agent filesystem capabilities are malformed")
	}
	if !oneOf(report.Security.Provider, "apparmor", "selinux", "none", "unknown") ||
		!oneOf(report.Security.Mode, "enforcing", "permissive", "enabled", "disabled", "unknown") {
		return errors.New("agent security capabilities are malformed")
	}
	if err := validatePorts(report.Ports); err != nil {
		return err
	}
	if err := validatePackages(report.Packages); err != nil {
		return err
	}
	return validateServices(report.Services)
}

func validateCapability(capability Capability) error {
	switch capability.Status {
	case CapabilityAvailable:
		if capability.ReasonCode != "" {
			return errors.New("available capability must not contain a reason")
		}
	case CapabilityUnavailable, CapabilityUnsupported, CapabilityUnknown:
		if !validBoundedIdentifier(capability.ReasonCode) {
			return errors.New("unavailable capability requires a bounded reason code")
		}
	default:
		return errors.New("agent capability status is malformed")
	}
	return nil
}

func validatePorts(ports []PortCapability) error {
	if len(ports) != 3 {
		return errors.New("agent port capabilities are incomplete")
	}
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if (port.Port != 80 && port.Port != 443 && port.Port != 8443) || port.Network != "tcp" {
			return errors.New("agent port capability is malformed")
		}
		if _, exists := seen[port.Port]; exists {
			return errors.New("agent port capability is duplicated")
		}
		seen[port.Port] = struct{}{}
		if err := validateCapability(port.Availability); err != nil {
			return err
		}
	}
	return nil
}

func validatePackages(packages []PackageCapability) error {
	if len(packages) != 12 {
		return errors.New("agent package capabilities are incomplete")
	}
	seen := make(map[string]struct{}, len(packages))
	for _, item := range packages {
		if !oneOf(item.Key, "nginx", "php-fpm", "mariadb", "vinyl", "podman", "netavark",
			"aardvark-dns", "passt", "slirp4netns", "fuse-overlayfs", "uidmap", "coraza") ||
			!boundedPackageName(item.PackageName) ||
			(item.Version != "" && !boundedText(item.Version, 128)) ||
			(item.Availability.Status == CapabilityAvailable && item.Version == "") {
			return errors.New("agent package capability is malformed")
		}
		if _, exists := seen[item.Key]; exists {
			return errors.New("agent package capability is duplicated")
		}
		seen[item.Key] = struct{}{}
		if err := validateCapability(item.Availability); err != nil {
			return err
		}
	}
	return nil
}

func validateServices(services []ServiceCapability) error {
	if len(services) != 8 {
		return errors.New("agent service capabilities are incomplete")
	}
	seen := make(map[string]struct{}, len(services))
	for _, item := range services {
		if !oneOf(item.Key, "nginx", "php-fpm", "mariadb", "vinyl", "podman", "firewall", "stackfort-api", "stackfort-agent") ||
			!boundedUnitName(item.Unit) || !boundedState(item.LoadState) ||
			!boundedState(item.ActiveState) || !boundedState(item.SubState) ||
			!boundedState(item.UnitFileState) {
			return errors.New("agent service capability is malformed")
		}
		if _, exists := seen[item.Key]; exists {
			return errors.New("agent service capability is duplicated")
		}
		seen[item.Key] = struct{}{}
		if err := validateCapability(item.Availability); err != nil {
			return err
		}
	}
	return nil
}

func boundedText(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func boundedPackageName(value string) bool {
	return len(value) <= 128 && validBoundedIdentifier(value)
}

func boundedUnitName(value string) bool {
	return len(value) > 0 && len(value) <= 256 && validUnitNamePattern.MatchString(value) &&
		(strings.HasSuffix(value, ".service") || strings.HasSuffix(value, ".socket"))
}

func boundedState(value string) bool {
	return value == "unknown" || (len(value) <= 64 && validBoundedIdentifier(value))
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateCapabilityUnion(response Response, expected Operation) error {
	resultCount := 0
	if response.Handshake != nil {
		resultCount++
	}
	if response.Capabilities != nil {
		resultCount++
	}
	if response.HostingIdentity != nil {
		resultCount++
	}
	if response.HostingFilesystem != nil {
		resultCount++
	}
	if response.FileListing != nil {
		resultCount++
	}
	if response.HostingLogs != nil {
		resultCount++
	}
	if response.WAFEvents != nil {
		resultCount++
	}
	if response.CacheMetrics != nil {
		resultCount++
	}
	if response.CachePurge != nil {
		resultCount++
	}
	if response.HostingResources != nil {
		resultCount++
	}
	if response.DocumentRoot != nil {
		resultCount++
	}
	if response.NGINXBaseline != nil {
		resultCount++
	}
	if response.NGINXActivation != nil {
		resultCount++
	}
	if response.ACMEHTTP01 != nil {
		resultCount++
	}
	if response.TLSCertificate != nil {
		resultCount++
	}
	if response.PHPPoolInspection != nil {
		resultCount++
	}
	if response.PHPPools != nil {
		resultCount++
	}
	if response.Database != nil {
		resultCount++
	}
	if response.DatabasePasswordRotation != nil {
		resultCount++
	}
	if response.DatabaseDrop != nil {
		resultCount++
	}
	if response.ScheduledJob != nil {
		resultCount++
	}
	if response.OCIImage != nil {
		resultCount++
	}
	if response.OCIResources != nil {
		resultCount++
	}
	if response.Error != nil {
		resultCount++
	}
	if resultCount != 1 {
		return errors.New("agent protocol response must contain exactly one result or error")
	}
	if response.Error != nil {
		return nil
	}
	switch expected {
	case OperationHandshake:
		if response.Handshake == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationInspectCapabilities:
		if response.Capabilities == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileIdentity, OperationDeleteIdentity:
		if response.HostingIdentity == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileFilesystem:
		if response.HostingFilesystem == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationListFiles:
		if response.FileListing == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReadHostingLogs:
		if response.HostingLogs == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReadWAFEvents:
		if response.WAFEvents == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationInspectCacheMetrics:
		if response.CacheMetrics == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationPurgeCache:
		if response.CachePurge == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileResources:
		if response.HostingResources == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationEnsureDocumentRoot:
		if response.DocumentRoot == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileNGINXBaseline:
		if response.NGINXBaseline == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationActivateNGINXSites:
		if response.NGINXActivation == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileACMEHTTP01:
		if response.ACMEHTTP01 == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationStageTLSCertificate:
		if response.TLSCertificate == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationInspectPHPPools:
		if response.PHPPoolInspection == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcilePHPPools:
		if response.PHPPools == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationProvisionDatabase:
		if response.Database == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationRotateDatabasePassword:
		if response.DatabasePasswordRotation == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationDropDatabase:
		if response.DatabaseDrop == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileScheduledJob:
		if response.ScheduledJob == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationPrepareOCIImage:
		if response.OCIImage == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	case OperationReconcileOCIResources:
		if response.OCIResources == nil {
			return fmt.Errorf("agent protocol response does not match %s", expected)
		}
	default:
		return errors.New("agent protocol response operation is unknown")
	}
	return nil
}
