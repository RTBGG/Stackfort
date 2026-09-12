// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func requireContinuationReady(state storageprep.State, exists bool, plan storageprep.Plan) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if !exists || state.Plan != plan || state.Phase != storageprep.Ready {
		return errors.New("package continuation requires the exact ready storage plan")
	}
	return nil
}

// This adapter never escapes the coordinator. Guard checks precede every
// package/service mutation and verification, including completed-install reruns.
type continuationRunner struct {
	Runner
	guard func(context.Context) error
}

func (runner continuationRunner) Preflight(ctx context.Context) error {
	if err := runner.guard(ctx); err != nil {
		return err
	}
	return runner.Runner.Preflight(ctx)
}

func (runner continuationRunner) Apply(ctx context.Context, id StageID, source Source) (bool, error) {
	if err := runner.guard(ctx); err != nil {
		return false, err
	}
	return runner.Runner.Apply(ctx, id, source)
}

func (runner continuationRunner) Verify(ctx context.Context, id StageID, source Source) error {
	if err := runner.guard(ctx); err != nil {
		return err
	}
	return runner.Runner.Verify(ctx, id, source)
}

func (runner continuationRunner) VerifyInstallation(ctx context.Context, source Source) error {
	if err := runner.guard(ctx); err != nil {
		return err
	}
	return runner.Runner.VerifyInstallation(ctx, source)
}
