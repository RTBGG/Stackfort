// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/installpreflight"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

// InspectNativeHost never creates installer state, installs packages or changes
// services. Missing optional prerequisites are a plan, not a host rejection.
func InspectNativeHost(ctx context.Context) (NativeHostReport, error) {
	return inspectNativeHost(ctx, false)
}

func inspectNativeHost(ctx context.Context, staged bool) (report NativeHostReport, err error) {
	report = NativeHostReport{SchemaVersion: 1, ReadOnly: true, Profile: NativeHostProfile, MissingPackages: []string{}, Checks: []installpreflight.Check{}}
	if ctx == nil || ctx.Err() != nil {
		return report, errors.New("host inspection requires active context")
	}
	defer report.finish()
	platform := hostcapabilities.NewInspector().InspectPlatform()
	if os.Geteuid() != 0 || runtime.GOARCH != "amd64" || platform.DistributionID != "debian" || platform.VersionID != "13" {
		report.add("native.platform", errors.New("requires root on Debian 13 amd64"), "Qualified operating system", "Use the qualified Debian image; Ubuntu/Rocky boot profiles are not enabled yet.")
		return report, nil
	}
	packagesGuard, lockErr := acquireNativePackageGuard(ctx)
	report.add("native.package-locks", lockErr, "Package writers excluded during inspection", "Wait for the other package operation; never delete or replace lock files.")
	if lockErr != nil {
		return report, nil
	}
	defer packagesGuard.close()
	defer func() {
		if err := packagesGuard.check(ctx); err != nil {
			report.Snapshot = nil
			report.add("native.package-lock-integrity", err, "Package lock identity retained", "Preserve installer evidence and review changed lock files.")
		}
	}()
	report.add("native.environment", nativeHostEnvironment(ctx), "Full host environment", "Use a VM or physical server booted normally with systemd; containers, chroots, WSL and unknown firmware state are not qualified.")
	base, baseErr := installpreflight.Inspect(ctx)
	if baseErr != nil {
		report.add("native.capabilities", baseErr, "Host capabilities", "Restore read-only host inspection.")
	} else {
		for _, check := range base.Checks {
			// The existing public preflight correctly requires ready hosting storage.
			// This earlier phase independently checks the unconverted root below.
			if strings.HasPrefix(check.ID, "storage.") {
				continue
			}
			report.Checks = append(report.Checks, check)
		}
	}
	packages, packageErr := nativeHostPackages(ctx)
	report.add("native.package-state", packageErr, "Complete native package database", "Finish pending package operations; no automatic package repair is performed.")
	if packageErr == nil {
		report.add("native.firewall", nativeHostFirewall(ctx, packages), "No conflicting host firewall", "Use a fresh host without managed firewall rules; existing policies are never flushed or disabled. Provider-side firewalls may remain enabled.")
		var missingBase, conflicts []string
		for _, name := range nativeBasePackages {
			if nativeInstalled(packages, name) == "" {
				missingBase = append(missingBase, name)
			}
		}
		for name := range packages {
			if nativeConflictingPackage(name) {
				conflicts = append(conflicts, name)
			}
		}
		if len(missingBase) > 0 {
			report.add("native.boot-packages", fmt.Errorf("missing base boot packages: %s", strings.Join(missingBase, ", ")), "Existing Debian boot toolchain", "Use a normally installed GRUB/initramfs-tools Debian image; the installer will not replace an unknown bootloader.")
		}
		if len(conflicts) > 0 {
			slices.Sort(conflicts)
			report.add("native.existing-packages", fmt.Errorf("existing hosting packages: %s", strings.Join(conflicts, ", ")), "No existing hosting stack", "Migrate existing workloads separately and use a fresh server; existing packages will not be removed.")
		}
		for _, name := range nativeAdditionalPackages {
			if nativeInstalled(packages, name) == "" {
				report.MissingPackages = append(report.MissingPackages, name)
			}
		}
	}
	for _, path := range []string{"/run/reboot-required", "/run/reboot-required.pkgs", "/var/run/quota.upgrade", "/usr/sbin/policy-rc.d"} {
		report.add("native.pending."+filepath.Base(path), nativeBootAbsent(path), "No pending reboot or foreign service policy", "Finish the pending operation or review the existing policy; it will not be replaced.")
	}
	report.add("native.cloud-init", nativeHostCloudInit(ctx), "Cloud initialization complete", "Wait until cloud-init has finished successfully before preparing the server.")
	for _, path := range []string{"/etc/nginx", "/etc/apache2", "/etc/mysql", "/etc/php", "/var/lib/mysql", "/var/lib/postgresql", "/var/www", "/usr/local/cpanel", "/usr/local/psa", "/etc/cloudpanel", "/data/coolify", "/etc/stackfort", "/var/lib/stackfort", "/var/lib/stackfort-agent", "/usr/local/bin/stackfort-api", "/usr/local/sbin/stackfort-agent", "/srv/hosting", "/srv/stackfort-native-hosting", "/var/lib/docker"} {
		if err := nativeBootAbsent(path); err != nil {
			report.add("native.existing-data."+strings.ReplaceAll(strings.TrimPrefix(path, "/"), "/", "."), err, "No foreign hosting data", "Use a fresh server; no existing data will be overwritten.")
		}
	}
	if !staged {
		report.add("native.existing-installer", nativeBootAbsent(DefaultJournalDirectory), "No previous installer state", "Inspect native status and preserve journals; there is no automatic reset.")
	}
	for _, path := range []string{"/var/lib/containers/storage", "/run/containers/storage", "/root/.local/share/containers/storage"} {
		report.add("native.container-data."+strings.ReplaceAll(path, "/", "."), nativeHostEmptyTree(path, 3), "No existing container storage", "Use a server without container workloads; unused installed Podman packages are allowed.")
	}
	rootless, globErr := filepath.Glob("/home/*/.local/share/containers/storage")
	if globErr != nil {
		return report, globErr
	}
	for _, path := range rootless {
		report.add("native.rootless-data."+path, nativeHostEmptyTree(path, 3), "No rootless container workloads", "Migrate existing rootless workloads separately.")
	}
	passwd, readErr := nativeHostKernelFile("/etc/passwd")
	if readErr == nil {
		for _, line := range strings.Split(string(passwd), "\n") {
			name, _, _ := strings.Cut(line, ":")
			if slices.Contains([]string{"stackfort", "stackfort-phpmyadmin"}, name) {
				readErr = errors.New("Stackfort service identity already exists")
			}
		}
	}
	report.add("native.service-identities", readErr, "No existing Stackfort identities", "Use a fresh host; existing users are not adopted.")
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6", "/proc/net/udp", "/proc/net/udp6"} {
		data, err := nativeHostKernelFile(path)
		if err == nil {
			err = nativeSocketConflicts(string(data), strings.Contains(path, "tcp"))
		}
		report.add("native.listeners."+filepath.Base(path), err, "Reserved TCP/UDP listeners free", "Stop only your reviewed conflicting workload or use a fresh server.")
	}
	if packageErr != nil {
		return report, nil
	}
	snapshot, layoutErr := nativeHostLayout(ctx, packages)
	report.add("native.boot-layout", layoutErr, "Plain ext4 root and GRUB one-shot support", "Use the qualified plain-GPT/ext4 layout; LVM, RAID, separate boot/state paths and unknown boot configurations are not converted.")
	if layoutErr == nil {
		report.Snapshot = &snapshot
	}
	return report, nil
}

func nativeHostEnvironment(ctx context.Context) error {
	text, err := nativeHostKernelFile("/proc/1/comm")
	if err != nil || strings.TrimSpace(string(text)) != "systemd" {
		return errors.New("systemd must be PID 1")
	}
	for _, pair := range [][2]string{{"/", "/proc/1/root"}, {"/proc/self/ns/mnt", "/proc/1/ns/mnt"}, {"/proc/self/ns/user", "/proc/1/ns/user"}} {
		left, err := os.Stat(pair[0])
		if err != nil {
			return err
		}
		right, err := os.Stat(pair[1])
		if err != nil || !os.SameFile(left, right) {
			return errors.New("chroot or isolated mount/user namespace")
		}
	}
	value, code, err := nativeHostCommand(ctx, "/usr/bin/systemd-detect-virt", "--container")
	if err != nil || code != 1 || strings.TrimSpace(value) != "none" {
		return errors.New("container/WSL or unknown virtualization environment")
	}
	value, code, err = nativeHostCommand(ctx, "/usr/bin/systemd-detect-virt", "--chroot")
	if err != nil || code != 1 || strings.TrimSpace(value) != "" {
		return errors.New("chroot detection failed")
	}
	kernel, err := nativeHostKernelFile("/proc/sys/kernel/osrelease")
	if err != nil || strings.Contains(strings.ToLower(string(kernel)), "microsoft") {
		return errors.New("WSL kernel is not a full boot target")
	}
	_, err = nativeHostSecureBoot()
	return err
}

func nativeHostSecureBoot() (string, error) {
	if _, err := os.Stat("/sys/firmware/efi"); errors.Is(err, os.ErrNotExist) {
		return "bios", nil
	} else if err != nil {
		return "", err
	}
	secure, err := filepath.Glob("/sys/firmware/efi/efivars/SecureBoot-*")
	if err != nil {
		return "", err
	}
	if len(secure) != 1 {
		return "", errors.New("UEFI Secure Boot state unavailable")
	}
	for _, path := range secure {
		data, err := nativeHostKernelFile(path)
		if err != nil || len(data) != 5 || data[4] > 1 {
			return "", errors.New("Secure Boot state is unknown")
		}
		if data[4] == 1 {
			return "enabled", nil
		}
	}
	return "disabled", nil
}

func nativeHostKernelFile(path string) ([]byte, error) {
	if !slices.Contains([]string{
		"/etc/passwd", "/proc/1/comm", "/proc/sys/kernel/osrelease",
		"/proc/sys/kernel/random/boot_id", "/sys/class/dmi/id/product_uuid",
		"/proc/net/tcp", "/proc/net/tcp6", "/proc/net/udp", "/proc/net/udp6",
		"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names",
		"/sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c",
	}, path) {
		return nil, errors.New("unsupported native host inspection file")
	}
	// #nosec G304 -- exact immutable path allowlist above; bounded read below.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, errors.New("host inspection output exceeds limit")
	}
	return data, nil
}

func nativeHostEmptyTree(path string, depth int) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || depth < 0 {
		return errors.New("existing container state or uninspectable path")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 16 {
		return errors.New("existing container workload tree")
	}
	for _, entry := range entries {
		if err := nativeHostEmptyTree(filepath.Join(path, entry.Name()), depth-1); err != nil {
			return err
		}
	}
	return nil
}

func nativeHostCloudInit(ctx context.Context) error {
	text, code, err := nativeHostCommand(ctx, "/usr/bin/systemctl", "show", "--property=LoadState", "--property=ActiveState", "--property=SubState", "--property=Result", "cloud-final.service")
	if err != nil || code != 0 {
		return errors.New("cannot inspect cloud-init")
	}
	if strings.Contains(text, "LoadState=not-found\n") {
		return nil
	}
	if !strings.Contains(text, "ActiveState=active\n") || !strings.Contains(text, "SubState=exited\n") || !strings.Contains(text, "Result=success\n") {
		return errors.New("cloud-init is incomplete or failed")
	}
	return nil
}

func nativeHostPackages(ctx context.Context) (map[string]string, error) {
	text, code, err := nativeHostCommand(ctx, "/usr/bin/dpkg-query", "-W", "-f=${binary:Package}\t${db:Status-Abbrev}\t${Version}\n")
	if err != nil || code != 0 {
		return nil, errors.Join(err, errors.New("package inventory unavailable"))
	}
	packages, err := nativePackageInventory(text)
	if err != nil {
		return nil, err
	}
	text, code, err = nativeHostCommand(ctx, "/usr/bin/dpkg", "--audit")
	if err != nil || code != 0 || strings.TrimSpace(text) != "" {
		return nil, errors.New("dpkg audit requires operator attention")
	}
	return packages, nil
}

func nativeHostLayout(ctx context.Context, packages map[string]string) (snapshot NativeHostSnapshot, err error) {
	observed, err := (nativeReadyBackend{}).Observe(ctx)
	if err != nil {
		return snapshot, err
	}
	device, block, err := nativeRootDevice(ctx)
	if err != nil {
		return snapshot, err
	}
	for key, expected := range map[string]string{"TYPE": "ext4", "PART_ENTRY_SCHEME": "gpt"} {
		value, err := nativeReadCommand(ctx, "/usr/sbin/blkid", "-p", "-s", key, "-o", "value", device)
		if err != nil || value != expected {
			return snapshot, errors.New("requires plain GPT/ext4 root")
		}
	}
	for _, path := range []string{"/boot", "/boot/grub", "/etc", "/srv", "/var", "/var/lib"} {
		fd, err := openSourceDirectory(path, false)
		if err != nil {
			return snapshot, err
		}
		var stat unix.Stat_t
		err = unix.Fstat(fd, &stat)
		_ = unix.Close(fd)
		if err != nil || stat.Dev != block.Rdev {
			return snapshot, errors.New("boot, state and hosting paths must share the root filesystem: " + path)
		}
	}
	resolved, err := filepath.EvalSymlinks("/sys/class/block/" + filepath.Base(device))
	if err != nil {
		return snapshot, err
	}
	for _, path := range []string{resolved + "/holders", filepath.Dir(resolved) + "/holders", filepath.Dir(resolved) + "/slaves"} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) > 0 {
			return snapshot, errors.New("stacked/held root block device is not supported")
		}
	}
	values, err := nativeBootSuper(ctx, device)
	if err != nil {
		return snapshot, err
	}
	allowed := strings.Fields("has_journal ext_attr resize_inode dir_index filetype extent 64bit flex_bg sparse_super large_file huge_file dir_nlink extra_isize metadata_csum metadata_csum_seed needs_recovery orphan_file orphan_present")
	for _, feature := range strings.Fields(values["Filesystem features"]) {
		if !slices.Contains(allowed, feature) {
			return snapshot, errors.New("unsupported or already-converted ext4 feature: " + feature)
		}
	}
	if values["Inode size"] != "256" {
		return snapshot, errors.New("unsupported ext4 inode size")
	}
	var capacity unix.Statfs_t
	if err := unix.Statfs("/", &capacity); err != nil {
		return snapshot, err
	}
	if err := nativeCheckInitialCapacity(capacity.Bsize, capacity.Bavail, capacity.Ffree); err != nil {
		return snapshot, err
	}
	options, err := nativeReadCommand(ctx, "/usr/bin/findmnt", "-nro", "FSROOT,OPTIONS", "--mountpoint", "/")
	if err != nil {
		return snapshot, err
	}
	fields := strings.Fields(options)
	if len(fields) != 2 || fields[0] != "/" || !slices.Contains(strings.Split(fields[1], ","), "rw") {
		return snapshot, errors.New("root must be writable and not a subtree bind")
	}
	fstab, err := nativeBootRead("/etc/fstab")
	if err != nil {
		return snapshot, err
	}
	if _, err := nativeBootFstab(string(fstab), observed.RootUUID, observed.PartitionUUID); err != nil {
		return snapshot, err
	}
	for _, line := range strings.Split(string(fstab), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 4 || (fields[1] != "/" && fields[1] != "/boot/efi" && fields[2] != "tmpfs" && fields[2] != "swap") {
			return snapshot, errors.New("additional persistent filesystems require separate qualification")
		}
		if strings.Contains(fields[3], "quota") {
			return snapshot, errors.New("existing quota mount policy")
		}
	}
	// A synthetic valid plan is used only for read-only GRUB syntax/layout checks;
	// no operation, journal, artifact or boot entry is created here.
	plan := storageprep.Plan{OperationID: observed.BootID, Version: "0.1.0-beta.3", SourceDigest: strings.Repeat("a", 64), Distribution: "debian", MachineID: observed.MachineID, RootUUID: observed.RootUUID, PartitionUUID: observed.PartitionUUID, PreviousBootID: observed.BootID, Kernel: observed.Kernel, ManifestDigest: strings.Repeat("a", 64)}
	if err := plan.Validate(); err != nil {
		return snapshot, err
	}
	if err := nativeBootCheckGRUB(ctx, plan); err != nil {
		return snapshot, err
	}
	paths := []string{"/boot/grub/custom.cfg", nativeBootHook, nativeBootPremount}
	units, err := NativeBootUnits(plan.OperationID)
	if err != nil {
		return snapshot, err
	}
	for name := range units {
		paths = append(paths, "/etc/systemd/system/"+name, "/etc/systemd/system/"+name+".d", "/etc/systemd/system/"+name+".requires", "/etc/systemd/system/"+name+".wants")
	}
	paths = append(paths, "/etc/systemd/system/nginx.service.requires")
	for _, name := range nativeBootConsumers() {
		paths = append(paths, "/etc/systemd/system/"+name+".d/90-stackfort-native-storage.conf")
	}
	for _, path := range paths {
		if err := nativeBootAbsent(path); err != nil {
			return snapshot, err
		}
	}
	snapshot = NativeHostSnapshot{Host: observed, Features: values["Filesystem features"], Blocks: values["Block count"], BlockSize: values["Block size"], InodeSize: values["Inode size"], BootArtifacts: map[string]string{}, PackagesSHA256: admissionDigest(nativeBootJSON(packages))}
	snapshot.SecureBoot, err = nativeHostSecureBoot()
	if err != nil {
		return snapshot, err
	}
	for _, path := range []string{"/etc/fstab", "/boot/vmlinuz-" + observed.Kernel, "/boot/initrd.img-" + observed.Kernel, "/boot/grub/grub.cfg"} {
		snapshot.BootArtifacts[path], err = nativeTrustedHash(ctx, path)
		if err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
}

type nativeHostOutput struct {
	data     []byte
	overflow bool
}

func (output *nativeHostOutput) Write(data []byte) (int, error) {
	if len(output.data)+len(data) > 256<<10 {
		output.overflow = true
		return 0, errors.New("host command output exceeds limit")
	}
	output.data = append(output.data, data...)
	return len(data), nil
}

func nativeHostCommand(ctx context.Context, executable string, args ...string) (string, int, error) {
	allowed := slices.Contains([]string{"/usr/bin/dpkg-query", "/usr/bin/dpkg", "/usr/bin/systemctl", "/usr/bin/systemd-detect-virt"}, executable)
	if executable == "/usr/sbin/nft" {
		allowed = slices.Equal(args, []string{"list", "tables"})
	}
	if ctx == nil || !allowed {
		return "", -1, errors.New("invalid host inspection command")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- private fixed executable and argument call sites.
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var out, stderr nativeHostOutput
	command.Stdout, command.Stderr = &out, &stderr
	err := command.Run()
	code := -1
	if command.ProcessState != nil {
		code = command.ProcessState.ExitCode()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = nil
	}
	if out.overflow || stderr.overflow {
		return "", code, errors.New("host command output exceeded limit")
	}
	return string(out.data), code, errors.Join(err, ctx.Err())
}
