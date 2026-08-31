// SPDX-License-Identifier: AGPL-3.0-or-later

// Package installapply implements Stackfort's journaled, idempotent fresh-host
// installation. The engine is platform-neutral; the production stage runner is
// Linux-only and exposes only the fixed stages declared here.
package installapply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
)

const JournalSchemaVersion = 3

type StageID string

const (
	StagePackages      StageID = "packages"
	StageWAFPackage    StageID = "waf-native-package"
	StageVinylPackage  StageID = "vinyl-native-package"
	StageIdentity      StageID = "service-identity"
	StagePayload       StageID = "release-payload"
	StageConfiguration StageID = "system-configuration"
	StageSecurity      StageID = "security-policy"
	StageNGINX         StageID = "nginx-baseline"
	StageServices      StageID = "services-and-health"
)

var orderedStages = []StageID{
	StagePackages,
	StageWAFPackage,
	StageVinylPackage,
	StageIdentity,
	StagePayload,
	StageConfiguration,
	StageSecurity,
	StageNGINX,
	StageServices,
}

type InstallStatus string
type StageStatus string

const (
	InstallApplying InstallStatus = "applying"
	InstallFailed   InstallStatus = "failed"
	InstallComplete InstallStatus = "complete"

	StagePending  StageStatus = "pending"
	StageApplying StageStatus = "applying"
	StageFailed   StageStatus = "failed"
	StageComplete StageStatus = "complete"
)

type StageState struct {
	ID          StageID     `json:"id"`
	Status      StageStatus `json:"status"`
	Attempts    int         `json:"attempts"`
	Changed     bool        `json:"changed"`
	StartedAt   string      `json:"startedAt,omitempty"`
	CompletedAt string      `json:"completedAt,omitempty"`
	ErrorCode   string      `json:"errorCode,omitempty"`
}

type Journal struct {
	SchemaVersion int           `json:"schemaVersion"`
	Version       string        `json:"version"`
	SourceDigest  string        `json:"sourceDigest"`
	Distribution  string        `json:"distribution"`
	Status        InstallStatus `json:"status"`
	StartedAt     string        `json:"startedAt"`
	UpdatedAt     string        `json:"updatedAt"`
	Stages        []StageState  `json:"stages"`
}

type Source struct {
	Root     string
	Version  string
	Digest   string
	Manifest releaseartifacts.Manifest
}

type Result struct {
	Version          string        `json:"version"`
	SourceDigest     string        `json:"sourceDigest"`
	Status           InstallStatus `json:"status"`
	Changed          bool          `json:"changed"`
	AlreadyInstalled bool          `json:"alreadyInstalled"`
	Resumed          bool          `json:"resumed"`
	Stages           []StageState  `json:"stages"`
}

type Store interface {
	Load() (Journal, bool, error)
	Save(Journal) error
}

type Runner interface {
	Distribution() string
	Preflight(context.Context) error
	Apply(context.Context, StageID, Source) (bool, error)
	Verify(context.Context, StageID, Source) error
	VerifyInstallation(context.Context, Source) error
}

type Engine struct {
	store  Store
	runner Runner
	now    func() time.Time
}

func NewEngine(store Store, runner Runner) (*Engine, error) {
	if store == nil || runner == nil {
		return nil, errors.New("installer store and runner are required")
	}
	return &Engine{store: store, runner: runner, now: time.Now}, nil
}

func (engine *Engine) Install(ctx context.Context, source Source) (Result, error) {
	if engine == nil || engine.store == nil || engine.runner == nil || ctx == nil ||
		source.Root == "" || source.Version == "" || source.Digest == "" {
		return Result{}, errors.New("invalid installer invocation")
	}
	journal, exists, err := engine.store.Load()
	if err != nil {
		return Result{}, fmt.Errorf("load installation journal: %w", err)
	}
	resumed := exists && journal.Status != InstallComplete
	if exists {
		if err := validateJournal(journal); err != nil {
			return Result{}, fmt.Errorf("validate installation journal: %w", err)
		}
		if journal.Version != source.Version || journal.SourceDigest != source.Digest ||
			journal.Distribution != engine.runner.Distribution() {
			return Result{}, errors.New("installation journal belongs to another immutable source or platform")
		}
		if journal.Status == InstallComplete {
			if err := engine.runner.VerifyInstallation(ctx, source); err != nil {
				return Result{}, fmt.Errorf("verify completed installation: %w", err)
			}
			return resultFromJournal(journal, false, true, false), nil
		}
	} else {
		if err := engine.runner.Preflight(ctx); err != nil {
			return Result{}, err
		}
		now := engine.timestamp()
		journal = Journal{
			SchemaVersion: JournalSchemaVersion,
			Version:       source.Version,
			SourceDigest:  source.Digest,
			Distribution:  engine.runner.Distribution(),
			Status:        InstallApplying,
			StartedAt:     now,
			UpdatedAt:     now,
			Stages:        make([]StageState, 0, len(orderedStages)),
		}
		for _, id := range orderedStages {
			journal.Stages = append(journal.Stages, StageState{ID: id, Status: StagePending})
		}
		if err := engine.store.Save(journal); err != nil {
			return Result{}, fmt.Errorf("initialize installation journal: %w", err)
		}
	}

	changed := false
	journal.Status = InstallApplying
	for index := range journal.Stages {
		stage := &journal.Stages[index]
		if stage.Status == StageComplete {
			if err := engine.runner.Verify(ctx, stage.ID, source); err != nil {
				return Result{}, fmt.Errorf("verify completed stage %s: %w", stage.ID, err)
			}
			continue
		}
		stage.Status = StageApplying
		stage.Attempts++
		stage.StartedAt = engine.timestamp()
		stage.CompletedAt = ""
		stage.ErrorCode = ""
		journal.UpdatedAt = stage.StartedAt
		if err := engine.store.Save(journal); err != nil {
			return Result{}, fmt.Errorf("record stage %s start: %w", stage.ID, err)
		}

		stageChanged, applyErr := engine.runner.Apply(ctx, stage.ID, source)
		if applyErr == nil {
			applyErr = engine.runner.Verify(ctx, stage.ID, source)
		}
		if applyErr != nil {
			stage.Status = StageFailed
			stage.Changed = stage.Changed || stageChanged
			stage.ErrorCode = "stage-failed"
			journal.Status = InstallFailed
			journal.UpdatedAt = engine.timestamp()
			if saveErr := engine.store.Save(journal); saveErr != nil {
				return Result{}, errors.Join(fmt.Errorf("apply stage %s: %w", stage.ID, applyErr),
					fmt.Errorf("record failed stage: %w", saveErr))
			}
			return resultFromJournal(journal, changed || stageChanged, false, resumed),
				fmt.Errorf("apply stage %s: %w", stage.ID, applyErr)
		}
		stage.Status = StageComplete
		stage.Changed = stage.Changed || stageChanged
		stage.CompletedAt = engine.timestamp()
		journal.UpdatedAt = stage.CompletedAt
		changed = changed || stageChanged
		if err := engine.store.Save(journal); err != nil {
			return Result{}, fmt.Errorf("record stage %s completion: %w", stage.ID, err)
		}
	}
	if err := engine.runner.VerifyInstallation(ctx, source); err != nil {
		journal.Status = InstallFailed
		journal.UpdatedAt = engine.timestamp()
		_ = engine.store.Save(journal)
		return resultFromJournal(journal, changed, false, resumed), fmt.Errorf("verify installation: %w", err)
	}
	journal.Status = InstallComplete
	journal.UpdatedAt = engine.timestamp()
	if err := engine.store.Save(journal); err != nil {
		return Result{}, fmt.Errorf("record installation completion: %w", err)
	}
	return resultFromJournal(journal, changed, false, resumed), nil
}

func (engine *Engine) timestamp() string {
	return engine.now().UTC().Format(time.RFC3339Nano)
}

func validateJournal(journal Journal) error {
	if journal.SchemaVersion != JournalSchemaVersion || journal.Version == "" || journal.SourceDigest == "" ||
		journal.Distribution == "" || journal.StartedAt == "" || journal.UpdatedAt == "" ||
		len(journal.Stages) != len(orderedStages) {
		return errors.New("installation journal shape is invalid")
	}
	if journal.Status != InstallApplying && journal.Status != InstallFailed && journal.Status != InstallComplete {
		return errors.New("installation journal status is invalid")
	}
	for index, stage := range journal.Stages {
		if stage.ID != orderedStages[index] || stage.Attempts < 0 ||
			(stage.Status != StagePending && stage.Status != StageApplying &&
				stage.Status != StageFailed && stage.Status != StageComplete) {
			return errors.New("installation stage journal is invalid")
		}
	}
	return nil
}

func resultFromJournal(journal Journal, changed, alreadyInstalled, resumed bool) Result {
	return Result{
		Version: journal.Version, SourceDigest: journal.SourceDigest, Status: journal.Status,
		Changed: changed, AlreadyInstalled: alreadyInstalled, Resumed: resumed,
		Stages: append([]StageState(nil), journal.Stages...),
	}
}
