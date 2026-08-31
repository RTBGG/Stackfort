// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostjobs reconciles closed scheduled-account-job intent into
// root-owned systemd service/timer pairs. It never accepts command text,
// executable paths, unit names, or arbitrary calendar expressions.
package hostjobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

var (
	ErrInvalid     = errors.New("scheduled job intent is invalid")
	ErrNotFound    = errors.New("scheduled job script was not found")
	ErrConflict    = errors.New("scheduled job conflicts with host state")
	ErrUnavailable = errors.New("scheduled jobs are unavailable")
	ErrMutation    = errors.New("scheduled job host mutation failed")
)

type CapabilityError struct{ Capability agentprotocol.Capability }

func (failure *CapabilityError) Error() string { return ErrUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrUnavailable }

type Result struct {
	JobID       string
	Changed     bool
	Present     bool
	Enabled     bool
	ServiceUnit string
	TimerUnit   string
	Capability  agentprotocol.Capability
}

type platformInspector interface {
	InspectPlatform() agentprotocol.PlatformCapabilities
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type configurationManager interface {
	ValidateScript(scheduledjobs.Spec) error
	Managed(scheduledjobs.Spec) (bool, error)
	Prepare(scheduledjobs.RuntimeProfile, scheduledjobs.Spec) (*configurationChange, error)
	Remove(scheduledjobs.Spec) (*configurationChange, error)
	Verify(scheduledjobs.RuntimeProfile, scheduledjobs.Spec) error
}

type configurationChange struct {
	Changed  bool
	rollback func() error
	commit   func() error
}

func (change *configurationChange) Rollback() error {
	if change == nil || change.rollback == nil {
		return nil
	}
	return change.rollback()
}

func (change *configurationChange) Commit() error {
	if change == nil || change.commit == nil {
		return nil
	}
	return change.commit()
}

type Reconciler struct {
	platform platformInspector
	runner   commandRunner
	files    configurationManager
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		platform: hostcapabilities.NewInspector(), runner: agentexec.NewRunner(), files: newConfigurationManager(),
	}
}

func (reconciler *Reconciler) Reconcile(
	ctx context.Context, spec scheduledjobs.Spec, present bool,
) (Result, error) {
	if reconciler == nil || reconciler.platform == nil || reconciler.runner == nil || reconciler.files == nil || ctx == nil ||
		scheduledjobs.Validate(spec) != nil {
		return Result{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	platform := reconciler.platform.InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return Result{}, &CapabilityError{Capability: platform.Support}
	}
	serviceUnit, timerUnit, _ := scheduledjobs.UnitNames(spec.Identity, spec.Definition.ID)
	values, _ := scheduledjobs.InvocationValues(spec.Identity, spec.Definition.ID)
	baseResult := Result{
		JobID: spec.Definition.ID, Present: present, Enabled: present && spec.Definition.Enabled,
		ServiceUnit: serviceUnit, TimerUnit: timerUnit,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}
	managed, err := reconciler.files.Managed(spec)
	if err != nil {
		return Result{}, fmt.Errorf("inspect scheduled job units: %w", normalizeFileError(err))
	}
	state, stateErr := reconciler.timerState(ctx, values)
	if stateErr != nil && managed {
		return Result{}, stateErr
	}
	if !present && !managed && (stateErr != nil || !state.loaded) {
		return baseResult, nil
	}
	if present {
		profile, err := scheduledjobs.Profile(platform.DistributionID, spec.Definition)
		if err != nil {
			return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnsupported, ReasonCode: "scheduled-job-runtime-unsupported",
			}}
		}
		if err := reconciler.files.ValidateScript(spec); err != nil {
			return Result{}, fmt.Errorf("validate scheduled job script: %w", normalizeFileError(err))
		}
		return reconciler.reconcilePresent(ctx, profile, spec, values, state, baseResult)
	}
	return reconciler.reconcileAbsent(ctx, spec, values, state, baseResult)
}

func (reconciler *Reconciler) reconcilePresent(
	ctx context.Context, profile scheduledjobs.RuntimeProfile, spec scheduledjobs.Spec,
	values []string, previous timerState, result Result,
) (Result, error) {
	change, err := reconciler.files.Prepare(profile, spec)
	if err != nil {
		return Result{}, normalizeFileError(err)
	}
	rollback := func() { reconciler.rollback(change, values, previous) }
	changed := change.Changed
	if change.Changed && !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload}) {
		rollback()
		return Result{}, ErrMutation
	}
	if spec.Definition.Enabled {
		if !previous.enabled {
			if !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdEnableScheduledJob, Values: values}) {
				rollback()
				return Result{}, ErrMutation
			}
			changed = true
		}
		if change.Changed || !previous.active {
			if !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdRestartScheduledJob, Values: values}) {
				rollback()
				return Result{}, ErrMutation
			}
			changed = true
		}
	} else if previous.enabled || previous.active {
		if !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDisableScheduledJob, Values: values}) {
			rollback()
			return Result{}, ErrMutation
		}
		changed = true
	}
	final, err := reconciler.timerState(ctx, values)
	wantedActive := spec.Definition.Enabled
	if err != nil || !final.loaded || final.enabled != wantedActive || final.active != wantedActive ||
		reconciler.files.Verify(profile, spec) != nil {
		rollback()
		return Result{}, ErrMutation
	}
	if err := change.Commit(); err != nil {
		return Result{}, ErrMutation
	}
	result.Changed = changed
	return result, nil
}

func (reconciler *Reconciler) reconcileAbsent(
	ctx context.Context, spec scheduledjobs.Spec, values []string, previous timerState, result Result,
) (Result, error) {
	changed := false
	if previous.enabled || previous.active {
		if !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDisableScheduledJob, Values: values}) {
			return Result{}, ErrMutation
		}
		changed = true
	}
	if previous.loaded {
		if !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdCleanScheduledJob, Values: values}) {
			reconciler.restoreState(values, previous)
			return Result{}, ErrMutation
		}
	}
	change, err := reconciler.files.Remove(spec)
	if err != nil {
		reconciler.restoreState(values, previous)
		return Result{}, normalizeFileError(err)
	}
	if change.Changed {
		changed = true
		if !reconciler.runOK(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload}) {
			_ = change.Rollback()
			reconciler.reloadAndRestore(values, previous)
			return Result{}, ErrMutation
		}
	}
	final, finalErr := reconciler.timerState(ctx, values)
	if finalErr == nil && (final.loaded || final.active || final.enabled) {
		_ = change.Rollback()
		reconciler.reloadAndRestore(values, previous)
		return Result{}, ErrMutation
	}
	if err := change.Commit(); err != nil {
		return Result{}, ErrMutation
	}
	result.Changed = changed
	return result, nil
}

type timerState struct {
	loaded  bool
	active  bool
	enabled bool
}

func (reconciler *Reconciler) timerState(ctx context.Context, values []string) (timerState, error) {
	result, err := reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdShowScheduledJob, Values: values})
	if err != nil || result.ExitCode != 0 {
		return timerState{}, ErrUnavailable
	}
	properties := map[string]string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			properties[key] = value
		}
	}
	load, loadOK := properties["LoadState"]
	active, activeOK := properties["ActiveState"]
	unitFile, unitFileOK := properties["UnitFileState"]
	if !loadOK || !activeOK || !unitFileOK {
		return timerState{}, ErrUnavailable
	}
	return timerState{
		loaded: load == "loaded", active: active == "active",
		enabled: unitFile == "enabled" || unitFile == "enabled-runtime",
	}, nil
}

func (reconciler *Reconciler) runOK(ctx context.Context, invocation agentexec.Invocation) bool {
	result, err := reconciler.run(ctx, invocation)
	return err == nil && result.ExitCode == 0
}

func (reconciler *Reconciler) run(ctx context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	result, err := reconciler.runner.Run(ctx, invocation)
	if errors.Is(err, agentexec.ErrUnsupportedPlatform) || errors.Is(err, agentexec.ErrStart) ||
		errors.Is(err, agentexec.ErrNotAllowlisted) {
		return agentexec.Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "scheduled-job-command-unavailable",
		}}
	}
	return result, err
}

func (reconciler *Reconciler) rollback(change *configurationChange, values []string, previous timerState) {
	_ = change.Rollback()
	reconciler.reloadAndRestore(values, previous)
}

func (reconciler *Reconciler) reloadAndRestore(values []string, previous timerState) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload})
	reconciler.restoreStateWithContext(ctx, values, previous)
}

func (reconciler *Reconciler) restoreState(values []string, previous timerState) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reconciler.restoreStateWithContext(ctx, values, previous)
}

func (reconciler *Reconciler) restoreStateWithContext(ctx context.Context, values []string, previous timerState) {
	if previous.enabled {
		_, _ = reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdEnableScheduledJob, Values: values})
		if previous.active {
			_, _ = reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdRestartScheduledJob, Values: values})
		}
		return
	}
	_, _ = reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDisableScheduledJob, Values: values})
}

func normalizeFileError(err error) error {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrNotFound), errors.Is(err, ErrConflict), errors.Is(err, ErrUnavailable):
		return err
	default:
		return ErrMutation
	}
}
