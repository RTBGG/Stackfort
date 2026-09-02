// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration && linux

package updateapply

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	_ "modernc.org/sqlite"
)

const updateQualificationEnvironment = "STACKFORT_DISPOSABLE_HOST_TEST"

func TestDisposableHostStagedUpdateTransaction(t *testing.T) {
	if os.Getenv(updateQualificationEnvironment) != "1" {
		t.Skip("set STACKFORT_DISPOSABLE_HOST_TEST=1 on a disposable Linux host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("staged update qualification must run as root")
	}

	t.Run("status inspection is read only before first update", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "not-created")
		store := &FileStore{directory: root, path: filepath.Join(root, "update-state.json")}
		if _, exists, err := store.Load(); err != nil || exists {
			t.Fatalf("exists=%t error=%v", exists, err)
		}
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only status created state: %v", err)
		}
	})

	t.Run("successful migration and health commit", func(t *testing.T) {
		fixture := newUpdateQualificationFixture(t)
		result, err := fixture.engine().Apply(t.Context(), fixture.current, fixture.target)
		if err != nil || result.Status != StatusComplete || result.Recovered {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		fixture.assertTarget(t)
		fixture.assertJournal(t, StatusComplete)
	})

	t.Run("health failure restores exact prior state", func(t *testing.T) {
		fixture := newUpdateQualificationFixture(t)
		fixture.runner.failHealth = true
		result, err := fixture.engine().Apply(t.Context(), fixture.current, fixture.target)
		if err == nil || result.Status != StatusRolledBack || result.Recovered {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		fixture.assertCurrent(t)
		fixture.assertJournal(t, StatusRolledBack)
	})

	t.Run("interrupted journal recovers before retry", func(t *testing.T) {
		fixture := newUpdateQualificationFixture(t)
		for _, stage := range orderedStages[:indexOfOrderedStage(StageStartServices)] {
			if err := fixture.runner.Apply(t.Context(), stage, fixture.current, fixture.target); err != nil {
				t.Fatalf("prepare interrupted %s: %v", stage, err)
			}
		}
		journal := qualificationJournal(fixture.current, fixture.target, StageMigration)
		if err := fixture.store.Save(journal); err != nil {
			t.Fatal(err)
		}

		// Construct a new runner and engine to model a new updater process after
		// the first process disappeared between durable stage transitions.
		fixture.runner = newQualificationRunner(fixture.root)
		result, err := fixture.engine().Apply(t.Context(), fixture.current, fixture.target)
		if err == nil || !result.Recovered || result.Status != StatusRolledBack ||
			!strings.Contains(err.Error(), "retry explicitly") {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		fixture.assertCurrent(t)
		fixture.assertJournal(t, StatusRolledBack)
	})

	fmt.Println("STACKFORT_QUALIFICATION staged-update-transaction=passed")
}

type updateQualificationFixture struct {
	root            string
	store           *FileStore
	runner          *qualificationRunner
	current, target installapply.Source
}

func newUpdateQualificationFixture(t *testing.T) *updateQualificationFixture {
	t.Helper()
	root := t.TempDir()
	stateDirectory := filepath.Join(root, "updater")
	if err := os.Mkdir(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := newQualificationRunner(root)
	if err := runner.initialize(); err != nil {
		t.Fatal(err)
	}
	return &updateQualificationFixture{
		root: root,
		store: &FileStore{
			directory: stateDirectory,
			path:      filepath.Join(stateDirectory, "update-state.json"),
		},
		runner: runner,
		current: installapply.Source{
			Root: filepath.Join(root, "release-1.0.0"), Version: "1.0.0", Digest: strings.Repeat("a", 64),
		},
		target: installapply.Source{
			Root: filepath.Join(root, "release-1.1.0"), Version: "1.1.0", Digest: strings.Repeat("b", 64),
		},
	}
}

func (fixture *updateQualificationFixture) engine() *Engine {
	engine, err := NewEngine(fixture.store, fixture.runner)
	if err != nil {
		panic(err)
	}
	return engine
}

func (fixture *updateQualificationFixture) assertCurrent(t *testing.T) {
	t.Helper()
	fixture.runner.assertState(t, 1, "current")
}

func (fixture *updateQualificationFixture) assertTarget(t *testing.T) {
	t.Helper()
	fixture.runner.assertState(t, 2, "target")
}

func (fixture *updateQualificationFixture) assertJournal(t *testing.T, wanted Status) {
	t.Helper()
	journal, exists, err := fixture.store.Load()
	if err != nil || !exists || journal.Status != wanted {
		t.Fatalf("journal=%#v exists=%t error=%v", journal, exists, err)
	}
	info, err := os.Lstat(fixture.store.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal metadata=%v error=%v", info, err)
	}
}

type qualificationRunner struct {
	root       string
	database   string
	backup     string
	failHealth bool
}

func newQualificationRunner(root string) *qualificationRunner {
	return &qualificationRunner{
		root: root, database: filepath.Join(root, "panel.sqlite"), backup: filepath.Join(root, "panel.sqlite.backup"),
	}
}

func (runner *qualificationRunner) initialize() error {
	for _, version := range []string{"1.0.0", "1.1.0"} {
		if err := os.Mkdir(filepath.Join(runner.root, "release-"+version), 0o755); err != nil {
			return err
		}
	}
	if err := runner.writeMarker("native-package", "current"); err != nil {
		return err
	}
	if err := runner.writeMarker("payload", "current"); err != nil {
		return err
	}
	if err := runner.writeMarker("configuration", "current"); err != nil {
		return err
	}
	if err := runner.writeMarker("services", "running"); err != nil {
		return err
	}
	database, err := sql.Open("sqlite", runner.database)
	if err != nil {
		return err
	}
	if _, err := database.Exec(`CREATE TABLE release_state (
        singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
        schema_version INTEGER NOT NULL,
        payload TEXT NOT NULL
    ); INSERT INTO release_state(singleton, schema_version, payload) VALUES (1, 1, 'current');`); err != nil {
		_ = database.Close()
		return err
	}
	return database.Close()
}

func (runner *qualificationRunner) Preflight(
	_ context.Context,
	current installapply.Source,
	target installapply.Source,
) error {
	if _, err := os.Stat(current.Root); err != nil {
		return err
	}
	if _, err := os.Stat(target.Root); err != nil {
		return err
	}
	return runner.verifyState(1, "current")
}

func (runner *qualificationRunner) Apply(
	ctx context.Context,
	stage StageID,
	_ installapply.Source,
	_ installapply.Source,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch stage {
	case StageStopServices:
		return runner.writeMarker("services", "stopped")
	case StageBackupState:
		return copyQualificationFile(runner.database, runner.backup)
	case StageNativePackages:
		return runner.writeMarker("native-package", "target")
	case StagePayload:
		return runner.writeMarker("payload", "target")
	case StageConfiguration:
		return runner.writeMarker("configuration", "target")
	case StageMigration:
		database, err := sql.Open("sqlite", runner.database)
		if err != nil {
			return err
		}
		_, migrationErr := database.Exec("UPDATE release_state SET schema_version = 2, payload = 'target' WHERE singleton = 1")
		return errors.Join(migrationErr, database.Close())
	case StageStartServices:
		return runner.writeMarker("services", "running")
	case StageHealth:
		if runner.failHealth {
			return errors.New("injected health failure")
		}
		return runner.verifyState(2, "target")
	default:
		return errors.New("unexpected qualification stage")
	}
}

func (runner *qualificationRunner) Verify(
	_ context.Context,
	stage StageID,
	_ installapply.Source,
	_ installapply.Source,
) error {
	switch stage {
	case StageStopServices:
		return runner.verifyMarker("services", "stopped")
	case StageBackupState:
		info, err := os.Lstat(runner.backup)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 1 {
			return errors.New("qualification backup is invalid")
		}
		return nil
	case StageNativePackages:
		return runner.verifyMarker("native-package", "target")
	case StagePayload:
		return runner.verifyMarker("payload", "target")
	case StageConfiguration:
		return runner.verifyMarker("configuration", "target")
	case StageMigration:
		return runner.verifyDatabase(2, "target")
	case StageStartServices:
		return runner.verifyMarker("services", "running")
	case StageHealth:
		return runner.verifyState(2, "target")
	default:
		return errors.New("unexpected qualification stage")
	}
}

func (runner *qualificationRunner) Rollback(
	_ context.Context,
	_ installapply.Source,
	_ installapply.Source,
	journal Journal,
) error {
	var result error
	if rollbackRequiresBackup(journal) {
		result = errors.Join(result, copyQualificationFile(runner.backup, runner.database))
	}
	for _, marker := range []string{"native-package", "payload", "configuration"} {
		result = errors.Join(result, runner.writeMarker(marker, "current"))
	}
	result = errors.Join(result, runner.writeMarker("services", "running"))
	return errors.Join(result, runner.verifyState(1, "current"))
}

func (runner *qualificationRunner) assertState(t *testing.T, schema int, value string) {
	t.Helper()
	if err := runner.verifyState(schema, value); err != nil {
		t.Fatal(err)
	}
}

func (runner *qualificationRunner) verifyState(schema int, value string) error {
	if err := runner.verifyDatabase(schema, value); err != nil {
		return err
	}
	for _, marker := range []string{"native-package", "payload", "configuration"} {
		if err := runner.verifyMarker(marker, value); err != nil {
			return err
		}
	}
	return runner.verifyMarker("services", "running")
}

func (runner *qualificationRunner) verifyDatabase(schema int, value string) error {
	database, err := sql.Open("sqlite", runner.database)
	if err != nil {
		return err
	}
	defer database.Close()
	var gotSchema int
	var gotValue string
	if err := database.QueryRow("SELECT schema_version, payload FROM release_state WHERE singleton = 1").Scan(&gotSchema, &gotValue); err != nil {
		return err
	}
	if gotSchema != schema || gotValue != value {
		return fmt.Errorf("database state=(%d,%q), want=(%d,%q)", gotSchema, gotValue, schema, value)
	}
	return nil
}

func (runner *qualificationRunner) writeMarker(name, value string) error {
	return os.WriteFile(filepath.Join(runner.root, name), []byte(value+"\n"), 0o600)
}

func (runner *qualificationRunner) verifyMarker(name, wanted string) error {
	content, err := os.ReadFile(filepath.Join(runner.root, name))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(content)) != wanted {
		return fmt.Errorf("%s marker=%q, want=%q", name, strings.TrimSpace(string(content)), wanted)
	}
	return nil
}

func copyQualificationFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temporary, err := os.OpenFile(targetPath+".new", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err := io.Copy(temporary, source); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), targetPath); err != nil {
		return err
	}
	cleanup = false
	directory, err := os.Open(filepath.Dir(targetPath))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func qualificationJournal(current, target installapply.Source, completedThrough StageID) Journal {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	journal := Journal{
		SchemaVersion: JournalSchemaVersion, Status: StatusApplying,
		CurrentVersion: current.Version, CurrentDigest: current.Digest,
		TargetVersion: target.Version, TargetDigest: target.Digest,
		StartedAt: now, UpdatedAt: now,
	}
	completedIndex := indexOfOrderedStage(completedThrough)
	for index, stage := range orderedStages {
		state := StageState{ID: stage, Status: StagePending}
		if index <= completedIndex {
			state.Status, state.Attempts, state.StartedAt, state.CompletedAt = StageComplete, 1, now, now
		}
		journal.Stages = append(journal.Stages, state)
	}
	return journal
}
