// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

// Disposable Debian initramfs experiment. Never invoked by the installer.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/quotastate"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	nativeLab        = "/var/lib/stackfort-native-quota-prototype"
	nativeHost       = "stackfort-native-quota-debian-13"
	nativeResume     = "stackfort-native-quota-resume.service"
	nativeConsumer   = "stackfort-native-quota-consumer.service"
	nativeBootResult = "/run/initramfs/stackfort-native-quota.json"
	nativeHook       = "/etc/initramfs-tools/hooks/stackfort-native-quota"
	nativePremount   = "/etc/initramfs-tools/scripts/local-premount/stackfort-native-quota"
)

type nativeIntent struct {
	Operation, UUID, PartUUID, Kernel, BootBefore, VMUUID string
	Features, Blocks, BlockSize, InodeSize                string
	FstabBefore, FstabAfter, SentinelHash                 string
}

func requireNativeLab(t *testing.T) {
	t.Helper()
	if os.Getenv(disposableHostOptIn) != "1" || os.Getenv("STACKFORT_NATIVE_QUOTA_PROTOTYPE") != "1" {
		t.Skip("dedicated native-quota lab only")
	}
	host, err := os.Hostname()
	if err != nil || host != nativeHost || os.Geteuid() != 0 {
		t.Fatal("wrong native-quota lab host or UID")
	}
}

func nativeHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func nativeAtomic(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		t.Fatal("refusing non-regular destination")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".native-quota-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		t.Fatal(err)
	}
}

func nativeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	nativeAtomic(t, path, data, 0600)
}

func nativeState(t *testing.T, path string) nativeIntent {
	t.Helper()
	var state nativeIntent
	if err := json.Unmarshal(imageRead(t, path), &state); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{state.Operation, state.UUID, state.PartUUID, state.BootBefore, state.VMUUID} {
		if _, err := uuid.Parse(value); err != nil {
			t.Fatal("invalid native intent UUID")
		}
	}
	return state
}

func nativeSuperblock(t *testing.T, device string) map[string]string {
	t.Helper()
	values := make(map[string]string)
	for _, line := range strings.Split(imageCommand(t, "/usr/sbin/tune2fs", "-l", device), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func nativeDevice(t *testing.T, path string) (string, unix.Stat_t) {
	t.Helper()
	// initramfs-tools may export the original root= specifier even after its
	// shell-local ROOT was resolved. Resolve only validated UUID specifiers.
	for _, prefix := range []struct{ key, dir string }{{"UUID=", "by-uuid"}, {"PARTUUID=", "by-partuuid"}} {
		if value, ok := strings.CutPrefix(path, prefix.key); ok {
			if _, err := uuid.Parse(value); err != nil {
				t.Fatal("invalid boot root UUID")
			}
			path = "/dev/disk/" + prefix.dir + "/" + value
			break
		}
	}
	device, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(device, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		t.Fatal("not a block device")
	}
	// This experiment intentionally excludes device-mapper, RAID and whole disks.
	if _, err := os.Stat("/sys/class/block/" + filepath.Base(device) + "/partition"); err != nil {
		t.Fatal("not a plain partition")
	}
	return device, stat
}

func nativeIdentity(t *testing.T, device string, state nativeIntent) {
	t.Helper()
	if strings.ToLower(strings.TrimSpace(string(imageRead(t, "/sys/class/dmi/id/product_uuid")))) != state.VMUUID {
		t.Fatal("target VM identity mismatch")
	}
	for _, identity := range []struct{ key, value string }{{"UUID", state.UUID}, {"PART_ENTRY_UUID", state.PartUUID}, {"TYPE", "ext4"}} {
		if imageCommand(t, "/usr/sbin/blkid", "-p", "-s", identity.key, "-o", "value", device) != identity.value {
			t.Fatal("target identity mismatch: " + identity.key)
		}
	}
	super := nativeSuperblock(t, device)
	if super["Inode size"] != state.InodeSize || super["Block count"] != state.Blocks || super["Block size"] != state.BlockSize {
		t.Fatal("filesystem geometry changed")
	}
	if err := nativeFeatureDelta(state.Features, super["Filesystem features"]); err != nil {
		t.Fatal(err)
	}
}

func nativeFeatureDelta(before, after string) error {
	old, current := strings.Fields(before), strings.Fields(after)
	// These are mount-state flags cleared by clean unmount, not configuration.
	transient := func(feature string) bool { return feature == "needs_recovery" || feature == "orphan_present" }
	for _, feature := range current {
		if !slices.Contains(old, feature) && feature != "project" && feature != "quota" && !transient(feature) {
			return fmt.Errorf("unexpected filesystem feature: %s", feature)
		}
	}
	for _, feature := range old {
		if !transient(feature) && !slices.Contains(current, feature) {
			return fmt.Errorf("filesystem feature disappeared: %s", feature)
		}
	}
	return nil
}

func nativeReady(super map[string]string) bool {
	features := strings.Fields(super["Filesystem features"])
	inode, err := strconv.ParseUint(super["Project quota inode"], 10, 64)
	return slices.Contains(features, "project") && slices.Contains(features, "quota") && err == nil && inode > 0
}

func nativeQuotaState(t *testing.T) quotastate.Project {
	t.Helper()
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	state, err := quotastate.ReadProject(fd)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func nativeFstab(source, rootUUID, partUUID string) (string, error) {
	lines := strings.Split(source, "\n")
	count := 0
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") || len(fields) < 2 || fields[1] != "/" {
			continue
		}
		if len(fields) < 6 || fields[2] != "ext4" || (fields[0] != "UUID="+rootUUID && fields[0] != "PARTUUID="+partUUID) {
			return "", errors.New("unsupported root fstab entry")
		}
		count++
		options := strings.Split(fields[3], ",")
		for _, option := range options {
			if option == "noquota" || option == "noprjquota" || option == "ro" {
				return "", errors.New("conflicting root options")
			}
		}
		if !slices.Contains(options, "prjquota") {
			fields[3] += ",prjquota"
			lines[i] = strings.Join(fields, "\t")
		}
	}
	if count != 1 {
		return "", errors.New("expected exactly one root entry")
	}
	return strings.Join(lines, "\n"), nil
}

func TestNativeQuotaPolicy(t *testing.T) {
	if err := nativeFeatureDelta("has_journal orphan_file needs_recovery orphan_present", "has_journal orphan_file project quota"); err != nil {
		t.Fatal(err)
	}
	for _, after := range []string{"has_journal project quota", "orphan_file project quota", "has_journal orphan_file encrypt"} {
		if nativeFeatureDelta("has_journal orphan_file", after) == nil {
			t.Fatal("unsafe feature delta accepted")
		}
	}
	before := "# keep\nUUID=a / ext4 errors=remount-ro 0 1\nUUID=b /boot/efi vfat defaults 0 2\n"
	after, err := nativeFstab(before, "a", "p")
	if err != nil || !strings.Contains(after, "errors=remount-ro,prjquota") {
		t.Fatal(after, err)
	}
	again, err := nativeFstab(after, "a", "p")
	if err != nil || again != after {
		t.Fatal("fstab not idempotent")
	}
	for _, bad := range []string{"", before + "UUID=a / ext4 defaults 0 1\n", strings.Replace(before, "UUID=a", "UUID=foreign", 1), strings.Replace(before, "errors=remount-ro", "noquota", 1), strings.Replace(before, "ext4", "xfs", 1)} {
		if _, err := nativeFstab(bad, "a", "p"); err == nil {
			t.Fatal("unsafe fstab accepted")
		}
	}
	for _, super := range []map[string]string{{}, {"Filesystem features": "project quota"}, {"Filesystem features": "project quota", "Project quota inode": "0"}, {"Filesystem features": "quota", "Project quota inode": "12"}} {
		if nativeReady(super) {
			t.Fatal("incomplete quota accepted")
		}
	}
	if !nativeReady(map[string]string{"Filesystem features": "extent project quota", "Project quota inode": "12"}) {
		t.Fatal("ready quota rejected")
	}
}

func TestDisposableNativeQuotaPrepare(t *testing.T) {
	requireNativeLab(t)
	if !strings.Contains(string(imageRead(t, "/etc/os-release")), "VERSION_ID=\"13\"") {
		t.Fatal("Debian 13 only")
	}
	for _, path := range []string{nativeLab, nativeHook, nativePremount, "/srv/hosting", "/srv/stackfort-native-hosting", "/etc/systemd/system/" + nativeResume, "/etc/systemd/system/" + nativeConsumer, "/etc/systemd/system/srv-hosting.mount"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refusing existing path %s", path)
		}
	}
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	super := nativeSuperblock(t, device)
	if super["Inode size"] != "256" || strings.Contains(super["Filesystem features"], "project") || strings.Contains(super["Filesystem features"], "quota") {
		t.Fatal("requires untouched 256-byte-inode ext4 without quotas")
	}
	var capacity unix.Statfs_t
	if err := unix.Statfs("/", &capacity); err != nil || capacity.Bavail*uint64(capacity.Bsize) < 8<<30 {
		t.Fatal("insufficient OS space reserve")
	}
	state := nativeIntent{Operation: uuid.NewString(), UUID: super["Filesystem UUID"], PartUUID: imageCommand(t, "/usr/sbin/blkid", "-s", "PARTUUID", "-o", "value", device), Kernel: imageCommand(t, "/usr/bin/uname", "-r"), BootBefore: strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/random/boot_id"))), Features: super["Filesystem features"], Blocks: super["Block count"], BlockSize: super["Block size"], InodeSize: super["Inode size"], FstabBefore: string(imageRead(t, "/etc/fstab"))}
	state.VMUUID = strings.ToLower(strings.TrimSpace(string(imageRead(t, "/sys/class/dmi/id/product_uuid"))))
	var err error
	state.FstabAfter, err = nativeFstab(state.FstabBefore, state.UUID, state.PartUUID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(imageRead(t, "/boot/grub/grub.cfg")), "initrd.img-"+state.Kernel) {
		t.Fatal("no matching GRUB/initramfs entry")
	}
	for _, path := range []string{nativeLab, "/srv/stackfort-native-hosting", "/srv/hosting"} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/srv/stackfort-native-hosting", "/srv/hosting"} {
		if err := os.Chmod(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir("/var/lib/stackfort-agent", 0755); err != nil {
		t.Fatal(err)
	}
	imageWrite(t, nativeLab+"/sentinel", []byte("native-root-sentinel:"+state.Operation), 0600)
	state.SentinelHash = nativeHash(imageRead(t, nativeLab+"/sentinel"))
	nativeJSON(t, nativeLab+"/intent.json", state)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, nativeLab+"/probe.test", imageRead(t, self), 0700)
	imageWrite(t, nativeLab+"/initrd.before", imageRead(t, "/boot/initrd.img-"+state.Kernel), 0600)
	imageWrite(t, nativeLab+"/root.before.txt", []byte(imageCommand(t, "/usr/sbin/tune2fs", "-l", device)), 0600)
	hook := "#!/bin/sh\nset -eu\ncase ${1:-} in prereqs) exit 0;; esac\n. /usr/share/initramfs-tools/hook-functions\ncopy_exec /usr/sbin/tune2fs\ncopy_exec /usr/sbin/e2fsck\ncopy_exec /usr/sbin/blkid\ncopy_file executable " + nativeLab + "/probe.test /stackfort-native-quota.test\ncopy_file config " + nativeLab + "/boot.json /stackfort-native-quota.json\n"
	premount := "#!/bin/sh\ncase ${1:-} in prereqs) exit 0;; esac\n. /scripts/functions\nmkdir -p /run/initramfs\nSTACKFORT_NATIVE_QUOTA_EARLY=1 /stackfort-native-quota.test -test.v -test.timeout=15m -test.run='^TestDisposableNativeQuotaEarly$' > /run/initramfs/stackfort-native-quota.log 2>&1\nresult=$?\nif [ $result -ne 0 ] && [ -e /run/initramfs/stackfort-native-quota-mutating ]; then\n  while :; do panic 'Stackfort lab quota conversion incomplete: filesystem recovery required'; done\nfi\n# Rejected preconditions do not prevent the normal OS boot; resume stays blocked.\nexit 0\n"
	imageWrite(t, nativeHook, []byte(hook), 0755)
	imageWrite(t, nativePremount, []byte(premount), 0755)
	resume := "[Unit]\nDescription=Stackfort native quota lab resume\nAfter=local-fs.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1\nExecStart=" + nativeLab + "/probe.test -test.v -test.run=^TestDisposableNativeQuotaResume$\nTimeoutStartSec=90\n"
	mount := "[Unit]\nDescription=Stackfort native quota lab bind\nDefaultDependencies=no\nRequires=" + nativeResume + "\nAfter=local-fs.target " + nativeResume + "\nBefore=umount.target\nConflicts=umount.target\n\n[Mount]\nWhat=/srv/stackfort-native-hosting\nWhere=/srv/hosting\nType=none\nOptions=bind\n"
	consumer := "[Unit]\nDescription=Stackfort native quota lab dependent consumer\nBindsTo=srv-hosting.mount\nAfter=srv-hosting.mount\n\n[Service]\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1\nExecStartPre=" + nativeLab + "/probe.test -test.run=^TestDisposableNativeQuotaGuard$\nExecStart=/usr/bin/sleep infinity\n\n[Install]\nWantedBy=multi-user.target\n"
	for name, value := range map[string]string{nativeResume: resume, "srv-hosting.mount": mount, nativeConsumer: consumer} {
		imageWrite(t, "/etc/systemd/system/"+name, []byte(value), 0644)
	}
	imageCommand(t, "/usr/bin/systemctl", "daemon-reload")
	imageCommand(t, "/usr/bin/systemctl", "enable", nativeConsumer)
	if err := exec.Command("/usr/bin/systemctl", "start", nativeConsumer).Run(); err == nil {
		t.Fatal("consumer started before conversion")
	}
	t.Log("NATIVE_QUOTA prepared; consumer fail-closed before conversion; root unchanged")
}

func TestDisposableNativeQuotaArm(t *testing.T) {
	requireNativeLab(t)
	state := nativeState(t, nativeLab+"/intent.json")
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, state)
	if os.Getenv("STACKFORT_NATIVE_QUOTA_REJECT") == "1" {
		state.UUID = uuid.NewString()
	}
	nativeJSON(t, nativeLab+"/boot.json", state)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	nativeAtomic(t, nativeLab+"/probe.test", imageRead(t, self), 0700)
	imageCommand(t, "/usr/sbin/update-initramfs", "-u", "-k", state.Kernel)
	listing := imageCommand(t, "/usr/bin/lsinitramfs", "/boot/initrd.img-"+state.Kernel)
	for _, required := range []string{"stackfort-native-quota.test", "stackfort-native-quota.json", "scripts/local-premount/stackfort-native-quota", "usr/sbin/tune2fs", "usr/sbin/e2fsck", "usr/sbin/blkid"} {
		if !strings.Contains(listing, required) {
			t.Fatal("initramfs missing " + required)
		}
	}
	t.Logf("NATIVE_QUOTA armed UUID=%s initrdSHA256=%s", state.UUID, nativeHash(imageRead(t, "/boot/initrd.img-"+state.Kernel)))
}

func TestDisposableNativeQuotaEarly(t *testing.T) {
	if os.Getenv("STACKFORT_NATIVE_QUOTA_EARLY") != "1" {
		t.Skip("embedded initramfs only")
	}
	// Never infer that read-only is unmounted: reject every mount of this device.
	state := nativeState(t, "/stackfort-native-quota.json")
	device, stat := nativeDevice(t, os.Getenv("ROOT"))
	majorMinor := fmt.Sprintf("%d:%d", unix.Major(uint64(stat.Rdev)), unix.Minor(uint64(stat.Rdev)))
	for _, line := range strings.Split(string(imageRead(t, "/proc/self/mountinfo")), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[2] == majorMinor {
			t.Fatal("refusing mounted target, including read-only")
		}
	}
	var root unix.Statfs_t
	if err := unix.Statfs("/", &root); err != nil || (root.Type != unix.TMPFS_MAGIC && root.Type != unix.RAMFS_MAGIC) {
		t.Fatal("not an initramfs root")
	}
	nativeIdentity(t, device, state)
	super := nativeSuperblock(t, device)
	status := "already-ready"
	if !nativeReady(super) {
		// e2fsck -p can already repair metadata: latch before this FIRST write,
		// not just before tune2fs. The marker is RAM-only, not crash recovery.
		imageWrite(t, "/run/initramfs/stackfort-native-quota-mutating", []byte(state.Operation), 0600)
		// Full offline check; never use -y, force tune2fs, or clear safety features.
		cmd := exec.Command("/usr/sbin/e2fsck", "-f", "-p", device)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		output, err := cmd.CombinedOutput()
		t.Log(string(output))
		code := cmd.ProcessState.ExitCode()
		if err != nil && code != 1 {
			t.Fatalf("pre-conversion fsck rejected: %d %v", code, err)
		}
		t.Log("NATIVE_QUOTA target-unmounted=verified metadata-change=starting")
		imageCommand(t, "/usr/sbin/tune2fs", "-O", "project,quota", "-Q", "prjquota", device)
		cmd = exec.Command("/usr/sbin/e2fsck", "-f", "-p", device)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		output, err = cmd.CombinedOutput()
		t.Log(string(output))
		code = cmd.ProcessState.ExitCode()
		if err != nil && code != 1 {
			t.Fatalf("post-conversion fsck rejected: %d %v", code, err)
		}
		nativeIdentity(t, device, state)
		if !nativeReady(nativeSuperblock(t, device)) {
			t.Fatal("quota features/inode missing after conversion")
		}
		if err := os.Remove("/run/initramfs/stackfort-native-quota-mutating"); err != nil {
			t.Fatal(err)
		}
		status = "converted"
	}
	nativeJSON(t, nativeBootResult, map[string]string{"operation": state.Operation, "uuid": state.UUID, "bootID": strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/random/boot_id"))), "status": status, "device": device})
	t.Log("NATIVE_QUOTA early=" + status)
}

func TestDisposableNativeQuotaResume(t *testing.T) {
	requireNativeLab(t)
	state := nativeState(t, nativeLab+"/intent.json")
	var result map[string]string
	if err := json.Unmarshal(imageRead(t, nativeBootResult), &result); err != nil {
		t.Fatal(err)
	}
	boot := strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/random/boot_id")))
	if result["operation"] != state.Operation || result["uuid"] != state.UUID || result["bootID"] != boot || boot == state.BootBefore || (result["status"] != "converted" && result["status"] != "already-ready") {
		t.Fatal("no matching successful conversion in current boot")
	}
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, state)
	if !nativeReady(nativeSuperblock(t, device)) {
		t.Fatal("missing native quota features")
	}
	if nativeHash(imageRead(t, nativeLab+"/sentinel")) != state.SentinelHash {
		t.Fatal("root sentinel changed")
	}
	current := string(imageRead(t, "/etc/fstab"))
	if current != state.FstabBefore && current != state.FstabAfter {
		t.Fatal("fstab changed outside this operation")
	}
	imageCommand(t, "/usr/bin/mount", "-o", "remount,prjquota", "/")
	imageCommand(t, "/usr/bin/findmnt", "-rn", "-M", "/", "-t", "ext4", "-O", "prjquota")
	if !nativeQuotaState(t).Ready() {
		imageCommand(t, "/usr/sbin/quotaon", "-P", "/")
	}
	if !nativeQuotaState(t).Ready() {
		t.Fatal("native quota accounting or enforcement still disabled")
	}
	// Persist the option only after a successful live remount; retry is idempotent.
	if current != state.FstabAfter {
		nativeAtomic(t, "/etc/fstab", []byte(state.FstabAfter), 0644)
	}
	nativeJSON(t, nativeLab+"/resume-"+boot+".json", result)
	t.Log("NATIVE_QUOTA resumed; prjquota mounted and fstab persisted")
}

func TestDisposableNativeQuotaGuard(t *testing.T) {
	requireNativeLab(t)
	state := nativeState(t, nativeLab+"/intent.json")
	device, stat := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, state)
	var hosting unix.Stat_t
	if err := unix.Stat("/srv/hosting", &hosting); err != nil || uint64(hosting.Dev) != uint64(stat.Rdev) {
		t.Fatal("hosting is not on native root device")
	}
	imageCommand(t, "/usr/bin/findmnt", "-rn", "-M", "/srv/hosting", "-t", "ext4", "-O", "prjquota")
	if !nativeReady(nativeSuperblock(t, device)) {
		t.Fatal("quota capability missing")
	}
	if !nativeQuotaState(t).Ready() {
		t.Fatal("quota accounting/enforcement missing")
	}
}

func TestDisposableNativeQuotaRejected(t *testing.T) {
	requireNativeLab(t)
	state := nativeState(t, nativeLab+"/intent.json")
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, state)
	if nativeSuperblock(t, device)["Filesystem features"] != state.Features || string(imageRead(t, "/etc/fstab")) != state.FstabBefore {
		t.Fatal("rejected operation mutated root")
	}
	if !strings.Contains(string(imageRead(t, "/run/initramfs/stackfort-native-quota.log")), "target identity mismatch") {
		t.Fatal("wrong rejection cause")
	}
	if err := exec.Command("/usr/bin/systemctl", "is-active", "--quiet", nativeConsumer).Run(); err == nil {
		t.Fatal("consumer active after rejected operation")
	}
	if err := exec.Command("/usr/bin/findmnt", "-M", "/srv/hosting").Run(); err == nil {
		t.Fatal("hosting mount active after rejected operation")
	}
	t.Log("NATIVE_QUOTA wrong-UUID booted OS; root/fstab unchanged; hosting consumer blocked")
}

func TestDisposableNativeQuotaValidate(t *testing.T) {
	requireNativeLab(t)
	TestDisposableNativeQuotaGuard(t)
	var bootResult map[string]string
	if err := json.Unmarshal(imageRead(t, nativeBootResult), &bootResult); err != nil {
		t.Fatal(err)
	}
	if expected := os.Getenv("STACKFORT_NATIVE_QUOTA_EXPECT_BOOT"); expected != "" && bootResult["status"] != expected {
		t.Fatalf("boot status=%s, expected %s", bootResult["status"], expected)
	}
	imageCommand(t, "/usr/bin/systemctl", "is-active", nativeConsumer, nativeResume)
	state := nativeState(t, nativeLab+"/intent.json")
	if string(imageRead(t, "/etc/fstab")) != state.FstabAfter || nativeHash(imageRead(t, nativeLab+"/sentinel")) != state.SentinelHash {
		t.Fatal("persisted state mismatch")
	}
	for _, test := range []struct {
		name string
		fn   func(*testing.T)
	}{{"QuotasAndIsolation", TestDisposableHostProjectQuotaAndAccountIsolation}, {"OCIPrivateResources", TestDisposableHostOCIPrivateResources}, {"OCILifecycle", TestDisposableHostOCIDeploymentLifecycle}, {"ContainerSubUIDQuota", testContainerProjectQuota}} {
		t.Run(test.name, test.fn)
	}
	t.Log("NATIVE_QUOTA native hosting and existing production reconciler checks completed")
}

func TestDisposableNativeQuotaResumeSafety(t *testing.T) {
	requireNativeLab(t)
	TestDisposableNativeQuotaGuard(t)
	imageCommand(t, "/usr/bin/systemctl", "stop", "srv-hosting.mount", nativeResume)
	if err := exec.Command("/usr/bin/systemctl", "is-active", "--quiet", nativeConsumer).Run(); err == nil {
		t.Fatal("consumer survived mount stop")
	}
	// Quota accounting and mount flags alone must not authorize tenant limits.
	imageCommand(t, "/usr/sbin/quotaoff", "-P", "/")
	quota := nativeQuotaState(t)
	if !quota.Accounting || quota.Enforcement {
		t.Fatal("accounting-only case not established")
	}
	identity := disposableIdentity(t, availableManagedID(t, 249_960))
	_, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{Identity: identity, ProjectID: identity.UID, ByteLimit: 2 << 20})
	if err == nil {
		t.Fatal("accounting-only root accepted quota mutation")
	}
	if _, err := os.Lstat(identity.HomeDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("accounting-only failure created account data")
	}
	if err := os.Rename(nativeBootResult, nativeBootResult+".unavailable"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := os.Stat(nativeBootResult + ".unavailable"); err == nil {
			if err := os.Rename(nativeBootResult+".unavailable", nativeBootResult); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := exec.Command("/usr/bin/systemctl", "start", nativeConsumer).Run(); err == nil {
		t.Fatal("consumer started without current boot evidence")
	}
	if err := os.Rename(nativeBootResult+".unavailable", nativeBootResult); err != nil {
		t.Fatal(err)
	}
	imageCommand(t, "/usr/bin/systemctl", "reset-failed", nativeResume, nativeConsumer, "srv-hosting.mount")
	imageCommand(t, "/usr/bin/systemctl", "start", nativeConsumer)
	TestDisposableNativeQuotaGuard(t)
	if string(imageRead(t, "/etc/fstab")) != nativeState(t, nativeLab+"/intent.json").FstabAfter {
		t.Fatal("resume duplicated or changed fstab")
	}
	t.Log("NATIVE_QUOTA mount-loss/current-boot-evidence/idempotent-resume=passed")
}

func TestDisposableNativeQuotaBenchmark(t *testing.T) {
	requireNativeLab(t)
	phase := os.Getenv("STACKFORT_NATIVE_QUOTA_BENCHMARK")
	state := nativeState(t, nativeLab+"/intent.json")
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, state)
	dir := nativeLab + "/fio-before"
	switch phase {
	case "before":
		if nativeSuperblock(t, device)["Filesystem features"] != state.Features {
			t.Fatal("baseline root has changed")
		}
	case "after":
		TestDisposableNativeQuotaGuard(t)
		dir = "/srv/hosting/fio-after"
	default:
		t.Fatal("explicit before or after benchmark required")
	}
	results := nativeLab + "/benchmark-" + phase
	if err := os.Mkdir(results, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := dir + "/fio.bin"
	t.Cleanup(func() {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if err := os.Remove(dir); err != nil {
			t.Error(err)
		}
	})
	if phase == "after" {
		imageCommand(t, "/usr/bin/chattr", "-p", "249570", "+P", dir)
		imageCommand(t, "/usr/sbin/setquota", "-P", "249570", "1048576", "1048576", "0", "0", "/")
	}
	previousEvidence := imageLabEvidenceDir
	imageLabEvidenceDir = results
	t.Cleanup(func() { imageLabEvidenceDir = previousEvidence })
	imageFIO(t, "prepare", file, "--rw=write", "--bs=1m", "--end_fsync=1")
	nativeJSON(t, results+"/facts.json", map[string]any{"phase": phase, "kernel": state.Kernel, "superblock": nativeSuperblock(t, device), "rootMount": imageCommand(t, "/usr/bin/findmnt", "-rn", "-o", "SOURCE,FSTYPE,OPTIONS", "/"), "fio": imageCommand(t, "/usr/bin/fio", "--version"), "fileBytes": 512 << 20, "note": "Three repeats per phase separated by reboot/conversion; not randomized or paired simultaneously"})
	for repeat := 1; repeat <= 3; repeat++ {
		for _, work := range []struct{ name, rw, depth string }{{"randread", "randread", "16"}, {"randwrite", "randwrite", "16"}, {"syncwrite", "write", "1"}} {
			args := []string{"--rw=" + work.rw, "--bs=4k", "--iodepth=" + work.depth, "--runtime=10", "--ramp_time=2", "--time_based=1", "--end_fsync=1"}
			if work.name == "syncwrite" {
				args = append(args, "--fsync=1")
			}
			imageFIO(t, fmt.Sprintf("%s-%d", work.name, repeat), file, args...)
		}
	}
	t.Log("NATIVE_QUOTA benchmark phase=" + phase + " raw=" + results)
}
