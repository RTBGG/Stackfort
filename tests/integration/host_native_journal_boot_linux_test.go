// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const journalGRUBScript = "/boot/grub/custom.cfg"
const journalProofPath = "/run/initramfs/stackfort-native-journal-proof.json"

type journalBootManifest struct {
	Native                                               nativeIntent
	BinaryDigest, KernelDigest, InitrdDigest, GRUBDigest string
	Mode                                                 string                            // Lab fault injection only: convert, reject, or lost-proof.
	Release                                              *installapply.ReleaseBinding      `json:",omitempty"`
	Install                                              bool                              `json:",omitempty"`
	InstallFault                                         string                            `json:",omitempty"`
	OperatorDigest                                       string                            `json:",omitempty"`
	Runtime                                              *installapply.NativeRuntimeIntent `json:",omitempty"`
}

func journalManifest(t *testing.T, path string) (journalBootManifest, storageprep.Plan) {
	t.Helper()
	data := imageRead(t, path)
	var manifest journalBootManifest
	if len(data) > 64<<10 {
		t.Fatal("manifest too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.MarshalIndent(manifest, "", "  ")
	// nativeJSON writes indented JSON without a trailing newline.
	if err != nil || !bytes.Equal(data, canonical) {
		t.Fatal("noncanonical boot manifest")
	}
	if manifest.Mode != "convert" && manifest.Mode != "reject" && manifest.Mode != "lost-proof" {
		t.Fatal("invalid lab mode")
	}
	if manifest.InstallFault != "" && (manifest.InstallFault != "pause-services-once" || !manifest.Install) {
		t.Fatal("invalid installation lab fault")
	}
	n := manifest.Native
	plan := storageprep.Plan{OperationID: n.Operation, Version: "0.0.0-native-boot-lab", SourceDigest: manifest.BinaryDigest,
		Distribution: "debian", MachineID: n.VMUUID, RootUUID: n.UUID, PartitionUUID: n.PartUUID, PreviousBootID: n.BootBefore,
		Kernel: n.Kernel, ManifestDigest: nativeHash(data)}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if manifest.Release != nil {
		var err error
		plan, err = journalReleaseManifest(t, manifest).Plan()
		if err != nil {
			t.Fatal(err)
		}
	}
	return manifest, plan
}

func journalReleaseManifest(t *testing.T, manifest journalBootManifest) installapply.NativeReleaseManifest {
	t.Helper()
	if manifest.Release == nil {
		t.Fatal("missing release binding")
	}
	binding := *manifest.Release
	manifest.Release = nil
	runtime := manifest.Runtime
	manifest.Runtime = nil
	boot, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	n := manifest.Native
	if n.Operation != binding.Source.OperationID {
		t.Fatal("release/boot operation mismatch")
	}
	bootDigest := nativeHash(boot)
	if runtime != nil {
		if runtime.BootIntentSHA256 != bootDigest {
			t.Fatal("runtime lost offline capsule binding")
		}
		bootDigest, err = runtime.Digest()
		if err != nil {
			t.Fatal(err)
		}
	}
	return installapply.NativeReleaseManifest{SchemaVersion: 1, Release: binding, BootSHA256: bootDigest,
		Host: storageprep.Observation{MachineID: n.VMUUID, RootUUID: n.UUID, PartitionUUID: n.PartUUID, BootID: n.BootBefore, Kernel: n.Kernel}}
}

func journalObservation(t *testing.T, n nativeIntent) storageprep.Observation {
	t.Helper()
	return storageprep.Observation{MachineID: strings.ToLower(strings.TrimSpace(string(imageRead(t, "/sys/class/dmi/id/product_uuid")))),
		RootUUID: n.UUID, PartitionUUID: n.PartUUID, BootID: strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/random/boot_id"))),
		Kernel: strings.TrimSpace(string(imageRead(t, "/proc/sys/kernel/osrelease")))}
}

func TestDisposableNativeJournalPrepare(t *testing.T) {
	requireNativeLab(t)
	withInstallation := os.Getenv("STACKFORT_NATIVE_JOURNAL_INSTALL") == "1"
	if withInstallation && os.Getenv("STACKFORT_NATIVE_JOURNAL_RELEASE") != "1" {
		t.Fatal("real installation requires the authenticated release path")
	}
	for _, path := range []string{storageprep.JournalPath, journalGRUBScript} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("existing journal boot state", path)
		}
	}
	if imageCommand(t, "/usr/bin/findmnt", "-nro", "TARGET", "-T", "/boot/grub/grubenv") != "/" ||
		imageCommand(t, "/usr/sbin/grub-probe", "--target=abstraction", "/boot/grub/grubenv") != "" {
		t.Fatal("GRUB environment must be on plain native root")
	}
	grub := imageRead(t, "/boot/grub/grub.cfg")
	if !bytes.Contains(grub, []byte("set next_entry=\n   save_env next_entry")) || !bytes.Contains(grub, []byte("set default=\"0\"")) {
		t.Fatal("unsupported GRUB next_entry/default policy")
	}
	if !bytes.Contains(grub, []byte("source ${config_directory}/custom.cfg")) || !bytes.Contains(grub, []byte("source $prefix/custom.cfg")) {
		t.Fatal("default GRUB lacks the qualified supplemental menu loader")
	}
	env, err := storageprep.ParseGRUBEnvironment(imageRead(t, "/boot/grub/grubenv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"next_entry", "prev_saved_entry", "stackfort_native_armed", "stackfort_native_consumed"} {
		if env[key] != "" {
			t.Fatal("conflicting GRUB environment", key)
		}
	}
	// Fixed cold-host fixture setup only after the read-only boot checks.
	// The default initrd is never rebuilt here.
	TestDisposableNativeQuotaPrepare(t)
	n := nativeState(t, nativeLab+"/intent.json")
	mode := os.Getenv("STACKFORT_NATIVE_JOURNAL_MODE")
	if mode == "" {
		mode = "convert"
	}
	manifest := journalBootManifest{Native: n, BinaryDigest: nativeHash(imageRead(t, nativeLab+"/probe.test")),
		KernelDigest: nativeHash(imageRead(t, "/boot/vmlinuz-"+n.Kernel)), InitrdDigest: nativeHash(imageRead(t, "/boot/initrd.img-"+n.Kernel)), GRUBDigest: nativeHash(grub), Mode: mode, Install: withInstallation, InstallFault: os.Getenv("STACKFORT_NATIVE_INSTALL_FAULT")}
	if withInstallation {
		// Current CLI is a separately built, sealed lab artifact, not a replacement
		// for any authenticated candidate executable.
		operator := imageRead(t, "/tmp/native-operator")
		imageWrite(t, nativeLab+"/operator", operator, 0700)
		manifest.OperatorDigest = nativeHash(operator)
	}
	if os.Getenv("STACKFORT_NATIVE_JOURNAL_RUNTIME") == "1" {
		if !withInstallation || manifest.InstallFault != "" {
			t.Fatal("runtime qualification requires installation without test-only in-process fault hooks")
		}
		capsule, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		manifest.Runtime = &installapply.NativeRuntimeIntent{SchemaVersion: 1, Profile: "debian-native-post-ready-qualification-v1",
			InstallerSHA256: manifest.OperatorDigest, BootIntentSHA256: nativeHash(capsule), Ready: installapply.NativeReadySpec{
				Features: n.Features, Blocks: n.Blocks, BlockSize: n.BlockSize, InodeSize: n.InodeSize,
				FstabSHA256: nativeHash([]byte(n.FstabAfter)), KernelSHA256: manifest.KernelDigest, InitrdSHA256: manifest.InitrdDigest, GRUBSHA256: manifest.GRUBDigest}}
	}
	var sourceStage *installapply.SourceStage
	if os.Getenv("STACKFORT_NATIVE_JOURNAL_RELEASE") == "1" {
		var err error
		sourceStage, err = installapply.OpenSourceStage()
		if err != nil {
			t.Fatal(err)
		}
		defer sourceStage.Close()
		pin, err := sourceStage.Prepare(t.Context(), "/var/tmp/stackfort-origin-lab/stackfort-0.1.0-beta.3-linux-amd64", n.Operation)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := sourceStage.BindOrigin(t.Context(), pin, installapply.OriginPolicy{Class: "lab-candidate", Version: "0.1.0-beta.3", Commit: "5282946bec1f865de7222128a6a5d0d8a656f34c"},
			"/var/tmp/stackfort-origin-lab/archive.tar.gz", "/var/tmp/stackfort-origin-lab/attestations.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		manifest.Release = &binding
	}
	nativeJSON(t, nativeLab+"/journal-manifest.json", manifest)
	_, plan := journalManifest(t, nativeLab+"/journal-manifest.json")
	imageWrite(t, nativeLab+"/grub.before", grub, 0600)
	imageWrite(t, nativeLab+"/grubenv.before", imageRead(t, "/boot/grub/grubenv"), 0600)
	// Replace only this fixture's synthetic consumer resumer, before arming.
	unit := "[Unit]\nDescription=Stackfort journaled native quota lab resume\nAfter=local-fs.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1\nExecStart=" + nativeLab + "/probe.test -test.v -test.run=^TestDisposableNativeJournalResume$\nTimeoutStartSec=120\n"
	nativeAtomic(t, "/etc/systemd/system/"+nativeResume, []byte(unit), 0644)
	imageCommand(t, "/usr/bin/systemctl", "daemon-reload")
	if sourceStage != nil {
		if manifest.Runtime != nil {
			if err := sourceStage.SealNativeRuntime(t.Context(), journalReleaseManifest(t, manifest), *manifest.Runtime, nativeLab+"/operator"); err != nil {
				t.Fatal(err)
			}
		}
		sealed, err := sourceStage.SealManifest(t.Context(), journalReleaseManifest(t, manifest))
		if err != nil || sealed != plan {
			t.Fatalf("seal authenticated source/boot plan: %v", err)
		}
		if manifest.Install {
			journalPrepareInstallation(t)
		}
		t.Log("JOURNAL_BOOT authenticated candidate, source and boot intent sealed under one installer lock")
		return
	}
	store, err := storageprep.OpenFileStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(storageprep.State{SchemaVersion: storageprep.SchemaVersion, Plan: plan, Phase: storageprep.Planned}); err != nil {
		t.Fatal(err)
	}
	t.Log("JOURNAL_BOOT planned; normal initrd unchanged; no boot selected")
}

type journalLabBackend struct {
	t        *testing.T
	manifest journalBootManifest
}

func (b journalLabBackend) Observe(context.Context) (storageprep.Observation, error) {
	t := b.t
	n := b.manifest.Native
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, n)
	if nativeState(t, nativeLab+"/intent.json") != n {
		return storageprep.Observation{}, errors.New("fixture intent differs from immutable manifest")
	}
	if nativeHash(imageRead(t, nativeLab+"/probe.test")) != b.manifest.BinaryDigest {
		return storageprep.Observation{}, errors.New("pinned helper changed")
	}
	if nativeHash(imageRead(t, "/boot/vmlinuz-"+n.Kernel)) != b.manifest.KernelDigest || nativeHash(imageRead(t, "/boot/initrd.img-"+n.Kernel)) != b.manifest.InitrdDigest {
		return storageprep.Observation{}, errors.New("normal boot artifacts changed")
	}
	return journalObservation(t, n), nil
}

func (b journalLabBackend) Arm(_ context.Context, plan storageprep.Plan) error {
	t := b.t
	n := b.manifest.Native
	id, err := storageprep.GRUBEntryID(plan)
	if err != nil {
		return err
	}
	entry, err := storageprep.RenderGRUBEntry(plan)
	if err != nil {
		return err
	}
	if nativeHash(imageRead(t, "/boot/grub/grub.cfg")) != b.manifest.GRUBDigest {
		return errors.New("GRUB changed before arming")
	}
	if _, err := os.Lstat("/boot/" + id + ".img"); !errors.Is(err, os.ErrNotExist) {
		return errors.New("existing one-shot image")
	}
	hook := "#!/bin/sh\nset -eu\ncase ${1:-} in prereqs) exit 0;; esac\n. /usr/share/initramfs-tools/hook-functions\ncopy_exec /usr/sbin/tune2fs\ncopy_exec /usr/sbin/e2fsck\ncopy_exec /usr/sbin/blkid\ncopy_exec /usr/sbin/debugfs\ncopy_file executable " + nativeLab + "/probe.test /stackfort-native-quota.test\ncopy_file config " + nativeLab + "/intent.json /stackfort-native-quota.json\ncopy_file config " + nativeLab + "/journal-manifest.json /stackfort-native-journal.json\n"
	premount := "#!/bin/sh\ncase ${1:-} in prereqs) exit 0;; esac\n. /scripts/functions\nmkdir -p /run/initramfs\nSTACKFORT_NATIVE_QUOTA_EARLY=1 /stackfort-native-quota.test -test.v -test.timeout=15m -test.run='^TestDisposableNativeJournalEarly$' > /run/initramfs/stackfort-native-quota.log 2>&1\nresult=$?\nif [ $result -ne 0 ] && [ -e /run/initramfs/stackfort-native-quota-mutating ]; then\n  while :; do panic 'Stackfort lab metadata operation uncertain; boot recovery required'; done\nfi\nexit 0\n"
	// Make early failures visible on the virtual console as well as in RAM.
	premount = strings.Replace(premount, "result=$?\n", "result=$?\ncat /run/initramfs/stackfort-native-quota.log > /dev/kmsg\n", 1)
	nativeAtomic(t, nativeHook, []byte(hook), 0755)
	nativeAtomic(t, nativePremount, []byte(premount), 0755)
	imageCommand(t, "/usr/sbin/mkinitramfs", "-o", "/boot/"+id+".img", n.Kernel)
	listing := imageCommand(t, "/usr/bin/lsinitramfs", "/boot/"+id+".img")
	for _, required := range []string{"stackfort-native-journal.json", "scripts/local-premount/stackfort-native-quota", "usr/sbin/debugfs", "stackfort-native-quota.test"} {
		if !strings.Contains(listing, required) {
			return errors.New("one-shot initrd incomplete")
		}
	}
	// Remove the build-only hooks so future normal kernel rebuilds stay clean.
	for _, path := range []string{nativeHook, nativePremount} {
		if err := os.Rename(path, nativeLab+"/retired-"+filepath.Base(filepath.Dir(path))+"-"+filepath.Base(path)); err != nil {
			return err
		}
	}
	imageWrite(t, journalGRUBScript, []byte(entry), 0644)
	imageCommand(t, "/usr/bin/grub-script-check", journalGRUBScript)
	if nativeHash(imageRead(t, "/boot/grub/grub.cfg")) != b.manifest.GRUBDigest {
		return errors.New("normal GRUB changed while staging supplemental menu")
	}
	nativeJSON(t, nativeLab+"/journal-artifacts.json", map[string]string{"initrd": nativeHash(imageRead(t, "/boot/"+id+".img")), "script": nativeHash(imageRead(t, journalGRUBScript)), "grub": nativeHash(imageRead(t, "/boot/grub/grub.cfg"))})
	imageCommand(t, "/usr/bin/grub-editenv", "/boot/grub/grubenv", "set", "stackfort_native_armed="+plan.OperationID)
	imageCommand(t, "/usr/sbin/grub-reboot", id)
	file, err := os.OpenFile("/boot/grub/grubenv", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	_ = file.Close()
	if err != nil {
		return err
	}
	env, err := storageprep.ParseGRUBEnvironment(imageRead(t, "/boot/grub/grubenv"))
	if err != nil {
		return err
	}
	if env["next_entry"] != id || env["stackfort_native_armed"] != plan.OperationID {
		return errors.New("one-shot selection not persisted")
	}
	t.Log("JOURNAL_BOOT staged separate initrd and one-shot entry; normal initrd unchanged")
	return nil
}

func (b journalLabBackend) VerifyBoot(_ context.Context, plan storageprep.Plan, boot string) error {
	t := b.t
	proof, err := os.ReadFile(journalProofPath)
	if err != nil {
		return err
	}
	var result map[string]string
	if err := json.Unmarshal(proof, &result); err != nil {
		return err
	}
	if result["operation"] != plan.OperationID || result["manifest"] != plan.ManifestDigest || result["bootID"] != boot || result["status"] != "converted" {
		return errors.New("wrong current-boot proof")
	}
	if err := storageprep.CheckBootAuthorization(storageprep.State{SchemaVersion: storageprep.SchemaVersion, Plan: plan, Phase: storageprep.AwaitingReboot, ArmAttempts: 1}, plan, journalObservation(t, b.manifest.Native), string(imageRead(t, "/proc/cmdline")), imageRead(t, "/boot/grub/grubenv")); err != nil {
		return err
	}
	var artifacts map[string]string
	if err := json.Unmarshal(imageRead(t, nativeLab+"/journal-artifacts.json"), &artifacts); err != nil {
		return err
	}
	id, _ := storageprep.GRUBEntryID(plan)
	for path, key := range map[string]string{"/boot/" + id + ".img": "initrd", journalGRUBScript: "script", "/boot/grub/grub.cfg": "grub"} {
		if nativeHash(imageRead(t, path)) != artifacts[key] {
			return errors.New("staged boot artifact changed")
		}
	}
	return nil
}

func (b journalLabBackend) Resume(_ context.Context, plan storageprep.Plan, _ string) error {
	t := b.t
	TestDisposableNativeQuotaResume(t)
	// Retain immutable evidence, but retire the selectable preparation entry.
	id, _ := storageprep.GRUBEntryID(plan)
	for path, destination := range map[string]string{journalGRUBScript: nativeLab + "/retired-grub-script", "/boot/" + id + ".img": nativeLab + "/retired-one-shot.img"} {
		if err := os.Rename(path, destination); err != nil {
			return err
		}
	}
	if nativeHash(imageRead(t, "/boot/grub/grub.cfg")) != b.manifest.GRUBDigest {
		return errors.New("normal GRUB configuration not restored exactly")
	}
	return nil
}

func (b journalLabBackend) VerifyReady(_ context.Context, _ storageprep.Plan) error {
	t := b.t
	n := b.manifest.Native
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, n)
	if !nativeReady(nativeSuperblock(t, device)) || !nativeQuotaState(t).Ready() {
		return errors.New("quota accounting/enforcement not ready")
	}
	if string(imageRead(t, "/etc/fstab")) != n.FstabAfter || nativeHash(imageRead(t, nativeLab+"/sentinel")) != n.SentinelHash {
		return errors.New("persisted hosting state changed")
	}
	if nativeHash(imageRead(t, "/boot/grub/grub.cfg")) != b.manifest.GRUBDigest {
		return errors.New("unexpected normal GRUB state")
	}
	for _, path := range []string{nativeHook, nativePremount, journalGRUBScript} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("preparation hook still installed")
		}
	}
	return nil
}

func journalAdvance(t *testing.T) storageprep.Decision {
	t.Helper()
	manifest, plan := journalManifest(t, nativeLab+"/journal-manifest.json")
	self, err := os.Executable()
	if err != nil || nativeHash(imageRead(t, self)) != manifest.BinaryDigest {
		t.Fatal("caller differs from pinned helper; restart the experiment from its fresh checkpoint")
	}
	if manifest.Release != nil {
		stage, err := installapply.OpenSourceStage()
		if err != nil {
			t.Fatal(err)
		}
		defer stage.Close()
		decision, err := stage.AdvanceManifest(t.Context(), journalReleaseManifest(t, manifest), journalLabBackend{t, manifest})
		if err != nil {
			t.Fatal(err)
		}
		return decision
	}
	store, err := storageprep.OpenFileStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	decision, err := storageprep.Advance(t.Context(), store, journalLabBackend{t, manifest}, plan)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func TestDisposableNativeJournalArm(t *testing.T) {
	requireNativeLab(t)
	if result := journalAdvance(t); !result.Waiting {
		t.Fatalf("not awaiting reboot: %+v", result)
	}
}

func TestDisposableNativeJournalResume(t *testing.T) {
	requireNativeLab(t)
	if result := journalAdvance(t); !result.Ready {
		t.Fatalf("not ready: %+v", result)
	}
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	if manifest.Install {
		// Package completion alone no longer admits public traffic. The separate
		// coordinator keeps its early-boot network gate closed until full health.
		if err := journalAdmissionGate(t).VerifyClosed(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("JOURNAL_BOOT journal ready; native quota enforced; normal boot restored")
}

type boundedJournalOutput struct{ buffer bytes.Buffer }

func (output *boundedJournalOutput) Write(data []byte) (int, error) {
	if output.buffer.Len()+len(data) > 64<<10 {
		return 0, errors.New("offline journal output exceeds limit")
	}
	return output.buffer.Write(data)
}

func (output *boundedJournalOutput) Bytes() []byte  { return output.buffer.Bytes() }
func (output *boundedJournalOutput) String() string { return output.buffer.String() }

func journalOfflineRead(t *testing.T, device, path string) []byte {
	t.Helper()
	if path != storageprep.JournalPath && path != "/boot/grub/grubenv" && path != installapply.NativeReleaseManifestPath &&
		path != "/var/lib/stackfort-installer/resume-origin/receipt.json" && path != "/var/lib/stackfort-installer/resume-source/receipt.json" {
		t.Fatal("offline path not allowed")
	}
	// No -w, no mount, no repair option: read the existing ext4 file only.
	command := exec.CommandContext(t.Context(), "/usr/sbin/debugfs", "-D", "-R", "cat "+path, device)
	var output boundedJournalOutput
	command.Stdout = &output
	if err := command.Run(); err != nil {
		t.Fatal("offline journal read", err)
	}
	return output.Bytes()
}

func TestDisposableNativeJournalEarly(t *testing.T) {
	if os.Getenv("STACKFORT_NATIVE_QUOTA_EARLY") != "1" {
		t.Skip("dedicated one-shot initramfs only")
	}
	manifest, plan := journalManifest(t, "/stackfort-native-journal.json")
	if nativeState(t, "/stackfort-native-quota.json") != manifest.Native {
		t.Fatal("embedded conversion intent differs from manifest")
	}
	device, stat := nativeDevice(t, os.Getenv("ROOT"))
	var root unix.Statfs_t
	if err := unix.Statfs("/", &root); err != nil || (root.Type != unix.TMPFS_MAGIC && root.Type != unix.RAMFS_MAGIC) {
		t.Fatal("not initramfs")
	}
	majorMinor := fmt.Sprintf("%d:%d", unix.Major(uint64(stat.Rdev)), unix.Minor(uint64(stat.Rdev)))
	for _, line := range strings.Split(string(imageRead(t, "/proc/self/mountinfo")), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && fields[2] == majorMinor {
			t.Fatal("offline target already mounted")
		}
	}
	nativeIdentity(t, device, manifest.Native)
	state, err := storageprep.DecodeState(journalOfflineRead(t, device, storageprep.JournalPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := storageprep.CheckBootAuthorization(state, plan, journalObservation(t, manifest.Native), string(imageRead(t, "/proc/cmdline")), journalOfflineRead(t, device, "/boot/grub/grubenv")); err != nil {
		t.Fatal(err)
	}
	if manifest.Release != nil {
		for path, expected := range map[string]any{installapply.NativeReleaseManifestPath: journalReleaseManifest(t, manifest),
			"/var/lib/stackfort-installer/resume-origin/receipt.json": *manifest.Release,
			"/var/lib/stackfort-installer/resume-source/receipt.json": manifest.Release.Source} {
			content, err := json.MarshalIndent(expected, "", "  ")
			if err != nil || !bytes.Equal(journalOfflineRead(t, device, path), append(content, '\n')) {
				t.Fatal("offline release binding differs from embedded manifest")
			}
		}
		t.Log("JOURNAL_BOOT offline origin/source/boot manifest binding verified")
	}
	if nativeHash(imageRead(t, "/stackfort-native-quota.test")) != manifest.BinaryDigest {
		t.Fatal("embedded helper digest mismatch")
	}
	if nativeReady(nativeSuperblock(t, device)) {
		t.Fatal("refusing a second conversion attempt")
	}
	if manifest.Mode == "reject" {
		t.Fatal("JOURNAL_BOOT injected pre-write rejection after GRUB consumption")
	}
	t.Log("JOURNAL_BOOT offline journal awaiting-reboot and GRUB consumption verified before first filesystem write")
	TestDisposableNativeQuotaEarly(t)
	if manifest.Mode == "lost-proof" {
		if err := os.Remove(nativeBootResult); err != nil {
			t.Fatal(err)
		}
		t.Fatal("JOURNAL_BOOT injected lost success proof after completed conversion")
	}
	nativeJSON(t, journalProofPath, map[string]string{"operation": plan.OperationID, "manifest": plan.ManifestDigest, "bootID": journalObservation(t, manifest.Native).BootID, "status": "converted"})
}

func TestDisposableNativeJournalValidate(t *testing.T) {
	requireNativeLab(t)
	manifest, plan := journalManifest(t, nativeLab+"/journal-manifest.json")
	if manifest.Install {
		journalWaitInstallation(t)
	}
	if manifest.Release != nil {
		stage, err := installapply.OpenSourceStage()
		if err != nil {
			t.Fatal(err)
		}
		_, verifyErr := stage.VerifyManifest(t.Context(), journalReleaseManifest(t, manifest))
		_ = stage.Close()
		if verifyErr != nil {
			t.Fatal(verifyErr)
		}
	}
	store, err := storageprep.OpenFileStore()
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := store.Load()
	_ = store.Close()
	if err != nil || !exists || state.Phase != storageprep.Ready || state.Plan != plan {
		t.Fatalf("journal not ready: %+v %v", state, err)
	}
	if err := (journalLabBackend{t, manifest}).VerifyReady(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	TestDisposableNativeQuotaGuard(t)
	if manifest.Runtime != nil {
		imageCommand(t, "/usr/bin/systemctl", "is-active", installapply.NativeRuntimeVerifyUnit, nativeConsumer)
		if os.Getenv("STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT") == "1" {
			if imageCommand(t, "/usr/bin/systemctl", "show", "--property=ConditionResult", "--value", nativeResume) != "no" {
				t.Fatal("normal boot executed test resumer")
			}
		}
	} else {
		imageCommand(t, "/usr/bin/systemctl", "is-active", nativeResume, nativeConsumer)
	}
	if os.Getenv("STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT") == "1" {
		if strings.Contains(string(imageRead(t, "/proc/cmdline")), "stackfort.native-quota=") {
			t.Fatal("normal reboot reused preparation kernel command")
		}
		if _, err := os.Stat(journalProofPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("normal reboot ran preparation helper")
		}
	} else {
		if _, err := os.Stat(journalProofPath); err != nil {
			t.Fatal("initial conversion proof missing")
		}
	}
	for _, test := range []struct {
		name string
		fn   func(*testing.T)
	}{{"QuotasAndIsolation", TestDisposableHostProjectQuotaAndAccountIsolation}, {"OCIPrivateResources", TestDisposableHostOCIPrivateResources}, {"OCILifecycle", TestDisposableHostOCIDeploymentLifecycle}, {"ContainerSubUIDQuota", testContainerProjectQuota}} {
		t.Run(test.name, test.fn)
	}
	t.Log("JOURNAL_BOOT ready, default boot clean, quotas/OCI pass")
}

func TestDisposableNativeJournalRecovery(t *testing.T) {
	requireNativeLab(t)
	manifest, plan := journalManifest(t, nativeLab+"/journal-manifest.json")
	store, err := storageprep.OpenFileStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, exists, err := store.Load()
	if err != nil || !exists || state.Phase != storageprep.RecoveryRequired || state.FailureCode != "boot-evidence-invalid" {
		t.Fatalf("not safe recovery: %+v %v", state, err)
	}
	before := imageRead(t, storageprep.JournalPath)
	if _, err := storageprep.Advance(t.Context(), store, journalLabBackend{t, manifest}, plan); !errors.Is(err, storageprep.ErrRecoveryRequired) {
		t.Fatal("terminal recovery was retried", err)
	}
	if !bytes.Equal(before, imageRead(t, storageprep.JournalPath)) {
		t.Fatal("recovery evidence changed")
	}
	if err := exec.Command("/usr/bin/systemctl", "is-active", "--quiet", nativeConsumer).Run(); err == nil {
		t.Fatal("consumer running during recovery")
	}
	if string(imageRead(t, "/etc/fstab")) != manifest.Native.FstabBefore {
		t.Fatal("unproven conversion changed fstab")
	}
	device, _ := nativeDevice(t, imageCommand(t, "/usr/bin/findmnt", "-nro", "SOURCE", "/"))
	nativeIdentity(t, device, manifest.Native)
	if ready := nativeReady(nativeSuperblock(t, device)); ready != (manifest.Mode == "lost-proof") {
		t.Fatal("unexpected conversion state")
	}
	env, err := storageprep.ParseGRUBEnvironment(imageRead(t, "/boot/grub/grubenv"))
	if err != nil || env["next_entry"] != "" || env["stackfort_native_armed"] != "" || env["stackfort_native_consumed"] != plan.OperationID {
		t.Fatal("one-shot latch not consumed", err)
	}
	if os.Getenv("STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT") == "1" {
		if strings.Contains(string(imageRead(t, "/proc/cmdline")), "stackfort.native-quota=") {
			t.Fatal("recovery reboot selected converter")
		}
		if _, err := os.Stat("/run/initramfs/stackfort-native-quota.log"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("recovery reboot reran converter")
		}
	}
	t.Log("JOURNAL_BOOT recovery terminal, hosting blocked, one-shot consumed, no automatic retry")
}
