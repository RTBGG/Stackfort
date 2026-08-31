// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInstallIsJournaledAndSuccessfulRerunIsNoOp(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	runner := newFakeRunner()
	engine, _ := NewEngine(store, runner)
	engine.now = advancingClock()
	source := Source{Root: "/release", Version: "1.2.3", Digest: "digest"}

	first, err := engine.Install(t.Context(), source)
	if err != nil || first.Status != InstallComplete || !first.Changed || first.AlreadyInstalled || first.Resumed {
		t.Fatalf("first result=%#v error=%v", first, err)
	}
	if runner.preflightCalls != 1 || len(runner.applyCalls) != len(orderedStages) {
		t.Fatalf("preflight=%d apply=%#v", runner.preflightCalls, runner.applyCalls)
	}
	second, err := engine.Install(t.Context(), source)
	if err != nil || second.Status != InstallComplete || second.Changed || !second.AlreadyInstalled || second.Resumed {
		t.Fatalf("second result=%#v error=%v", second, err)
	}
	if runner.preflightCalls != 1 || len(runner.applyCalls) != len(orderedStages) {
		t.Fatalf("rerun mutated: preflight=%d apply=%#v", runner.preflightCalls, runner.applyCalls)
	}
	for _, stage := range second.Stages {
		if stage.Status != StageComplete || stage.Attempts != 1 || !stage.Changed {
			t.Fatalf("stage = %#v", stage)
		}
	}
}

func TestInterruptedStageResumesFromJournal(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	runner := newFakeRunner()
	runner.failOnceAt = StageSecurity
	engine, _ := NewEngine(store, runner)
	engine.now = advancingClock()
	source := Source{Root: "/release", Version: "1.2.3", Digest: "digest"}

	first, err := engine.Install(t.Context(), source)
	if err == nil || first.Status != InstallFailed {
		t.Fatalf("first result=%#v error=%v", first, err)
	}
	failed := first.Stages[indexOfStage(t, StageSecurity)]
	if failed.Status != StageFailed || failed.Attempts != 1 || !failed.Changed {
		t.Fatalf("failed stage = %#v", failed)
	}
	completedApplyCalls := len(runner.applyCalls)

	second, err := engine.Install(t.Context(), source)
	if err != nil || second.Status != InstallComplete || !second.Resumed || second.AlreadyInstalled {
		t.Fatalf("resumed result=%#v error=%v", second, err)
	}
	if runner.preflightCalls != 1 {
		t.Fatalf("resume reran fresh-host preflight %d times", runner.preflightCalls)
	}
	security := second.Stages[indexOfStage(t, StageSecurity)]
	if security.Attempts != 2 || security.Status != StageComplete {
		t.Fatalf("resumed stage = %#v", security)
	}
	for _, call := range runner.applyCalls[completedApplyCalls:] {
		if call == StagePackages || call == StageIdentity || call == StagePayload || call == StageConfiguration {
			t.Fatalf("resume reapplied completed stage %s", call)
		}
	}
}

func TestApplyingJournalLeftByProcessInterruptionResumes(t *testing.T) {
	t.Parallel()

	store := &completionInterruptStore{stage: StageSecurity}
	runner := newFakeRunner()
	engine, _ := NewEngine(store, runner)
	engine.now = advancingClock()
	source := Source{Root: "/release", Version: "1.2.3", Digest: "digest"}

	if _, err := engine.Install(t.Context(), source); err == nil {
		t.Fatal("simulated journal interruption was accepted")
	}
	interrupted := store.journal.Stages[indexOfStage(t, StageSecurity)]
	if interrupted.Status != StageApplying || interrupted.Attempts != 1 || !runner.applied[StageSecurity] {
		t.Fatalf("interrupted stage = %#v", interrupted)
	}

	resumed, err := engine.Install(t.Context(), source)
	if err != nil || resumed.Status != InstallComplete || !resumed.Resumed {
		t.Fatalf("resumed result=%#v error=%v", resumed, err)
	}
	security := resumed.Stages[indexOfStage(t, StageSecurity)]
	if security.Status != StageComplete || security.Attempts != 2 {
		t.Fatalf("resumed interrupted stage = %#v", security)
	}
}

func TestJournalCannotBeReboundToAnotherSource(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	runner := newFakeRunner()
	engine, _ := NewEngine(store, runner)
	engine.now = advancingClock()
	if _, err := engine.Install(t.Context(), Source{Root: "/release", Version: "1.2.3", Digest: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Install(t.Context(), Source{Root: "/other", Version: "1.2.3", Digest: "two"}); err == nil {
		t.Fatal("completed journal was rebound to another source")
	}
}

func TestPreflightFailureDoesNotCreateJournal(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	runner := newFakeRunner()
	runner.preflightErr = errors.New("blocked")
	engine, _ := NewEngine(store, runner)
	if _, err := engine.Install(t.Context(), Source{Root: "/release", Version: "1.2.3", Digest: "one"}); err == nil {
		t.Fatal("blocked preflight was accepted")
	}
	if store.exists || len(runner.applyCalls) != 0 {
		t.Fatal("blocked preflight mutated installation state")
	}
}

type memoryStore struct {
	journal Journal
	exists  bool
}

type completionInterruptStore struct {
	memoryStore
	stage  StageID
	failed bool
}

func (store *completionInterruptStore) Save(journal Journal) error {
	for _, stage := range journal.Stages {
		if !store.failed && stage.ID == store.stage && stage.Status == StageComplete {
			store.failed = true
			return errors.New("simulated process interruption before completion journal write")
		}
	}
	return store.memoryStore.Save(journal)
}

func (store *memoryStore) Load() (Journal, bool, error) { return store.journal, store.exists, nil }
func (store *memoryStore) Save(journal Journal) error {
	store.journal = journal
	store.journal.Stages = append([]StageState(nil), journal.Stages...)
	store.exists = true
	return nil
}

type fakeRunner struct {
	distribution   string
	preflightErr   error
	preflightCalls int
	applyCalls     []StageID
	applied        map[StageID]bool
	failOnceAt     StageID
	failed         bool
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{distribution: "debian", applied: make(map[StageID]bool)}
}

func (runner *fakeRunner) Distribution() string { return runner.distribution }
func (runner *fakeRunner) Preflight(context.Context) error {
	runner.preflightCalls++
	return runner.preflightErr
}
func (runner *fakeRunner) Apply(_ context.Context, stage StageID, _ Source) (bool, error) {
	runner.applyCalls = append(runner.applyCalls, stage)
	runner.applied[stage] = true
	if stage == runner.failOnceAt && !runner.failed {
		runner.failed = true
		return true, errors.New("simulated interruption after idempotent side effect")
	}
	return true, nil
}
func (runner *fakeRunner) Verify(_ context.Context, stage StageID, _ Source) error {
	if !runner.applied[stage] {
		return errors.New("stage is not applied")
	}
	return nil
}
func (runner *fakeRunner) VerifyInstallation(ctx context.Context, source Source) error {
	for _, stage := range orderedStages {
		if err := runner.Verify(ctx, stage, source); err != nil {
			return err
		}
	}
	return nil
}

func advancingClock() func() time.Time {
	current := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		current = current.Add(time.Second)
		return current
	}
}

func indexOfStage(t *testing.T, wanted StageID) int {
	t.Helper()
	for index, stage := range orderedStages {
		if stage == wanted {
			return index
		}
	}
	t.Fatalf("stage %s not found", wanted)
	return -1
}
