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
	"slices"
	"time"

	"golang.org/x/sys/unix"
)

func (stage *SourceStage) readNativePrerequisites() (nativePrerequisiteRecord, bool, error) {
	var record nativePrerequisiteRecord
	data, exists, err := admissionReadAt(stage.dir, nativePrerequisiteName)
	if err != nil || !exists {
		return record, exists, err
	}
	if err := nativeBootDecode(data, &record); err != nil {
		return record, true, err
	}
	return record, true, record.validate()
}

func (stage *SourceStage) saveNativePrerequisites(record nativePrerequisiteRecord) error {
	if stage.check() != nil || stage.journal == nil {
		return errors.New("prerequisites require shared lock")
	}
	if err := record.validate(); err != nil {
		return err
	}
	previous, exists, err := stage.readNativePrerequisites()
	if err != nil {
		return err
	}
	if !exists {
		if record.Phase != "checking" {
			return errors.New("prerequisite journal must begin with checking")
		}
		return writeOriginRecord(stage.dir, nativePrerequisiteName, nativeBootJSON(record))
	}
	if previous.RecoveryChoiceSHA256 != record.RecoveryChoiceSHA256 || previous.OperationID != record.OperationID || previous.ReleaseSHA256 != record.ReleaseSHA256 || previous.InstallerSHA256 != record.InstallerSHA256 || string(nativeBootJSON(previous.Before)) != string(nativeBootJSON(record.Before)) {
		return errors.New("prerequisite journal cannot be rebound")
	}
	if !slices.Contains([]string{"checking", "applying"}, previous.Phase) || !slices.Contains([]string{"applying", "complete", "recovery-required"}, record.Phase) {
		return errors.New("prerequisite journal cannot retry or reset")
	}
	if previous.Phase == "applying" && string(nativeBootJSON(previous.Planned)) != string(nativeBootJSON(record.Planned)) {
		return errors.New("package transaction plan changed")
	}
	return atomicWriteFile(DefaultJournalDirectory+"/"+nativePrerequisiteName, nativeBootJSON(record), 0, 0, 0600)
}

// Called only by preparation under its shared lock and after release origin
// verification. Existing completed results are rechecked, ambiguous ones stop.
func (stage *SourceStage) ensureNativePrerequisites(ctx context.Context, binding ReleaseBinding, dispatcher string, choice nativeRecoveryChoice, packagesGuard *nativePackageGuard) (digest string, err error) {
	if stage.check() != nil || stage.journal == nil || ctx == nil || ctx.Err() != nil {
		return "", errors.New("prerequisite preparation requires active shared lock")
	}
	if err := packagesGuard.check(ctx); err != nil {
		return "", err
	}
	if err := choice.validate(); err != nil {
		return "", err
	}
	choiceData := nativeBootJSON(choice)
	if err := equalOriginRecord(stage.dir, nativeRecoveryChoiceName, choiceData); err != nil {
		return "", err
	}
	installerHash, err := nativeTrustedHash(ctx, dispatcher)
	if err != nil {
		return "", err
	}
	releaseHash := admissionDigest(nativeBootJSON(binding))
	record, exists, err := stage.readNativePrerequisites()
	if err != nil {
		return "", err
	}
	if exists && (record.RecoveryChoiceSHA256 != admissionDigest(choiceData) || record.OperationID != binding.Source.OperationID || record.ReleaseSHA256 != releaseHash || record.InstallerSHA256 != installerHash) {
		return "", errors.New("foreign prerequisite operation")
	}
	if exists {
		if err := checkNativeRecoveryPrerequisites(choiceData, record); err != nil {
			return "", err
		}
	}
	if exists && record.Phase != "complete" {
		return "", errors.New("interrupted prerequisite operation requires review; no automatic package retry or repair")
	}
	if !exists {
		if err := nativeBootAbsent(NativePrerequisiteInstaller); err != nil {
			return "", err
		}
	}
	report, err := inspectNativeHost(ctx, true)
	if err != nil {
		return "", err
	}
	if err := report.failure(); err != nil {
		return "", err
	}
	if report.Snapshot == nil {
		return "", errors.New("missing native host snapshot")
	}
	beforePackages, err := nativeHostPackages(ctx)
	if err != nil || admissionDigest(nativeBootJSON(beforePackages)) != report.Snapshot.PackagesSHA256 {
		return "", errors.Join(err, errors.New("package inventory changed during inspection"))
	}
	if exists {
		if !report.PrerequisitesReady || string(nativeBootJSON(record.After)) != string(nativeBootJSON(report.Snapshot)) {
			return "", errors.New("completed prerequisite state changed")
		}
		return admissionDigest(nativeBootJSON(record)), nil
	}
	record = nativePrerequisiteRecord{RecoveryChoiceSHA256: admissionDigest(choiceData), SchemaVersion: 1, OperationID: binding.Source.OperationID, ReleaseSHA256: releaseHash, InstallerSHA256: installerHash, Phase: "checking", Before: *report.Snapshot, Planned: map[string]string{}}
	if err := checkNativeRecoveryPrerequisites(choiceData, record); err != nil {
		return "", err
	}
	if err := stage.saveNativePrerequisites(record); err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			record.Phase = "recovery-required"
			record.After = nil
			err = errors.Join(err, stage.saveNativePrerequisites(record))
		}
	}()
	if len(report.MissingPackages) > 0 {
		if _, err = packagesGuard.apt(ctx, "-o", "DPkg::Lock::Timeout=0", "-o", "APT::Update::Error-Mode=any", "update"); err != nil {
			return "", err
		}
		var simulation string
		args := append([]string{"-s", "--no-remove", "--no-upgrade", "--no-install-recommends", "install"}, report.MissingPackages...)
		simulation, err = packagesGuard.apt(ctx, args...)
		if err != nil {
			return "", err
		}
		record.Planned, err = nativeAPTPlan(simulation, report.MissingPackages)
		if err != nil {
			return "", err
		}
		// Recheck all host conflicts and the package inventory after refreshing
		// indexes. The APT hook checks the inventory again while APT holds locks.
		var current NativeHostReport
		current, err = inspectNativeHost(ctx, true)
		if err != nil {
			return "", err
		}
		if err = current.failure(); err != nil {
			return "", err
		}
		if current.Snapshot == nil || string(nativeBootJSON(*current.Snapshot)) != string(nativeBootJSON(record.Before)) {
			return "", errors.New("host changed while planning prerequisites")
		}
		var binary []byte
		parent, openErr := openSourceDirectory(filepath.Dir(dispatcher), false)
		if openErr != nil {
			return "", openErr
		}
		file, openErr := openResumeFile(parent, filepath.Base(dispatcher), 64<<20)
		_ = unix.Close(parent)
		if openErr != nil {
			return "", openErr
		}
		binary, err = io.ReadAll(io.LimitReader(file, (64<<20)+1))
		_ = file.Close()
		if err != nil || len(binary) > 64<<20 || admissionDigest(binary) != installerHash {
			return "", errors.New("prerequisite executable changed")
		}
		if err = writeNativeExecutable(stage.dir, filepath.Base(NativePrerequisiteInstaller), binary); err != nil {
			return "", err
		}
		record.Phase = "applying"
		if err = stage.saveNativePrerequisites(record); err != nil {
			return "", err
		}
		policy := nativePrerequisitePolicy(record.OperationID)
		if err = nativeBootCreate("/usr/sbin/policy-rc.d", []byte(policy), 0755); err != nil {
			return "", err
		}
		hook := NativePrerequisiteInstaller + " native-prerequisite-check --operation-id=" + record.OperationID
		args = []string{"-o", "DPkg::Lock::Timeout=0", "-o", "DPkg::Pre-Install-Pkgs::=" + hook, "-o", "DPkg::Tools::Options::" + NativePrerequisiteInstaller + "::Version=2", "-o", "DPkg::Tools::Options::" + NativePrerequisiteInstaller + "::InfoFD=0", "--no-remove", "--no-upgrade", "--no-install-recommends", "-y", "install"}
		names := make([]string, 0, len(record.Planned))
		for name := range record.Planned {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			args = append(args, name+"="+record.Planned[name])
		}
		_, installErr := packagesGuard.apt(ctx, args...)
		// Cleanup only this exact operation-owned file, never an administrator's
		// policy. Process loss retains policy + journal for explicit review.
		cleanupErr := nativePrerequisiteRemovePolicy(policy)
		if err = errors.Join(installErr, cleanupErr); err != nil {
			return "", err
		}
	}
	post, err := inspectNativeHost(ctx, true)
	if err != nil {
		return "", err
	}
	if err := post.failure(); err != nil {
		return "", err
	}
	if !post.PrerequisitesReady || post.Snapshot == nil || !sameNativeHost(record.Before, *post.Snapshot) {
		return "", errors.New("prerequisites changed the boot layout or are incomplete")
	}
	packages, err := nativeHostPackages(ctx)
	if err != nil {
		return "", err
	}
	if err := checkNativePackageDelta(beforePackages, packages, record.Planned); err != nil {
		return "", err
	}
	if admissionDigest(nativeBootJSON(packages)) != post.Snapshot.PackagesSHA256 {
		return "", errors.New("post-install package inventory changed")
	}
	if err := packagesGuard.check(ctx); err != nil {
		return "", err
	}
	record.Phase = "complete"
	record.After = post.Snapshot
	if err := stage.saveNativePrerequisites(record); err != nil {
		return "", err
	}
	return admissionDigest(nativeBootJSON(record)), nil
}

func nativePrerequisitePolicy(operation string) string {
	return "#!/bin/sh\n# Stackfort prerequisite service guard: " + operation + "\ncase \"$1\" in quota|quota.service|quotarpc|quotarpc.service|nftables|nftables.service) exit 101;; esac\nexit 0\n"
}

func nativePrerequisiteRemovePolicy(expected string) error {
	dir, err := openSourceDirectory("/usr/sbin", false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	if err := verifyFile("/usr/sbin/policy-rc.d", []byte(expected), 0, 0, 0755); err != nil {
		return err
	}
	if err := unix.Unlinkat(dir, "policy-rc.d", 0); err != nil {
		return err
	}
	return unix.Fsync(dir)
}

func nativePrerequisiteAPT(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// #nosec G204 -- private fixed apt-get and validated literal package plans.
	command := exec.CommandContext(ctx, "/usr/bin/apt-get", args...)
	containNativeCommand(command)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "APT_LISTCHANGES_FRONTEND=none", "NEEDRESTART_MODE=l"}
	var out, stderr nativeHostOutput
	command.Stdout, command.Stderr = &out, &stderr
	err := command.Run()
	if err != nil || ctx.Err() != nil {
		return "", fmt.Errorf("native prerequisite APT: %w: %s", errors.Join(err, ctx.Err()), stderr.data)
	}
	if out.overflow || stderr.overflow {
		return "", errors.New("APT output exceeded limit")
	}
	return string(out.data), nil
}

// APT owns the dpkg locks while invoking this read-only child. Do not acquire
// the parent-held Stackfort lock or expose any mutation/reset command here.
func CheckNativePrerequisiteTransaction(ctx context.Context, operation string, input io.Reader) error {
	if ctx == nil || ctx.Err() != nil || input == nil || os.Geteuid() != 0 || !validSourceOperation(operation) || os.Getenv("APT_HOOK_INFO_FD") != "0" {
		return errors.New("invalid prerequisite hook invocation")
	}
	self, err := os.Executable()
	if err != nil || self != NativePrerequisiteInstaller {
		return errors.New("prerequisite hook must use sealed executable")
	}
	dir, err := openSourceDirectory(DefaultJournalDirectory, false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	data, exists, err := admissionReadAt(dir, nativePrerequisiteName)
	if err != nil || !exists {
		return errors.New("missing prerequisite transaction journal")
	}
	var record nativePrerequisiteRecord
	if err := nativeBootDecode(data, &record); err != nil {
		return err
	}
	if record.validate() != nil || record.OperationID != operation || record.Phase != "applying" || len(record.Planned) == 0 {
		return errors.New("prerequisite transaction is not authorized")
	}
	if record.RecoveryChoiceSHA256 != "" {
		choice, found, err := admissionReadAt(dir, nativeRecoveryChoiceName)
		if err != nil || !found {
			return errors.New("missing prerequisite recovery choice")
		}
		if err := checkNativeRecoveryPrerequisites(choice, record); err != nil {
			return err
		}
	}
	digest, err := nativeTrustedHash(ctx, NativePrerequisiteInstaller)
	if err != nil || digest != record.InstallerSHA256 {
		return errors.New("prerequisite dispatcher changed")
	}
	running, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	contents, err := io.ReadAll(io.LimitReader(running, (64<<20)+1))
	_ = running.Close()
	if err != nil || len(contents) > 64<<20 || admissionDigest(contents) != record.InstallerSHA256 {
		return errors.New("running prerequisite dispatcher changed")
	}
	packages, err := nativeHostPackages(ctx)
	if err != nil {
		return err
	}
	if admissionDigest(nativeBootJSON(packages)) != record.Before.PackagesSHA256 {
		return errors.New("package database changed since prerequisite planning")
	}
	// The package-manager gap must not silently accept a changed kernel/initrd,
	// GRUB configuration or filesystem identity either. This runs under APT's
	// own locks, so it must not acquire the parent's installer/package guards.
	snapshot, err := nativeHostLayout(ctx, packages)
	if err != nil || string(nativeBootJSON(snapshot)) != string(nativeBootJSON(record.Before)) {
		return errors.Join(err, errors.New("boot layout changed since prerequisite planning"))
	}
	data, err = io.ReadAll(io.LimitReader(input, (256<<10)+1))
	if err != nil {
		return err
	}
	return checkNativeAPTTransaction(string(data), record.Planned)
}

func verifyNativePrerequisiteBinding(ctx context.Context, manifest NativeReleaseManifest, intent NativeRuntimeIntent) error {
	if intent.Offline == nil || intent.Offline.PrerequisitesSHA256 == "" {
		return nil
	} // historical qualification profiles
	data, err := nativeBootRead(DefaultJournalDirectory + "/" + nativePrerequisiteName)
	if err != nil {
		return err
	}
	if err := checkNativePrerequisiteData(data, manifest, intent); err != nil {
		return err
	}
	if intent.Offline.RecoveryChoiceSHA256 != "" {
		choice, err := nativeBootRead(DefaultJournalDirectory + "/" + nativeRecoveryChoiceName)
		if err != nil {
			return err
		}
		if err := checkNativeRecoveryChoiceData(choice, manifest, intent); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func checkNativePrerequisiteData(data []byte, manifest NativeReleaseManifest, intent NativeRuntimeIntent) error {
	if intent.Offline == nil || intent.Offline.PrerequisitesSHA256 == "" {
		return errors.New("missing prerequisite boot binding")
	}
	var record nativePrerequisiteRecord
	if err := nativeBootDecode(data, &record); err != nil {
		return err
	}
	if record.validate() != nil || record.RecoveryChoiceSHA256 != intent.Offline.RecoveryChoiceSHA256 || record.Phase != "complete" || admissionDigest(data) != intent.Offline.PrerequisitesSHA256 || record.OperationID != manifest.Release.Source.OperationID || record.ReleaseSHA256 != admissionDigest(nativeBootJSON(manifest.Release)) || record.Before.Host != manifest.Host || record.InstallerSHA256 != intent.InstallerSHA256 {
		return errors.New("prerequisite receipt differs from sealed boot")
	}
	return nil
}
