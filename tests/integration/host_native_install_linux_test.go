// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
)

const nativeInstallUnit = "stackfort-native-install.service"
const nativeAdmissionUnit = "stackfort-native-admission-gate.service"
const nativeInstallDropIn = "90-stackfort-native-storage.conf"
const nativeInstallDependency = "# Stackfort disposable native-installation lab only.\n[Unit]\nBindsTo=srv-hosting.mount\nAfter=srv-hosting.mount\n"

var nativeInstallConsumers = []string{"nginx.service", "mariadb.service", "vinyl.service", "stackfort-agent.service", "stackfort-api.service", "stackfort-phpmyadmin.service", "stackfort-panel-renew.service"}

func journalPrepareInstallation(t *testing.T) {
	t.Helper()
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	unit := "[Unit]\nDescription=Stackfort authenticated native installation lab\nWants=network-online.target\nRequires=srv-hosting.mount\nAfter=network-online.target srv-hosting.mount\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1\nExecStart=" + nativeLab + "/probe.test -test.v -test.timeout=18m -test.run=^TestDisposableNativeJournalInstall$\nTimeoutStartSec=20min\n\n[Install]\nWantedBy=multi-user.target\n"
	// Do not order after consumers: quarantine must be able to stop them from
	// ExecStopPost without waiting on this coordinator's own stop transaction.
	unit = strings.Replace(unit, "TimeoutStartSec=20min\n", "TimeoutStartSec=20min\nTimeoutStopSec=90s\nExecStopPost="+nativeLab+"/probe.test -test.v -test.run=^TestDisposableNativeAdmissionQuarantine$\n", 1)
	imageWrite(t, "/etc/systemd/system/"+nativeInstallUnit, []byte(unit), 0644)
	gateUnit := "[Unit]\nDescription=Stackfort native admission closed web gate lab\nDefaultDependencies=no\nAfter=local-fs.target\nBefore=network-pre.target\nWants=network-pre.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nEnvironment=STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1\nExecStart=" + nativeLab + "/probe.test -test.v -test.run=^TestDisposableNativeAdmissionClose$\nTimeoutStartSec=60s\n\n[Install]\nWantedBy=multi-user.target\n"
	imageWrite(t, "/etc/systemd/system/"+nativeAdmissionUnit, []byte(gateUnit), 0644)
	resumePath := "/etc/systemd/system/" + nativeResume
	resume := strings.Replace(string(imageRead(t, resumePath)), "[Unit]\n", "[Unit]\nRequires="+nativeAdmissionUnit+"\nAfter="+nativeAdmissionUnit+"\n", 1)
	nativeAtomic(t, resumePath, []byte(resume), 0644)
	// NGINX deliberately rejects foreign .conf drop-ins. Express its dependency
	// via the mount's standard RequiredBy link and reverse ordering instead.
	mountPath := "/etc/systemd/system/srv-hosting.mount"
	mount := string(imageRead(t, mountPath))
	if strings.Count(mount, "Before=umount.target\n") != 1 || strings.Contains(mount, "[Install]") {
		t.Fatal("unexpected lab mount template")
	}
	mount = strings.Replace(mount, "Before=umount.target\n", "Before=umount.target nginx.service\n", 1)
	nativeAtomic(t, mountPath, []byte(mount+"\n[Install]\nRequiredBy=nginx.service\n"), 0644)
	if manifest.Runtime != nil {
		units, err := installapply.NativeRuntimeUnits(manifest.Native.Operation)
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range units {
			path := "/etc/systemd/system/" + name
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				imageWrite(t, path, []byte(content), 0644)
			} else if err == nil {
				nativeAtomic(t, path, []byte(content), 0644)
			} else {
				t.Fatal(err)
			}
		}
		// Only the initial proof-bearing conversion boot invokes the lab resumer.
		// Normal boots, admission and quarantine execute the sealed real CLI.
		resume = strings.Replace(resume, "[Unit]\n", "[Unit]\nConditionPathExists="+journalProofPath+"\nBefore="+installapply.NativeRuntimeVerifyUnit+"\n", 1)
		nativeAtomic(t, resumePath, []byte(resume), 0644)
		directory := "/etc/systemd/system/" + installapply.NativeRuntimeVerifyUnit + ".requires"
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(resumePath, directory+"/"+nativeResume); err != nil {
			t.Fatal(err)
		}
		nativeAtomic(t, "/etc/systemd/system/"+nativeConsumer, []byte("[Unit]\nDescription=Stackfort runtime-qualified synthetic consumer\nRequires=srv-hosting.mount\nAfter=srv-hosting.mount\n\n[Service]\nExecStart=/usr/bin/sleep infinity\n\n[Install]\nWantedBy=multi-user.target\n"), 0644)
	}
	for _, name := range nativeInstallConsumers {
		if name == "nginx.service" {
			continue
		}
		directory := "/etc/systemd/system/" + name + ".d"
		if err := os.Mkdir(directory, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		imageWrite(t, filepath.Join(directory, nativeInstallDropIn), []byte(nativeInstallDependency), 0644)
	}
	imageCommand(t, "/usr/bin/systemctl", "daemon-reload")
	imageCommand(t, "/usr/bin/systemctl", "enable", "srv-hosting.mount")
	imageCommand(t, "/usr/bin/systemctl", "enable", nativeInstallUnit)
	imageCommand(t, "/usr/bin/systemctl", "enable", nativeAdmissionUnit)
	imageCommand(t, "/usr/bin/systemctl", "start", nativeAdmissionUnit)
	t.Log("NATIVE_INSTALL enabled separate post-mount continuation; managed consumers bound to verified hosting mount")
}

func journalCheckPackageBootGate(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(installapply.DefaultJournalPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil || len(data) > 64<<10 {
		t.Fatal("read package boot gate", err)
	}
	var journal installapply.Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.MarshalIndent(journal, "", "  ")
	manifest, plan := journalManifest(t, nativeLab+"/journal-manifest.json")
	if !bytes.Equal(data, append(canonical, '\n')) || journal.SchemaVersion != installapply.JournalSchemaVersion ||
		journal.Status != installapply.InstallComplete || journal.Version != plan.Version || journal.SourceDigest != plan.SourceDigest ||
		journal.Distribution != plan.Distribution || manifest.Release == nil || len(journal.Stages) != 9 {
		t.Fatal("incomplete or foreign package state blocks hosting on this boot; operator recovery required")
	}
	for _, stage := range journal.Stages {
		if stage.Status != installapply.StageComplete {
			t.Fatal("incomplete package stage blocks hosting")
		}
	}
	for index, expected := range []installapply.StageID{installapply.StagePackages, installapply.StageWAFPackage, installapply.StageVinylPackage,
		installapply.StageIdentity, installapply.StagePayload, installapply.StageConfiguration, installapply.StageSecurity, installapply.StageNGINX, installapply.StageServices} {
		if journal.Stages[index].ID != expected || journal.Stages[index].Attempts < 1 {
			t.Fatal("invalid package stage identity or attempt count")
		}
	}
	binding, _ := json.MarshalIndent(plan, "", "  ")
	if !bytes.Equal(imageRead(t, "/var/lib/stackfort-installer/native-install-binding.json"), append(binding, '\n')) {
		t.Fatal("package boot binding mismatch")
	}
}

func TestDisposableNativeJournalInstall(t *testing.T) {
	requireNativeLab(t)
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	self, err := os.Executable()
	if err != nil || !manifest.Install || manifest.Release == nil || nativeHash(imageRead(t, self)) != manifest.BinaryDigest {
		t.Fatal("installation requires the pinned release-bound lab manifest and helper")
	}
	TestDisposableNativeQuotaGuard(t)
	for _, unit := range nativeInstallConsumers {
		if unit == "nginx.service" {
			target, err := os.Readlink("/etc/systemd/system/nginx.service.requires/srv-hosting.mount")
			if err != nil || target != "/etc/systemd/system/srv-hosting.mount" {
				t.Fatal("NGINX mount dependency changed", err)
			}
			continue
		}
		if string(imageRead(t, "/etc/systemd/system/"+unit+".d/"+nativeInstallDropIn)) != nativeInstallDependency {
			t.Fatal("native consumer dependency changed", unit)
		}
	}
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	if _, err := os.Lstat(nativeLab + "/admission-recovery.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy recovery request must not be silently adopted", err)
	}
	inspection, err := stage.InspectAdmission()
	if err != nil {
		t.Fatal(err)
	}
	gate := journalAdmissionGate(t)
	result, err := stage.AdmitPendingInstallation(t.Context(), journalReleaseManifest(t, manifest), journalLabBackend{t, manifest}, admissionLabGate{gate, t, manifest.InstallFault, inspection.Exists}, os.Stdout)
	if err != nil {
		t.Fatal("NATIVE_INSTALL continuation failed", err)
	}
	if result.Status != installapply.InstallComplete || len(result.Stages) != 9 {
		t.Fatal("installation not complete", result)
	}
	if _, err := installapply.NewLinuxRunner(io.Discard); !errors.Is(err, storageprep.ErrNotQualified) {
		t.Fatal("public installer gate bypassed", err)
	}
	if _, _, err := installapply.NewFileStore().Load(); !errors.Is(err, storageprep.ErrNotQualified) {
		t.Fatal("public journal gate bypassed", err)
	}
	nativeJSON(t, nativeLab+"/installation-result.json", result)
	t.Logf("NATIVE_INSTALL real stages complete; version=%s changed=%t resumed=%t alreadyInstalled=%t", result.Version, result.Changed, result.Resumed, result.AlreadyInstalled)
}

func journalWaitInstallation(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Minute)
	for {
		state := imageCommand(t, "/usr/bin/systemctl", "show", "--property=ActiveState", "--value", nativeInstallUnit)
		if state == "active" {
			break
		}
		if state != "activating" || time.Now().After(deadline) {
			t.Fatal("native installation service did not complete", state)
		}
		t.Log("NATIVE_INSTALL waiting for actual package/service stages")
		time.Sleep(5 * time.Second)
	}
	journalCheckPackageBootGate(t)
	if err := journalAdmissionGate(t).VerifyOpen(t.Context()); err != nil {
		t.Fatal(err)
	}
	var result installapply.Result
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	var resultData []byte
	if manifest.Runtime != nil {
		log := imageCommand(t, "/usr/bin/journalctl", "-b", "-u", nativeInstallUnit, "--no-pager", "-o", "cat")
		for _, line := range strings.Split(log, "\n") {
			if data, found := strings.CutPrefix(line, installapply.NativeRuntimeResultPrefix); found {
				resultData = []byte(data)
			}
		}
		if strings.Contains(log, "=== RUN") || strings.Contains(log, "probe.test") {
			t.Fatal("runtime admission ran test code")
		}
	} else {
		resultData = imageRead(t, nativeLab+"/installation-result.json")
	}
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != installapply.InstallComplete {
		t.Fatal("missing completed install result")
	}
	if os.Getenv("STACKFORT_NATIVE_JOURNAL_NORMAL_BOOT") == "1" && (!result.AlreadyInstalled || result.Changed) {
		t.Fatal("normal boot reapplied installation", result)
	}
	for _, unit := range nativeInstallConsumers {
		property := "BindsTo"
		if unit == "nginx.service" {
			property = "Requires"
		}
		dependencies := strings.Fields(imageCommand(t, "/usr/bin/systemctl", "show", "--property="+property, "--value", unit))
		if !slices.Contains(dependencies, "srv-hosting.mount") {
			t.Fatal("consumer missing storage dependency", unit)
		}
		ordering := strings.Fields(imageCommand(t, "/usr/bin/systemctl", "show", "--property=After", "--value", unit))
		if !slices.Contains(ordering, "srv-hosting.mount") {
			t.Fatal("consumer missing mount ordering", unit)
		}
		if unit != "stackfort-panel-renew.service" {
			imageCommand(t, "/usr/bin/systemctl", "is-active", unit)
		}
	}
	t.Log("NATIVE_INSTALL journal complete; native services active and storage-bound; authenticated source still retained")
}

func TestDisposableNativeInstallBlockedBoot(t *testing.T) {
	requireNativeLab(t)
	journalCheckQuarantine(t)
	if strings.Contains(string(imageRead(t, "/proc/cmdline")), "stackfort.native-quota=") {
		t.Fatal("negative normal boot rearmed conversion")
	}
}

func journalCheckQuarantine(t *testing.T) {
	t.Helper()
	manifest, _ := journalManifest(t, nativeLab+"/journal-manifest.json")
	if !manifest.Install {
		t.Fatal("not an installation lab")
	}
	for _, unit := range append([]string{nativeInstallUnit}, nativeInstallConsumers...) {
		if err := exec.Command("/usr/bin/systemctl", "is-active", "--quiet", unit).Run(); err == nil {
			t.Fatal("consumer active despite rejected boot", unit)
		}
	}
	if err := journalAdmissionGate(t).VerifyClosed(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Log("NATIVE_ADMISSION web gate closed; installer and all checked managed services inactive; verified storage may remain mounted for operator recovery")
}

func TestNativeJournalOutputBound(t *testing.T) {
	var output boundedJournalOutput
	if _, err := io.WriteString(&output, strings.Repeat("x", (64<<10)+1)); err == nil {
		t.Fatal("offline reader output limit bypassed")
	}
}
