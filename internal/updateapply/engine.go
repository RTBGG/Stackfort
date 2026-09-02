// SPDX-License-Identifier: AGPL-3.0-or-later

// Package updateapply implements Stackfort's staged, health-gated release
// activation transaction. Its journal deliberately lives outside the panel
// database so database rollback cannot erase recovery state.
package updateapply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
)

const JournalSchemaVersion = 1

type StageID string

const (
	StageStopServices   StageID = "stop-services"
	StageBackupState    StageID = "backup-state"
	StageNativePackages StageID = "native-packages"
	StagePayload        StageID = "release-payload"
	StageConfiguration  StageID = "system-configuration"
	StageMigration      StageID = "database-migration"
	StageStartServices  StageID = "start-services"
	StageHealth         StageID = "health-gate"
)

var orderedStages = []StageID{
	StageStopServices,
	StageBackupState,
	StageNativePackages,
	StagePayload,
	StageConfiguration,
	StageMigration,
	StageStartServices,
	StageHealth,
}

type Status string
type StageStatus string

const (
	StatusApplying       Status = "applying"
	StatusRollingBack    Status = "rolling_back"
	StatusRolledBack     Status = "rolled_back"
	StatusRollbackFailed Status = "rollback_failed"
	StatusComplete       Status = "complete"

	StagePending  StageStatus = "pending"
	StageApplying StageStatus = "applying"
	StageFailed   StageStatus = "failed"
	StageComplete StageStatus = "complete"
)

type StageState struct {
	ID          StageID     `json:"id"`
	Status      StageStatus `json:"status"`
	Attempts    int         `json:"attempts"`
	StartedAt   string      `json:"startedAt,omitempty"`
	CompletedAt string      `json:"completedAt,omitempty"`
	ErrorCode   string      `json:"errorCode,omitempty"`
}

type Journal struct {
	SchemaVersion  int          `json:"schemaVersion"`
	Status         Status       `json:"status"`
	CurrentVersion string       `json:"currentVersion"`
	CurrentDigest  string       `json:"currentDigest"`
	TargetVersion  string       `json:"targetVersion"`
	TargetDigest   string       `json:"targetDigest"`
	StartedAt      string       `json:"startedAt"`
	UpdatedAt      string       `json:"updatedAt"`
	CompletedAt    string       `json:"completedAt,omitempty"`
	ErrorCode      string       `json:"errorCode,omitempty"`
	Stages         []StageState `json:"stages"`
}

type Result struct {
	Status         Status       `json:"status"`
	CurrentVersion string       `json:"currentVersion"`
	TargetVersion  string       `json:"targetVersion"`
	Recovered      bool         `json:"recovered"`
	Stages         []StageState `json:"stages"`
}

type Store interface {
	Load() (Journal, bool, error)
	Save(Journal) error
}

type Runner interface {
	Preflight(context.Context, installapply.Source, installapply.Source) error
	Apply(context.Context, StageID, installapply.Source, installapply.Source) error
	Verify(context.Context, StageID, installapply.Source, installapply.Source) error
	Rollback(context.Context, installapply.Source, installapply.Source, Journal) error
}

type Engine struct {
	store           Store
	runner          Runner
	now             func() time.Time
	rollbackTimeout time.Duration
}

func NewEngine(store Store, runner Runner) (*Engine, error) {
	if store == nil || runner == nil {
		return nil, errors.New("update store and runner are required")
	}
	return &Engine{store: store, runner: runner, now: time.Now, rollbackTimeout: 5 * time.Minute}, nil
}

func (engine *Engine) Apply(
	ctx context.Context,
	current installapply.Source,
	target installapply.Source,
) (Result, error) {
	if engine == nil || engine.store == nil || engine.runner == nil || ctx == nil {
		return Result{}, errors.New("invalid updater invocation")
	}
	if err := validateSourcePair(current, target); err != nil {
		return Result{}, err
	}

	journal, exists, err := engine.store.Load()
	if err != nil {
		return Result{}, fmt.Errorf("load update journal: %w", err)
	}
	if exists {
		if err := validateJournal(journal); err != nil {
			return Result{}, fmt.Errorf("validate update journal: %w", err)
		}
		if !journalMatches(journal, current, target) {
			if !terminalJournalMatchesInstalled(journal, current) {
				return Result{}, errors.New("update journal belongs to another immutable source pair")
			}
			exists = false
		}
	}
	if exists {
		switch journal.Status {
		case StatusComplete:
			if err := engine.runner.Verify(ctx, StageHealth, current, target); err != nil {
				return resultFromJournal(journal, false), fmt.Errorf("verify completed update: %w", err)
			}
			return resultFromJournal(journal, false), nil
		case StatusApplying, StatusRollingBack:
			return engine.recoverInterrupted(current, target, journal)
		case StatusRollbackFailed:
			return engine.recoverInterrupted(current, target, journal)
		case StatusRolledBack:
			// An explicit retry of the same immutable pair starts a new journal.
		default:
			return Result{}, errors.New("update journal has an unsupported status")
		}
	}

	if err := engine.runner.Preflight(ctx, current, target); err != nil {
		return Result{}, fmt.Errorf("update preflight: %w", err)
	}
	now := engine.timestamp()
	journal = Journal{
		SchemaVersion:  JournalSchemaVersion,
		Status:         StatusApplying,
		CurrentVersion: current.Version,
		CurrentDigest:  current.Digest,
		TargetVersion:  target.Version,
		TargetDigest:   target.Digest,
		StartedAt:      now,
		UpdatedAt:      now,
		Stages:         make([]StageState, 0, len(orderedStages)),
	}
	for _, id := range orderedStages {
		journal.Stages = append(journal.Stages, StageState{ID: id, Status: StagePending})
	}
	if err := engine.store.Save(journal); err != nil {
		return Result{}, fmt.Errorf("initialize update journal: %w", err)
	}

	for index := range journal.Stages {
		stage := &journal.Stages[index]
		stage.Status = StageApplying
		stage.Attempts++
		stage.StartedAt = engine.timestamp()
		stage.CompletedAt = ""
		stage.ErrorCode = ""
		journal.UpdatedAt = stage.StartedAt
		if err := engine.store.Save(journal); err != nil {
			rollbackErr := engine.rollback(current, target, &journal)
			return resultFromJournal(journal, false), errors.Join(
				fmt.Errorf("record update stage %s start: %w", stage.ID, err), rollbackErr,
			)
		}
		applyErr := engine.runner.Apply(ctx, stage.ID, current, target)
		if applyErr == nil {
			applyErr = engine.runner.Verify(ctx, stage.ID, current, target)
		}
		if applyErr != nil {
			stage.Status = StageFailed
			stage.ErrorCode = "stage-failed"
			journal.Status = StatusRollingBack
			journal.ErrorCode = "stage-failed"
			journal.UpdatedAt = engine.timestamp()
			if saveErr := engine.store.Save(journal); saveErr != nil {
				rollbackErr := engine.rollback(current, target, &journal)
				return resultFromJournal(journal, false), errors.Join(
					fmt.Errorf("apply update stage %s: %w", stage.ID, applyErr),
					fmt.Errorf("record update failure: %w", saveErr),
					rollbackErr,
				)
			}
			rollbackErr := engine.rollback(current, target, &journal)
			return resultFromJournal(journal, false), errors.Join(
				fmt.Errorf("apply update stage %s: %w", stage.ID, applyErr), rollbackErr,
			)
		}
		stage.Status = StageComplete
		stage.CompletedAt = engine.timestamp()
		journal.UpdatedAt = stage.CompletedAt
		if err := engine.store.Save(journal); err != nil {
			rollbackErr := engine.rollback(current, target, &journal)
			return resultFromJournal(journal, false), errors.Join(
				fmt.Errorf("record update stage %s completion: %w", stage.ID, err), rollbackErr,
			)
		}
	}
	journal.Status = StatusComplete
	journal.ErrorCode = ""
	journal.CompletedAt = engine.timestamp()
	journal.UpdatedAt = journal.CompletedAt
	if err := engine.store.Save(journal); err != nil {
		journal.Status = StatusRollingBack
		journal.ErrorCode = "commit-failed"
		rollbackErr := engine.rollback(current, target, &journal)
		return resultFromJournal(journal, false), errors.Join(fmt.Errorf("record completed update: %w", err), rollbackErr)
	}
	return resultFromJournal(journal, false), nil
}

func (engine *Engine) recoverInterrupted(
	current installapply.Source,
	target installapply.Source,
	journal Journal,
) (Result, error) {
	journal.Status = StatusRollingBack
	journal.ErrorCode = "interrupted"
	journal.UpdatedAt = engine.timestamp()
	if err := engine.store.Save(journal); err != nil {
		return resultFromJournal(journal, true), fmt.Errorf("record interrupted update recovery: %w", err)
	}
	err := engine.rollback(current, target, &journal)
	return resultFromJournal(journal, true), errors.Join(errors.New("interrupted update was rolled back; retry explicitly"), err)
}

func (engine *Engine) rollback(
	current installapply.Source,
	target installapply.Source,
	journal *Journal,
) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), engine.rollbackTimeout)
	defer cancel()
	err := engine.runner.Rollback(rollbackCtx, current, target, *journal)
	journal.CompletedAt = engine.timestamp()
	journal.UpdatedAt = journal.CompletedAt
	if err != nil {
		journal.Status = StatusRollbackFailed
		journal.ErrorCode = "rollback-failed"
	} else {
		journal.Status = StatusRolledBack
	}
	if saveErr := engine.store.Save(*journal); saveErr != nil {
		err = errors.Join(err, fmt.Errorf("record rollback result: %w", saveErr))
	}
	if err != nil {
		return fmt.Errorf("roll back update: %w", err)
	}
	return nil
}

func (engine *Engine) timestamp() string {
	return engine.now().UTC().Format(time.RFC3339Nano)
}

func validateSourcePair(current, target installapply.Source) error {
	for name, source := range map[string]installapply.Source{"current": current, "target": target} {
		if source.Root == "" || source.Version == "" || !journalDigestPattern.MatchString(source.Digest) {
			return fmt.Errorf("%s immutable release source is incomplete", name)
		}
	}
	comparison, err := CompareVersions(current.Version, target.Version)
	if err != nil {
		return err
	}
	if comparison >= 0 {
		return errors.New("target release must be newer than the installed release")
	}
	return nil
}

func journalMatches(journal Journal, current, target installapply.Source) bool {
	return journal.CurrentVersion == current.Version && journal.CurrentDigest == current.Digest &&
		journal.TargetVersion == target.Version && journal.TargetDigest == target.Digest
}

func terminalJournalMatchesInstalled(journal Journal, current installapply.Source) bool {
	switch journal.Status {
	case StatusComplete:
		return journal.TargetVersion == current.Version && journal.TargetDigest == current.Digest
	case StatusRolledBack:
		return journal.CurrentVersion == current.Version && journal.CurrentDigest == current.Digest
	default:
		return false
	}
}

func resultFromJournal(journal Journal, recovered bool) Result {
	stages := append([]StageState(nil), journal.Stages...)
	return Result{
		Status: journal.Status, CurrentVersion: journal.CurrentVersion,
		TargetVersion: journal.TargetVersion, Recovered: recovered, Stages: stages,
	}
}
