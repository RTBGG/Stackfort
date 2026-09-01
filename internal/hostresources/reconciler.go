// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostresources owns Stackfort's fixed systemd slice hierarchy and
// cgroup-v2 account limits. It exposes no generic unit, property, path, or
// command operation.
package hostresources

import (
	"context"
	"errors"
	"runtime"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostingresources"
)

var (
	ErrConflict           = errors.New("managed account resource unit conflicts with existing host state")
	ErrControlUnavailable = errors.New("systemd cgroup-v2 resource control is unavailable")
	ErrMutationFailed     = errors.New("managed account resource mutation failed")
	errUnsupportedHost    = errors.New("managed account resources are unsupported on this host")
)

type Result struct {
	UnitName                string
	ControlGroup            string
	UnitsChanged            bool
	LimitsApplied           bool
	UserManagerControlGroup string
	Capability              agentprotocol.Capability
}

type CapabilityError struct {
	Capability agentprotocol.Capability
}

func (failure *CapabilityError) Error() string { return ErrControlUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrControlUnavailable }

type capabilityInspector interface {
	InspectResourceControl() (agentprotocol.Capability, agentprotocol.CgroupCapabilities)
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type unitManager interface {
	Reconcile(hostingresources.Spec, int) (bool, error)
	Verify(hostingresources.Spec) (string, error)
}

type Reconciler struct {
	capabilities capabilityInspector
	commands     commandRunner
	units        unitManager
	processors   func() int
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		capabilities: hostcapabilities.NewInspector(), commands: agentexec.NewRunner(),
		units: newUnitManager(), processors: runtime.NumCPU,
	}
}

func (reconciler *Reconciler) Reconcile(ctx context.Context, spec hostingresources.Spec) (Result, error) {
	if reconciler == nil || reconciler.capabilities == nil || reconciler.commands == nil ||
		reconciler.units == nil || reconciler.processors == nil || ctx == nil {
		return Result{}, ErrMutationFailed
	}
	if err := hostingresources.Validate(spec); err != nil {
		return Result{}, ErrMutationFailed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	systemd, cgroup := reconciler.capabilities.InspectResourceControl()
	if capability := unavailableResourceCapability(systemd, cgroup); capability != nil {
		return Result{}, &CapabilityError{Capability: *capability}
	}
	processorCount := reconciler.processors()
	if processorCount < 1 || processorCount > 16_384 {
		return Result{}, ErrMutationFailed
	}
	changed, err := reconciler.units.Reconcile(spec, processorCount)
	if errors.Is(err, ErrConflict) {
		return Result{}, ErrConflict
	}
	if errors.Is(err, errUnsupportedHost) {
		return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "resource-control-host-unsupported",
		}}
	}
	if err != nil {
		return Result{}, ErrMutationFailed
	}
	values, err := hostingresources.InvocationValues(spec)
	if err != nil {
		return Result{}, ErrMutationFailed
	}
	// Always reload. This closes the crash gap where the managed drop-in was
	// fsynced but PID 1 had not consumed it before the previous reconciliation
	// stopped.
	if err := reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload}); err != nil {
		return Result{}, err
	}
	for _, profile := range []agentexec.ProfileID{
		agentexec.ProfileSystemdStartAccountSlice,
		agentexec.ProfileSystemdApplyAccountLimits,
	} {
		if err := reconciler.run(ctx, agentexec.Invocation{Profile: profile, Values: values}); err != nil {
			return Result{}, err
		}
	}
	controlGroup, err := reconciler.units.Verify(spec)
	if err != nil {
		return Result{}, ErrMutationFailed
	}
	userManagerControlGroup, err := reconciler.ensureUserManagerPlacement(ctx, values, spec.Identity.UID)
	if err != nil {
		return Result{}, err
	}
	unitName, _ := hostingresources.AccountSliceName(spec.Identity.UID)
	return Result{
		UnitName: unitName, ControlGroup: controlGroup, UnitsChanged: changed, LimitsApplied: true,
		UserManagerControlGroup: userManagerControlGroup,
		Capability:              agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}, nil
}

func (reconciler *Reconciler) ensureUserManagerPlacement(
	ctx context.Context, values []string, uid uint32,
) (string, error) {
	wanted, err := hostingresources.UserManagerControlGroup(uid)
	if err != nil {
		return "", ErrMutationFailed
	}
	state, controlGroup, err := reconciler.userManagerState(ctx, values)
	if err != nil {
		return "", err
	}
	if state == "inactive" || state == "failed" {
		if controlGroup != "" {
			return "", ErrMutationFailed
		}
		return "", nil
	}
	if state != "active" && state != "activating" && state != "reloading" {
		return "", ErrMutationFailed
	}
	if controlGroup == wanted {
		return controlGroup, nil
	}
	if err := reconciler.run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileSystemdRestartAccountUserManager, Values: values,
	}); err != nil {
		return "", err
	}
	state, controlGroup, err = reconciler.userManagerState(ctx, values)
	if err != nil || state != "active" || controlGroup != wanted {
		return "", ErrMutationFailed
	}
	return controlGroup, nil
}

func (reconciler *Reconciler) userManagerState(ctx context.Context, values []string) (string, string, error) {
	result, err := reconciler.commands.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileSystemdShowAccountUserManager, Values: values,
	})
	if err != nil || result.ExitCode != 0 {
		return "", "", ErrMutationFailed
	}
	properties := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		name, value, found := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if !found || (name != "ActiveState" && name != "ControlGroup") {
			return "", "", ErrMutationFailed
		}
		if _, duplicate := properties[name]; duplicate {
			return "", "", ErrMutationFailed
		}
		properties[name] = value
	}
	state, stateOK := properties["ActiveState"]
	controlGroup, groupOK := properties["ControlGroup"]
	if !stateOK || !groupOK || strings.ContainsRune(controlGroup, '\x00') {
		return "", "", ErrMutationFailed
	}
	return state, controlGroup, nil
}

func (reconciler *Reconciler) run(ctx context.Context, invocation agentexec.Invocation) error {
	result, err := reconciler.commands.Run(ctx, invocation)
	if errors.Is(err, agentexec.ErrStart) || errors.Is(err, agentexec.ErrNotAllowlisted) {
		return &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "systemctl-unavailable",
		}}
	}
	if errors.Is(err, agentexec.ErrUnsupportedPlatform) {
		return &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "resource-control-host-unsupported",
		}}
	}
	if err != nil || result.ExitCode != 0 {
		return ErrMutationFailed
	}
	return nil
}

func unavailableResourceCapability(
	systemd agentprotocol.Capability,
	cgroup agentprotocol.CgroupCapabilities,
) *agentprotocol.Capability {
	for _, capability := range []agentprotocol.Capability{
		systemd, cgroup.Unified, cgroup.CPU, cgroup.Memory, cgroup.PIDs,
	} {
		if capability.Status != agentprotocol.CapabilityAvailable {
			value := capability
			return &value
		}
	}
	return nil
}
