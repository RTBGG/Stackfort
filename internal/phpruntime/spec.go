// SPDX-License-Identifier: AGPL-3.0-or-later

// Package phpruntime defines the fixed PHP runtimes and account-pool intent
// accepted by Stackfort. It deliberately exposes no caller-controlled binary,
// configuration, socket, or systemd-unit path.
package phpruntime

import (
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const (
	ConfigurationRoot         = "/etc/stackfort/php"
	RuntimeRoot               = "/run/stackfort-php"
	DefaultMaxChildren uint32 = 4
	DefaultMemoryMiB   uint32 = 128
	MaximumVersions           = 3
	MaximumMaxChildren uint32 = 128
	MinimumMemoryMiB   uint32 = 16
	MaximumMemoryMiB   uint32 = 2_048
)

var ErrInvalidSpec = errors.New("invalid PHP runtime specification")

// Profile contains installation-owned paths for one native distribution
// runtime. All fields are selected by ForDistribution, never by an RPC caller.
type Profile struct {
	DistributionID string
	Version        string
	PackageName    string
	BinaryPath     string
	VendorUnit     string
	NGINXUser      string
}

// PoolSetSpec describes the desired version-specific PHP-FPM pools for one
// hosting account. RetireAbsent makes Versions authoritative; without it the
// request only ensures the listed pools and leaves other managed pools intact.
type PoolSetSpec struct {
	Identity       hostingidentity.Spec `json:"identity"`
	Versions       []string             `json:"versions"`
	MaxChildren    uint32               `json:"maxChildren"`
	MemoryLimitMiB uint32               `json:"memoryLimitMib"`
	RetireAbsent   bool                 `json:"retireAbsent"`
}

func ForDistribution(distributionID, version string) (Profile, error) {
	var profile Profile
	switch distributionID + ":" + version {
	case "debian:8.4":
		profile = Profile{
			DistributionID: "debian", Version: "8.4", PackageName: "php8.4-fpm",
			BinaryPath: "/usr/sbin/php-fpm8.4", VendorUnit: "php8.4-fpm.service", NGINXUser: "www-data",
		}
	case "ubuntu:8.5":
		profile = Profile{
			DistributionID: "ubuntu", Version: "8.5", PackageName: "php8.5-fpm",
			BinaryPath: "/usr/sbin/php-fpm8.5", VendorUnit: "php8.5-fpm.service", NGINXUser: "www-data",
		}
	case "rocky:8.3":
		profile = Profile{
			DistributionID: "rocky", Version: "8.3", PackageName: "php-fpm",
			BinaryPath: "/usr/sbin/php-fpm", VendorUnit: "php-fpm.service", NGINXUser: "nginx",
		}
	default:
		return Profile{}, ErrInvalidSpec
	}
	return profile, nil
}

func ApprovedVersion(distributionID string) (string, error) {
	for _, version := range []string{"8.3", "8.4", "8.5"} {
		if _, err := ForDistribution(distributionID, version); err == nil {
			return version, nil
		}
	}
	return "", ErrInvalidSpec
}

func Validate(spec PoolSetSpec) error {
	if hostingidentity.Validate(spec.Identity) != nil || len(spec.Versions) > MaximumVersions ||
		spec.MaxChildren < 1 || spec.MaxChildren > MaximumMaxChildren ||
		spec.MemoryLimitMiB < MinimumMemoryMiB || spec.MemoryLimitMiB > MaximumMemoryMiB ||
		!slices.IsSorted(spec.Versions) {
		return ErrInvalidSpec
	}
	for index, version := range spec.Versions {
		if !isApprovedVersion(version) || index > 0 && version == spec.Versions[index-1] {
			return ErrInvalidSpec
		}
	}
	return nil
}

func Canonicalize(spec PoolSetSpec) (PoolSetSpec, error) {
	spec.Versions = append([]string(nil), spec.Versions...)
	slices.Sort(spec.Versions)
	spec.Versions = slices.Compact(spec.Versions)
	if spec.MaxChildren == 0 {
		spec.MaxChildren = DefaultMaxChildren
	}
	if spec.MemoryLimitMiB == 0 {
		spec.MemoryLimitMiB = DefaultMemoryMiB
	}
	if err := Validate(spec); err != nil {
		return PoolSetSpec{}, err
	}
	return spec, nil
}

func SocketPath(identity hostingidentity.Spec, version string) (string, error) {
	if hostingidentity.Validate(identity) != nil || !isApprovedVersion(version) {
		return "", ErrInvalidSpec
	}
	return path.Join(RuntimeRoot, "account-"+strconv.FormatUint(uint64(identity.UID), 10)+"-php"+version+".sock"), nil
}

func PIDPath(identity hostingidentity.Spec, version string) (string, error) {
	if hostingidentity.Validate(identity) != nil || !isApprovedVersion(version) {
		return "", ErrInvalidSpec
	}
	return path.Join(RuntimeRoot, "account-"+strconv.FormatUint(uint64(identity.UID), 10)+"-php"+version+".pid"), nil
}

func ConfigurationPath(identity hostingidentity.Spec, version string) (string, error) {
	if hostingidentity.Validate(identity) != nil || !isApprovedVersion(version) {
		return "", ErrInvalidSpec
	}
	return path.Join(ConfigurationRoot, "account-"+strconv.FormatUint(uint64(identity.UID), 10)+"-php"+version+".conf"), nil
}

func UnitName(identity hostingidentity.Spec, version string) (string, error) {
	if hostingidentity.Validate(identity) != nil || !isApprovedVersion(version) {
		return "", ErrInvalidSpec
	}
	return "stackfort-php-" + strings.ReplaceAll(version, ".", "-") + "-" +
		strconv.FormatUint(uint64(identity.UID), 10) + ".service", nil
}

// VersionInvocationValues serializes only the redundant identity and approved
// version needed by agentexec's fixed PHP profiles.
func VersionInvocationValues(identity hostingidentity.Spec, version string) ([]string, error) {
	if hostingidentity.Validate(identity) != nil || !isApprovedVersion(version) {
		return nil, ErrInvalidSpec
	}
	return []string{
		identity.AccountID, identity.Username,
		strconv.FormatUint(uint64(identity.UID), 10),
		strconv.FormatUint(uint64(identity.GID), 10),
		identity.HomeDirectory, version,
	}, nil
}

func SpecFromVersionInvocationValues(values []string) (hostingidentity.Spec, string, error) {
	if len(values) != 6 {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	uid, err := strconv.ParseUint(values[2], 10, 32)
	if err != nil {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	gid, err := strconv.ParseUint(values[3], 10, 32)
	if err != nil {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	identity := hostingidentity.Spec{
		AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid), HomeDirectory: values[4],
	}
	if hostingidentity.Validate(identity) != nil || !isApprovedVersion(values[5]) {
		return hostingidentity.Spec{}, "", ErrInvalidSpec
	}
	return identity, values[5], nil
}

func isApprovedVersion(version string) bool {
	return version == "8.3" || version == "8.4" || version == "8.5"
}

func ValidateVersion(version string) error {
	if !isApprovedVersion(version) {
		return ErrInvalidSpec
	}
	return nil
}
