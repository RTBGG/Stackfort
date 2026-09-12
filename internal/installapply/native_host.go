// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/installpreflight"
	"github.com/RTBGG/stackfort/internal/storageprep"
)

const NativeHostProfile = "debian-13-plain-ext4-grub-v1"
const nativePrerequisiteName = "native-prerequisites.json"
const NativePrerequisiteInstaller = "/var/lib/stackfort-installer/native-prerequisite-installer"

type NativeHostReport struct {
	SchemaVersion      int                      `json:"schemaVersion"`
	ReadOnly           bool                     `json:"readOnly"`
	PublicActivation   bool                     `json:"publicActivation"`
	Profile            string                   `json:"profile"`
	Eligible           bool                     `json:"eligible"`
	PrerequisitesReady bool                     `json:"prerequisitesReady"`
	MissingPackages    []string                 `json:"missingPackages"`
	Checks             []installpreflight.Check `json:"checks"`
	Snapshot           *NativeHostSnapshot      `json:"snapshot,omitempty"`
}

type NativeHostSnapshot struct {
	SecureBoot     string                  `json:"secureBoot"`
	Host           storageprep.Observation `json:"host"`
	Features       string                  `json:"features"`
	Blocks         string                  `json:"blocks"`
	BlockSize      string                  `json:"blockSize"`
	InodeSize      string                  `json:"inodeSize"`
	BootArtifacts  map[string]string       `json:"bootArtifacts"`
	PackagesSHA256 string                  `json:"packagesSHA256"`
}

var nativeBasePackages = []string{"apt", "dpkg", "ca-certificates", "debian-archive-keyring", "e2fsprogs", "util-linux", "mount", "systemd", "grub-common", "grub2-common", "initramfs-tools-core"}
var nativeAdditionalPackages = []string{"nftables", "quota"}
var nativeDependencyPackages = []string{"libnftables1", "libnftnl11", "libmnl0", "libxtables12", "libedit2", "libnl-3-200", "libnl-genl-3-200", "libtirpc3t64", "libtirpc-common", "libwrap0"}
var nativePackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+(?::(?:amd64|all))?$`)
var nativePackageVersion = regexp.MustCompile(`^[0-9][A-Za-z0-9.+:~\-]{0,127}$`)

func nativePackageInventory(text string) (map[string]string, error) {
	result := map[string]string{}
	if text == "" || len(text) > 256<<10 {
		return nil, errors.New("invalid package inventory size")
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || !nativePackageName.MatchString(fields[0]) || len(fields[1]) != 3 {
			return nil, errors.New("invalid package inventory record")
		}
		if _, exists := result[fields[0]]; exists {
			return nil, errors.New("duplicate package record")
		}
		if fields[1] == "rc " || fields[1] == "un " {
			result[fields[0]] = ""
			continue
		}
		if fields[1] != "ii " || !nativePackageVersion.MatchString(fields[2]) {
			return nil, errors.New("held, broken, foreign-architecture or incomplete package state")
		}
		result[fields[0]] = fields[2]
	}
	return result, nil
}

func nativeInstalled(packages map[string]string, name string) string {
	if value := packages[name]; value != "" {
		return value
	}
	return packages[name+":amd64"]
}

// Only the reviewed new packages may differ. Checking their versions alone
// would miss an unrelated install/removal/upgrade in the APT lock handoff gap.
func checkNativePackageDelta(before, after, planned map[string]string) error {
	if len(before) == 0 || len(after) == 0 || planned == nil {
		return errors.New("missing package delta inventory")
	}
	expected := maps.Clone(before)
	for name, version := range planned {
		if !nativeAllowedAddition(name) || !nativePackageVersion.MatchString(version) || nativeInstalled(before, name) != "" {
			return errors.New("package delta is not an approved addition")
		}
		key := name
		if _, exists := after[name+":amd64"]; exists {
			if _, duplicate := after[name]; duplicate {
				return errors.New("ambiguous package delta architecture")
			}
			key += ":amd64"
		}
		if after[key] != version {
			return errors.New("installed prerequisite version mismatch")
		}
		delete(expected, name)
		delete(expected, name+":amd64")
		expected[key] = version
	}
	if !maps.Equal(expected, after) {
		return errors.New("unplanned package mutation during prerequisites")
	}
	return nil
}

func nativeConflictingPackage(name string) bool {
	name, _, _ = strings.Cut(name, ":")
	if slices.Contains([]string{"ufw", "firewalld", "iptables-persistent", "netfilter-persistent"}, name) {
		return true
	}
	for _, prefix := range []string{"nginx", "apache2", "mariadb-", "mysql-", "postgresql", "php", "plesk", "cpanel", "cloudpanel", "coolify", "stackfort-", "docker", "containerd"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (report *NativeHostReport) add(id string, err error, summary, remedy string) {
	check := installpreflight.Check{ID: id, Status: installpreflight.CheckPass, Summary: summary}
	if err != nil {
		check.Status = installpreflight.CheckFail
		check.ReasonCode = id
		check.Detail = err.Error()
		check.Remediation = remedy
	}
	report.Checks = append(report.Checks, check)
}

func (report *NativeHostReport) finish() {
	report.Eligible = true
	for _, check := range report.Checks {
		if check.Status == installpreflight.CheckFail {
			report.Eligible = false
		}
	}
	report.PrerequisitesReady = report.Eligible && len(report.MissingPackages) == 0
	sort.Slice(report.Checks, func(i, j int) bool { return report.Checks[i].ID < report.Checks[j].ID })
}

func (report NativeHostReport) failure() error {
	for _, check := range report.Checks {
		if check.Status == installpreflight.CheckFail {
			return fmt.Errorf("%s: %s (%s)", check.ID, check.Detail, check.Remediation)
		}
	}
	return nil
}

func nativeSocketConflicts(text string, tcp bool) error {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 || !strings.Contains(lines[0], "local_address") {
		return errors.New("socket inventory unavailable")
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return errors.New("malformed socket inventory")
		}
		_, portText, ok := strings.Cut(fields[1], ":")
		if !ok {
			return errors.New("malformed socket address")
		}
		port, err := strconv.ParseUint(portText, 16, 16)
		if err != nil {
			return err
		}
		if (!tcp || fields[3] == "0A") && slices.Contains([]uint64{80, 443, 8443}, port) {
			return fmt.Errorf("reserved hosting port %d is already bound", port)
		}
	}
	return nil
}

type nativePrerequisiteRecord struct {
	RecoveryChoiceSHA256 string              `json:"recoveryChoiceSHA256,omitempty"`
	SchemaVersion        int                 `json:"schemaVersion"`
	OperationID          string              `json:"operationId"`
	ReleaseSHA256        string              `json:"releaseSHA256"`
	InstallerSHA256      string              `json:"installerSHA256"`
	Phase                string              `json:"phase"`
	Before               NativeHostSnapshot  `json:"before"`
	Planned              map[string]string   `json:"planned"`
	After                *NativeHostSnapshot `json:"after,omitempty"`
}

func (record nativePrerequisiteRecord) validate() error {
	if record.RecoveryChoiceSHA256 != "" && !pinDigestPattern.MatchString(record.RecoveryChoiceSHA256) {
		return errors.New("invalid prerequisite recovery-choice binding")
	}
	if record.SchemaVersion != 1 || !validSourceOperation(record.OperationID) || !pinDigestPattern.MatchString(record.ReleaseSHA256) || !pinDigestPattern.MatchString(record.InstallerSHA256) || !slices.Contains([]string{"checking", "applying", "complete", "recovery-required"}, record.Phase) {
		return errors.New("invalid prerequisite journal")
	}
	if !pinDigestPattern.MatchString(record.Before.PackagesSHA256) || !validSourceOperation(record.Before.Host.BootID) || len(record.Before.BootArtifacts) != 4 {
		return errors.New("invalid prerequisite host binding")
	}
	if !validSourceOperation(record.Before.Host.RootUUID) || !validSourceOperation(record.Before.Host.PartitionUUID) || !validSourceOperation(record.Before.Host.MachineID) || !slices.Contains([]string{"bios", "enabled", "disabled"}, record.Before.SecureBoot) || record.Planned == nil || (record.Phase == "applying" && len(record.Planned) == 0) {
		return errors.New("invalid prerequisite identity or plan")
	}
	for _, path := range []string{"/etc/fstab", "/boot/grub/grub.cfg", "/boot/vmlinuz-" + record.Before.Host.Kernel, "/boot/initrd.img-" + record.Before.Host.Kernel} {
		digest := record.Before.BootArtifacts[path]
		if !pinDigestPattern.MatchString(digest) {
			return errors.New("invalid prerequisite boot pin")
		}
	}
	for name, version := range record.Planned {
		if !nativeAllowedAddition(name) || !nativePackageVersion.MatchString(version) {
			return errors.New("invalid prerequisite package plan")
		}
	}
	if (record.Phase == "complete") != (record.After != nil) {
		return errors.New("incomplete prerequisite result")
	}
	if record.After != nil && (!sameNativeHost(record.Before, *record.After) || !pinDigestPattern.MatchString(record.After.PackagesSHA256)) {
		return errors.New("prerequisite installation changed boot identity")
	}
	return nil
}

func sameNativeHost(before, after NativeHostSnapshot) bool {
	before.PackagesSHA256 = ""
	after.PackagesSHA256 = ""
	return string(nativeBootJSON(before)) == string(nativeBootJSON(after))
}

func nativeAllowedAddition(name string) bool {
	return slices.Contains(nativeAdditionalPackages, name) || slices.Contains(nativeDependencyPackages, name)
}

func nativeAPTPlan(text string, missing []string) (map[string]string, error) {
	plan := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Remv ") {
			return nil, errors.New("prerequisites must not remove packages")
		}
		if strings.HasPrefix(line, "Inst ") {
			fields := strings.Fields(line)
			if len(fields) < 4 || !nativeAllowedAddition(fields[1]) || !strings.HasPrefix(fields[2], "(") || !nativePackageVersion.MatchString(strings.TrimPrefix(fields[2], "(")) {
				return nil, errors.New("prerequisites must not upgrade or install unapproved packages")
			}
			if _, exists := plan[fields[1]]; exists {
				return nil, errors.New("duplicate APT action")
			}
			plan[fields[1]] = strings.TrimPrefix(fields[2], "(")
		}
	}
	for _, name := range missing {
		if plan[name] == "" {
			return nil, errors.New("APT plan omitted a prerequisite")
		}
	}
	return plan, nil
}

// Version 2 is explicitly requested. Every actual unpack/configure action must
// be a new, exact planned package; simulation alone does not establish this.
func checkNativeAPTTransaction(data string, planned map[string]string) error {
	if len(data) > 256<<10 || !strings.HasPrefix(data, "VERSION 2\n") {
		return errors.New("unsupported APT hook protocol")
	}
	_, actions, ok := strings.Cut(data, "\n\n")
	if !ok {
		return errors.New("missing APT actions")
	}
	unpacked := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(actions), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || fields[1] != "-" || fields[2] != "<" || planned[fields[0]] == "" || fields[3] != planned[fields[0]] {
			return errors.New("APT transaction differs from new-package-only plan")
		}
		if fields[4] == "**CONFIGURE**" {
			continue
		}
		if !strings.HasPrefix(fields[4], "/var/cache/apt/archives/") || !strings.HasSuffix(fields[4], ".deb") || unpacked[fields[0]] {
			return errors.New("unexpected APT unpack action")
		}
		unpacked[fields[0]] = true
	}
	if len(unpacked) != len(planned) {
		return errors.New("APT transaction omitted planned packages")
	}
	return nil
}
