// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostnginx reconciles Stackfort's conflict-safe NGINX baseline.
package hostnginx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

var (
	ErrConflict         = errors.New("managed NGINX baseline conflicts with existing host state")
	ErrValidationFailed = errors.New("managed NGINX configuration validation failed")
	ErrActivationFailed = errors.New("managed NGINX service activation failed")
)

type CapabilityError struct {
	Capability agentprotocol.Capability
}

func (failure *CapabilityError) Error() string { return "managed NGINX capability is unavailable" }

type Result struct {
	Changed             bool
	ConfigurationTested bool
	ActivationPerformed bool
	EnablementChanged   bool
	Capability          agentprotocol.Capability
}

type platformInspector interface {
	InspectPlatform() agentprotocol.PlatformCapabilities
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type configurationManager interface {
	Managed() (bool, error)
	Prepare(nginxbaseline.Spec) (*configurationChange, error)
}

type configurationChange struct {
	Changed           bool
	DropInChanged     bool
	PreviouslyManaged bool
	commit            func() error
	rollback          func() error
}

func (change *configurationChange) Commit() error {
	if change == nil || change.commit == nil {
		return nil
	}
	return change.commit()
}

func (change *configurationChange) Rollback() error {
	if change == nil || change.rollback == nil {
		return nil
	}
	return change.rollback()
}

type Reconciler struct {
	platform      platformInspector
	runner        commandRunner
	configuration configurationManager
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		platform: hostcapabilities.NewInspector(), runner: agentexec.NewRunner(),
		configuration: newConfigurationManager(),
	}
}

func (reconciler *Reconciler) Reconcile(ctx context.Context) (Result, error) {
	platform := reconciler.platform.InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return Result{}, &CapabilityError{Capability: platform.Support}
	}
	spec, err := nginxbaseline.ForDistribution(platform.DistributionID)
	if err != nil {
		return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "nginx-distribution-unsupported",
		}}
	}
	if result, runErr := reconciler.run(ctx, agentexec.ProfileNGINXVersion); runErr != nil || result.ExitCode != 0 {
		return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "nginx-binary-unavailable",
		}}
	}

	before, err := reconciler.serviceState(ctx)
	if err != nil || before.loadState != "loaded" {
		return Result{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "nginx-systemd-unit-unavailable",
		}}
	}
	managed, err := reconciler.configuration.Managed()
	if err != nil {
		return Result{}, fmt.Errorf("%w: inspect ownership marker", ErrConflict)
	}
	if before.active && !managed {
		return Result{}, fmt.Errorf("%w: unmanaged nginx.service is active", ErrConflict)
	}

	change, err := reconciler.configuration.Prepare(spec)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("prepare managed NGINX baseline: %w", err)
	}
	rollback := func(activationAttempted, enablementAttempted bool) {
		_ = change.Rollback()
		if change.DropInChanged {
			_, _ = reconciler.run(ctx, agentexec.ProfileSystemdDaemonReload)
		}
		switch {
		case before.active && change.PreviouslyManaged:
			_, _ = reconciler.run(ctx, agentexec.ProfileSystemdRestartNGINX)
		case activationAttempted && !before.active:
			_, _ = reconciler.run(ctx, agentexec.ProfileSystemdStopNGINX)
		}
		if enablementAttempted && !before.enabled {
			_, _ = reconciler.run(ctx, agentexec.ProfileSystemdDisableNGINX)
		}
	}

	if change.DropInChanged {
		if result, runErr := reconciler.run(ctx, agentexec.ProfileSystemdDaemonReload); runErr != nil || result.ExitCode != 0 {
			rollback(false, false)
			return Result{}, fmt.Errorf("%w: reload systemd manager configuration", ErrActivationFailed)
		}
	}
	if result, runErr := reconciler.run(ctx, agentexec.ProfileNGINXTestBaseline); runErr != nil || result.ExitCode != 0 {
		rollback(false, false)
		return Result{}, ErrValidationFailed
	}

	enablementChanged := false
	if !before.enabled {
		enablementChanged = true
		if result, runErr := reconciler.run(ctx, agentexec.ProfileSystemdEnableNGINX); runErr != nil || result.ExitCode != 0 {
			rollback(false, true)
			return Result{}, ErrActivationFailed
		}
	}

	activationPerformed := false
	if change.Changed || !before.active {
		profile := agentexec.ProfileSystemdRestartNGINX
		if before.active && managed && !change.DropInChanged {
			profile = agentexec.ProfileSystemdReloadNGINX
		}
		activationPerformed = true
		if result, runErr := reconciler.run(ctx, profile); runErr != nil || result.ExitCode != 0 {
			rollback(true, enablementChanged)
			return Result{}, ErrActivationFailed
		}
	}
	after, err := reconciler.serviceState(ctx)
	if err != nil || after.loadState != "loaded" || !after.active || !after.enabled {
		rollback(activationPerformed, enablementChanged)
		return Result{}, ErrActivationFailed
	}
	if err := change.Commit(); err != nil {
		rollback(activationPerformed, enablementChanged)
		return Result{}, fmt.Errorf("commit managed NGINX baseline: %w", err)
	}
	return Result{
		Changed: change.Changed || enablementChanged, ConfigurationTested: true,
		ActivationPerformed: activationPerformed,
		EnablementChanged:   enablementChanged,
		Capability:          agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}, nil
}

type serviceStatus struct {
	loadState string
	active    bool
	enabled   bool
}

func (reconciler *Reconciler) serviceState(ctx context.Context) (serviceStatus, error) {
	return serviceStateWithRunner(ctx, reconciler.runner)
}

func serviceStateWithRunner(ctx context.Context, runner commandRunner) (serviceStatus, error) {
	result, err := runner.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileSystemctlShow, Values: []string{nginxbaseline.NGINXUnit},
	})
	if err != nil || result.ExitCode != 0 {
		return serviceStatus{}, errors.New("inspect nginx.service")
	}
	values := make(map[string]string)
	for _, line := range strings.Split(result.Stdout, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			values[key] = value
		}
	}
	if values["LoadState"] == "" || values["ActiveState"] == "" || values["UnitFileState"] == "" {
		return serviceStatus{}, errors.New("malformed nginx.service state")
	}
	return serviceStatus{
		loadState: values["LoadState"], active: values["ActiveState"] == "active",
		enabled: values["UnitFileState"] == "enabled",
	}, nil
}

func (reconciler *Reconciler) run(ctx context.Context, profile agentexec.ProfileID) (agentexec.Result, error) {
	return reconciler.runner.Run(ctx, agentexec.Invocation{Profile: profile})
}
