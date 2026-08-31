// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostacme owns the root-only HTTP-01 response directory. It exposes
// token reconciliation only, never an arbitrary file or path primitive.
package hostacme

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
)

var (
	ErrConflict       = errors.New("ACME HTTP-01 response conflicts with managed host state")
	ErrMutationFailed = errors.New("ACME HTTP-01 host mutation failed")
	ErrUnavailable    = errors.New("ACME HTTP-01 host mutation is unavailable")
)

type Result struct {
	Changed   bool
	Presented bool
}

type storage interface {
	Reconcile(context.Context, string, acmehttp01.Intent) (Result, error)
}

type platformInspector interface {
	InspectPlatform() agentprotocol.PlatformCapabilities
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type Presenter struct {
	storage  storage
	platform platformInspector
	commands commandRunner
}

func NewPresenter() *Presenter {
	return &Presenter{
		storage: newStorage(), platform: hostcapabilities.NewInspector(), commands: agentexec.NewRunner(),
	}
}

func (presenter *Presenter) Reconcile(
	ctx context.Context,
	operationID string,
	intent acmehttp01.Intent,
) (Result, error) {
	if presenter == nil || presenter.storage == nil || presenter.platform == nil || presenter.commands == nil ||
		ctx == nil || operationID == "" || acmehttp01.Validate(intent) != nil {
		return Result{}, ErrMutationFailed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, err := presenter.storage.Reconcile(ctx, operationID, intent)
	if err != nil {
		return Result{}, err
	}
	if intent.Action == acmehttp01.ActionPresent {
		platform := presenter.platform.InspectPlatform()
		if platform.Support.Status != agentprotocol.CapabilityAvailable {
			return Result{}, ErrUnavailable
		}
		if platform.DistributionID == "rocky" {
			if err := presenter.ensureSELinuxContext(ctx); err != nil {
				return Result{}, err
			}
		}
	}
	return result, nil
}

func (presenter *Presenter) ensureSELinuxContext(ctx context.Context) error {
	result, err := presenter.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfileAddSELinuxACMEContext})
	if err != nil {
		return ErrMutationFailed
	}
	if result.ExitCode != 0 {
		result, err = presenter.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfileModifySELinuxACMEContext})
		if err != nil || result.ExitCode != 0 {
			return ErrMutationFailed
		}
	}
	result, err = presenter.commands.Run(ctx, agentexec.Invocation{Profile: agentexec.ProfileRestoreSELinuxACMEContext})
	if err != nil || result.ExitCode != 0 {
		return ErrMutationFailed
	}
	return nil
}
