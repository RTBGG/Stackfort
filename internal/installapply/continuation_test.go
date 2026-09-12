// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

func TestContinuationRequiresExactReadyPlan(t *testing.T) {
	plan := pinPlan(testSourcePin())
	state := storageprep.State{SchemaVersion: 1, Plan: plan, Phase: storageprep.Ready, ArmAttempts: 1, ResumeBootID: "30000000-0000-4000-8000-000000000099"}
	if err := requireContinuationReady(state, true, plan); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.RecoveryRequired} {
		other := state
		other.Phase = phase
		if phase == storageprep.Planned {
			other.ArmAttempts = 0
		}
		if phase != storageprep.Verifying && phase != storageprep.RecoveryRequired {
			other.ResumeBootID = ""
		}
		if phase == storageprep.RecoveryRequired {
			other.FailureCode = "release-invalid"
		}
		if err := other.Validate(); err != nil {
			t.Fatal("invalid test fixture", err)
		}
		if requireContinuationReady(other, true, plan) == nil {
			t.Fatal("accepted", phase)
		}
	}
	if requireContinuationReady(state, false, plan) == nil {
		t.Fatal("missing journal accepted")
	}
	other := plan
	other.OperationID = "30000000-0000-4000-8000-000000000098"
	if requireContinuationReady(state, true, other) == nil {
		t.Fatal("different operation accepted")
	}
}

func TestContinuationGuardStopsEveryRunnerEntry(t *testing.T) {
	denied := errors.New("readiness lost")
	underlying := newFakeRunner()
	calls := 0
	runner := continuationRunner{Runner: underlying, guard: func(context.Context) error { calls++; return denied }}
	source := Source{Root: "/retained", Version: "1.2.3", Digest: "digest"}
	if !errors.Is(runner.Preflight(t.Context()), denied) {
		t.Fatal("preflight bypass")
	}
	if changed, err := runner.Apply(t.Context(), StagePackages, source); changed || !errors.Is(err, denied) {
		t.Fatal("apply bypass")
	}
	if !errors.Is(runner.Verify(t.Context(), StagePackages, source), denied) {
		t.Fatal("verify bypass")
	}
	if !errors.Is(runner.VerifyInstallation(t.Context(), source), denied) {
		t.Fatal("completion bypass")
	}
	if calls != 4 || underlying.preflightCalls != 0 || len(underlying.applyCalls) != 0 {
		t.Fatal("runner called despite failed guard")
	}
}

func TestContinuationRechecksReadinessOnCompletedRerun(t *testing.T) {
	underlying := newFakeRunner()
	denied := false
	calls := 0
	runner := continuationRunner{Runner: underlying, guard: func(context.Context) error {
		calls++
		if denied {
			return errors.New("changed source")
		}
		return nil
	}}
	engine, _ := NewEngine(&memoryStore{}, runner)
	source := Source{Root: "/retained", Version: "1.2.3", Digest: "digest"}
	if _, err := engine.Install(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if calls != 2*len(orderedStages)+2 {
		t.Fatal("missing per-stage guard", calls)
	}
	denied = true
	if _, err := engine.Install(t.Context(), source); err == nil {
		t.Fatal("completed installation bypassed guard")
	}
	if len(underlying.applyCalls) != len(orderedStages) {
		t.Fatal("replayed completed stages")
	}
}
