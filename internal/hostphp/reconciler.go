// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostphp owns version-specific PHP-FPM pools for hosting accounts.
// It accepts only phpruntime's closed intent and fixed process profiles.
package hostphp

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

var (
	ErrConflict       = errors.New("managed PHP-FPM pool conflicts with existing host state")
	ErrUnavailable    = errors.New("managed PHP-FPM runtime is unavailable")
	ErrValidation     = errors.New("managed PHP-FPM configuration validation failed")
	ErrActivation     = errors.New("managed PHP-FPM pool activation failed")
	ErrInspection     = errors.New("managed PHP-FPM pool inspection failed")
	ErrMutationFailed = errors.New("managed PHP-FPM pool mutation failed")
)

type CapabilityError struct{ Capability agentprotocol.Capability }

func (failure *CapabilityError) Error() string { return ErrUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrUnavailable }

type Result struct {
	Versions   []string
	Changed    bool
	Active     bool
	Capability agentprotocol.Capability
}

type platformInspector interface {
	InspectPlatform() agentprotocol.PlatformCapabilities
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type configurationManager interface {
	Prepare(phpruntime.Profile, phpruntime.PoolSetSpec) (*configurationChange, error)
	Managed(phpruntime.PoolSetSpec, string) (bool, error)
	Remove(phpruntime.PoolSetSpec, string) (bool, error)
	VerifyRuntime(phpruntime.Profile, phpruntime.PoolSetSpec) error
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

func (reconciler *Reconciler) Reconcile(ctx context.Context, spec phpruntime.PoolSetSpec) (Result, error) {
	if reconciler == nil || reconciler.platform == nil || reconciler.runner == nil || reconciler.files == nil || ctx == nil {
		return Result{}, ErrMutationFailed
	}
	if err := phpruntime.Validate(spec); err != nil {
		return Result{}, ErrMutationFailed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	platform := reconciler.platform.InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return Result{}, &CapabilityError{Capability: platform.Support}
	}
	nativeVersion, err := phpruntime.ApprovedVersion(platform.DistributionID)
	if err != nil {
		return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "php-runtime-distribution-unsupported",
		}}
	}
	profiles := make([]phpruntime.Profile, 0, len(spec.Versions))
	for _, version := range spec.Versions {
		if version != nativeVersion {
			return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnsupported, ReasonCode: "php-runtime-version-unsupported",
			}}
		}
		profile, profileErr := phpruntime.ForDistribution(platform.DistributionID, version)
		if profileErr != nil {
			return Result{}, ErrMutationFailed
		}
		profiles = append(profiles, profile)
	}

	changes := make([]*configurationChange, 0, len(profiles))
	previousStates := make(map[string]serviceState, len(profiles))
	activationBegan := false
	rollback := func() {
		if activationBegan {
			for _, profile := range profiles {
				if _, recorded := previousStates[profile.Version]; !recorded {
					continue
				}
				values, _ := phpruntime.VersionInvocationValues(spec.Identity, profile.Version)
				_, _ = reconciler.run(ctx, agentexec.Invocation{
					Profile: agentexec.ProfileSystemdDisablePHPPool, Values: values,
				})
			}
		}
		for index := len(changes) - 1; index >= 0; index-- {
			_ = changes[index].Rollback()
		}
		_, _ = reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload})
		if activationBegan {
			for _, profile := range profiles {
				state, recorded := previousStates[profile.Version]
				if !recorded {
					continue
				}
				values, _ := phpruntime.VersionInvocationValues(spec.Identity, profile.Version)
				if state.enabled {
					_, _ = reconciler.run(ctx, agentexec.Invocation{
						Profile: agentexec.ProfileSystemdEnablePHPPool, Values: values,
					})
				}
				if state.active {
					_, _ = reconciler.run(ctx, agentexec.Invocation{
						Profile: agentexec.ProfileSystemdRestartPHPPool, Values: values,
					})
				}
			}
		}
	}
	changed := false
	for _, profile := range profiles {
		change, prepareErr := reconciler.files.Prepare(profile, spec)
		if prepareErr != nil {
			rollback()
			if errors.Is(prepareErr, ErrConflict) {
				return Result{}, ErrConflict
			}
			return Result{}, ErrMutationFailed
		}
		changes = append(changes, change)
		changed = changed || change.Changed
		values, _ := phpruntime.VersionInvocationValues(spec.Identity, profile.Version)
		if result, runErr := reconciler.run(ctx, agentexec.Invocation{
			Profile: phpTestProfile(profile.Version), Values: values,
		}); runErr != nil || result.ExitCode != 0 {
			rollback()
			return Result{}, ErrValidation
		}
	}
	if changed {
		if result, runErr := reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload}); runErr != nil || result.ExitCode != 0 {
			rollback()
			return Result{}, ErrActivation
		}
	}
	for _, profile := range profiles {
		values, _ := phpruntime.VersionInvocationValues(spec.Identity, profile.Version)
		state, stateErr := reconciler.poolState(ctx, values)
		if stateErr != nil {
			state = serviceState{}
		}
		previousStates[profile.Version] = state
		activationBegan = true
		if !state.enabled {
			if result, runErr := reconciler.run(ctx, agentexec.Invocation{
				Profile: agentexec.ProfileSystemdEnablePHPPool, Values: values,
			}); runErr != nil || result.ExitCode != 0 {
				rollback()
				return Result{}, ErrActivation
			}
			changed = true
		}
		if changed || !state.active {
			if result, runErr := reconciler.run(ctx, agentexec.Invocation{
				Profile: agentexec.ProfileSystemdRestartPHPPool, Values: values,
			}); runErr != nil || result.ExitCode != 0 {
				rollback()
				return Result{}, ErrActivation
			}
			changed = true
		}
		state, stateErr = reconciler.poolState(ctx, values)
		expectedGroup := "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-" +
			strconv.FormatUint(uint64(spec.Identity.UID), 10) + ".slice/"
		if stateErr != nil || !state.active || !state.enabled || !strings.HasPrefix(state.controlGroup, expectedGroup) ||
			reconciler.files.VerifyRuntime(profile, spec) != nil {
			rollback()
			return Result{}, ErrActivation
		}
	}

	removed := false
	if spec.RetireAbsent {
		desired := make(map[string]struct{}, len(spec.Versions))
		for _, version := range spec.Versions {
			desired[version] = struct{}{}
		}
		for _, version := range []string{"8.3", "8.4", "8.5"} {
			if _, keep := desired[version]; keep {
				continue
			}
			managed, inspectErr := reconciler.files.Managed(spec, version)
			if inspectErr != nil {
				return Result{}, ErrConflict
			}
			if !managed {
				continue
			}
			values, _ := phpruntime.VersionInvocationValues(spec.Identity, version)
			if result, runErr := reconciler.run(ctx, agentexec.Invocation{
				Profile: agentexec.ProfileSystemdDisablePHPPool, Values: values,
			}); runErr != nil || result.ExitCode != 0 {
				return Result{}, ErrActivation
			}
			if _, removeErr := reconciler.files.Remove(spec, version); removeErr != nil {
				return Result{}, removeErr
			}
			removed = true
		}
	}
	if removed {
		if result, runErr := reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdDaemonReload}); runErr != nil || result.ExitCode != 0 {
			return Result{}, ErrActivation
		}
		changed = true
	}
	for _, change := range changes {
		if err := change.Commit(); err != nil {
			return Result{}, ErrMutationFailed
		}
	}
	return Result{
		Versions: append([]string(nil), spec.Versions...), Changed: changed, Active: true,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}, nil
}

type serviceState struct {
	active       bool
	enabled      bool
	controlGroup string
}

func (reconciler *Reconciler) poolState(ctx context.Context, values []string) (serviceState, error) {
	result, err := reconciler.run(ctx, agentexec.Invocation{Profile: agentexec.ProfileSystemdShowPHPPool, Values: values})
	if err != nil || result.ExitCode != 0 {
		return serviceState{}, ErrActivation
	}
	properties := make(map[string]string)
	for _, line := range strings.Split(result.Stdout, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			properties[key] = value
		}
	}
	if properties["LoadState"] == "" || properties["ActiveState"] == "" ||
		properties["UnitFileState"] == "" || properties["ControlGroup"] == "" {
		return serviceState{}, ErrActivation
	}
	return serviceState{
		active:       properties["LoadState"] == "loaded" && properties["ActiveState"] == "active",
		enabled:      properties["UnitFileState"] == "enabled",
		controlGroup: properties["ControlGroup"],
	}, nil
}

func (reconciler *Reconciler) run(ctx context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	result, err := reconciler.runner.Run(ctx, invocation)
	if errors.Is(err, agentexec.ErrUnsupportedPlatform) || errors.Is(err, agentexec.ErrStart) ||
		errors.Is(err, agentexec.ErrNotAllowlisted) {
		return agentexec.Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "php-runtime-command-unavailable",
		}}
	}
	return result, err
}

func phpTestProfile(version string) agentexec.ProfileID {
	switch version {
	case "8.3":
		return agentexec.ProfilePHPFPM83Test
	case "8.4":
		return agentexec.ProfilePHPFPM84Test
	case "8.5":
		return agentexec.ProfilePHPFPM85Test
	default:
		return ""
	}
}
