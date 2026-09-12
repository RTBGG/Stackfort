// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

// Follow-up to the ext4 image experiment. This remains lab-only; no production
// installer backend or service graph is installed or changed.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	xfsLabFile   = imageLabRoot + "/hosting.xfs"
	xfsLabMount  = "/srv/stackfortxfs"
	xfsLabUnit   = "stackfort-storage-xfs-consumer.service"
	xfsMountUnit = "srv-stackfortxfs.mount"
)

func xfsState(t *testing.T) imageLabState {
	t.Helper()
	var state imageLabState
	if err := json.Unmarshal(imageRead(t, imageLabRoot+"/xfs-state.json"), &state); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(state.UUID); err != nil {
		t.Fatal(err)
	}
	if state.Stage != "formatted" {
		t.Fatal("XFS prototype formatting was not confirmed")
	}
	return state
}

func TestDisposableStorageXFSProvision(t *testing.T) {
	requireImageLab(t)
	imageLoop(t)
	if imageState(t).RootFingerprint != imageRootFingerprint(t) {
		t.Fatal("root configuration changed")
	}
	for _, path := range []string{xfsLabFile, xfsLabMount, imageLabRoot + "/xfs-state.json", imageLabRoot + "/xfs-probe.test", "/etc/systemd/system/" + xfsMountUnit, "/etc/systemd/system/" + xfsLabUnit} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refusing existing XFS prototype path: %s", path)
		}
	}
	var free unix.Statfs_t
	if err := unix.Statfs(imageLabRoot, &free); err != nil {
		t.Fatal(err)
	}
	if free.Bavail*uint64(free.Bsize) < uint64(imageLabBytes)+(8<<30) {
		t.Fatal("insufficient OS reserve for a second 8-GiB image")
	}
	file, err := os.OpenFile(xfsLabFile, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
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
	state := imageLabState{UUID: uuid.NewString(), Stage: "formatted", RootFingerprint: imageRootFingerprint(t)}
	// -K preserves preallocation. Other filesystem features remain at the
	// distribution defaults, including CRCs/reflink and the internal log.
	output := imageCommand(t, "/usr/sbin/mkfs.xfs", "-K", "-m", "uuid="+state.UUID, xfsLabFile)
	t.Log(output)
	if imageAllocatedAt(t, xfsLabFile) < imageLabBytes {
		t.Fatal("XFS formatting discarded reserved blocks")
	}
	if imageCommand(t, "/usr/sbin/blkid", "-p", "-s", "UUID", "-o", "value", xfsLabFile) != state.UUID {
		t.Fatal("XFS format UUID mismatch")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, imageLabRoot+"/xfs-state.json", data, 0600)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	imageWrite(t, imageLabRoot+"/xfs-probe.test", imageRead(t, self), 0700)
	if err := os.Mkdir(xfsLabMount, 0755); err != nil {
		t.Fatal(err)
	}
	mount := "[Unit]\nDescription=Stackfort disposable XFS image comparison\nBefore=local-fs.target\n\n[Mount]\nWhat=" + xfsLabFile + "\nWhere=" + xfsLabMount + "\nType=xfs\nOptions=loop,prjquota,nodev,nosuid,nodiscard\nTimeoutSec=30\n\n[Install]\nWantedBy=local-fs.target\n"
	consumer := "[Unit]\nDescription=Stackfort disposable XFS dependent consumer\nBindsTo=" + xfsMountUnit + "\nAfter=" + xfsMountUnit + "\n\n[Service]\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_STORAGE_IMAGE_PROTOTYPE=1 STACKFORT_STORAGE_XFS_GUARD=1\nExecStartPre=" + imageLabRoot + "/xfs-probe.test -test.run=^TestDisposableStorageXFSGuard$\nExecStart=/usr/bin/sleep infinity\n\n[Install]\nWantedBy=multi-user.target\n"
	imageWrite(t, "/etc/systemd/system/"+xfsMountUnit, []byte(mount), 0644)
	imageWrite(t, "/etc/systemd/system/"+xfsLabUnit, []byte(consumer), 0644)
	imageCommand(t, "/usr/bin/systemctl", "daemon-reload")
	imageCommand(t, "/usr/bin/systemctl", "enable", "--now", xfsLabUnit)
	imageCommand(t, "/usr/bin/systemctl", "enable", xfsMountUnit)
	imageWrite(t, xfsLabMount+"/reboot-sentinel", []byte(state.UUID), 0600)
	imageWrite(t, imageLabRoot+"/xfs-boot-before", imageRead(t, "/proc/sys/kernel/random/boot_id"), 0600)
	imageEvidence(t, "xfs-format.txt", []byte(output))
	t.Log("STORAGE_XFS provisioning-reservation-and-guard=passed")
}

func xfsAssertGuard(t *testing.T) {
	t.Helper()
	source := imageMountedLoop(t, xfsLabMount, xfsLabFile)
	state := xfsState(t)
	if imageRootFingerprint(t) != state.RootFingerprint {
		t.Fatal("root filesystem configuration changed")
	}
	if imageCommand(t, "/usr/sbin/blkid", "-s", "UUID", "-o", "value", source) != state.UUID {
		t.Fatal("wrong XFS UUID")
	}
	imageCommand(t, "/usr/bin/findmnt", "-rn", "-M", xfsLabMount, "-t", "xfs", "-O", "prjquota")
	if imageAllocatedAt(t, xfsLabFile) < imageLabBytes {
		t.Fatal("XFS reservation lost")
	}
	stateText := strings.Join(strings.Fields(imageCommand(t, "/usr/sbin/xfs_quota", "-x", "-c", "state -p", xfsLabMount)), " ")
	if !strings.Contains(stateText, "Accounting: ON") || !strings.Contains(stateText, "Enforcement: ON") {
		t.Fatalf("project quotas not enforced: %s", stateText)
	}
	imageCommand(t, "/usr/sbin/losetup", "--direct-io=on", source)
	queue := "/sys/block/" + filepath.Base(source) + "/queue/discard_max_bytes"
	if err := os.WriteFile(queue, []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(imageRead(t, queue))) != "0" || strings.TrimSpace(string(imageRead(t, "/sys/block/"+filepath.Base(source)+"/loop/dio"))) != "1" {
		t.Fatal("XFS loop policy not applied")
	}
}

func TestDisposableStorageXFSGuard(t *testing.T) {
	requireImageLab(t)
	if os.Getenv("STACKFORT_STORAGE_XFS_GUARD") != "1" {
		t.Skip("XFS service guard only")
	}
	xfsAssertGuard(t)
}

func TestDisposableStorageXFSSafety(t *testing.T) {
	requireImageLab(t)
	xfsAssertGuard(t)
	output, err := exec.Command("/usr/sbin/fstrim", "-v", xfsLabMount).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "discard operation is not supported") {
		t.Fatalf("unexpected trim outcome: %v %s", err, output)
	}
	if imageAllocatedAt(t, xfsLabFile) < imageLabBytes {
		t.Fatal("fstrim removed reservation")
	}
	imageCommand(t, "/usr/bin/systemctl", "stop", xfsMountUnit)
	if err := exec.Command("/usr/bin/systemctl", "is-active", "--quiet", xfsLabUnit).Run(); err == nil {
		t.Fatal("XFS consumer survived mount loss")
	}
	entries, err := os.ReadDir(xfsLabMount)
	if err != nil || len(entries) != 0 {
		t.Fatal("underlying XFS mount directory is not empty")
	}
	if err := os.Rename(xfsLabFile, xfsLabFile+".unavailable"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := os.Lstat(xfsLabFile + ".unavailable"); err == nil {
			if err := os.Rename(xfsLabFile+".unavailable", xfsLabFile); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := exec.Command("/usr/bin/systemctl", "start", xfsLabUnit).Run(); err == nil {
		t.Fatal("XFS consumer started without image")
	}
	if err := os.Rename(xfsLabFile+".unavailable", xfsLabFile); err != nil {
		t.Fatal(err)
	}
	imageCommand(t, "/usr/bin/systemctl", "reset-failed", xfsLabUnit, xfsMountUnit)
	imageCommand(t, "/usr/bin/systemctl", "start", xfsLabUnit)
	if string(imageRead(t, xfsLabMount+"/reboot-sentinel")) != xfsState(t).UUID {
		t.Fatal("XFS sentinel lost")
	}
	t.Log("STORAGE_XFS trim-mount-loss-missing-image-remount=passed")
}

// The production reconcilers use /srv/hosting. Temporarily bind the XFS root
// there while its normal test mount remains, then restore the original ext4
// mount. No filesystem is reformatted or copied during this operation.
func xfsAtHosting(t *testing.T) {
	t.Helper()
	xfsAssertGuard(t)
	imageMountedLoop(t, imageLabMount, imageLabFile)
	imageCommand(t, "/usr/bin/systemctl", "stop", "srv-hosting.mount")
	bound := false
	t.Cleanup(func() {
		// Testing.Context is cancelled before Cleanup: use a bounded independent
		// context so restoration is attempted even after a failed assertion.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if bound {
			output, err := exec.CommandContext(ctx, "/usr/bin/umount", imageLabMount).CombinedOutput()
			if err != nil {
				t.Errorf("restore ext4: unmount XFS bind: %v %s", err, output)
				return
			}
		}
		output, err := exec.CommandContext(ctx, "/usr/bin/systemctl", "start", imageLabUnit).CombinedOutput()
		if err != nil {
			t.Errorf("restore ext4 consumer: %v %s", err, output)
		}
	})
	imageCommand(t, "/usr/bin/mount", "--bind", xfsLabMount, imageLabMount)
	bound = true
	imageMountedLoop(t, imageLabMount, xfsLabFile)
	t.Setenv("STACKFORT_STORAGE_FUNCTIONAL_XFS", "1")
}

func TestDisposableStorageXFSValidate(t *testing.T) {
	requireImageLab(t)
	xfsAtHosting(t)
	for _, test := range []struct {
		name string
		fn   func(*testing.T)
	}{
		{"ProjectQuotaAndIsolation", TestDisposableHostProjectQuotaAndAccountIsolation},
		{"OCIPrivateResources", TestDisposableHostOCIPrivateResources},
		{"OCILifecycle", TestDisposableHostOCIDeploymentLifecycle},
		{"ContainerSubUIDQuota", TestDisposableStorageImageContainerQuota},
		{"Bandwidth", TestDisposableStorageImageIOThrottle},
		{"IOPS", TestDisposableStorageImageIOPS},
		{"FullImage", TestDisposableStorageImageFull},
	} {
		if !t.Run(test.name, test.fn) {
			t.Logf("XFS validation failed in %s; continuing independent checks", test.name)
		}
	}
}

func TestDisposableStorageXFSReboot(t *testing.T) {
	requireImageLab(t)
	if string(imageRead(t, imageLabRoot+"/xfs-boot-before")) == string(imageRead(t, "/proc/sys/kernel/random/boot_id")) {
		t.Fatal("XFS guest has not rebooted")
	}
	xfsAssertGuard(t)
	imageCommand(t, "/usr/bin/systemctl", "is-active", xfsLabUnit)
	imageMountedLoop(t, imageLabMount, imageLabFile)
	imageCommand(t, "/usr/bin/systemctl", "is-active", imageLabUnit)
	if string(imageRead(t, xfsLabMount+"/reboot-sentinel")) != xfsState(t).UUID {
		t.Fatal("XFS reboot sentinel lost")
	}
	t.Run("QuotasAfterReboot", func(t *testing.T) { xfsAtHosting(t); TestDisposableHostProjectQuotaAndAccountIsolation(t) })
	t.Log("STORAGE_XFS reboot-and-quota=passed")
}

func TestDisposableStorageXFSComparison(t *testing.T) {
	requireImageLab(t)
	imageMountedLoop(t, imageLabMount, imageLabFile)
	xfsAssertGuard(t)
	backends := []struct{ name, root string }{
		{"native", imageLabRoot}, {"ext4", imageLabMount}, {"xfs", xfsLabMount},
	}
	files := make(map[string]string)
	for _, backend := range backends {
		dir := backend.root + "/" + backend.name + "-comparison"
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := dir + "/fio.bin"
		files[backend.name] = file
		t.Cleanup(func() {
			if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error(err)
			}
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		if backend.name != "native" {
			imageCommand(t, "/usr/bin/chattr", "-p", "249580", "+P", dir)
			imageCommand(t, "/usr/sbin/setquota", "-P", "249580", "1048576", "1048576", "0", "0", backend.root)
		}
		imageFIO(t, "prepare-"+backend.name, file, "--rw=write", "--bs=1m", "--end_fsync=1")
	}
	facts := map[string]any{
		"kernel":             imageCommand(t, "/usr/bin/uname", "-r"),
		"xfsInfo":            imageCommand(t, "/usr/sbin/xfs_info", xfsLabMount),
		"mounts":             imageCommand(t, "/usr/bin/findmnt", "-rn", "-o", "TARGET,SOURCE,FSTYPE,OPTIONS"),
		"loops":              imageCommand(t, "/usr/sbin/losetup", "--list", "--output", "NAME,BACK-FILE,DIO"),
		"ext4AllocatedBytes": imageAllocatedAt(t, imageLabFile), "xfsAllocatedBytes": imageAllocatedAt(t, xfsLabFile),
		"fioVersion": imageCommand(t, "/usr/bin/fio", "--version"),
		"method":     "three backends; 3 repetitions; rotating order; 512 MiB; 2s ramp + 10s sample; no other guest tests concurrently",
	}
	encoded, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	imageEvidence(t, "comparison-facts.json", encoded)
	workloads := []struct{ name, rw, bs, depth string }{
		{"seqread", "read", "1m", "16"}, {"seqwrite", "write", "1m", "16"},
		{"randread", "randread", "4k", "16"}, {"randwrite", "randwrite", "4k", "16"},
		{"syncwrite", "write", "4k", "1"},
	}
	for repeat := 1; repeat <= 3; repeat++ {
		for _, work := range workloads {
			// Each backend occupies every position once, reducing order bias.
			for index := 0; index < len(backends); index++ {
				backend := backends[(index+repeat-1)%len(backends)]
				args := []string{"--rw=" + work.rw, "--bs=" + work.bs, "--iodepth=" + work.depth, "--runtime=10", "--ramp_time=2", "--time_based=1", "--end_fsync=1"}
				if work.name == "syncwrite" {
					args = append(args, "--fsync=1")
				}
				imageFIO(t, fmt.Sprintf("%s-%s-%d", backend.name, work.name, repeat), files[backend.name], args...)
			}
		}
	}
	t.Logf("STORAGE_XFS comparison=complete rawResults=%s", imageLabEvidenceDir)
}
