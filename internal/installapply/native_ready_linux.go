// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/quotastate"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

// nativeReadyBackend is intentionally incapable of boot preparation or quota
// mutation. Admission uses it only after checking an existing Ready journal.
type nativeReadyBackend struct {
	plan           storageprep.Plan
	spec           NativeReadySpec
	hostingMounted bool
	intent         NativeRuntimeIntent
	sourceStage    *SourceStage
}

func (b nativeReadyBackend) Arm(context.Context, storageprep.Plan) error {
	return storageprep.ErrNotQualified
}
func (b nativeReadyBackend) VerifyBoot(context.Context, storageprep.Plan, string) error {
	return storageprep.ErrNotQualified
}
func (b nativeReadyBackend) Resume(context.Context, storageprep.Plan, string) error {
	return storageprep.ErrNotQualified
}

func nativeReadCommand(ctx context.Context, executable string, args ...string) (string, error) {
	if ctx == nil || !slices.Contains([]string{"/usr/bin/findmnt", "/usr/sbin/blkid", "/usr/sbin/tune2fs"}, executable) {
		return "", errors.New("invalid native read command")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// #nosec G204 -- private fixed read-only call sites, never a shell.
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var stdout, stderr boundedOriginOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("native inspection failed: %w: %s", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func nativeRootDevice(ctx context.Context) (string, unix.Stat_t, error) {
	var stat unix.Stat_t
	path, err := nativeReadCommand(ctx, "/usr/bin/findmnt", "-nro", "SOURCE", "--mountpoint", "/")
	if err != nil {
		return "", stat, err
	}
	device, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Dir(device) != "/dev" {
		return "", stat, errors.New("native root must be a plain partition device")
	}
	if err := unix.Stat(device, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		return "", stat, errors.New("native root is not a block device")
	}
	if _, err := os.Stat("/sys/class/block/" + filepath.Base(device) + "/partition"); err != nil {
		return "", stat, errors.New("native root is not a plain partition")
	}
	return device, stat, nil
}

func (b nativeReadyBackend) Observe(ctx context.Context) (storageprep.Observation, error) {
	device, _, err := nativeRootDevice(ctx)
	if err != nil {
		return storageprep.Observation{}, err
	}
	result := storageprep.Observation{}
	for path, target := range map[string]*string{"/sys/class/dmi/id/product_uuid": &result.MachineID, "/proc/sys/kernel/random/boot_id": &result.BootID, "/proc/sys/kernel/osrelease": &result.Kernel} {
		data, err := nativeHostKernelFile(path)
		if err != nil || len(data) > 4096 {
			return result, errors.New("invalid native kernel identity")
		}
		*target = strings.TrimSpace(string(data))
	}
	result.MachineID = strings.ToLower(result.MachineID)
	for key, target := range map[string]*string{"UUID": &result.RootUUID, "PART_ENTRY_UUID": &result.PartitionUUID} {
		*target, err = nativeReadCommand(ctx, "/usr/sbin/blkid", "-p", "-s", key, "-o", "value", device)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (b nativeReadyBackend) VerifyReady(ctx context.Context, plan storageprep.Plan) error {
	if plan != b.plan || plan.Validate() != nil {
		return errors.New("native readiness plan mismatch")
	}
	completed, err := b.postConversionReady(ctx)
	if err != nil {
		return err
	}
	observation, err := b.Observe(ctx)
	if err != nil {
		return err
	}
	if observation.MachineID != plan.MachineID || observation.RootUUID != plan.RootUUID || observation.PartitionUUID != plan.PartitionUUID || !validSourceOperation(observation.BootID) || observation.BootID == plan.PreviousBootID {
		return errors.New("native host or boot identity drift")
	}
	artifacts, err := nativeReadyArtifactHashes(plan, b.spec, observation.Kernel, completed)
	if err != nil {
		return err
	}
	device, block, err := nativeRootDevice(ctx)
	if err != nil {
		return err
	}
	kind, err := nativeReadCommand(ctx, "/usr/sbin/blkid", "-p", "-s", "TYPE", "-o", "value", device)
	if err != nil || kind != "ext4" {
		return errors.New("native root is not ext4")
	}
	text, err := nativeReadCommand(ctx, "/usr/sbin/tune2fs", "-l", device)
	if err != nil {
		return err
	}
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if values["Block count"] != b.spec.Blocks || values["Block size"] != b.spec.BlockSize || values["Inode size"] != b.spec.InodeSize {
		return errors.New("native filesystem geometry drift")
	}
	if err := nativeFeatureDelta(b.spec.Features, values["Filesystem features"]); err != nil {
		return err
	}
	features := strings.Fields(values["Filesystem features"])
	inode, err := strconv.ParseUint(values["Project quota inode"], 10, 64)
	if err != nil || inode == 0 || !slices.Contains(features, "project") || !slices.Contains(features, "quota") {
		return errors.New("native project quota features missing")
	}
	if err := nativeVerifyQuotaMount(ctx, "/", block.Rdev); err != nil {
		return err
	}
	// Pin the actual bind source, not merely an arbitrary directory on the same
	// filesystem. The pre-mount verifier never creates or mounts either path.
	var source unix.Stat_t
	fd, err := openSourceDirectory("/srv/stackfort-native-hosting", false)
	if err != nil {
		return err
	}
	err = unix.Fstat(fd, &source)
	_ = unix.Close(fd)
	if err != nil || source.Dev != block.Rdev {
		return errors.New("native hosting source is not on root")
	}
	if b.hostingMounted {
		if err := nativeVerifyQuotaMount(ctx, "/srv/hosting", block.Rdev); err != nil {
			return err
		}
		var target unix.Stat_t
		if err := unix.Stat("/srv/hosting", &target); err != nil || source.Dev != target.Dev || source.Ino != target.Ino {
			return errors.New("native hosting bind source drift")
		}
	}
	for path, digest := range artifacts {
		if digest == "" {
			if err := nativeReadyCurrentArtifact(ctx, path); err != nil {
				return err
			}
			continue
		}
		actual, err := nativeTrustedHash(ctx, path)
		if err != nil || actual != digest {
			return fmt.Errorf("native configuration/artifact drift: %s: %w", path, errors.Join(err, errors.New("digest mismatch")))
		}
	}
	id, err := storageprep.GRUBEntryID(plan)
	if err != nil {
		return err
	}
	for _, path := range []string{"/boot/grub/custom.cfg", "/boot/" + id + ".img", "/etc/initramfs-tools/hooks/stackfort-native-quota", "/etc/initramfs-tools/scripts/local-premount/stackfort-native-quota"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("native preparation artifact still present or inaccessible: " + path)
		}
	}
	dir, err := openSourceDirectory("/boot/grub", false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	file, err := openResumeFile(dir, "grubenv", 4096)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return err
	}
	env, err := storageprep.ParseGRUBEnvironment(data)
	if err != nil {
		return err
	}
	if env["next_entry"] != "" || env["prev_saved_entry"] != "" || env["stackfort_native_armed"] != "" || env["stackfort_native_consumed"] != plan.OperationID {
		return errors.New("native one-shot boot selection is not safely retired")
	}
	return ctx.Err()
}

func nativeVerifyQuotaMount(ctx context.Context, path string, device uint64) error {
	if _, err := nativeReadCommand(ctx, "/usr/bin/findmnt", "-rn", "--mountpoint", path, "-t", "ext4", "-O", "rw,prjquota"); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Dev != device {
		return errors.New("native mount device drift")
	}
	quota, err := quotastate.ReadProject(fd)
	if err != nil || !quota.Ready() {
		return errors.Join(err, errors.New("native kernel quota accounting/enforcement disabled"))
	}
	return nil
}

func nativeTrustedHash(ctx context.Context, path string) (string, error) {
	dir, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return "", err
	}
	defer unix.Close(dir)
	file, err := openResumeFile(dir, filepath.Base(path), 512<<20)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, (512<<20)+1))
	if err != nil || n > 512<<20 {
		return "", errors.New("native artifact exceeds bound")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
