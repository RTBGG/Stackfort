// SPDX-License-Identifier: AGPL-3.0-or-later

package hostresources

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
)

func TestReconcileRequiresEveryResourceControllerBeforeMutation(t *testing.T) {
	t.Parallel()
	spec := resourceSpec(t)
	commands := &fakeResourceCommands{}
	units := &fakeUnitManager{}
	reconciler := &Reconciler{
		capabilities: fakeResourceCapabilities{
			systemd: availableCapability(),
			cgroup: agentprotocol.CgroupCapabilities{
				Version: 2, Unified: availableCapability(), CPU: availableCapability(),
				Memory: availableCapability(), PIDs: agentprotocol.Capability{
					Status: agentprotocol.CapabilityUnavailable, ReasonCode: "cgroup-controller-pids-missing",
				},
			},
		},
		commands: commands, units: units, processors: func() int { return 4 },
	}
	_, err := reconciler.Reconcile(t.Context(), spec)
	var capabilityError *CapabilityError
	if !errors.As(err, &capabilityError) || capabilityError.Capability.ReasonCode != "cgroup-controller-pids-missing" {
		t.Fatalf("Reconcile error = %v", err)
	}
	if units.reconcileCalls != 0 || len(commands.invocations) != 0 {
		t.Fatal("resource mutation occurred despite unavailable controller")
	}
}

func TestReconcileWritesStartsAppliesAndVerifiesFixedSlice(t *testing.T) {
	t.Parallel()
	spec := resourceSpec(t)
	values, _ := hostingresources.InvocationValues(spec)
	commands := &fakeResourceCommands{}
	units := &fakeUnitManager{changed: true, controlGroup: "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200000.slice"}
	reconciler := &Reconciler{
		capabilities: availableResourceCapabilities(), commands: commands, units: units,
		processors: func() int { return 8 },
	}
	result, err := reconciler.Reconcile(t.Context(), spec)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if result.UnitName != "stackfort-accounts-200000.slice" || result.ControlGroup != units.controlGroup ||
		!result.UnitsChanged || !result.LimitsApplied || result.Capability.Status != agentprotocol.CapabilityAvailable {
		t.Fatalf("result = %#v", result)
	}
	want := []agentexec.Invocation{
		{Profile: agentexec.ProfileSystemdDaemonReload},
		{Profile: agentexec.ProfileSystemdStartAccountSlice, Values: values},
		{Profile: agentexec.ProfileSystemdApplyAccountLimits, Values: values},
	}
	if units.processorCount != 8 || !reflect.DeepEqual(commands.invocations, want) {
		t.Fatalf("processor count = %d, invocations = %#v", units.processorCount, commands.invocations)
	}
}

func TestRenderUnitsReservesHostCapacityAndMapsAllAccountLimits(t *testing.T) {
	t.Parallel()
	spec := resourceSpec(t)
	units, err := renderUnits(spec, 4)
	if err != nil {
		t.Fatalf("renderUnits: %v", err)
	}
	if len(units) != 3 || units[1].name != "stackfort-accounts.slice" ||
		!strings.Contains(units[1].content, "CPUQuota=320%\n") ||
		!strings.Contains(units[1].content, "MemoryMax=80%\n") ||
		!strings.Contains(units[0].content, "MemoryLow=20%\n") {
		t.Fatalf("platform units = %#v", units)
	}
	account := units[2].content
	for _, line := range []string{
		"CPUQuota=250%\n", "CPUWeight=800\n", "MemoryMax=536870912\n",
		"MemorySwapMax=0\n", "TasksMax=64\n",
	} {
		if !strings.Contains(account, line) {
			t.Fatalf("account unit omitted %q:\n%s", line, account)
		}
	}
}

type fakeResourceCapabilities struct {
	systemd agentprotocol.Capability
	cgroup  agentprotocol.CgroupCapabilities
}

func (capabilities fakeResourceCapabilities) InspectResourceControl() (
	agentprotocol.Capability,
	agentprotocol.CgroupCapabilities,
) {
	return capabilities.systemd, capabilities.cgroup
}

func availableResourceCapabilities() fakeResourceCapabilities {
	available := availableCapability()
	return fakeResourceCapabilities{systemd: available, cgroup: agentprotocol.CgroupCapabilities{
		Version: 2, Unified: available, CPU: available, Memory: available, IO: available, PIDs: available,
	}}
}

func availableCapability() agentprotocol.Capability {
	return agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
}

type fakeResourceCommands struct {
	invocations []agentexec.Invocation
	result      agentexec.Result
	err         error
}

func (commands *fakeResourceCommands) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	commands.invocations = append(commands.invocations, invocation)
	return commands.result, commands.err
}

type fakeUnitManager struct {
	changed        bool
	controlGroup   string
	err            error
	reconcileCalls int
	processorCount int
}

func (manager *fakeUnitManager) Reconcile(_ hostingresources.Spec, processors int) (bool, error) {
	manager.reconcileCalls++
	manager.processorCount = processors
	return manager.changed, manager.err
}

func (manager *fakeUnitManager) Verify(hostingresources.Spec) (string, error) {
	return manager.controlGroup, manager.err
}

func resourceSpec(t *testing.T) hostingresources.Spec {
	t.Helper()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return hostingresources.Spec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
			GID: hostingidentity.MinimumID, HomeDirectory: home,
		},
		CPUQuotaPercent: hostingresources.OptionalUint64{Set: true, Value: 250},
		CPUWeight:       hostingresources.OptionalUint64{Set: true, Value: 800},
		MemoryBytes:     hostingresources.OptionalUint64{Set: true, Value: 512 << 20},
		SwapBytes:       hostingresources.OptionalUint64{Set: true, Value: 0},
		ProcessLimit:    hostingresources.OptionalUint64{Set: true, Value: 64},
	}
}
