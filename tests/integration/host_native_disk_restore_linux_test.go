// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

// Explicit disposable-lab tests, never packaged into the public installer.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/RTBGG/stackfort/tests/internal/diskrestore"
	"golang.org/x/sys/unix"
)

const restoreLab = "/var/lib/stackfort-rescue-lab"
const restoreValidation = "/var/lib/stackfort-restore-validation"
const restoredBytes int64 = 50 << 30
const originalRawSHA = "eb2eea722d0dfd8926e0e017e6eb8ee9c47d350ce61ac544f64220df69b6e32e"

type wholeDiskPlan struct {
	SchemaVersion int               `json:"schemaVersion"`
	RescueDMI     string            `json:"rescueDMI"`
	TargetWWN     string            `json:"targetWWN"`
	BackupSHA256  string            `json:"backupSHA256"`
	RawSHA256     string            `json:"rawSHA256"`
	Files         map[string]string `json:"files,omitempty"`
}

var restoredFiles = []string{"/etc/fstab", "/etc/machine-id", "/etc/hostname", "/etc/ssh/ssh_host_ed25519_key.pub", "/boot/grub/grub.cfg", "/boot/grub/grubenv", "/boot/vmlinuz-6.12.107+deb13-cloud-amd64", "/boot/initrd.img-6.12.107+deb13-cloud-amd64"}

func requireDiskRestore(t *testing.T, boot bool) wholeDiskPlan {
	t.Helper()
	if os.Getenv(disposableHostOptIn) != "1" || os.Getenv("STACKFORT_NATIVE_DISK_RESTORE") != "1" {
		t.Skip("explicit full-disk disposable recovery lab only")
	}
	host, err := os.Hostname()
	expected := "stackfort-native-restore-rescue"
	if boot {
		expected = nativeHost
	}
	if err != nil || host != expected || os.Geteuid() != 0 {
		t.Fatal("wrong host or UID", err)
	}
	path := restoreLab + "/plan.json"
	if boot {
		path = restoreValidation + "/plan.json"
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Mode&0777 != 0600 || stat.Nlink != 1 || stat.Size > 8192 {
		t.Fatal("unsafe restore plan", err)
	}
	var plan wholeDiskPlan
	if err := json.Unmarshal(imageRead(t, path), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != 1 || plan.RawSHA256 != originalRawSHA || plan.RescueDMI == "6365bd88-5141-4f15-b3f8-2ba9996baad2" || !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(plan.RescueDMI) || !regexp.MustCompile(`^naa\.60022480[0-9a-f]{24}$`).MatchString(plan.TargetWWN) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(plan.BackupSHA256) {
		t.Fatal("invalid restore fixture binding")
	}
	if strings.TrimSpace(string(imageRead(t, "/sys/class/dmi/id/product_uuid"))) != plan.RescueDMI {
		t.Fatal("wrong rescue DMI; original must never be operated")
	}
	return plan
}

func diskRestoreDevice(plan wholeDiskPlan) (string, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return "", err
	}
	var found []string
	for _, entry := range entries {
		if !regexp.MustCompile(`^sd[a-z]+$`).MatchString(entry.Name()) {
			continue
		}
		wwn, err := os.ReadFile("/sys/block/" + entry.Name() + "/device/wwid")
		if err == nil && strings.TrimSpace(string(wwn)) == plan.TargetWWN {
			found = append(found, entry.Name())
		}
	}
	if len(found) != 1 {
		return "", errors.New("replacement WWN not uniquely present")
	}
	return "/dev/" + found[0], nil
}

// Mounted partitions/holders/swap are checked independently of the disk name.
func rejectDiskMounts(mounts, swaps string, devices map[string]bool) error {
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && devices[fields[2]] {
			return errors.New("replacement disk/partition is mounted")
		}
	}
	// This rescue profile intentionally has no swap, including file/device swap.
	if len(strings.Fields(swaps)) > 5 {
		return errors.New("swap must be disabled in rescue")
	}
	return nil
}

func openRestoreDisk(plan wholeDiskPlan, blank bool) (*os.File, error) {
	device, err := diskRestoreDevice(plan)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(device)
	hctl, err := filepath.EvalSymlinks("/sys/block/" + base + "/device")
	if err != nil || !strings.HasSuffix(hctl, "/0:0:0:2") {
		return nil, errors.New("replacement is not in the reserved rescue SCSI slot")
	}
	fd, err := unix.Open(device, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_EXCL, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), device)
	fail := func(err error) (*os.File, error) { file.Close(); return nil, err }
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		return fail(errors.New("replacement is not a block device"))
	}
	var geometry uint64
	_, _, ioctlErr := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), unix.BLKGETSIZE64, uintptr(unsafe.Pointer(&geometry)))
	if ioctlErr != 0 || geometry != uint64(restoredBytes) {
		return fail(errors.New("replacement size differs"))
	}
	sector, err := unix.IoctlGetInt(fd, unix.BLKSSZGET)
	if err != nil || sector != 512 {
		return fail(errors.New("replacement sector geometry differs"))
	}
	devices := map[string]bool{fmt.Sprintf("%d:%d", unix.Major(stat.Rdev), unix.Minor(stat.Rdev)): true}
	children, err := os.ReadDir("/sys/block/" + base)
	if err != nil {
		return fail(err)
	}
	nodes := []string{"/sys/block/" + base}
	for _, child := range children {
		p := "/sys/block/" + base + "/" + child.Name()
		if _, err := os.Stat(p + "/partition"); err == nil {
			nodes = append(nodes, p)
		}
	}
	for _, node := range nodes {
		dev, err := os.ReadFile(node + "/dev")
		if err != nil {
			return fail(err)
		}
		devices[strings.TrimSpace(string(dev))] = true
		holders, err := os.ReadDir(node + "/holders")
		if err != nil || len(holders) != 0 {
			return fail(errors.New("replacement has block holders"))
		}
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fail(err)
	}
	swaps, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return fail(err)
	}
	if err := rejectDiskMounts(string(mounts), string(swaps), devices); err != nil {
		return fail(err)
	}
	if blank {
		if len(nodes) != 1 {
			return fail(errors.New("replacement already has partitions"))
		}
		if err := diskrestore.CheckBlank(file, restoredBytes); err != nil {
			return fail(err)
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return fail(err)
		}
	}
	return file, nil
}

func TestWholeDiskMountPolicy(t *testing.T) {
	for _, id := range []string{"8:32", "8:33", "8:47"} {
		if err := rejectDiskMounts("1 2 "+id+" / /root rw - ext4 /dev/x rw", "Filename Type Size Used Priority\n", map[string]bool{id: true}); err == nil {
			t.Fatal("mounted target accepted")
		}
	}
	if err := rejectDiskMounts("1 2 8:1 / / rw - ext4 /dev/sda1 rw", "Filename Type Size Used Priority\n", map[string]bool{"8:32": true}); err != nil {
		t.Fatal(err)
	}
	if err := rejectDiskMounts("", "Filename Type Size Used Priority\n/swap file 10 0 -2\n", nil); err == nil {
		t.Fatal("swap accepted")
	}
}

func TestDisposableNativeWholeDiskRestore(t *testing.T) {
	plan := requireDiskRestore(t, false)
	if _, err := os.Stat("/var/lib/stackfort-rescue-ready"); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"udisks2.service", "autofs.service"} {
		target, err := os.Readlink("/etc/systemd/system/" + unit)
		if err != nil || target != "/dev/null" {
			t.Fatal("automount not masked", unit, err)
		}
	}
	if crashDigest(t, restoreLab+"/backup.vhdx") != plan.BackupSHA256 {
		t.Fatal("external backup transport digest differs")
	}
	source, err := os.OpenFile(restoreLab+"/backup.raw", os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != restoredBytes {
		t.Fatal("invalid raw backup", err)
	}
	if crashDigest(t, source.Name()) != plan.RawSHA256 {
		t.Fatal("raw backup differs from independent checkpoint pin")
	}
	bad := plan
	bad.TargetWWN = "naa.60022480ffffffffffffffffffffffff"
	if file, err := openRestoreDisk(bad, false); err == nil {
		file.Close()
		t.Fatal("foreign WWN accepted")
	}
	// Lock the one-shot record before any target mutation; never reset it.
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, restoreLab+"/restore-started.json", encoded, 0600)
	t.Log("DISK_RESTORE verifying all 50 GiB of replacement are zero")
	destination, err := openRestoreDisk(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	t.Log("DISK_RESTORE copying complete logical disk; only previously verified zero ranges may be skipped")
	written, err := diskrestore.CopyBlank(destination, source, restoredBytes, plan.RawSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	t.Log("DISK_RESTORE flushed; verifying complete 50 GiB readback")
	if crashDigest(t, destination.Name()) != plan.RawSHA256 {
		t.Fatal("restored complete disk differs")
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	// A second destructive invocation cannot pass the initial blank admission.
	if file, err := openRestoreDisk(plan, true); err == nil {
		file.Close()
		t.Fatal("restored disk accepted as blank")
	}
	if crashDigest(t, source.Name()) != plan.RawSHA256 || crashDigest(t, restoreLab+"/backup.vhdx") != plan.BackupSHA256 {
		t.Fatal("backup changed")
	}
	result := map[string]any{"schemaVersion": 1, "rescueDMI": plan.RescueDMI, "targetWWN": plan.TargetWWN, "logicalBytes": restoredBytes, "writtenBytes": written, "skippedVerifiedZeroBytes": restoredBytes - written, "sha256": plan.RawSHA256, "fullReadbackExact": true, "backupUnchanged": true, "foreignWWNRejected": true, "repeatRestoreRejected": true}
	encoded, err = json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, restoreLab+"/restore-complete.json", encoded, 0600)
	t.Logf("DISK_RESTORE PASS bytes=%d written=%d fullReadbackExact=true", restoredBytes, written)
}

func TestDisposableNativeRestoredBoot(t *testing.T) {
	plan := requireDiskRestore(t, true)
	if len(plan.Files) != len(restoredFiles) {
		t.Fatal("missing offline file pins")
	}
	for _, path := range restoredFiles {
		if crashDigest(t, path) != plan.Files[path] {
			t.Fatal("restored boot file differs", path)
		}
	}
	device, err := diskRestoreDevice(plan)
	if err != nil {
		t.Fatal(err)
	}
	if imageCommand(t, "/usr/sbin/blkid", "-s", "UUID", "-o", "value", device+"1") != "a88eaa57-e875-4855-a3cb-c231758653f8" {
		t.Fatal("root UUID differs")
	}
	if imageCommand(t, "/usr/sbin/blkid", "-s", "PARTUUID", "-o", "value", device+"1") != "54964bdf-2add-41b7-b41e-6d483962d021" {
		t.Fatal("root partition UUID differs")
	}
	root := imageCommand(t, "/usr/bin/findmnt", "-n", "-o", "SOURCE", "--target", "/")
	if root != device+"1" {
		t.Fatal("boot did not mount replacement root", root, device)
	}
	efi := imageCommand(t, "/usr/bin/findmnt", "-n", "-o", "SOURCE", "--target", "/boot/efi")
	if efi != device+"15" {
		t.Fatal("EFI mount is not from replacement", efi)
	}
	if imageCommand(t, "/usr/bin/uname", "-r") != "6.12.107+deb13-cloud-amd64" {
		t.Fatal("wrong restored kernel")
	}
	if strings.TrimSpace(string(imageRead(t, "/proc/1/comm"))) != "systemd" {
		t.Fatal("not an ordinary systemd boot")
	}
	cmdline := string(imageRead(t, "/proc/cmdline"))
	if strings.Contains(cmdline, "stackfort.native-") {
		t.Fatal("unexpected conversion/recovery token")
	}
	if strings.Contains(string(imageRead(t, "/etc/fstab")), "prjquota") {
		t.Fatal("restored pre-conversion fstab was changed")
	}
	features := imageCommand(t, "/usr/sbin/tune2fs", "-l", device+"1")
	for _, line := range strings.Split(features, "\n") {
		if strings.HasPrefix(line, "Filesystem features:") && (strings.Contains(line, "quota") || strings.Contains(line, "project")) {
			t.Fatal("conversion replayed")
		}
	}
	for _, path := range []string{"/var/lib/stackfort-installer", "/boot/grub/custom.cfg", "/run/stackfort-native-quota-writing", "/run/initramfs/stackfort-native-quota.json"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("unexpected native operation artifact", path, err)
		}
	}
	if bytes.Contains(imageRead(t, "/boot/grub/grubenv"), []byte("stackfort_native_")) {
		t.Fatal("unexpected native GRUB latch")
	}
	secure, err := filepath.Glob("/sys/firmware/efi/efivars/SecureBoot-*")
	if err != nil || len(secure) != 1 {
		t.Fatal("missing EFI SecureBoot state")
	}
	secureBytes := imageRead(t, secure[0])
	if len(secureBytes) != 5 || secureBytes[4] != 1 {
		t.Fatal("Secure Boot is not enabled")
	}
	partSizes := map[string]int64{"1": 104595423, "14": 6144, "15": 253952}
	for suffix, wanted := range partSizes {
		actual, err := strconv.ParseInt(strings.TrimSpace(string(imageRead(t, "/sys/class/block/"+filepath.Base(device)+suffix+"/size"))), 10, 64)
		if err != nil || actual != wanted {
			t.Fatal("partition geometry differs", suffix, err)
		}
	}
	result := map[string]any{"schemaVersion": 1, "rescueDMI": plan.RescueDMI, "targetWWN": plan.TargetWWN, "bootID": strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/random/boot_id"))), "root": root, "efi": efi, "kernel": "6.12.107+deb13-cloud-amd64", "secureBoot": true, "normalSystemdBoot": true, "files": plan.Files, "nativeReplayAbsent": true}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, restoreValidation+"/boot-verified.json", encoded, 0600)
	t.Log("RESTORED_BOOT PASS normal systemd + EFI Secure Boot; exact file/partition pins; no native replay")
}

// Independent offline inspection follows the full-disk readback. It compares
// source and replacement boot files before either filesystem can be mounted.
func TestDisposableNativeWholeDiskOffline(t *testing.T) {
	plan := requireDiskRestore(t, false)
	var completion struct {
		FullReadbackExact bool   `json:"fullReadbackExact"`
		SHA256            string `json:"sha256"`
	}
	if err := json.Unmarshal(imageRead(t, restoreLab+"/restore-complete.json"), &completion); err != nil || !completion.FullReadbackExact || completion.SHA256 != plan.RawSHA256 {
		t.Fatal("missing completed readback", err)
	}
	file, err := openRestoreDisk(plan, false)
	if err != nil {
		t.Fatal(err)
	}
	device := file.Name()
	file.Close()
	imageCommand(t, "/usr/sbin/blockdev", "--rereadpt", device)
	imageCommand(t, "/usr/bin/udevadm", "settle")
	file, err = openRestoreDisk(plan, false)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	const sourceLoop = "/dev/loop0"
	if imageCommand(t, "/usr/sbin/losetup", "--noheadings", "--raw", "--output", "BACK-FILE", sourceLoop) != restoreLab+"/backup.raw" || imageCommand(t, "/usr/sbin/losetup", "--noheadings", "--raw", "--output", "RO", sourceLoop) != "1" {
		t.Fatal("wrong or writable source loop")
	}
	imageWrite(t, restoreLab+"/target-partitions.json", []byte(imageCommand(t, "/usr/sbin/sfdisk", "--json", device)), 0600)
	for _, disk := range []string{sourceLoop + "p", device} {
		imageWrite(t, restoreLab+"/fsck-"+filepath.Base(disk)+".log", []byte(imageCommand(t, "/usr/sbin/e2fsck", "-f", "-n", disk+"1")), 0600)
		imageWrite(t, restoreLab+"/fat-"+filepath.Base(disk)+".log", []byte(imageCommand(t, "/usr/sbin/fsck.fat", "-n", disk+"15")), 0600)
	}
	dir := restoreLab + "/file-pins"
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	plan.Files = map[string]string{}
	for index, path := range restoredFiles {
		before := filepath.Join(dir, fmt.Sprintf("source-%02d", index))
		after := filepath.Join(dir, fmt.Sprintf("target-%02d", index))
		imageCommand(t, "/usr/sbin/debugfs", "-R", "dump "+path+" "+before, sourceLoop+"p1")
		imageCommand(t, "/usr/sbin/debugfs", "-R", "dump "+path+" "+after, device+"1")
		plan.Files[path] = crashDigest(t, before)
		if crashDigest(t, after) != plan.Files[path] {
			t.Fatal("offline boot file differs", path)
		}
	}
	if crashDigest(t, device) != plan.RawSHA256 || crashDigest(t, restoreLab+"/backup.raw") != plan.RawSHA256 {
		t.Fatal("offline inspection changed disk or backup")
	}
	imageCommand(t, "/usr/sbin/losetup", "--detach", sourceLoop)
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, restoreLab+"/boot-plan.json", encoded, 0600)
	t.Log("DISK_OFFLINE PASS root + EFI read-only fsck; 8 source/replacement file pins match; full disk and backup unchanged; source loop detached")
}
