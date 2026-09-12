// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

// This is a disposable-VM experiment, NOT a production installer backend.
// Fixed paths/hostname and a second opt-in prevent accidental host adoption.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	imageLabRoot        = "/var/lib/stackfort-storage-prototype"
	imageLabFile        = imageLabRoot + "/hosting.ext4"
	imageLabMount       = "/srv/hosting"
	imageLabUnit        = "stackfort-storage-prototype-consumer.service"
	imageLabBytes int64 = 8 << 30
)

type imageLabState struct {
	UUID            string `json:"uuid"`
	Stage           string `json:"stage"`
	RootFingerprint string `json:"rootFingerprint"`
}

var imageLabEvidenceDir = imageLabRoot + "/results-" + uuid.NewString()

func imageEvidence(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(imageLabEvidenceDir, 0700); err != nil {
		t.Fatal(err)
	}
	imageWrite(t, filepath.Join(imageLabEvidenceDir, name), data, 0600)
}

func requireImageLab(t *testing.T) {
	t.Helper()
	if os.Getenv(disposableHostOptIn) != "1" || os.Getenv("STACKFORT_STORAGE_IMAGE_PROTOTYPE") != "1" {
		t.Skip("requires both disposable-host and storage-image prototype opt-ins")
	}
	host, err := os.Hostname()
	if err != nil || host != "stackfort-storage-prototype-debian-13" || os.Geteuid() != 0 {
		t.Fatal("storage experiment requires its dedicated root-operated Debian VM")
	}
}

// Match the existing installer's public parent mode. The OCI tests exercise
// normal production reconcilers, but this experiment does not install the panel.
func TestDisposableStorageImageRuntimeFixture(t *testing.T) {
	requireImageLab(t)
	const path = "/var/lib/stackfort-agent"
	if err := os.Mkdir(path, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0777 != 0755 || stat.Uid != 0 || stat.Gid != 0 {
		t.Fatal("unexpected agent-state parent")
	}
}

func imageCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), "LC_ALL=C")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func imageWrite(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func imageRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func imageState(t *testing.T) imageLabState {
	t.Helper()
	var state imageLabState
	if err := json.Unmarshal(imageRead(t, imageLabRoot+"/state.json"), &state); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(state.UUID); err != nil {
		t.Fatal(err)
	}
	return state
}

func imageSave(t *testing.T, state imageLabState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, imageLabRoot+"/state.next", data, 0600)
	if err := os.Rename(imageLabRoot+"/state.next", imageLabRoot+"/state.json"); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(imageLabRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		t.Fatal(err)
	}
}

// Ignore dynamic superblock fields (mount timestamps, free counts, recovery).
func imageRootFingerprint(t *testing.T) string {
	return imageRootFingerprintIdentity(t, "")
}

func imageRootFingerprintIdentity(t *testing.T, legacySource string) string {
	t.Helper()
	source := imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/")
	identity := imageCommand(t, "/usr/sbin/blkid", "-s", "UUID", "-o", "value", source)
	if _, err := uuid.Parse(identity); err != nil {
		t.Fatal("root filesystem has no valid UUID")
	}
	if legacySource != "" {
		identity = legacySource
	}
	mount := identity + " " + imageCommand(t, "/usr/bin/findmnt", "-nro", "FSTYPE,OPTIONS", "/")
	super := imageCommand(t, "/usr/sbin/tune2fs", "-l", source)
	features := ""
	for _, line := range strings.Split(super, "\n") {
		if strings.HasPrefix(line, "Filesystem features:") {
			for _, feature := range strings.Fields(strings.TrimPrefix(line, "Filesystem features:")) {
				if feature != "needs_recovery" && feature != "orphan_present" {
					features += feature + " "
				}
			}
		}
	}
	if features == "" || strings.Contains(features, "quota") || strings.Contains(features, "project") {
		t.Fatal("experiment needs an unprepared ext4 root filesystem")
	}
	sum := sha256.Sum256(append([]byte(mount+"\n"+features+"\n"), imageRead(t, "/etc/fstab")...))
	return hex.EncodeToString(sum[:])
}

// One-time test-evidence migration for the first prototype run: Hyper-V may
// enumerate the seed/system disks in a different order after reboot. Require
// the independently recorded ORIGINAL source and UUID; never blindly replace a
// changed fingerprint. This updates only test evidence, not the root filesystem.
func TestDisposableStorageImageNormalizeRootIdentity(t *testing.T) {
	requireImageLab(t)
	if os.Getenv("STACKFORT_STORAGE_NORMALIZE_LEGACY") != "1" {
		t.Skip("explicit legacy evidence migration only")
	}
	legacy := os.Getenv("STACKFORT_STORAGE_ORIGINAL_ROOT_SOURCE")
	originalUUID := os.Getenv("STACKFORT_STORAGE_ORIGINAL_ROOT_UUID")
	if legacy != "/dev/sda1" {
		t.Fatal("unrecognized original disposable root source")
	}
	if _, err := uuid.Parse(originalUUID); err != nil {
		t.Fatal(err)
	}
	current := imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/")
	if imageCommand(t, "/usr/sbin/blkid", "-s", "UUID", "-o", "value", current) != originalUUID {
		t.Fatal("root UUID changed")
	}
	state := imageState(t)
	if state.RootFingerprint != imageRootFingerprintIdentity(t, legacy) {
		t.Fatal("root changed beyond its device name")
	}
	state.RootFingerprint = imageRootFingerprint(t)
	imageSave(t, state)
	t.Log("root UUID, features, mount options and fstab unchanged; normalized transient device name")
}

func imageAllocated(t *testing.T) int64 {
	file := imageLabFile
	if os.Getenv("STACKFORT_STORAGE_FUNCTIONAL_XFS") == "1" {
		file = xfsLabFile
	}
	return imageAllocatedAt(t, file)
}

func imageAllocatedAt(t *testing.T, file string) int64 {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Lstat(file, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0777 != 0600 || stat.Uid != 0 || stat.Nlink != 1 || stat.Size != imageLabBytes {
		t.Fatalf("unsafe/unexpected prototype image metadata: %+v", stat)
	}
	return stat.Blocks * 512
}

// ProvisionWorker is invoked in separate processes so an injected exit really
// loses process state. Already formatted images are never blindly reformatted.
func TestDisposableStorageImageProvisionWorker(t *testing.T) {
	requireImageLab(t)
	if os.Getenv("STACKFORT_STORAGE_WORKER") != "1" {
		t.Skip("child only")
	}
	state := imageState(t)
	if state.RootFingerprint != imageRootFingerprint(t) {
		t.Fatal("root filesystem changed")
	}
	switch state.Stage {
	case "new":
		var free unix.Statfs_t
		if err := unix.Statfs(imageLabRoot, &free); err != nil {
			t.Fatal(err)
		}
		if free.Bavail*uint64(free.Bsize) < uint64(imageLabBytes)+(8<<30) {
			t.Fatal("insufficient OS reserve")
		}
		file, err := os.OpenFile(imageLabFile, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Fallocate(int(file.Fd()), 0, 0, imageLabBytes); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		state.Stage = "allocated"
		imageSave(t, state)
		if os.Getenv("STACKFORT_STORAGE_STOP_AFTER") == "allocated" {
			os.Exit(75)
		}
		fallthrough
	case "allocated":
		if imageAllocated(t) < imageLabBytes {
			t.Fatal("image is sparse")
		}
		probe := exec.Command("/usr/sbin/blkid", "-p", "-s", "UUID", "-o", "value", imageLabFile)
		output, err := probe.Output()
		if err == nil {
			if strings.TrimSpace(string(output)) != state.UUID {
				t.Fatal("refusing foreign filesystem")
			}
		} else {
			// Only the first formatting attempt is automatic. A crash during mkfs
			// requires inspection; the prototype never repairs uncertain metadata.
			imageWrite(t, imageLabRoot+"/format-attempted", []byte(state.UUID), 0600)
			imageCommand(t, "/usr/sbin/mkfs.ext4", "-q", "-O", "quota,project", "-E", "nodiscard,lazy_itable_init=0,lazy_journal_init=0", "-m", "0", "-U", state.UUID, imageLabFile)
		}
		if os.Getenv("STACKFORT_STORAGE_STOP_AFTER") == "formatted-before-journal" {
			os.Exit(75)
		}
		state.Stage = "formatted"
		imageSave(t, state)
	case "formatted":
	default:
		t.Fatalf("unexpected stage %q", state.Stage)
	}
	if imageAllocated(t) < imageLabBytes {
		t.Fatal("formatting discarded the reservation")
	}
}

func TestDisposableStorageImageProvision(t *testing.T) {
	requireImageLab(t)
	// Refuse all pre-existing paths, even an apparently empty hosting directory.
	for _, path := range []string{imageLabRoot, imageLabMount, "/etc/systemd/system/srv-hosting.mount", "/etc/systemd/system/" + imageLabUnit} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refusing existing path: %s (%v)", path, err)
		}
	}
	if err := os.Mkdir(imageLabRoot, 0700); err != nil {
		t.Fatal(err)
	}
	imageSave(t, imageLabState{UUID: uuid.NewString(), Stage: "new", RootFingerprint: imageRootFingerprint(t)})
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, imageLabRoot+"/probe.test", imageRead(t, self), 0700)
	for _, stop := range []string{"allocated", "formatted-before-journal", ""} {
		command := exec.CommandContext(t.Context(), self, "-test.v", "-test.run=^TestDisposableStorageImageProvisionWorker$")
		command.Env = append(os.Environ(), "STACKFORT_STORAGE_WORKER=1", "STACKFORT_STORAGE_STOP_AFTER="+stop)
		output, err := command.CombinedOutput()
		if stop != "" {
			var failure *exec.ExitError
			if !errors.As(err, &failure) || failure.ExitCode() != 75 {
				t.Fatalf("injection %s: %v\n%s", stop, err, output)
			}
		} else if err != nil {
			t.Fatalf("resume: %v\n%s", err, output)
		}
		t.Logf("provision process stop=%q recovered; allocated=%d", stop, imageAllocated(t))
	}
	if err := os.Mkdir(imageLabMount, 0755); err != nil {
		t.Fatal(err)
	}
	mountUnit := "[Unit]\nDescription=Stackfort disposable storage image experiment\nBefore=local-fs.target\n\n[Mount]\nWhat=" + imageLabFile + "\nWhere=" + imageLabMount + "\nType=ext4\nOptions=loop,prjquota,nodev,nosuid,nodiscard\nTimeoutSec=30\n\n[Install]\nWantedBy=local-fs.target\n"
	consumerUnit := "[Unit]\nDescription=Stackfort disposable storage dependent consumer\nBindsTo=srv-hosting.mount\nAfter=srv-hosting.mount\n\n[Service]\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_STORAGE_IMAGE_PROTOTYPE=1 STACKFORT_STORAGE_GUARD=1\nExecStartPre=" + imageLabRoot + "/probe.test -test.run=^TestDisposableStorageImageGuard$\nExecStart=/usr/bin/sleep infinity\n\n[Install]\nWantedBy=multi-user.target\n"
	imageWrite(t, "/etc/systemd/system/srv-hosting.mount", []byte(mountUnit), 0644)
	imageWrite(t, "/etc/systemd/system/"+imageLabUnit, []byte(consumerUnit), 0644)
	imageCommand(t, "/usr/bin/systemctl", "daemon-reload")
	imageCommand(t, "/usr/bin/systemctl", "enable", "--now", imageLabUnit)
	imageCommand(t, "/usr/bin/systemctl", "enable", "srv-hosting.mount")
	imageWrite(t, imageLabMount+"/reboot-sentinel", []byte(imageState(t).UUID), 0600)
	imageWrite(t, imageLabRoot+"/boot-before", imageRead(t, "/proc/sys/kernel/random/boot_id"), 0600)
	t.Log("STORAGE_IMAGE provision-resume=passed root-unchanged=passed reservation=passed")
}

func imageLoop(t *testing.T) string {
	file := imageLabFile
	if os.Getenv("STACKFORT_STORAGE_FUNCTIONAL_XFS") == "1" {
		file = xfsLabFile
	}
	return imageMountedLoop(t, imageLabMount, file)
}

func imageMountedLoop(t *testing.T, mount, file string) string {
	t.Helper()
	source := imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "-M", mount)
	if !strings.HasPrefix(source, "/dev/loop") {
		t.Fatalf("not a loop source: %s", source)
	}
	if _, err := strconv.ParseUint(strings.TrimPrefix(source, "/dev/loop"), 10, 32); err != nil {
		t.Fatal(err)
	}
	backing := imageCommand(t, "/usr/sbin/losetup", "-n", "-O", "BACK-FILE", source)
	if backing != file {
		t.Fatalf("foreign backing image: %q", backing)
	}
	return source
}

func TestDisposableStorageImageGuard(t *testing.T) {
	requireImageLab(t)
	if os.Getenv("STACKFORT_STORAGE_GUARD") != "1" {
		t.Skip("service guard only")
	}
	source := imageLoop(t)
	state := imageState(t)
	if imageCommand(t, "/usr/sbin/blkid", "-s", "UUID", "-o", "value", source) != state.UUID {
		t.Fatal("wrong filesystem UUID")
	}
	imageCommand(t, "/usr/bin/findmnt", "-rn", "-M", imageLabMount, "-t", "ext4", "-O", "prjquota")
	if imageAllocated(t) < imageLabBytes {
		t.Fatal("lost reserved image blocks")
	}
	// Direct I/O avoids an extra page cache for the backing file. Queue-level
	// discard suppression is necessary: nodiscard alone does not block fstrim.
	imageCommand(t, "/usr/sbin/losetup", "--direct-io=on", source)
	queue := "/sys/block/" + filepath.Base(source) + "/queue/discard_max_bytes"
	if err := os.WriteFile(queue, []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(imageRead(t, queue))) != "0" {
		t.Fatal("discard remains enabled")
	}
	if strings.TrimSpace(string(imageRead(t, "/sys/block/"+filepath.Base(source)+"/loop/dio"))) != "1" {
		t.Fatal("backing direct I/O not enabled")
	}
}

func TestDisposableStorageImageSafety(t *testing.T) {
	requireImageLab(t)
	imageLoop(t)
	state := imageState(t)
	if state.RootFingerprint != imageRootFingerprint(t) {
		t.Fatal("root filesystem modified")
	}
	// This must not punch holes even though the host root uses online discard.
	output, err := exec.Command("/usr/sbin/fstrim", "-v", imageLabMount).CombinedOutput()
	if err == nil {
		t.Fatalf("discard unexpectedly accepted: %s", output)
	}
	if !strings.Contains(string(output), "discard operation is not supported") {
		t.Fatalf("unexpected trim error: %v %s", err, output)
	}
	if imageAllocated(t) < imageLabBytes {
		t.Fatal("fstrim released preallocation")
	}
	t.Log("STORAGE_IMAGE explicit-fstrim-reservation=passed")

	imageCommand(t, "/usr/bin/systemctl", "stop", "srv-hosting.mount")
	if err := exec.Command("/usr/bin/systemctl", "is-active", "--quiet", imageLabUnit).Run(); err == nil {
		t.Fatal("consumer remained active without mount")
	}
	entries, err := os.ReadDir(imageLabMount)
	if err != nil || len(entries) != 0 {
		t.Fatalf("underlying directory received writes: %v %v", entries, err)
	}
	if err := os.Rename(imageLabFile, imageLabFile+".unavailable"); err != nil {
		t.Fatal(err)
	}
	// Restore only the exact owned file; never recreate a missing data image.
	defer func() {
		if _, err := os.Lstat(imageLabFile + ".unavailable"); err == nil {
			if err := os.Rename(imageLabFile+".unavailable", imageLabFile); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := exec.Command("/usr/bin/systemctl", "start", imageLabUnit).Run(); err == nil {
		t.Fatal("consumer started with missing image")
	}
	if err := os.Rename(imageLabFile+".unavailable", imageLabFile); err != nil {
		t.Fatal(err)
	}
	imageCommand(t, "/usr/bin/systemctl", "reset-failed")
	imageCommand(t, "/usr/bin/systemctl", "start", imageLabUnit)
	if string(imageRead(t, imageLabMount+"/reboot-sentinel")) != state.UUID {
		t.Fatal("sentinel changed on remount")
	}
	t.Log("STORAGE_IMAGE mount-loss-stop=passed missing-image-fail-closed=passed remount=passed")
}

func TestDisposableStorageImageReboot(t *testing.T) {
	requireImageLab(t)
	if string(imageRead(t, imageLabRoot+"/boot-before")) == string(imageRead(t, "/proc/sys/kernel/random/boot_id")) {
		t.Fatal("VM has not rebooted")
	}
	imageLoop(t)
	imageCommand(t, "/usr/bin/systemctl", "is-active", imageLabUnit)
	if imageState(t).RootFingerprint != imageRootFingerprint(t) {
		t.Fatal("root configuration changed after reboot")
	}
	if string(imageRead(t, imageLabMount+"/reboot-sentinel")) != imageState(t).UUID {
		t.Fatal("lost persisted data")
	}
	if imageAllocated(t) < imageLabBytes {
		t.Fatal("lost reservation after reboot")
	}
	t.Log("STORAGE_IMAGE automatic-reboot-mount-and-consumer=passed")
}

func TestDisposableStorageImageFull(t *testing.T) {
	requireImageLab(t)
	imageLoop(t)
	// Fill only the bounded image, never the outer root filesystem. Reserve a
	// sentinel path first; stop at image capacity even if the mount disappears.
	fd, err := unix.Open(imageLabMount+"/full-probe", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	defer os.Remove(imageLabMount + "/full-probe")
	var inner, outer unix.Stat_t
	if err := unix.Fstat(fd, &inner); err != nil {
		t.Fatal(err)
	}
	if err := unix.Stat("/", &outer); err != nil || inner.Dev == outer.Dev {
		t.Fatal("refusing to fill root filesystem")
	}
	var offset int64
	for _, chunk := range []int64{64 << 20, 4096} {
		for offset+chunk <= imageLabBytes {
			if err := t.Context().Err(); err != nil {
				t.Fatal(err)
			}
			err = unix.Fallocate(fd, 0, offset, chunk)
			if errors.Is(err, unix.ENOSPC) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			offset += chunk
		}
	}
	if _, err := unix.Pwrite(fd, []byte("full"), offset); !errors.Is(err, unix.ENOSPC) {
		t.Fatalf("expected write ENOSPC, got %v", err)
	}
	var free unix.Statfs_t
	if err := unix.Fstatfs(fd, &free); err != nil {
		t.Fatal(err)
	}
	// XFS can return ENOSPC while a few blocks remain unusable for the next
	// allocation's metadata reservation. Still require an actual failed write
	// and a near-empty pool; do not confuse quota failure with pool exhaustion.
	maximumRemaining := uint64(0)
	if os.Getenv("STACKFORT_STORAGE_FUNCTIONAL_XFS") == "1" {
		maximumRemaining = (1 << 20) / uint64(free.Bsize)
	}
	if free.Bavail > maximumRemaining {
		t.Fatalf("image still has %d available blocks", free.Bavail)
	}
	imageEvidence(t, "outer-write-after-full.txt", []byte("OS storage remains writable while hosting image is full\n"))
	if imageAllocated(t) < imageLabBytes {
		t.Fatal("reservation changed on ENOSPC")
	}
	t.Logf("STORAGE_IMAGE bounded-ENOSPC-and-outer-write=passed allocated=%d innerAvailableBlocks=%d", offset, free.Bavail)
}

type imageFIODirection struct {
	IOPS    float64 `json:"iops"`
	BWBytes float64 `json:"bw_bytes"`
	Latency struct {
		Mean       float64            `json:"mean"`
		Percentile map[string]float64 `json:"percentile"`
	} `json:"clat_ns"`
}

type imageFIOResult struct {
	Jobs []struct {
		Error     int               `json:"error"`
		Read      imageFIODirection `json:"read"`
		Write     imageFIODirection `json:"write"`
		UserCPU   float64           `json:"usr_cpu"`
		SystemCPU float64           `json:"sys_cpu"`
	} `json:"jobs"`
}

func imageCPU(t *testing.T) (busy, total uint64) {
	t.Helper()
	fields := strings.Fields(strings.SplitN(string(imageRead(t, "/proc/stat")), "\n", 2)[0])
	for index := 1; index <= 8; index++ {
		value, err := strconv.ParseUint(fields[index], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		total += value
		if index != 4 && index != 5 {
			busy += value
		}
	}
	return
}

func imageFIO(t *testing.T, label, file string, extra ...string) imageFIOResult {
	t.Helper()
	args := []string{"--name=" + label, "--filename=" + file, "--size=512m", "--ioengine=libaio", "--direct=1", "--iodepth=16", "--numjobs=1", "--group_reporting=1", "--randrepeat=1", "--randseed=42173", "--refill_buffers=1", "--buffer_compress_percentage=0", "--output-format=json"}
	args = append(args, extra...)
	busy0, total0 := imageCPU(t)
	before := time.Now()
	output := imageCommand(t, "/usr/bin/fio", args...)
	wall := time.Since(before)
	busy1, total1 := imageCPU(t)
	imageEvidence(t, label+".json", []byte(output))
	var result imageFIOResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Jobs) != 1 || result.Jobs[0].Error != 0 {
		t.Fatalf("fio job error: %+v", result)
	}
	job := result.Jobs[0]
	cpu := 100 * float64(busy1-busy0) / float64(total1-total0)
	t.Logf("FIO %s read=%.1fMiB/s %.0fIOPS p99=%.3fms write=%.1fMiB/s %.0fIOPS p99=%.3fms guest_busy=%.1f%% wall=%.2fs", label, job.Read.BWBytes/(1<<20), job.Read.IOPS, job.Read.Latency.Percentile["99.000000"]/1e6, job.Write.BWBytes/(1<<20), job.Write.IOPS, job.Write.Latency.Percentile["99.000000"]/1e6, cpu, wall.Seconds())
	metrics, _ := json.Marshal(map[string]any{"label": label, "guestBusyPercent": cpu, "wallSeconds": wall.Seconds()})
	imageEvidence(t, label+"-cpu.json", metrics)
	return result
}

func TestDisposableStorageImageBenchmark(t *testing.T) {
	requireImageLab(t)
	imageLoop(t)
	// Native root is intentionally quota-free; this is the ordinary VPS
	// baseline, NOT an apples-to-apples native-project-quota comparison.
	native := imageLabRoot + "/native-fio.bin"
	loop := imageLabMount + "/loop-fio.bin"
	for label, file := range map[string]string{"native": native, "image": loop} {
		if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refusing benchmark file %s", file)
		}
		imageFIO(t, "prepare-"+label, file, "--rw=write", "--bs=1m", "--end_fsync=1")
		defer os.Remove(file)
	}
	// Charge the image's benchmark data to a real project with a generous cap.
	imageCommand(t, "/usr/bin/chattr", "-p", "249590", loop)
	imageCommand(t, "/usr/sbin/setquota", "-P", "249590", "1048576", "1048576", "0", "0", imageLabMount)
	workloads := []struct{ name, rw, bs, depth string }{
		{"seqread", "read", "1m", "16"}, {"seqwrite", "write", "1m", "16"},
		{"randread", "randread", "4k", "16"}, {"randwrite", "randwrite", "4k", "16"},
		{"syncwrite", "write", "4k", "1"},
	}
	for repeat := 1; repeat <= 3; repeat++ {
		for _, work := range workloads {
			labels := []string{"native", "image"}
			if repeat%2 == 0 {
				labels = []string{"image", "native"}
			}
			for _, label := range labels {
				file := native
				if label == "image" {
					file = loop
				}
				args := []string{"--rw=" + work.rw, "--bs=" + work.bs, "--iodepth=" + work.depth, "--runtime=10", "--ramp_time=2", "--time_based=1", "--end_fsync=1"}
				if work.name == "syncwrite" {
					args = append(args, "--fsync=1")
				}
				imageFIO(t, fmt.Sprintf("%s-%s-%d", label, work.name, repeat), file, args...)
			}
		}
	}
	t.Log("STORAGE_IMAGE benchmark-complete (measurements, not a performance acceptance gate)")
}

func TestDisposableStorageImageIOThrottle(t *testing.T) {
	requireImageLab(t)
	source := imageLoop(t)
	// Exercise both the actual loop device and outer root block device: path
	// resolution alone can select the wrong enforcement layer.
	rootDevice := imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/")
	for _, target := range []struct{ label, device, dir string }{
		{"native", rootDevice, imageLabRoot}, {"image-loop", source, imageLabMount}, {"image-root", rootDevice, imageLabMount},
	} {
		for _, direct := range []string{"1", "0"} {
			name := "throttle-" + target.label + "-fresh-direct" + direct
			file := target.dir + "/" + name + ".bin"
			if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("existing throttle file")
			}
			defer os.Remove(file)
			unit := "stackfort-storage-prototype-" + name
			before := time.Now()
			output := imageCommand(t, "/usr/bin/systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--unit="+unit,
				"-p", "IOAccounting=yes", "-p", "IOWriteBandwidthMax="+target.device+" 4M",
				"/usr/bin/fio", "--name="+name, "--filename="+file, "--size=32m", "--rw=write", "--bs=1m", "--ioengine=libaio", "--iodepth=4", "--direct="+direct, "--end_fsync=1", "--output-format=json")
			wall := time.Since(before).Seconds()
			imageEvidence(t, name+".json", []byte(output))
			var result imageFIOResult
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Jobs) != 1 || result.Jobs[0].Error != 0 {
				t.Fatal("throttle probe failed")
			}
			bw := result.Jobs[0].Write.BWBytes / (1 << 20)
			effective := 32 / wall
			metrics, _ := json.Marshal(map[string]any{"label": name, "wallSeconds": wall, "effectiveMiBPerSecond": effective})
			imageEvidence(t, name+"-wall.json", metrics)
			t.Logf("IO_THROTTLE device=%s application_direct=%s fio=%.2fMiB/s wall=%.2fs effective=%.2fMiB/s configured=4MiB/s", target.label, direct, bw, wall, effective)
			if effective > 5.0 || effective < 0.5 {
				t.Errorf("I/O bandwidth enforcement outside expected range on %s direct=%s: %.2fMiB/s", target.label, direct, effective)
			}
		}
	}
}

func TestDisposableStorageImageIOPS(t *testing.T) {
	requireImageLab(t)
	source := imageLoop(t)
	file := imageLabMount + "/iops-fio.bin"
	if _, err := os.Lstat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("existing IOPS file")
	}
	defer os.Remove(file)
	imageFIO(t, "prepare-iops", file, "--rw=write", "--bs=1m", "--end_fsync=1")
	for _, direction := range []string{"read", "write"} {
		property := "IOReadIOPSMax="
		if direction == "write" {
			property = "IOWriteIOPSMax="
		}
		name := "iops-" + direction
		output := imageCommand(t, "/usr/bin/systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--unit=stackfort-storage-prototype-"+name,
			"-p", "IOAccounting=yes", "-p", property+source+" 500",
			"/usr/bin/fio", "--name="+name, "--filename="+file, "--size=512m", "--runtime=10", "--ramp_time=2", "--time_based=1", "--rw=rand"+direction, "--bs=4k", "--ioengine=libaio", "--iodepth=1", "--direct=1", "--end_fsync=1", "--output-format=json")
		imageEvidence(t, name+".json", []byte(output))
		var result imageFIOResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Jobs) != 1 || result.Jobs[0].Error != 0 {
			t.Fatal("IOPS probe failed")
		}
		actual := result.Jobs[0].Read.IOPS
		if direction == "write" {
			actual = result.Jobs[0].Write.IOPS
		}
		t.Logf("IOPS_THROTTLE %s configured=500 observed=%.2f", direction, actual)
		if actual < 250 || actual > 600 {
			t.Errorf("IOPS limit outside expected range: %.2f", actual)
		}
	}
}

func TestDisposableStorageImageContainerQuota(t *testing.T) {
	requireImageLab(t)
	imageLoop(t)
	testContainerProjectQuota(t)
}

// Shared workload; callers must establish their disposable host and filesystem
// guard first. No mount/provisioning policy is relaxed by this extraction.
func testContainerProjectQuota(t *testing.T) {
	t.Helper()
	identity := disposableIdentity(t, availableManagedID(t, 249_940))
	t.Cleanup(func() { cleanupOCIRuntimeIdentity(t, identity) })
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	storage := hostingstorage.Spec{Identity: identity, ProjectID: identity.UID}
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), storage); err != nil {
		t.Fatal(err)
	}
	boundary, err := hostresources.NewReconciler().Reconcile(t.Context(), hostingresources.Spec{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupResourceSlice(t, boundary.UnitName) })
	if _, err := hostidentity.NewReconciler().ReconcileRuntime(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	// Resolve the same image used by the existing OCI lifecycle gate, record
	// its immutable ID, and run only that local ID (no tag re-resolution).
	reference := "docker.io/nginxinc/nginx-unprivileged:alpine"
	if output, err := runRootlessPodman(identity, "pull", "--quiet", reference); err != nil {
		t.Fatalf("image pull: %v %s", err, output)
	}
	output, err := runRootlessPodman(identity, "image", "inspect", "--format", "{{.Id}}", reference)
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.TrimSpace(string(output))
	if len(strings.TrimPrefix(digest, "sha256:")) != 64 {
		t.Fatal("invalid local image ID")
	}
	t.Logf("container image=%s", digest)
	storage.ByteLimit = 256 << 20
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), storage); err != nil {
		t.Fatal(err)
	}
	probeDir := filepath.Join(identity.HomeDirectory, "tmp", "subuid-quota")
	if err := runAs(identity, "/usr/bin/mkdir", "-m", "0700", probeDir); err != nil {
		t.Fatal(err)
	}
	if output, err := runRootlessPodman(identity, "unshare", "/usr/bin/chown", "1:1", probeDir); err != nil {
		t.Fatalf("subuid ownership: %v %s", err, output)
	}
	output, err = runRootlessPodman(identity, "run", "--rm", "--network=none", "--read-only", "--cap-drop=all", "--security-opt=no-new-privileges", "--user=1:1", "--volume="+probeDir+":/data:rw", "--entrypoint=/bin/sh", digest,
		"-c", "dd if=/dev/zero of=/data/probe bs=1M count=512")
	message := strings.ToLower(string(output))
	if err == nil || (!strings.Contains(message, "quota") && !strings.Contains(message, "no space left on device")) {
		t.Fatalf("container did not hit a storage boundary: %v %s", err, output)
	}
	var free unix.Statfs_t
	if err := unix.Statfs(imageLabMount, &free); err != nil {
		t.Fatal(err)
	}
	if free.Bavail*uint64(free.Bsize) < 1<<30 {
		t.Fatal("pool too full to attribute this failure to account quota")
	}
	var stat unix.Stat_t
	if err := unix.Stat(probeDir+"/probe", &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Uid == identity.UID || stat.Size <= 0 || stat.Size >= 512<<20 {
		t.Fatalf("subuid/size boundary not exercised: %+v", stat)
	}
	// Prove the causal limit instead of depending solely on an errno message:
	// XFS can report ENOSPC for project reservations. Increase ONLY this account
	// limit, then the same subordinate-UID container must append successfully.
	storage.ByteLimit = 320 << 20
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), storage); err != nil {
		t.Fatal(err)
	}
	output, err = runRootlessPodman(identity, "run", "--rm", "--network=none", "--read-only", "--cap-drop=all", "--security-opt=no-new-privileges", "--user=1:1", "--volume="+probeDir+":/data:rw", "--entrypoint=/bin/sh", digest,
		"-c", "dd if=/dev/zero bs=1M count=16 >> /data/probe")
	if err != nil {
		t.Fatalf("append after increasing quota: %v %s", err, output)
	}
	var after unix.Stat_t
	if err := unix.Stat(probeDir+"/probe", &after); err != nil {
		t.Fatal(err)
	}
	if after.Size != stat.Size+(16<<20) || after.Uid != stat.Uid {
		t.Fatal("quota increase did not permit the expected subordinate-UID append")
	}
	if err := os.Remove(probeDir + "/probe"); err != nil {
		t.Fatal(err)
	}
	t.Logf("STORAGE_IMAGE rootless-container-subuid-quota=passed partialBytes=%d fileUID=%d accountUID=%d recoveredAppendBytes=%d", stat.Size, stat.Uid, identity.UID, after.Size-stat.Size)
}
