// SPDX-License-Identifier: AGPL-3.0-or-later

package updateapply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestUpdateCompletesEveryHealthGatedStage(t *testing.T) {
	store := &memoryStore{}
	runner := &fakeRunner{}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()

	result, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.1.0", "b"))
	if err != nil || result.Status != StatusComplete || result.Recovered || len(runner.applied) != len(orderedStages) {
		t.Fatalf("result=%#v applied=%#v error=%v", result, runner.applied, err)
	}
	if runner.preflight != 1 || runner.rollbacks != 0 {
		t.Fatalf("preflight=%d rollbacks=%d", runner.preflight, runner.rollbacks)
	}
	for _, stage := range result.Stages {
		if stage.Status != StageComplete || stage.Attempts != 1 || stage.CompletedAt == "" {
			t.Fatalf("stage=%#v", stage)
		}
	}

	second, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.1.0", "b"))
	if err != nil || second.Status != StatusComplete || len(runner.applied) != len(orderedStages) {
		t.Fatalf("idempotent result=%#v applied=%#v error=%v", second, runner.applied, err)
	}
}

func TestStageFailureRollsBackAndExplicitRetryStartsFresh(t *testing.T) {
	store := &memoryStore{}
	runner := &fakeRunner{failAt: StageMigration}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()
	current, target := source("1.0.0", "a"), source("1.1.0", "b")

	result, err := engine.Apply(t.Context(), current, target)
	if err == nil || result.Status != StatusRolledBack || runner.rollbacks != 1 {
		t.Fatalf("result=%#v error=%v rollbacks=%d", result, err, runner.rollbacks)
	}
	if got := result.Stages[indexStage(StageMigration)]; got.Status != StageFailed || got.ErrorCode != "stage-failed" {
		t.Fatalf("migration stage=%#v", got)
	}

	runner.failAt = ""
	result, err = engine.Apply(t.Context(), current, target)
	if err != nil || result.Status != StatusComplete || runner.preflight != 2 {
		t.Fatalf("retry result=%#v error=%v preflight=%d", result, err, runner.preflight)
	}
}

func TestInterruptedJournalFailsClosedThroughRollback(t *testing.T) {
	current, target := source("1.0.0", "a"), source("1.1.0", "b")
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	journal := Journal{
		SchemaVersion: JournalSchemaVersion, Status: StatusApplying,
		CurrentVersion: current.Version, CurrentDigest: current.Digest,
		TargetVersion: target.Version, TargetDigest: target.Digest,
		StartedAt: now, UpdatedAt: now,
	}
	for _, stage := range orderedStages {
		state := StageState{ID: stage, Status: StagePending}
		if stage == StageStopServices {
			state = StageState{ID: stage, Status: StageComplete, Attempts: 1, StartedAt: now, CompletedAt: now}
		}
		journal.Stages = append(journal.Stages, state)
	}
	store := &memoryStore{journal: journal, exists: true}
	runner := &fakeRunner{}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()

	result, err := engine.Apply(t.Context(), current, target)
	if err == nil || !strings.Contains(err.Error(), "retry explicitly") ||
		result.Status != StatusRolledBack || !result.Recovered || runner.rollbacks != 1 || runner.preflight != 0 {
		t.Fatalf("result=%#v error=%v runner=%#v", result, err, runner)
	}
}

func TestRollbackFailureRemainsDurablyRecoverable(t *testing.T) {
	store := &memoryStore{}
	runner := &fakeRunner{failAt: StagePayload, rollbackErr: errors.New("restore failed")}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()

	result, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.1.0", "b"))
	if err == nil || result.Status != StatusRollbackFailed || store.journal.ErrorCode != "rollback-failed" {
		t.Fatalf("result=%#v journal=%#v error=%v", result, store.journal, err)
	}
}

func TestJournalWriteFailureAfterInitializationTriggersRollback(t *testing.T) {
	store := &failingMemoryStore{failAt: 2}
	runner := &fakeRunner{}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()
	result, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.1.0", "b"))
	if err == nil || !strings.Contains(err.Error(), "record update stage") ||
		result.Status != StatusRolledBack || runner.rollbacks != 1 || len(runner.applied) != 0 {
		t.Fatalf("result=%#v error=%v runner=%#v", result, err, runner)
	}
}

func TestUpdateRejectsDowngradeAndJournalRebinding(t *testing.T) {
	store := &memoryStore{}
	runner := &fakeRunner{}
	engine, _ := NewEngine(store, runner)
	if _, err := engine.Apply(t.Context(), source("1.1.0", "a"), source("1.0.0", "b")); err == nil {
		t.Fatal("downgrade was accepted")
	}
	if _, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.1.0", "b")); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.2.0", "c")); err == nil {
		t.Fatal("journal was rebound")
	}
}

func TestTerminalJournalAdvancesToNextImmutablePair(t *testing.T) {
	store := &memoryStore{}
	runner := &fakeRunner{}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()
	firstCurrent, firstTarget := source("1.0.0", "a"), source("1.1.0", "b")
	if _, err := engine.Apply(t.Context(), firstCurrent, firstTarget); err != nil {
		t.Fatal(err)
	}
	secondTarget := source("1.2.0", "c")
	result, err := engine.Apply(t.Context(), firstTarget, secondTarget)
	if err != nil || result.Status != StatusComplete || result.CurrentVersion != "1.1.0" ||
		result.TargetVersion != "1.2.0" || runner.preflight != 2 || len(runner.applied) != 2*len(orderedStages) {
		t.Fatalf("result=%#v runner=%#v error=%v", result, runner, err)
	}
}

func TestCanonicalVersionOrdering(t *testing.T) {
	for _, test := range []struct {
		left, right string
		wanted      int
	}{
		{"1.0.0-beta.1", "1.0.0-beta.2", -1},
		{"1.0.0-beta.2", "1.0.0", -1},
		{"1.0.0", "1.0.1-beta.1", -1},
		{"2.0.0", "1.99.99", 1},
		{"1.2.3", "1.2.3", 0},
	} {
		got, err := CompareVersions(test.left, test.right)
		if err != nil || got != test.wanted {
			t.Fatalf("compare %s %s = %d, %v", test.left, test.right, got, err)
		}
	}
	for _, invalid := range []string{"v1.2.3", "1.02.3", "1.2.3-rc.1", "1.2.3-beta.0"} {
		if _, err := ParseVersion(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestJournalValidationRejectsCorruptRecoveryMetadata(t *testing.T) {
	store := &memoryStore{}
	runner := &fakeRunner{}
	engine, _ := NewEngine(store, runner)
	engine.now = updateClock()
	if _, err := engine.Apply(t.Context(), source("1.0.0", "a"), source("1.1.0", "b")); err != nil {
		t.Fatal(err)
	}
	valid := store.journal
	for name, mutate := range map[string]func(*Journal){
		"non-hex digest": func(journal *Journal) { journal.TargetDigest = strings.Repeat("z", 64) },
		"reversed clock": func(journal *Journal) {
			journal.UpdatedAt = journal.StartedAt
			journal.CompletedAt = "2026-09-02T09:59:00Z"
		},
		"missing terminal time":      func(journal *Journal) { journal.CompletedAt = "" },
		"impossible completed stage": func(journal *Journal) { journal.Stages[0].Attempts = 0 },
		"unsafe error code":          func(journal *Journal) { journal.ErrorCode = "contains spaces" },
	} {
		t.Run(name, func(t *testing.T) {
			journal := valid
			journal.Stages = append([]StageState(nil), valid.Stages...)
			mutate(&journal)
			if err := ValidateJournal(journal); err == nil {
				t.Fatal("corrupt recovery metadata was accepted")
			}
		})
	}
}

func source(version, marker string) installapply.Source {
	return installapply.Source{Root: "/release/" + version, Version: version, Digest: strings.Repeat(marker, 64)}
}

type memoryStore struct {
	journal Journal
	exists  bool
}

type failingMemoryStore struct {
	memoryStore
	saves  int
	failAt int
}

func (store *failingMemoryStore) Save(journal Journal) error {
	store.saves++
	if store.saves == store.failAt {
		return errors.New("simulated journal write failure")
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
	preflight   int
	applied     []StageID
	verified    []StageID
	failAt      StageID
	rollbackErr error
	rollbacks   int
}

func (runner *fakeRunner) Preflight(context.Context, installapply.Source, installapply.Source) error {
	runner.preflight++
	return nil
}
func (runner *fakeRunner) Apply(_ context.Context, stage StageID, _, _ installapply.Source) error {
	runner.applied = append(runner.applied, stage)
	if stage == runner.failAt {
		return errors.New("simulated stage failure")
	}
	return nil
}
func (runner *fakeRunner) Verify(_ context.Context, stage StageID, _, _ installapply.Source) error {
	runner.verified = append(runner.verified, stage)
	return nil
}
func (runner *fakeRunner) Rollback(context.Context, installapply.Source, installapply.Source, Journal) error {
	runner.rollbacks++
	return runner.rollbackErr
}

func indexStage(wanted StageID) int {
	for index, stage := range orderedStages {
		if stage == wanted {
			return index
		}
	}
	return -1
}

func updateClock() func() time.Time {
	current := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	return func() time.Time { current = current.Add(time.Second); return current }
}
