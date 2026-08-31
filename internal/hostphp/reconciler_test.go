// SPDX-License-Identifier: AGPL-3.0-or-later

package hostphp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

func TestReconcileValidatesThenActivatesNativeAccountPool(t *testing.T) {
	t.Parallel()
	files := &fakePHPFiles{prepareChanged: true}
	runner := &fakePHPRunner{}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "debian"}, runner: runner, files: files,
	}
	result, err := reconciler.Reconcile(t.Context(), phpPoolSpec("8.4"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !result.Changed || !result.Active || !slices.Equal(result.Versions, []string{"8.4"}) ||
		result.Capability.Status != agentprotocol.CapabilityAvailable {
		t.Fatalf("result = %#v", result)
	}
	wanted := []agentexec.ProfileID{
		agentexec.ProfilePHPFPM84Test,
		agentexec.ProfileSystemdDaemonReload,
		agentexec.ProfileSystemdShowPHPPool,
		agentexec.ProfileSystemdEnablePHPPool,
		agentexec.ProfileSystemdRestartPHPPool,
		agentexec.ProfileSystemdShowPHPPool,
	}
	if !slices.Equal(runner.profiles, wanted) {
		t.Fatalf("profiles = %#v", runner.profiles)
	}
	if files.prepareCalls != 1 || files.verifyCalls != 1 || files.rollbackCalls != 0 || files.commitCalls != 1 {
		t.Fatalf("file calls = %#v", files)
	}
}

func TestReconcileRejectsNonNativeVersionBeforeMutation(t *testing.T) {
	t.Parallel()
	files := &fakePHPFiles{}
	runner := &fakePHPRunner{}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "rocky"}, runner: runner, files: files,
	}
	_, err := reconciler.Reconcile(t.Context(), phpPoolSpec("8.4"))
	var capability *CapabilityError
	if !errors.As(err, &capability) || capability.Capability.ReasonCode != "php-runtime-version-unsupported" {
		t.Fatalf("error = %v", err)
	}
	if files.prepareCalls != 0 || len(runner.profiles) != 0 {
		t.Fatal("unsupported version reached a host mutation")
	}
}

func TestReconcileRollsBackCandidateOnConfigurationFailure(t *testing.T) {
	t.Parallel()
	files := &fakePHPFiles{prepareChanged: true}
	runner := &fakePHPRunner{failProfile: agentexec.ProfilePHPFPM85Test}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "ubuntu"}, runner: runner, files: files,
	}
	_, err := reconciler.Reconcile(t.Context(), phpPoolSpec("8.5"))
	if !errors.Is(err, ErrValidation) || files.rollbackCalls != 1 || files.commitCalls != 0 {
		t.Fatalf("failure = %v, file calls = %#v", err, files)
	}
}

func TestReconcileDisablesNewPoolWhenActivationFails(t *testing.T) {
	t.Parallel()
	files := &fakePHPFiles{prepareChanged: true}
	runner := &fakePHPRunner{failProfile: agentexec.ProfileSystemdRestartPHPPool}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "debian"}, runner: runner, files: files,
	}
	_, err := reconciler.Reconcile(t.Context(), phpPoolSpec("8.4"))
	if !errors.Is(err, ErrActivation) || files.rollbackCalls != 1 || files.commitCalls != 0 ||
		!slices.Contains(runner.profiles, agentexec.ProfileSystemdDisablePHPPool) {
		t.Fatalf("failure=%v profiles=%#v file calls=%#v", err, runner.profiles, files)
	}
}

func TestReconcileEmptySetStopsAndRemovesManagedPool(t *testing.T) {
	t.Parallel()
	files := &fakePHPFiles{managed: map[string]bool{"8.3": true}}
	runner := &fakePHPRunner{}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "rocky"}, runner: runner, files: files,
	}
	spec := phpPoolSpec()
	spec.RetireAbsent = true
	result, err := reconciler.Reconcile(t.Context(), spec)
	if err != nil {
		t.Fatalf("Reconcile empty: %v", err)
	}
	if !result.Changed || !result.Active || !slices.Equal(runner.profiles, []agentexec.ProfileID{
		agentexec.ProfileSystemdDisablePHPPool, agentexec.ProfileSystemdDaemonReload,
	}) || !slices.Equal(files.removed, []string{"8.3"}) {
		t.Fatalf("result=%#v profiles=%#v removed=%#v", result, runner.profiles, files.removed)
	}
}

func TestReconcileAdditiveSetLeavesAbsentManagedPoolRunning(t *testing.T) {
	t.Parallel()
	files := &fakePHPFiles{managed: map[string]bool{"8.3": true}}
	runner := &fakePHPRunner{}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "rocky"}, runner: runner, files: files,
	}
	result, err := reconciler.Reconcile(t.Context(), phpPoolSpec())
	if err != nil {
		t.Fatalf("Reconcile additive empty set: %v", err)
	}
	if result.Changed || len(runner.profiles) != 0 || len(files.removed) != 0 {
		t.Fatalf("result=%#v profiles=%#v removed=%#v", result, runner.profiles, files.removed)
	}
}

func TestInspectReturnsOnlyBoundedAggregateMetrics(t *testing.T) {
	t.Parallel()
	runner := &fakePHPRunner{showResult: &agentexec.Result{Stdout: strings.Join([]string{
		"LoadState=loaded", "ActiveState=active", "SubState=running", "UnitFileState=enabled",
		"ControlGroup=/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200123.slice/pool.service",
		"MemoryCurrent=33554432", "CPUUsageNSec=9000000", "TasksCurrent=2", "",
	}, "\n")}}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "debian"}, runner: runner, files: &fakePHPFiles{},
	}
	spec := phpPoolSpec("8.4")
	result, err := reconciler.Inspect(t.Context(), agentprotocol.PHPPoolInspectRequest{
		Identity: spec.Identity, Versions: spec.Versions,
	})
	if err != nil || len(result.Pools) != 1 || result.Pools[0].State != agentprotocol.PHPPoolActive ||
		result.Pools[0].MemoryBytes == nil || *result.Pools[0].MemoryBytes != 33_554_432 ||
		result.Pools[0].CPUTimeNanosec == nil || *result.Pools[0].CPUTimeNanosec != 9_000_000 ||
		result.Pools[0].Processes == nil || *result.Pools[0].Processes != 2 {
		t.Fatalf("inspection=%#v err=%v", result, err)
	}
	if !slices.Equal(runner.profiles, []agentexec.ProfileID{agentexec.ProfileSystemdShowPHPPool}) {
		t.Fatalf("profiles=%#v", runner.profiles)
	}
}

func TestInspectClassifiesMissingPoolWithoutLeakingHostDetails(t *testing.T) {
	t.Parallel()
	runner := &fakePHPRunner{showResult: &agentexec.Result{
		Stdout: "LoadState=not-found\nActiveState=inactive\nSubState=dead\nUnitFileState=\nControlGroup=\n" +
			"MemoryCurrent=[not set]\nCPUUsageNSec=[not set]\nTasksCurrent=[not set]\n",
		ExitCode: 4,
	}}
	reconciler := &Reconciler{
		platform: fakePHPPlatform{distribution: "ubuntu"}, runner: runner, files: &fakePHPFiles{},
	}
	spec := phpPoolSpec("8.5")
	result, err := reconciler.Inspect(t.Context(), agentprotocol.PHPPoolInspectRequest{
		Identity: spec.Identity, Versions: spec.Versions,
	})
	if err != nil || len(result.Pools) != 1 || result.Pools[0].State != agentprotocol.PHPPoolMissing ||
		result.Pools[0].MemoryBytes != nil || result.Pools[0].CPUTimeNanosec != nil ||
		result.Pools[0].Processes != nil {
		t.Fatalf("inspection=%#v err=%v", result, err)
	}
}

type fakePHPPlatform struct{ distribution string }

func (platform fakePHPPlatform) InspectPlatform() agentprotocol.PlatformCapabilities {
	return agentprotocol.PlatformCapabilities{
		DistributionID: platform.distribution,
		Support:        agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}
}

type fakePHPRunner struct {
	profiles    []agentexec.ProfileID
	failProfile agentexec.ProfileID
	active      bool
	enabled     bool
	showResult  *agentexec.Result
}

func (runner *fakePHPRunner) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	runner.profiles = append(runner.profiles, invocation.Profile)
	if invocation.Profile == runner.failProfile {
		return agentexec.Result{ExitCode: 1}, nil
	}
	switch invocation.Profile {
	case agentexec.ProfileSystemdEnablePHPPool:
		runner.enabled = true
	case agentexec.ProfileSystemdRestartPHPPool:
		runner.active = true
	case agentexec.ProfileSystemdDisablePHPPool:
		runner.enabled, runner.active = false, false
	case agentexec.ProfileSystemdShowPHPPool:
		if runner.showResult != nil {
			return *runner.showResult, nil
		}
		active, unitFile := "inactive", "disabled"
		if runner.active {
			active = "active"
		}
		if runner.enabled {
			unitFile = "enabled"
		}
		return agentexec.Result{Stdout: "LoadState=loaded\nActiveState=" + active +
			"\nSubState=running\nUnitFileState=" + unitFile +
			"\nControlGroup=/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200123.slice/pool.service\n"}, nil
	}
	return agentexec.Result{}, nil
}

type fakePHPFiles struct {
	prepareChanged bool
	managed        map[string]bool
	removed        []string
	prepareCalls   int
	verifyCalls    int
	rollbackCalls  int
	commitCalls    int
}

func (files *fakePHPFiles) Prepare(_ phpruntime.Profile, _ phpruntime.PoolSetSpec) (*configurationChange, error) {
	files.prepareCalls++
	return &configurationChange{
		Changed:  files.prepareChanged,
		rollback: func() error { files.rollbackCalls++; return nil },
		commit:   func() error { files.commitCalls++; return nil },
	}, nil
}

func (files *fakePHPFiles) Managed(_ phpruntime.PoolSetSpec, version string) (bool, error) {
	return files.managed[version], nil
}

func (files *fakePHPFiles) Remove(_ phpruntime.PoolSetSpec, version string) (bool, error) {
	files.removed = append(files.removed, version)
	return true, nil
}

func (files *fakePHPFiles) VerifyRuntime(_ phpruntime.Profile, _ phpruntime.PoolSetSpec) error {
	files.verifyCalls++
	return nil
}

func phpPoolSpec(versions ...string) phpruntime.PoolSetSpec {
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return phpruntime.PoolSetSpec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home,
		},
		Versions: versions, MaxChildren: 4, MemoryLimitMiB: 128,
	}
}
