// SPDX-License-Identifier: AGPL-3.0-or-later

package hostnginx

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

type fakePlatformInspector struct {
	platform agentprotocol.PlatformCapabilities
}

func (inspector fakePlatformInspector) InspectPlatform() agentprotocol.PlatformCapabilities {
	return inspector.platform
}

type fakeRunner struct {
	active   bool
	enabled  bool
	fail     agentexec.ProfileID
	failCall map[agentexec.ProfileID]int
	calls    map[agentexec.ProfileID]int
	invoked  []agentexec.ProfileID
}

func (runner *fakeRunner) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	runner.invoked = append(runner.invoked, invocation.Profile)
	if runner.calls == nil {
		runner.calls = make(map[agentexec.ProfileID]int)
	}
	runner.calls[invocation.Profile]++
	if invocation.Profile == runner.fail || runner.failCall[invocation.Profile] == runner.calls[invocation.Profile] {
		return agentexec.Result{ExitCode: 1}, nil
	}
	switch invocation.Profile {
	case agentexec.ProfileSystemctlShow:
		active := "inactive"
		if runner.active {
			active = "active"
		}
		enabled := "disabled"
		if runner.enabled {
			enabled = "enabled"
		}
		return agentexec.Result{Stdout: "LoadState=loaded\nActiveState=" + active + "\nUnitFileState=" + enabled + "\n"}, nil
	case agentexec.ProfileSystemdRestartNGINX, agentexec.ProfileSystemdReloadNGINX:
		runner.active = true
	case agentexec.ProfileSystemdStopNGINX:
		runner.active = false
	case agentexec.ProfileSystemdEnableNGINX:
		runner.enabled = true
	case agentexec.ProfileSystemdDisableNGINX:
		runner.enabled = false
	}
	return agentexec.Result{}, nil
}

type fakeConfigurationManager struct {
	managed       bool
	managedError  error
	prepareError  error
	change        *configurationChange
	prepareCalled bool
	rollbackCount int
	commitCount   int
}

func (manager *fakeConfigurationManager) Managed() (bool, error) {
	return manager.managed, manager.managedError
}

func (manager *fakeConfigurationManager) Prepare(nginxbaseline.Spec) (*configurationChange, error) {
	manager.prepareCalled = true
	if manager.prepareError != nil {
		return nil, manager.prepareError
	}
	if manager.change == nil {
		manager.change = &configurationChange{}
	}
	manager.change.rollback = func() error { manager.rollbackCount++; return nil }
	manager.change.commit = func() error { manager.commitCount++; return nil }
	return manager.change, nil
}

func supportedPlatform() agentprotocol.PlatformCapabilities {
	return agentprotocol.PlatformCapabilities{
		DistributionID: "debian", VersionID: "13", Architecture: "amd64",
		KernelRelease: "fixture", Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}
}

func TestReconcileAdoptsOnlyInactiveUnmanagedService(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	manager := &fakeConfigurationManager{change: &configurationChange{Changed: true, DropInChanged: true}}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{supportedPlatform()}, runner: runner, configuration: manager,
	}
	result, err := reconciler.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !result.Changed || !result.ConfigurationTested || !result.ActivationPerformed ||
		!result.EnablementChanged || result.Capability.Status != agentprotocol.CapabilityAvailable ||
		manager.commitCount != 1 {
		t.Fatalf("result = %#v, commits = %d", result, manager.commitCount)
	}
	want := []agentexec.ProfileID{
		agentexec.ProfileNGINXVersion, agentexec.ProfileSystemctlShow,
		agentexec.ProfileSystemdDaemonReload, agentexec.ProfileNGINXTestBaseline,
		agentexec.ProfileSystemdEnableNGINX, agentexec.ProfileSystemdRestartNGINX,
		agentexec.ProfileSystemctlShow,
	}
	if !reflect.DeepEqual(runner.invoked, want) {
		t.Fatalf("profiles = %#v", runner.invoked)
	}
}

func TestReconcileRejectsActiveUnmanagedServiceBeforeWriting(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{active: true, enabled: true}
	manager := &fakeConfigurationManager{}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{supportedPlatform()}, runner: runner, configuration: manager,
	}
	_, err := reconciler.Reconcile(t.Context())
	if !errors.Is(err, ErrConflict) || manager.prepareCalled {
		t.Fatalf("error = %v, prepare = %t", err, manager.prepareCalled)
	}
}

func TestReconcileRollsBackInvalidConfiguration(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{fail: agentexec.ProfileNGINXTestBaseline}
	manager := &fakeConfigurationManager{change: &configurationChange{Changed: true, DropInChanged: true}}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{supportedPlatform()}, runner: runner, configuration: manager,
	}
	_, err := reconciler.Reconcile(t.Context())
	if !errors.Is(err, ErrValidationFailed) || manager.rollbackCount != 1 || manager.commitCount != 0 {
		t.Fatalf("error = %v, rollbacks = %d, commits = %d", err, manager.rollbackCount, manager.commitCount)
	}
}

func TestReconcileIsIdempotentAndStillTestsConfiguration(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{active: true, enabled: true}
	manager := &fakeConfigurationManager{managed: true, change: &configurationChange{PreviouslyManaged: true}}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{supportedPlatform()}, runner: runner, configuration: manager,
	}
	result, err := reconciler.Reconcile(t.Context())
	if err != nil || result.Changed || result.ActivationPerformed || !result.ConfigurationTested {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	for _, forbidden := range []agentexec.ProfileID{
		agentexec.ProfileSystemdRestartNGINX, agentexec.ProfileSystemdReloadNGINX,
		agentexec.ProfileSystemdDaemonReload, agentexec.ProfileSystemdEnableNGINX,
	} {
		for _, invoked := range runner.invoked {
			if invoked == forbidden {
				t.Fatalf("idempotent reconcile invoked %s", forbidden)
			}
		}
	}
}

func TestReconcileRollsBackEnablementWhenActivationFails(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{fail: agentexec.ProfileSystemdRestartNGINX}
	manager := &fakeConfigurationManager{change: &configurationChange{Changed: true, DropInChanged: true}}
	reconciler := &Reconciler{
		platform: fakePlatformInspector{supportedPlatform()}, runner: runner, configuration: manager,
	}
	_, err := reconciler.Reconcile(t.Context())
	if !errors.Is(err, ErrActivationFailed) || manager.rollbackCount != 1 || runner.enabled || runner.active {
		t.Fatalf("error=%v rollbacks=%d enabled=%t active=%t", err, manager.rollbackCount, runner.enabled, runner.active)
	}
	foundDisable := false
	for _, profile := range runner.invoked {
		foundDisable = foundDisable || profile == agentexec.ProfileSystemdDisableNGINX
	}
	if !foundDisable {
		t.Fatalf("rollback profiles = %#v", runner.invoked)
	}
}

func TestReconcileReturnsTypedPlatformCapability(t *testing.T) {
	t.Parallel()
	reconciler := &Reconciler{
		platform: fakePlatformInspector{agentprotocol.PlatformCapabilities{
			Support: agentprotocol.Capability{Status: agentprotocol.CapabilityUnsupported, ReasonCode: "distribution-version-unsupported"},
		}},
		runner: &fakeRunner{}, configuration: &fakeConfigurationManager{},
	}
	_, err := reconciler.Reconcile(t.Context())
	var capabilityError *CapabilityError
	if !errors.As(err, &capabilityError) || capabilityError.Capability.ReasonCode != "distribution-version-unsupported" {
		t.Fatalf("error = %v", err)
	}
}
