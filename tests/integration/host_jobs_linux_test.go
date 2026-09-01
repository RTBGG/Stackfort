// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostjobs"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/google/uuid"
)

func TestDisposableHostScheduledJobs(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}

	identity := disposableIdentity(t, availableManagedID(t, 598_000))
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	if _, err := hostfilesystem.NewReconciler().Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}
	resourceResult, err := hostresources.NewReconciler().Reconcile(t.Context(), hostingresources.Spec{Identity: identity})
	if err != nil {
		t.Fatalf("reconcile account resource slice: %v", err)
	}
	t.Cleanup(func() { cleanupResourceSlice(t, resourceResult.UnitName) })
	jobsRoot := filepath.Join(identity.HomeDirectory, "jobs")
	createManagedTestDirectory(t, jobsRoot, identity.UID, 0o750)

	jobUUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("create job ID: %v", err)
	}
	jobID := jobUUID.String()
	definition := scheduledjobs.Definition{
		ID: jobID, Runtime: scheduledjobs.RuntimeShell, ScriptPath: "jobs/probe.sh", Enabled: true,
		Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleInterval, IntervalMinutes: 5},
	}
	spec := scheduledjobs.Spec{Identity: identity, Definition: definition}
	serviceUnit, timerUnit, err := scheduledjobs.UnitNames(identity, jobID)
	if err != nil {
		t.Fatalf("derive scheduled-job units: %v", err)
	}
	t.Cleanup(func() { cleanupScheduledJob(t, spec, serviceUnit, timerUnit) })

	shellResult := filepath.Join(identity.HomeDirectory, "shell-result.txt")
	privateTmpProbe := "stackfort-job-private-tmp-" + strconv.FormatUint(uint64(identity.UID), 10)
	shell := fmt.Sprintf("#!/bin/sh\nset -eu\nprintf 'shell:%%s:%%s\\n' \"$(id -u)\" \"$PWD\" > %s\nprintf private > /tmp/%s\n", shellResult, privateTmpProbe)
	writeManagedTestFile(t, filepath.Join(jobsRoot, "probe.sh"), []byte(shell), identity.UID, 0o640)

	assertScheduledCalendarsAccepted(t)
	reconciler := hostjobs.NewReconciler()
	created, err := reconciler.Reconcile(t.Context(), spec, true)
	if err != nil || !created.Changed || !created.Present || !created.Enabled ||
		created.Capability.Status != agentprotocol.CapabilityAvailable ||
		created.ServiceUnit != serviceUnit || created.TimerUnit != timerUnit {
		t.Fatalf("create scheduled job = %#v, %v", created, err)
	}
	assertScheduledUnits(t, identity.Username, identity.UID, serviceUnit, timerUnit)
	startSystemdUnit(t, serviceUnit)
	content := waitForManagedFile(t, shellResult)
	wantedShell := "shell:" + strconv.FormatUint(uint64(identity.UID), 10) + ":" + identity.HomeDirectory
	if strings.TrimSpace(content) != wantedShell {
		t.Fatalf("shell scheduled-job output = %q, want %q", content, wantedShell)
	}
	if _, err := os.Stat(filepath.Join("/tmp", privateTmpProbe)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scheduled job escaped PrivateTmp: %v", err)
	}
	assertRootOwnedMode(t, filepath.Join("/etc/systemd/system", serviceUnit), 0o644)
	assertRootOwnedMode(t, filepath.Join("/etc/systemd/system", timerUnit), 0o644)

	replayed, err := reconciler.Reconcile(t.Context(), spec, true)
	if err != nil || replayed.Changed {
		t.Fatalf("idempotent scheduled-job reconciliation = %#v, %v", replayed, err)
	}

	version, err := phpruntime.ApprovedVersion(distributionID(t))
	if err != nil {
		t.Fatalf("select installed PHP CLI: %v", err)
	}
	phpResult := filepath.Join(identity.HomeDirectory, "php-result.txt")
	php := "<?php file_put_contents(" + strconv.Quote(phpResult) + ", 'php:' . getmyuid() . ':' . getcwd() . PHP_EOL);\n"
	writeManagedTestFile(t, filepath.Join(jobsRoot, "probe.php"), []byte(php), identity.UID, 0o640)
	spec.Definition.Runtime = scheduledjobs.RuntimePHP
	spec.Definition.ScriptPath = "jobs/probe.php"
	spec.Definition.PHPVersion = version
	spec.Definition.Schedule = scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleDaily, HourUTC: 3, MinuteUTC: 17}
	updated, err := reconciler.Reconcile(t.Context(), spec, true)
	if err != nil || !updated.Changed || !updated.Enabled {
		t.Fatalf("update scheduled job to PHP = %#v, %v", updated, err)
	}
	startSystemdUnit(t, serviceUnit)
	phpContent := waitForManagedFile(t, phpResult)
	wantedPHP := "php:" + strconv.FormatUint(uint64(identity.UID), 10) + ":" + identity.HomeDirectory
	if strings.TrimSpace(phpContent) != wantedPHP {
		t.Fatalf("PHP scheduled-job output = %q, want %q", phpContent, wantedPHP)
	}

	spec.Definition.Enabled = false
	disabled, err := reconciler.Reconcile(t.Context(), spec, true)
	if err != nil || !disabled.Changed || disabled.Enabled {
		t.Fatalf("disable scheduled job = %#v, %v", disabled, err)
	}
	assertSystemdProperty(t, timerUnit, "ActiveState", "inactive")
	if state := systemdProperty(t, timerUnit, "UnitFileState"); state != "disabled" {
		t.Fatalf("disabled timer UnitFileState = %q", state)
	}

	assertScheduledScriptFences(t, reconciler, spec, jobsRoot)
	removed, err := reconciler.Reconcile(t.Context(), spec, false)
	if err != nil || !removed.Changed || removed.Present || removed.Enabled {
		t.Fatalf("remove scheduled job = %#v, %v", removed, err)
	}
	for _, name := range []string{serviceUnit, timerUnit} {
		if _, err := os.Lstat(filepath.Join("/etc/systemd/system", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("removed scheduled-job unit %s remains: %v", name, err)
		}
	}
	repeatedRemoval, err := reconciler.Reconcile(t.Context(), spec, false)
	if err != nil || repeatedRemoval.Changed {
		t.Fatalf("idempotent scheduled-job removal = %#v, %v", repeatedRemoval, err)
	}
	t.Log("STACKFORT_QUALIFICATION scheduled-jobs=passed")
}

func assertScheduledCalendarsAccepted(t *testing.T) {
	t.Helper()
	schedules := []scheduledjobs.Schedule{
		{Kind: scheduledjobs.ScheduleInterval, IntervalMinutes: 5},
		{Kind: scheduledjobs.ScheduleInterval, IntervalMinutes: 15},
		{Kind: scheduledjobs.ScheduleInterval, IntervalMinutes: 30},
		{Kind: scheduledjobs.ScheduleHourly, MinuteUTC: 11},
		{Kind: scheduledjobs.ScheduleDaily, HourUTC: 3, MinuteUTC: 17},
		{Kind: scheduledjobs.ScheduleWeekly, Weekday: scheduledjobs.Monday, HourUTC: 4, MinuteUTC: 19},
	}
	for _, schedule := range schedules {
		calendar, err := scheduledjobs.Calendar(schedule)
		if err != nil {
			t.Fatalf("render calendar %#v: %v", schedule, err)
		}
		if output, err := exec.Command("/usr/bin/systemd-analyze", "calendar", calendar).CombinedOutput(); err != nil {
			t.Fatalf("systemd rejected calendar %q: %v: %s", calendar, err, output)
		}
	}
}

func assertScheduledUnits(t *testing.T, username string, uid uint32, serviceUnit, timerUnit string) {
	t.Helper()
	paths := []string{filepath.Join("/etc/systemd/system", serviceUnit), filepath.Join("/etc/systemd/system", timerUnit)}
	arguments := append([]string{"verify"}, paths...)
	if output, err := exec.Command("/usr/bin/systemd-analyze", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("systemd rejected scheduled-job units: %v: %s", err, output)
	}
	assertSystemdProperty(t, serviceUnit, "User", username)
	assertSystemdProperty(t, serviceUnit, "Slice", "stackfort-accounts-"+strconv.FormatUint(uint64(uid), 10)+".slice")
	assertSystemdProperty(t, serviceUnit, "NoNewPrivileges", "yes")
	assertSystemdProperty(t, serviceUnit, "PrivateTmp", "yes")
	assertSystemdProperty(t, serviceUnit, "ProtectSystem", "strict")
	assertSystemdProperty(t, timerUnit, "Persistent", "yes")
}

func assertScheduledScriptFences(t *testing.T, reconciler *hostjobs.Reconciler, base scheduledjobs.Spec, jobsRoot string) {
	t.Helper()
	base.Definition.Enabled = false
	symlinkPath := filepath.Join(jobsRoot, "linked.php")
	if err := os.Symlink(filepath.Join(jobsRoot, "probe.php"), symlinkPath); err != nil {
		t.Fatalf("create scheduled-job symlink fixture: %v", err)
	}
	base.Definition.ScriptPath = "jobs/linked.php"
	if _, err := reconciler.Reconcile(t.Context(), base, true); !errors.Is(err, hostjobs.ErrConflict) {
		t.Fatalf("symlink scheduled-job script error = %v, want conflict", err)
	}
	if err := os.Remove(symlinkPath); err != nil {
		t.Fatalf("remove scheduled-job symlink fixture: %v", err)
	}
	hardlinkPath := filepath.Join(jobsRoot, "hardlinked.php")
	if err := os.Link(filepath.Join(jobsRoot, "probe.php"), hardlinkPath); err != nil {
		t.Fatalf("create scheduled-job hard-link fixture: %v", err)
	}
	base.Definition.ScriptPath = "jobs/hardlinked.php"
	if _, err := reconciler.Reconcile(t.Context(), base, true); !errors.Is(err, hostjobs.ErrConflict) {
		t.Fatalf("hard-linked scheduled-job script error = %v, want conflict", err)
	}
	if err := os.Remove(hardlinkPath); err != nil {
		t.Fatalf("remove scheduled-job hard-link fixture: %v", err)
	}
}

func distributionID(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		t.Fatalf("read distribution identity: %v", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
		}
	}
	t.Fatal("distribution ID is absent")
	return ""
}

func startSystemdUnit(t *testing.T, unit string) {
	t.Helper()
	if output, err := exec.Command("/usr/bin/systemctl", "start", unit).CombinedOutput(); err != nil {
		status, _ := exec.Command("/usr/bin/systemctl", "status", "--no-pager", unit).CombinedOutput()
		t.Fatalf("start scheduled-job unit %s: %v: %s\n%s", unit, err, output, status)
	}
}

func waitForManagedFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content)
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled-job output %s was not created: %v", path, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func assertSystemdProperty(t *testing.T, unit, property, wanted string) {
	t.Helper()
	if value := systemdProperty(t, unit, property); value != wanted {
		t.Fatalf("%s %s = %q, want %q", unit, property, value, wanted)
	}
}

func systemdProperty(t *testing.T, unit, property string) string {
	t.Helper()
	output, err := exec.Command("/usr/bin/systemctl", "show", "--property="+property, "--value", unit).CombinedOutput()
	if err != nil {
		t.Fatalf("read %s property %s: %v: %s", unit, property, err, output)
	}
	return strings.TrimSpace(string(output))
}

func cleanupScheduledJob(t *testing.T, spec scheduledjobs.Spec, serviceUnit, timerUnit string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := hostjobs.NewReconciler().Reconcile(ctx, spec, false); err != nil {
		t.Errorf("remove disposable scheduled job: %v", err)
	}
	for _, unit := range []string{serviceUnit, timerUnit} {
		if !strings.HasPrefix(unit, "stackfort-job-"+strconv.FormatUint(uint64(spec.Identity.UID), 10)+"-") {
			t.Errorf("refusing unsafe scheduled-job cleanup unit: %q", unit)
			continue
		}
		_ = exec.Command("/usr/bin/systemctl", "disable", "--now", unit).Run()
		path := filepath.Join("/etc/systemd/system", unit)
		if filepath.Dir(path) != "/etc/systemd/system" {
			t.Errorf("refusing unsafe scheduled-job cleanup path: %q", path)
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove disposable scheduled-job unit: %v", err)
		}
	}
	_ = exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
}
