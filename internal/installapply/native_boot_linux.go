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

// PrepareNativeBoot owns the real initial preparation. Internal callers must
// first retain/authenticate a release under this same lock. This API is not
// exposed by the public installer, and does not reboot or convert a filesystem.
func (stage *SourceStage) PrepareNativeBoot(ctx context.Context, binding ReleaseBinding, dispatcher string, decision NativeRecoveryDecision) (NativeReleaseManifest, error) {
	var manifest NativeReleaseManifest
	if err := stage.check(); err != nil {
		return manifest, err
	}
	if ctx == nil || ctx.Err() != nil || stage.journal == nil {
		return manifest, errors.New("boot preparation requires the shared installer lock")
	}
	if err := decision.Validate(); err != nil {
		return manifest, err
	}
	if _, exists, err := stage.journal.Load(); err != nil || exists {
		return manifest, errors.Join(err, errors.New("existing storage preparation cannot be adopted"))
	}
	if _, err := stage.VerifyBinding(ctx, binding); err != nil {
		return manifest, err
	}
	setupDigest, err := stage.nativeSetupDigest(binding)
	if err != nil || (binding.Policy.Class == "tag-release" && setupDigest == "") {
		return manifest, errors.Join(err, errors.New("tagged native installation requires the pre-reboot setup commitment"))
	}
	// Reject partial boot artifacts before any prerequisite package mutation.
	for _, path := range []string{NativeRuntimePath, DefaultJournalDirectory + "/" + nativeRuntimeName, NativeReleaseManifestPath} {
		if err := nativeBootAbsent(path); err != nil {
			return manifest, err
		}
	}
	packagesGuard, err := acquireNativePackageGuard(ctx)
	if err != nil {
		return manifest, err
	}
	defer packagesGuard.close()
	choice, err := stage.startNativeRecoveryChoice(ctx, binding, dispatcher, decision)
	if err != nil {
		return manifest, err
	}
	prerequisites, err := stage.ensureNativePrerequisites(ctx, binding, dispatcher, choice, packagesGuard)
	if err != nil {
		return manifest, err
	}
	gate, err := NewLinuxAdmissionGate(binding.Source.OperationID)
	if err != nil {
		return manifest, err
	}
	osRelease, err := nativeBootRead("/usr/lib/os-release")
	if err != nil || !strings.Contains(string(osRelease), "VERSION_ID=\"13\"") {
		return manifest, errors.New("offline preparation currently requires Debian 13")
	}
	observed, err := (nativeReadyBackend{}).Observe(ctx)
	if err != nil {
		return manifest, err
	}
	device, _, err := nativeRootDevice(ctx)
	if err != nil {
		return manifest, err
	}
	values, err := nativeBootSuper(ctx, device)
	if err != nil {
		return manifest, err
	}
	if values["Inode size"] != "256" || nativeBootHasQuota(values) || strings.Contains(values["Filesystem features"], "quota") || strings.Contains(values["Filesystem features"], "project") {
		return manifest, errors.New("requires unconverted ext4 with 256-byte inodes")
	}
	for key, expected := range map[string]string{"TYPE": "ext4", "PART_ENTRY_SCHEME": "gpt"} {
		value, err := nativeReadCommand(ctx, "/usr/sbin/blkid", "-p", "-s", key, "-o", "value", device)
		if err != nil || value != expected {
			return manifest, errors.New("requires plain GPT/ext4 root")
		}
	}
	var capacity unix.Statfs_t
	if err := unix.Statfs("/", &capacity); err != nil || capacity.Bsize <= 0 || capacity.Bavail < (8<<30)/uint64(capacity.Bsize) {
		return manifest, errors.New("less than 8 GiB free for qualification; general capacity policy remains unqualified")
	}
	fstab, err := nativeBootRead("/etc/fstab")
	if err != nil {
		return manifest, err
	}
	after, err := nativeBootFstab(string(fstab), observed.RootUUID, observed.PartitionUUID)
	if err != nil {
		return manifest, err
	}
	intent := NativeRuntimeIntent{SchemaVersion: 1, Profile: NativeBootProfile, Offline: &NativeBootIntent{SchemaVersion: 1, FstabBefore: string(fstab), FstabAfter: after, Tools: map[string]string{}}, Ready: NativeReadySpec{Features: values["Filesystem features"], Blocks: values["Block count"], BlockSize: values["Block size"], InodeSize: values["Inode size"], FstabSHA256: admissionDigest([]byte(after))}}
	intent.Offline.PrerequisitesSHA256 = prerequisites
	intent.Offline.RecoveryChoiceSHA256 = admissionDigest(nativeBootJSON(choice))
	intent.Offline.PowerLossGuard = true
	intent.SetupSHA256, err = stage.nativeSetupDigest(binding)
	if err != nil || intent.SetupSHA256 != setupDigest {
		return manifest, errors.Join(err, errors.New("setup commitment changed during prerequisite preparation"))
	}
	for _, path := range nativeBootTools {
		intent.Offline.Tools[path], err = nativeTrustedHash(ctx, path)
		if err != nil {
			return manifest, err
		}
	}
	for path, target := range map[string]*string{dispatcher: &intent.InstallerSHA256, "/boot/vmlinuz-" + observed.Kernel: &intent.Ready.KernelSHA256, "/boot/initrd.img-" + observed.Kernel: &intent.Ready.InitrdSHA256, "/boot/grub/grub.cfg": &intent.Ready.GRUBSHA256} {
		*target, err = nativeTrustedHash(ctx, path)
		if err != nil {
			return manifest, err
		}
	}
	intent.BootIntentSHA256, err = intent.Offline.Digest()
	if err != nil {
		return manifest, err
	}
	manifest = NativeReleaseManifest{SchemaVersion: 1, Release: binding, Host: observed}
	manifest.BootSHA256, err = intent.Digest()
	if err != nil {
		return manifest, err
	}
	plan, err := manifest.Plan()
	if err != nil {
		return manifest, err
	}
	if err := checkNativeRecoveryChoiceData(nativeBootJSON(choice), manifest, intent); err != nil {
		return manifest, err
	}
	if err := nativeBootCheckGRUB(ctx, plan); err != nil {
		return manifest, err
	}
	units, _ := NativeBootUnits(plan.OperationID)
	paths := []string{NativeRuntimePath, DefaultJournalDirectory + "/" + nativeRuntimeName, NativeReleaseManifestPath, "/srv/hosting", "/srv/stackfort-native-hosting", "/var/lib/stackfort-agent", nativeBootHook, nativeBootPremount, "/boot/grub/custom.cfg"}
	for name := range units {
		paths = append(paths, "/etc/systemd/system/"+name, "/etc/systemd/system/"+name+".d", "/etc/systemd/system/"+name+".requires", "/etc/systemd/system/"+name+".wants")
	}
	paths = append(paths, "/etc/systemd/system/nginx.service.requires")
	for _, name := range nativeBootConsumers() {
		paths = append(paths, "/etc/systemd/system/"+name+".d/90-stackfort-native-storage.conf")
	}
	for _, path := range paths {
		if err := nativeBootAbsent(path); err != nil {
			return manifest, err
		}
	}
	// Sealed orphan runtime blocks public installation even if a later write fails.
	if err := packagesGuard.check(ctx); err != nil {
		return manifest, err
	}
	if err := stage.SealNativeRuntime(ctx, manifest, intent, dispatcher); err != nil {
		return manifest, err
	}
	if _, err := stage.SealManifest(ctx, manifest); err != nil {
		return manifest, err
	}
	if err := gate.Close(ctx); err != nil {
		return manifest, err
	}
	if err := gate.VerifyClosed(ctx); err != nil {
		return manifest, err
	}
	for _, path := range []string{"/srv/stackfort-native-hosting", "/srv/hosting"} {
		if err := nativeBootMkdirMode(path, nativeHostingDirectoryMode); err != nil {
			return manifest, err
		}
	}
	if err := nativeBootMkdir("/var/lib/stackfort-agent"); err != nil {
		return manifest, err
	}
	for name, content := range units {
		if err := nativeBootCreate("/etc/systemd/system/"+name, []byte(content), 0644); err != nil {
			return manifest, err
		}
	}
	for _, name := range nativeBootConsumers() {
		dir := "/etc/systemd/system/" + name + ".d"
		if err := nativeBootDirectory(dir); err != nil {
			return manifest, err
		}
		if err := nativeBootCreate(dir+"/90-stackfort-native-storage.conf", []byte(NativeRuntimeConsumerDependency), 0644); err != nil {
			return manifest, err
		}
	}
	if _, err := runAdmissionCommand(ctx, "", "/usr/bin/systemctl", "daemon-reload"); err != nil {
		return manifest, err
	}
	if _, err := runAdmissionCommand(ctx, "", "/usr/bin/systemctl", "enable", "srv-hosting.mount", NativeRuntimeGateUnit, NativeRuntimeInstallUnit); err != nil {
		return manifest, err
	}
	if err := verifyNativeProfileUnits(ctx, plan.OperationID, intent.Profile); err != nil {
		return manifest, err
	}
	return manifest, packagesGuard.check(ctx)
}

func nativeBootConsumers() []string {
	return []string{"mariadb.service", "vinyl.service", "stackfort-agent.service", "stackfort-api.service", "stackfort-phpmyadmin.service", "stackfort-panel-renew.service"}
}

func nativeBootRead(path string) ([]byte, error) {
	dir, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	file, err := openResumeFile(dir, filepath.Base(path), 64<<10)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, (64<<10)+1))
}

func nativeBootAbsent(path string) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.Join(err, errors.New("refusing existing boot preparation path: "+path))
	}
	return nil
}

func nativeBootCreate(path string, data []byte, mode os.FileMode) error {
	if len(data) == 0 || len(data) > 64<<10 {
		return errors.New("invalid preparation artifact size")
	}
	dir, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, filepath.Base(path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return unix.Fsync(dir)
}

func nativeBootMkdir(path string) error {
	return nativeBootMkdirMode(path, 0755)
}

func nativeBootMkdirMode(path string, mode uint32) error {
	dir, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	return nativeBootMkdirAt(dir, filepath.Base(path), mode)
}

func nativeBootMkdirAt(parent int, name string, mode uint32) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || (mode != 0755 && mode != nativeHostingDirectoryMode) {
		return errors.New("invalid native boot directory contract")
	}
	if err := unix.Mkdirat(parent, name, 0700); err != nil {
		return err
	}
	dir, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	var metadata unix.Stat_t
	if err := unix.Fstat(dir, &metadata); err != nil || metadata.Uid != 0 || metadata.Gid != 0 || metadata.Mode&unix.S_IFMT != unix.S_IFDIR || metadata.Mode&0077 != 0 {
		return errors.Join(err, errors.New("new native boot directory ownership drift"))
	}
	// Mkdir honors the installer umask (normally 0077). Apply the exact mode
	// through the new directory FD; never chmod or adopt a pre-existing path.
	if err := unix.Fchmod(dir, mode); err != nil {
		return err
	}
	return errors.Join(unix.Fsync(dir), unix.Fsync(parent))
}
func nativeBootDirectory(path string) error {
	if err := nativeBootMkdir(path); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	dir, err := openSourceDirectory(path, false)
	if err == nil {
		_ = unix.Close(dir)
	}
	return err
}

// Private closed executable list and call sites; never accepts a command from
// a journal, environment, user input, or shell expansion. fsck accepts 0/1 only.
func nativeBootCommand(ctx context.Context, executable string, args ...string) (string, error) {
	allowed := []string{"/usr/sbin/e2fsck", "/usr/sbin/tune2fs", "/usr/sbin/debugfs", "/usr/sbin/mkinitramfs", "/usr/bin/lsinitramfs", "/usr/bin/grub-script-check", "/usr/bin/grub-editenv", "/usr/sbin/grub-reboot", "/usr/sbin/grub-probe", "/usr/bin/mount", "/usr/sbin/quotaon"}
	if ctx == nil || !slices.Contains(allowed, executable) {
		return "", errors.New("invalid boot command")
	}
	timeout := 15 * time.Second
	if executable == "/usr/sbin/mkinitramfs" || executable == "/usr/sbin/e2fsck" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- fixed private allowlisted executable/call sites, no shell.
	command := exec.CommandContext(ctx, executable, args...)
	containNativeCommand(command)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var stdout, stderr nativeBootOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return "", errors.New("native boot command output exceeds limit")
	}
	if err != nil && executable == "/usr/sbin/e2fsck" && command.ProcessState != nil && command.ProcessState.ExitCode() == 1 && ctx.Err() == nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("native boot %s: %w: %s", executable, err, stderr.String())
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return stdout.String(), nil
}

type nativeBootOutput struct {
	output   boundedOriginOutput
	overflow bool
}

func (output *nativeBootOutput) Write(data []byte) (int, error) {
	n, err := output.output.Write(data)
	if err != nil {
		output.overflow = true
	}
	return n, err
}
func (output *nativeBootOutput) String() string { return output.output.String() }

func nativeBootSuper(ctx context.Context, device string) (map[string]string, error) {
	text, err := nativeReadCommand(ctx, "/usr/sbin/tune2fs", "-l", device)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values, nil
}
func nativeBootHasQuota(values map[string]string) bool {
	inode, err := strconv.ParseUint(values["Project quota inode"], 10, 64)
	features := strings.Fields(values["Filesystem features"])
	return err == nil && inode > 0 && slices.Contains(features, "project") && slices.Contains(features, "quota")
}

func nativeBootCheckGRUB(ctx context.Context, plan storageprep.Plan) error {
	target, err := nativeReadCommand(ctx, "/usr/bin/findmnt", "-nro", "TARGET", "-T", "/boot/grub/grubenv")
	if err != nil || target != "/" {
		return errors.New("GRUB environment must be on root")
	}
	abstraction, err := nativeBootCommand(ctx, "/usr/sbin/grub-probe", "--target=abstraction", "/boot/grub/grubenv")
	if err != nil || strings.TrimSpace(abstraction) != "" {
		return errors.New("unsupported GRUB storage backend")
	}
	grub, err := nativeBootRead("/boot/grub/grub.cfg")
	if err != nil {
		return err
	}
	for _, expected := range []string{"set next_entry=\n   save_env next_entry", "set default=\"0\"", "source ${config_directory}/custom.cfg", "source $prefix/custom.cfg", "initrd.img-" + plan.Kernel} {
		if !bytes.Contains(grub, []byte(expected)) {
			return errors.New("unsupported GRUB policy")
		}
	}
	data, err := nativeBootRead("/boot/grub/grubenv")
	if err != nil {
		return err
	}
	env, err := storageprep.ParseGRUBEnvironment(data)
	if err != nil {
		return err
	}
	for _, key := range []string{"next_entry", "prev_saved_entry", "stackfort_native_armed", "stackfort_native_consumed"} {
		if env[key] != "" {
			return errors.New("conflicting GRUB selection")
		}
	}
	return nil
}

type nativeBootBackend struct {
	nativeReadyBackend
	stage *SourceStage
}

func (b nativeBootBackend) checkArtifacts(ctx context.Context) error {
	for path, digest := range map[string]string{"/boot/vmlinuz-" + b.plan.Kernel: b.spec.KernelSHA256, "/boot/initrd.img-" + b.plan.Kernel: b.spec.InitrdSHA256, "/boot/grub/grub.cfg": b.spec.GRUBSHA256} {
		actual, err := nativeTrustedHash(ctx, path)
		if err != nil || actual != digest {
			return errors.Join(err, errors.New("normal boot artifact changed: "+path))
		}
	}
	return nil
}

func (b nativeBootBackend) Arm(ctx context.Context, plan storageprep.Plan) error {
	if plan != b.plan || b.intent.Offline == nil {
		return errors.New("unbound native boot")
	}
	packagesGuard, err := acquireNativePackageGuard(ctx)
	if err != nil {
		return err
	}
	defer packagesGuard.close()
	if err := b.checkPackageBaseline(ctx); err != nil {
		return err
	}
	if err := b.checkArtifacts(ctx); err != nil {
		return err
	}
	if err := nativeBootCheckGRUB(ctx, plan); err != nil {
		return err
	}
	if err := verifyFile("/etc/fstab", []byte(b.intent.Offline.FstabBefore), 0, 0, 0644); err != nil {
		return err
	}
	device, _, err := nativeRootDevice(ctx)
	if err != nil {
		return err
	}
	if err := nativeBootCheckDevice(ctx, device, plan, b.spec, false); err != nil {
		return err
	}
	if err := nativeBootVerifyTools(ctx, *b.intent.Offline); err != nil {
		return err
	}
	id, _ := storageprep.GRUBEntryID(plan)
	imagePath := "/boot/" + id + ".img"
	for _, path := range []string{imagePath, "/boot/grub/custom.cfg", nativeBootHook, nativeBootPremount} {
		if err := nativeBootAbsent(path); err != nil {
			return err
		}
	}
	build, err := prepareNativeBootBuild(ctx, b.stage, plan, b.intent)
	if err != nil {
		return err
	}
	if _, err := nativeBootCommand(ctx, "/usr/sbin/mkinitramfs", "-d", filepath.Join(build.path, "config"), "-o", imagePath, plan.Kernel); err != nil {
		return err
	}
	if err := build.verify(ctx); err != nil {
		return err
	}
	listing, err := nativeBootCommand(ctx, "/usr/bin/lsinitramfs", imagePath)
	if err != nil {
		return err
	}
	for _, required := range []string{strings.TrimPrefix(nativeBootEmbeddedInstaller, "/"), strings.TrimPrefix(nativeBootEmbeddedManifest, "/"), strings.TrimPrefix(nativeBootEmbeddedRuntime, "/"), "scripts/local-premount/stackfort-native-quota", "usr/sbin/debugfs"} {
		if !slices.Contains(strings.Split(strings.TrimSpace(listing), "\n"), required) {
			return errors.New("incomplete one-shot initrd: " + required)
		}
	}
	if strings.Contains(listing, "stackfort-native-quota.test") {
		return errors.New("test executable in initrd")
	}
	entry, _ := nativeBootGRUBEntry(plan, b.intent)
	if err := nativeBootCreate("/boot/grub/custom.cfg", []byte(entry), 0644); err != nil {
		return err
	}
	if _, err := nativeBootCommand(ctx, "/usr/bin/grub-script-check", "/boot/grub/custom.cfg"); err != nil {
		return err
	}
	if err := b.checkArtifacts(ctx); err != nil {
		return err
	}
	imageHash, err := nativeTrustedHash(ctx, imagePath)
	if err != nil {
		return err
	}
	if err := nativeBootSync(imagePath); err != nil {
		return err
	}
	if err := writeOriginRecord(b.stage.dir, nativeBootArtifactsName, nativeBootJSON(map[string]string{"operation": plan.OperationID, "manifest": plan.ManifestDigest, "initrd": imageHash, "script": admissionDigest([]byte(entry))})); err != nil {
		return err
	}
	if err := packagesGuard.check(ctx); err != nil {
		return err
	}
	if _, err := nativeBootCommand(ctx, "/usr/bin/grub-editenv", "/boot/grub/grubenv", "set", "stackfort_native_armed="+plan.OperationID); err != nil {
		return err
	}
	if _, err := nativeBootCommand(ctx, "/usr/sbin/grub-reboot", id); err != nil {
		return err
	}
	if err := nativeBootSync("/boot/grub/grubenv"); err != nil {
		return err
	}
	data, err := nativeBootRead("/boot/grub/grubenv")
	if err != nil {
		return err
	}
	env, err := storageprep.ParseGRUBEnvironment(data)
	if err != nil || env["next_entry"] != id || env["stackfort_native_armed"] != plan.OperationID || env["stackfort_native_consumed"] != "" {
		return errors.New("one-shot boot selection not persisted")
	}
	return packagesGuard.check(ctx)
}

func (b nativeBootBackend) checkPackageBaseline(ctx context.Context) error {
	if b.intent.Offline.PrerequisitesSHA256 == "" {
		return nil // Historical qualification profile without a package receipt.
	}
	if b.stage == nil {
		return errors.New("missing native source stage")
	}
	record, exists, err := b.stage.readNativePrerequisites()
	if err != nil || !exists || record.After == nil || record.OperationID != b.plan.OperationID || admissionDigest(nativeBootJSON(record)) != b.intent.Offline.PrerequisitesSHA256 {
		return errors.Join(err, errors.New("missing completed package baseline"))
	}
	packages, err := nativeHostPackages(ctx)
	if err != nil || admissionDigest(nativeBootJSON(packages)) != record.After.PackagesSHA256 {
		return errors.Join(err, errors.New("packages changed after native preparation"))
	}
	return nil
}

func nativeBootSync(path string) error {
	dir, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	file, err := openResumeFile(dir, filepath.Base(path), 512<<20)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return err
	}
	return unix.Fsync(dir)
}

// Renames only a verified fixed artifact into private evidence, never overwrites.
func nativeBootRetire(path, name string) error {
	source, err := openSourceDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer unix.Close(source)
	destination, err := openSourceDirectory(DefaultJournalDirectory, false)
	if err != nil {
		return err
	}
	defer unix.Close(destination)
	file, err := openResumeFile(source, filepath.Base(path), 512<<20)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := unix.Renameat2(source, filepath.Base(path), destination, name, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := unix.Fsync(source); err != nil {
		return err
	}
	return unix.Fsync(destination)
}

func (b nativeBootBackend) VerifyBoot(ctx context.Context, plan storageprep.Plan, boot string) error {
	if plan != b.plan {
		return errors.New("boot plan mismatch")
	}
	if err := b.checkArtifacts(ctx); err != nil {
		return err
	}
	data, err := nativeBootRead(nativeBootProofPath)
	if err != nil {
		return err
	}
	var proof nativeBootProof
	if err := nativeBootDecode(data, &proof); err != nil {
		return err
	}
	if proof != (nativeBootProof{Operation: plan.OperationID, Manifest: plan.ManifestDigest, BootID: boot, Status: "converted"}) {
		return errors.New("missing exact current-boot conversion proof")
	}
	observed, err := b.Observe(ctx)
	if err != nil {
		return err
	}
	env, err := nativeBootRead("/boot/grub/grubenv")
	if err != nil {
		return err
	}
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}
	if err := storageprep.CheckBootAuthorization(storageprep.State{SchemaVersion: 1, Plan: plan, Phase: storageprep.AwaitingReboot, ArmAttempts: 1}, plan, observed, string(cmdline), env); err != nil {
		return err
	}
	data, exists, err := admissionReadAt(b.stage.dir, nativeBootArtifactsName)
	if err != nil || !exists {
		return errors.Join(err, errors.New("missing boot artifact receipt"))
	}
	var artifacts map[string]string
	if err := nativeBootDecode(data, &artifacts); err != nil {
		return err
	}
	if len(artifacts) != 4 || artifacts["operation"] != plan.OperationID || artifacts["manifest"] != plan.ManifestDigest {
		return errors.New("boot artifact receipt mismatch")
	}
	id, _ := storageprep.GRUBEntryID(plan)
	entry, _ := nativeBootGRUBEntry(plan, b.intent)
	if artifacts["script"] != admissionDigest([]byte(entry)) {
		return errors.New("boot script pin mismatch")
	}
	for path, key := range map[string]string{"/boot/" + id + ".img": "initrd", "/boot/grub/custom.cfg": "script"} {
		actual, err := nativeTrustedHash(ctx, path)
		if err != nil || actual != artifacts[key] {
			return errors.Join(err, errors.New("boot artifact drift: "+path))
		}
	}
	return nil
}

func (b nativeBootBackend) Resume(ctx context.Context, plan storageprep.Plan, boot string) error {
	// Revalidate proof immediately before any live mount/configuration changes.
	if err := b.VerifyBoot(ctx, plan, boot); err != nil {
		return err
	}
	device, block, err := nativeRootDevice(ctx)
	if err != nil {
		return err
	}
	if err := nativeBootCheckDevice(ctx, device, plan, b.spec, true); err != nil {
		return err
	}
	if err := verifyFile("/etc/fstab", []byte(b.intent.Offline.FstabBefore), 0, 0, 0644); err != nil {
		return err
	}
	if _, err := nativeBootCommand(ctx, "/usr/bin/mount", "-o", "remount,prjquota", "/"); err != nil {
		return err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	quota, err := quotastate.ReadProject(fd)
	if err != nil {
		return err
	}
	if !quota.Ready() {
		if _, err := nativeBootCommand(ctx, "/usr/sbin/quotaon", "-P", "/"); err != nil {
			return err
		}
	}
	if err := nativeVerifyQuotaMount(ctx, "/", block.Rdev); err != nil {
		return err
	}
	if err := verifyFile("/etc/fstab", []byte(b.intent.Offline.FstabBefore), 0, 0, 0644); err != nil {
		return err
	}
	if err := atomicWriteFile("/etc/fstab", []byte(b.intent.Offline.FstabAfter), 0, 0, 0644); err != nil {
		return err
	}
	if err := writeOriginRecord(b.stage.dir, "native-boot-completed.json", nativeBootJSON(nativeBootProof{Operation: plan.OperationID, Manifest: plan.ManifestDigest, BootID: boot, Status: "converted"})); err != nil {
		return err
	}
	id, _ := storageprep.GRUBEntryID(plan)
	for path, name := range map[string]string{"/boot/grub/custom.cfg": "retired-grub-script", "/boot/" + id + ".img": "retired-one-shot.img"} {
		if err := nativeBootRetire(path, name); err != nil {
			return err
		}
	}
	return nil
}

func RunNativeBoot(ctx context.Context, request NativeBootRequest, output io.Writer) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if ctx == nil || ctx.Err() != nil || output == nil || os.Geteuid() != 0 {
		return errors.New("native boot requires root and an active context")
	}
	if request.Action == "early" {
		return nativeBootEarly(ctx, request.OperationID, output)
	}
	gate, err := NewLinuxAdmissionGate(request.OperationID)
	if err != nil {
		return err
	}
	if err := gate.Close(ctx); err != nil {
		return err
	}
	if err := gate.VerifyClosed(ctx); err != nil {
		return err
	}
	stage, exists, err := openExistingSourceStage()
	if err != nil || !exists {
		return errors.Join(err, errors.New("missing sealed boot state"))
	}
	defer stage.Close()
	manifest, ready, err := stage.loadNativeRuntimeMode(ctx, request.OperationID, true)
	if err != nil {
		return err
	}
	state, exists, err := stage.journal.Load()
	if err != nil || !exists {
		return errors.Join(err, errors.New("missing boot journal"))
	}
	if request.Action == "arm" && state.Phase != storageprep.Planned && state.Phase != storageprep.AwaitingReboot {
		return errors.New("arm cannot resume, reset or adopt storage")
	}
	if request.Action == "finalize" && state.Phase == storageprep.Planned {
		return errors.New("finalize cannot arm storage")
	}
	if request.Action == "arm" {
		observed, err := ready.Observe(ctx)
		if err != nil || observed.BootID != ready.plan.PreviousBootID {
			return errors.New("arming requires the original boot")
		}
	}
	backend := nativeBootBackend{nativeReadyBackend: ready, stage: stage}
	// Exclude package writers while accepting current-boot conversion proof and
	// persisting/retiring boot state. Ready revalidation after installation must
	// not compare against the pre-install package inventory.
	var packagesGuard *nativePackageGuard
	if request.Action == "finalize" {
		packagesGuard, err = acquireNativePackageGuard(ctx)
		if err != nil {
			return err
		}
		defer packagesGuard.close()
		if state.Phase != storageprep.Ready {
			if err := backend.checkPackageBaseline(ctx); err != nil {
				return err
			}
		}
	}
	decision, err := stage.AdvanceManifest(ctx, manifest, backend)
	if err != nil {
		return err
	}
	if request.Action == "finalize" && !decision.Ready {
		return errors.New("storage is not ready; one-shot reboot required")
	}
	if packagesGuard != nil {
		if err := packagesGuard.check(ctx); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output, "NATIVE_BOOT action=%s phase=%s waiting=%t ready=%t\n", request.Action, decision.State.Phase, decision.Waiting, decision.Ready)
	return err
}
