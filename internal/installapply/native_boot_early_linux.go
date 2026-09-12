// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

type nativeBootProof struct {
	Operation string `json:"operation"`
	Manifest  string `json:"manifest"`
	BootID    string `json:"bootID"`
	Status    string `json:"status"`
}

func nativeBootVerifyTools(ctx context.Context, intent NativeBootIntent) error {
	for _, path := range nativeBootTools {
		actual, err := nativeTrustedHash(ctx, path)
		if err != nil || actual != intent.Tools[path] {
			return errors.Join(err, errors.New("offline tool changed: "+path))
		}
	}
	return nil
}

func nativeBootCheckDevice(ctx context.Context, device string, plan storageprep.Plan, spec NativeReadySpec, converted bool) error {
	for key, expected := range map[string]string{"UUID": plan.RootUUID, "PART_ENTRY_UUID": plan.PartitionUUID, "TYPE": "ext4", "PART_ENTRY_SCHEME": "gpt"} {
		value, err := nativeReadCommand(ctx, "/usr/sbin/blkid", "-p", "-s", key, "-o", "value", device)
		if err != nil || value != expected {
			return errors.New("native device identity mismatch: " + key)
		}
	}
	values, err := nativeBootSuper(ctx, device)
	if err != nil {
		return err
	}
	if values["Block count"] != spec.Blocks || values["Block size"] != spec.BlockSize || values["Inode size"] != spec.InodeSize {
		return errors.New("native geometry drift")
	}
	if err := nativeFeatureDelta(spec.Features, values["Filesystem features"]); err != nil {
		return err
	}
	if converted {
		if !nativeBootHasQuota(values) {
			return errors.New("quota features/inode missing after conversion")
		}
	} else {
		features := strings.Fields(values["Filesystem features"])
		if slices.Contains(features, "project") || slices.Contains(features, "quota") || values["Project quota inode"] != "" {
			return errors.New("refusing repeated or partial quota conversion")
		}
	}
	return nil
}

func nativeBootCheckUnmounted(mountinfo []byte, device uint64) error {
	if len(mountinfo) == 0 || len(mountinfo) > 1<<20 {
		return errors.New("invalid mount inventory")
	}
	identity := fmt.Sprintf("%d:%d", unix.Major(device), unix.Minor(device))
	for _, line := range strings.Split(strings.TrimSpace(string(mountinfo)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || !strings.Contains(line, " - ") {
			return errors.New("malformed mount inventory")
		}
		if fields[2] == identity {
			return errors.New("refusing mounted target, including read-only mounts")
		}
	}
	return nil
}

func nativeBootOfflineRead(ctx context.Context, device, path string) ([]byte, error) {
	if !slices.Contains([]string{storageprep.JournalPath, "/boot/grub/grubenv", "/boot/grub/custom.cfg", NativeReleaseManifestPath, DefaultJournalDirectory + "/" + nativeRecoveryChoiceName, DefaultJournalDirectory + "/" + nativePrerequisiteName, DefaultJournalDirectory + "/" + nativeRuntimeName, DefaultJournalDirectory + "/" + nativeSetupName, DefaultJournalDirectory + "/resume-origin/receipt.json", DefaultJournalDirectory + "/resume-source/receipt.json", "/etc/fstab"}, path) {
		return nil, errors.New("offline record path forbidden")
	}
	// debugfs reads the unmounted filesystem directly. No -w or repair option.
	data, err := nativeBootCommand(ctx, "/usr/sbin/debugfs", "-D", "-R", "cat "+path, device)
	if err != nil || data == "" {
		return nil, errors.Join(err, errors.New("offline record unavailable: "+path))
	}
	return []byte(data), nil
}

func nativeBootEarly(ctx context.Context, operation string, output io.Writer) error {
	var root unix.Statfs_t
	if err := unix.Statfs("/", &root); err != nil || (root.Type != unix.TMPFS_MAGIC && root.Type != unix.RAMFS_MAGIC) {
		return errors.New("offline conversion requires initramfs root")
	}
	path, err := os.Executable()
	if err != nil || path != nativeBootEmbeddedInstaller {
		return errors.New("offline entry must use the embedded installer")
	}
	data, err := nativeBootRead(nativeBootEmbeddedManifest)
	if err != nil {
		return err
	}
	var manifest NativeReleaseManifest
	if err := nativeBootDecode(data, &manifest); err != nil {
		return err
	}
	plan, err := manifest.Plan()
	if err != nil || plan.OperationID != operation {
		return errors.New("offline operation mismatch")
	}
	data, err = nativeBootRead(nativeBootEmbeddedRuntime)
	if err != nil {
		return err
	}
	var intent NativeRuntimeIntent
	if err := nativeBootDecode(data, &intent); err != nil {
		return err
	}
	digest, err := intent.Digest()
	if err != nil || intent.Profile != NativeBootProfile || digest != manifest.BootSHA256 {
		return errors.New("offline runtime binding mismatch")
	}
	after, err := nativeBootFstab(intent.Offline.FstabBefore, plan.RootUUID, plan.PartitionUUID)
	if err != nil || after != intent.Offline.FstabAfter {
		return errors.New("offline fstab transition mismatch")
	}
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	live, err := io.ReadAll(io.LimitReader(running, (64<<20)+1))
	_ = running.Close()
	if err != nil || len(live) > 64<<20 || admissionDigest(live) != intent.InstallerSHA256 {
		return errors.New("embedded installer digest mismatch")
	}
	if err := nativeBootVerifyTools(ctx, *intent.Offline); err != nil {
		return err
	}
	rootSpec := os.Getenv("ROOT")
	// ROOT is a claim, not authority: it must exactly match the sealed identity.
	if rootSpec == "UUID="+plan.RootUUID {
		path = "/dev/disk/by-uuid/" + plan.RootUUID
	} else if rootSpec == "PARTUUID="+plan.PartitionUUID {
		path = "/dev/disk/by-partuuid/" + plan.PartitionUUID
	} else {
		return errors.New("unexpected initramfs ROOT specifier")
	}
	device, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Dir(device) != "/dev" {
		return errors.New("offline root is not a plain device")
	}
	var stat unix.Stat_t
	if err := unix.Stat(device, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		return errors.New("offline target is not a block device")
	}
	if _, err := os.Stat("/sys/class/block/" + filepath.Base(device) + "/partition"); err != nil {
		return errors.New("offline target is not a partition")
	}
	mountinfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	if err := nativeBootCheckUnmounted(mountinfo, stat.Rdev); err != nil {
		return err
	}
	commandLine, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}
	for _, token := range strings.Fields(string(commandLine)) {
		if strings.HasPrefix(token, "stackfort.native-recovery=") {
			_ = nativeBootEvent(output, operation, "recovery-only-root-unmounted")
			return errors.New("interrupted conversion: offline review required; automatic root mount, repair and conversion are forbidden")
		}
	}
	observed := storageprep.Observation{RootUUID: plan.RootUUID, PartitionUUID: plan.PartitionUUID}
	for path, target := range map[string]*string{"/sys/class/dmi/id/product_uuid": &observed.MachineID, "/proc/sys/kernel/random/boot_id": &observed.BootID, "/proc/sys/kernel/osrelease": &observed.Kernel} {
		data, err := nativeHostKernelFile(path)
		if err != nil || len(data) > 4096 {
			return errors.Join(err, errors.New("invalid kernel identity"))
		}
		*target = strings.TrimSpace(string(data))
	}
	observed.MachineID = strings.ToLower(observed.MachineID)
	if err := nativeBootCheckDevice(ctx, device, plan, intent.Ready, false); err != nil {
		return err
	}
	data, err = nativeBootOfflineRead(ctx, device, storageprep.JournalPath)
	if err != nil {
		return err
	}
	state, err := storageprep.DecodeState(data)
	if err != nil {
		return err
	}
	env, err := nativeBootOfflineRead(ctx, device, "/boot/grub/grubenv")
	if err != nil {
		return err
	}
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}
	if err := storageprep.CheckBootAuthorization(state, plan, observed, string(cmdline), env); err != nil {
		return err
	}
	for path, expected := range map[string][]byte{
		NativeReleaseManifestPath: nativeBootJSON(manifest), DefaultJournalDirectory + "/" + nativeRuntimeName: nativeBootJSON(intent),
		DefaultJournalDirectory + "/resume-origin/receipt.json": nativeBootJSON(manifest.Release), DefaultJournalDirectory + "/resume-source/receipt.json": nativeBootJSON(manifest.Release.Source), "/etc/fstab": []byte(intent.Offline.FstabBefore),
	} {
		actual, err := nativeBootOfflineRead(ctx, device, path)
		if err != nil || !bytes.Equal(actual, expected) {
			return errors.Join(err, errors.New("offline binding drift: "+path))
		}
	}
	if intent.Offline.PrerequisitesSHA256 != "" {
		receipt, err := nativeBootOfflineRead(ctx, device, DefaultJournalDirectory+"/"+nativePrerequisiteName)
		if err != nil {
			return err
		}
		if err := checkNativePrerequisiteData(receipt, manifest, intent); err != nil {
			return err
		}
	}
	if intent.Offline.RecoveryChoiceSHA256 != "" {
		choice, err := nativeBootOfflineRead(ctx, device, DefaultJournalDirectory+"/"+nativeRecoveryChoiceName)
		if err != nil {
			return err
		}
		if err := checkNativeRecoveryChoiceData(choice, manifest, intent); err != nil {
			return err
		}
	}
	if err := nativeBootAbsent(nativeBootProofPath); err != nil {
		return err
	}
	if intent.SetupSHA256 != "" {
		data, err := nativeBootOfflineRead(ctx, device, DefaultJournalDirectory+"/"+nativeSetupName)
		if err != nil || admissionDigest(data) != intent.SetupSHA256 {
			return errors.Join(err, errors.New("offline setup commitment drift"))
		}
		if _, err := decodeNativeSetup(data, manifest.Release); err != nil {
			return err
		}
	}
	// Reject late normal-boot drift BEFORE e2fsck/tune2fs, not only after the
	// converted root is mounted in finalization. Device identity/unmounted state
	// and the embedded debugfs pin have already been checked above.
	for path, expected := range map[string]string{
		"/boot/vmlinuz-" + plan.Kernel:    intent.Ready.KernelSHA256,
		"/boot/initrd.img-" + plan.Kernel: intent.Ready.InitrdSHA256,
		"/boot/grub/grub.cfg":             intent.Ready.GRUBSHA256,
	} {
		actual, err := nativeOfflineArtifactDigest(ctx, device, path, plan)
		if err != nil || actual != expected {
			return errors.Join(err, errors.New("offline normal-boot artifact drift: "+path))
		}
	}
	if intent.Offline.PowerLossGuard {
		entry, err := nativeBootGRUBEntry(plan, intent)
		if err != nil {
			return err
		}
		actual, err := nativeBootOfflineRead(ctx, device, "/boot/grub/custom.cfg")
		if err != nil || string(actual) != entry {
			return errors.New("offline recovery default differs from sealed boot")
		}
		// The GRUB consumer wrote the latch before Linux started. Flush the raw
		// unmounted block device and reread that latch before any fsck/tune2fs.
		// This assumes honest device/controller flush semantics, not an undo log.
		if err := nativeBootFlushDevice(device, stat.Rdev); err != nil {
			return err
		}
		env, err = nativeBootOfflineRead(ctx, device, "/boot/grub/grubenv")
		if err != nil {
			return err
		}
		if err := storageprep.CheckBootAuthorization(state, plan, observed, string(cmdline), env); err != nil {
			return err
		}
		if err := nativeBootEvent(output, operation, "recovery-latch-flushed"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "NATIVE_BOOT offline authorization verified; target unmounted; first metadata operation follows"); err != nil {
		return err
	}
	// Retain the same-boot RAM marker as well as the guarded GRUB default.
	// Neither is a filesystem undo journal or an automatic repair authorization.
	if err := nativeBootCreate(nativeBootMarker, []byte(operation), 0600); err != nil {
		return err
	}
	for _, step := range []struct {
		name       string
		executable string
		args       []string
	}{
		{"precheck", "/usr/sbin/e2fsck", []string{"-f", "-p", device}},
		{"quota", "/usr/sbin/tune2fs", []string{"-O", "project,quota", "-Q", "prjquota", device}},
		{"postcheck", "/usr/sbin/e2fsck", []string{"-f", "-p", device}},
	} {
		if err := nativeBootEvent(output, operation, step.name+"-start"); err != nil {
			return err
		}
		text, err := nativeBootCommand(ctx, step.executable, step.args...)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, text); err != nil {
			return err
		}
		if intent.Offline.PowerLossGuard {
			if err := nativeBootFlushDevice(device, stat.Rdev); err != nil {
				return err
			}
		}
		if err := nativeBootEvent(output, operation, step.name+"-flushed"); err != nil {
			return err
		}
	}
	if err := nativeBootCheckDevice(ctx, device, plan, intent.Ready, true); err != nil {
		return err
	}
	proof := nativeBootProof{Operation: operation, Manifest: plan.ManifestDigest, BootID: observed.BootID, Status: "converted"}
	if err := nativeBootCreate(nativeBootProofPath, nativeBootJSON(proof), 0600); err != nil {
		return err
	}
	if err := os.Remove(nativeBootMarker); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "NATIVE_BOOT converted; current-boot proof persisted in RAM")
	return err
}
