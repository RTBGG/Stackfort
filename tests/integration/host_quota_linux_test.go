// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/google/uuid"
)

const disposableHostOptIn = "STACKFORT_DISPOSABLE_HOST_TEST"

func TestDisposableHostProjectQuotaAndAccountIsolation(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	filesystem := hostcapabilities.NewInspector().InspectManagedFilesystem()
	if filesystem.Inspection.Status != agentprotocol.CapabilityAvailable ||
		filesystem.ProjectQuota.Status != agentprotocol.CapabilityAvailable {
		t.Fatalf("managed filesystem lacks project quota: %#v", filesystem)
	}

	first := disposableIdentity(t, availableManagedID(t, 590_000))
	second := disposableIdentity(t, availableManagedID(t, first.UID+1))
	for _, identity := range []hostingidentity.Spec{second, first} {
		identity := identity
		t.Cleanup(func() { cleanupIdentity(t, identity) })
	}

	identityReconciler := hostidentity.NewReconciler()
	for _, identity := range []hostingidentity.Spec{first, second} {
		if _, err := identityReconciler.Reconcile(t.Context(), identity); err != nil {
			t.Fatalf("reconcile identity %s: %v", identity.Username, err)
		}
	}

	filesystemReconciler := hostfilesystem.NewReconciler()
	firstStorage := hostingstorage.Spec{
		Identity: first, ProjectID: first.UID, ByteLimit: 2 << 20, InodeLimit: 32,
	}
	secondStorage := hostingstorage.Spec{Identity: second, ProjectID: second.UID}
	for _, storage := range []hostingstorage.Spec{firstStorage, secondStorage} {
		if _, err := filesystemReconciler.Reconcile(t.Context(), storage); err != nil {
			t.Fatalf("reconcile filesystem %s: %v", storage.Identity.Username, err)
		}
	}
	testAccountResourceLimits(t, first)

	if err := runAs(second, "/usr/bin/test", "-x", first.HomeDirectory); err == nil {
		t.Fatal("second hosting account can traverse the first account root")
	}
	escapeTarget := t.TempDir()
	escapeLink := filepath.Join(first.HomeDirectory, "escape")
	if err := os.Symlink(escapeTarget, escapeLink); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}
	_, err := filesystemReconciler.EnsureDocumentRoot(
		t.Context(), first, "escape/nested", agentprotocol.DocumentRootAccessStatic,
	)
	if !errors.Is(err, hostfilesystem.ErrConflict) {
		t.Fatalf("symlink document root error = %v, want ErrConflict", err)
	}
	if _, err := os.Stat(filepath.Join(escapeTarget, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink escape target was changed: %v", err)
	}
	if err := os.Remove(escapeLink); err != nil {
		t.Fatalf("remove escape symlink: %v", err)
	}

	byteProbe := filepath.Join(first.HomeDirectory, "public_html", "byte-limit.bin")
	if err := runAs(first, "/usr/bin/dd", "if=/dev/zero", "of="+byteProbe, "bs=1M", "count=4", "status=none"); err == nil {
		t.Fatal("byte quota allowed a four-MiB file under a two-MiB hard limit")
	}
	info, err := os.Stat(byteProbe)
	if err != nil {
		t.Fatalf("stat partial byte probe: %v", err)
	}
	if info.Size() >= 4<<20 {
		t.Fatalf("byte quota did not truncate the probe: size=%d", info.Size())
	}
	if err := os.Remove(byteProbe); err != nil {
		t.Fatalf("remove byte probe: %v", err)
	}

	inodeRoot := filepath.Join(first.HomeDirectory, "tmp")
	inodeLimitObserved := false
	for index := 0; index < 64; index++ {
		name := filepath.Join(inodeRoot, fmt.Sprintf("inode-%03d", index))
		if err := runAs(first, "/usr/bin/touch", name); err != nil {
			inodeLimitObserved = true
			break
		}
	}
	if !inodeLimitObserved {
		t.Fatal("inode quota allowed 64 files under a 32-inode hard limit")
	}
	entries, err := os.ReadDir(inodeRoot)
	if err != nil {
		t.Fatalf("read inode probe directory: %v", err)
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(inodeRoot, entry.Name())); err != nil {
			t.Fatalf("remove inode probe: %v", err)
		}
	}
	t.Log("STACKFORT_QUALIFICATION filesystem-isolation-and-quota=passed")

}

func testAccountResourceLimits(t *testing.T, identity hostingidentity.Spec) {
	t.Helper()
	reconciler := hostresources.NewReconciler()
	initial := hostingresources.Spec{
		Identity:        identity,
		CPUQuotaPercent: hostingresources.OptionalUint64{Set: true, Value: 50},
		CPUWeight:       hostingresources.OptionalUint64{Set: true, Value: 200},
		MemoryBytes:     hostingresources.OptionalUint64{Set: true, Value: 64 << 20},
		SwapBytes:       hostingresources.OptionalUint64{Set: true, Value: 0},
		ProcessLimit:    hostingresources.OptionalUint64{Set: true, Value: 4},
	}
	result, err := reconciler.Reconcile(t.Context(), initial)
	if err != nil {
		t.Fatalf("reconcile initial account resources: %v", err)
	}
	if !result.LimitsApplied || result.Capability.Status != agentprotocol.CapabilityAvailable {
		t.Fatalf("initial resource result = %#v", result)
	}
	t.Cleanup(func() { cleanupResourceSlice(t, result.UnitName) })
	cgroup := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(result.ControlGroup, "/"))

	pidsBefore := cgroupEvent(t, filepath.Join(cgroup, "pids.events"), "max")
	pidsUnit := fmt.Sprintf("stackfort-integration-pids-%d.service", identity.UID)
	pidsCommand := exec.Command(
		"/usr/bin/systemd-run", "--quiet", "--wait", "--collect", "--unit="+pidsUnit,
		"--slice="+result.UnitName, "--uid="+identity.Username,
		"/bin/sh", "-c", `n=0; while [ "$n" -lt 16 ]; do /usr/bin/sleep 1 & n=$((n+1)); done; wait`,
	)
	_ = pidsCommand.Run()
	pidsAfter := cgroupEvent(t, filepath.Join(cgroup, "pids.events"), "max")
	if pidsAfter <= pidsBefore {
		t.Fatalf("process limit was not enforced: pids.events max before=%d after=%d", pidsBefore, pidsAfter)
	}

	changed := initial
	changed.CPUQuotaPercent = hostingresources.OptionalUint64{Set: true, Value: 125}
	changed.CPUWeight = hostingresources.OptionalUint64{Set: true, Value: 800}
	changed.MemoryBytes = hostingresources.OptionalUint64{Set: true, Value: 32 << 20}
	changed.ProcessLimit = hostingresources.OptionalUint64{Set: true, Value: 128}
	changedResult, err := reconciler.Reconcile(t.Context(), changed)
	if err != nil {
		t.Fatalf("reconcile changed account resources: %v", err)
	}
	if !changedResult.UnitsChanged || changedResult.UnitName != result.UnitName {
		t.Fatalf("changed resource result = %#v", changedResult)
	}

	oomBefore := cgroupEvent(t, filepath.Join(cgroup, "memory.events"), "oom_kill")
	memoryUnit := fmt.Sprintf("stackfort-integration-memory-%d.service", identity.UID)
	memoryCommand := exec.Command(
		"/usr/bin/systemd-run", "--quiet", "--wait", "--collect", "--unit="+memoryUnit,
		"--slice="+result.UnitName, "--uid="+identity.Username,
		"/usr/bin/dd", "if=/dev/zero", "of=/dev/null", "bs=64M", "count=1", "status=none",
	)
	if err := memoryCommand.Run(); err == nil {
		t.Fatal("memory probe unexpectedly succeeded above the account MemoryMax")
	}
	oomAfter := cgroupEvent(t, filepath.Join(cgroup, "memory.events"), "oom_kill")
	if oomAfter <= oomBefore {
		t.Fatalf("memory limit did not produce a cgroup OOM kill: before=%d after=%d", oomBefore, oomAfter)
	}
}

func cgroupEvent(t *testing.T, path, key string) uint64 {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cgroup event %s: %v", path, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != key {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			t.Fatalf("parse cgroup event %s %s: %v", path, key, err)
		}
		return value
	}
	t.Fatalf("cgroup event %s is absent from %s", key, path)
	return 0
}

func cleanupResourceSlice(t *testing.T, unit string) {
	t.Helper()
	uid, err := hostingresources.ParseAccountSliceName(unit)
	if err != nil || uid < hostingidentity.MinimumID || uid > hostingidentity.MaximumID {
		t.Errorf("refusing unsafe integration-test resource cleanup unit: %q", unit)
		return
	}
	_ = exec.Command("/usr/bin/systemctl", "stop", unit).Run()
	unitPath := filepath.Join("/etc/systemd/system", unit)
	if filepath.Dir(unitPath) != "/etc/systemd/system" {
		t.Errorf("refusing unsafe integration-test unit cleanup path: %q", unitPath)
		return
	}
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("remove disposable account slice: %v", err)
	}
	runtimeDropIn := filepath.Join("/run/systemd/system.control", unit+".d")
	if filepath.Dir(runtimeDropIn) != "/run/systemd/system.control" {
		t.Errorf("refusing unsafe integration-test drop-in cleanup path: %q", runtimeDropIn)
		return
	}
	if err := os.RemoveAll(runtimeDropIn); err != nil {
		t.Errorf("remove disposable runtime resource drop-in: %v", err)
	}
	_ = exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
}

func disposableIdentity(t *testing.T, id uint32) hostingidentity.Spec {
	t.Helper()
	accountID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("create account ID: %v", err)
	}
	username, err := hostingidentity.UsernameForAccount(accountID.String())
	if err != nil {
		t.Fatalf("derive username: %v", err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID.String())
	if err != nil {
		t.Fatalf("derive home: %v", err)
	}
	return hostingidentity.Spec{
		AccountID: accountID.String(), Username: username, UID: id, GID: id, HomeDirectory: home,
	}
}

func availableManagedID(t *testing.T, start uint32) uint32 {
	t.Helper()
	for id := start; id <= hostingidentity.MaximumID; id++ {
		text := strconv.FormatUint(uint64(id), 10)
		_, userErr := user.LookupId(text)
		_, groupErr := user.LookupGroupId(text)
		if _, userMissing := userErr.(user.UnknownUserIdError); userMissing {
			if _, groupMissing := groupErr.(user.UnknownGroupIdError); groupMissing {
				return id
			}
		}
	}
	t.Fatal("no unused managed UID/GID is available")
	return 0
}

func runAs(identity hostingidentity.Spec, executable string, arguments ...string) error {
	args := []string{"-u", identity.Username, "--", executable}
	args = append(args, arguments...)
	return exec.Command("/usr/sbin/runuser", args...).Run()
}

func cleanupIdentity(t *testing.T, identity hostingidentity.Spec) {
	t.Helper()
	if identity.HomeDirectory == "" || filepath.Dir(identity.HomeDirectory) != hostingidentity.ManagedAccountsRoot {
		t.Errorf("refusing unsafe integration-test cleanup path: %q", identity.HomeDirectory)
		return
	}
	if err := os.RemoveAll(identity.HomeDirectory); err != nil {
		t.Errorf("remove disposable account root: %v", err)
	}
	commands := []*exec.Cmd{
		exec.Command("/usr/sbin/setquota", "-P", strconv.FormatUint(uint64(identity.UID), 10),
			"0", "0", "0", "0", "/srv/hosting"),
		exec.Command("/usr/sbin/userdel", identity.Username),
		exec.Command("/usr/sbin/groupdel", identity.Username),
	}
	for _, command := range commands {
		_ = command.Run()
	}
}
