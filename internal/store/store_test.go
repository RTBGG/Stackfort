// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"
)

func TestOpenConfiguresAndMigratesDatabase(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "state", "stackfort.db")
	state := openTestStore(t, databasePath)

	var journalMode string
	var foreignKeys, synchronous, busyTimeout, trustedSchema, schemaVersion int
	err := state.Read(context.Background(), func(reader Reader) error {
		checks := []struct {
			query       string
			destination any
		}{
			{"PRAGMA journal_mode", &journalMode},
			{"PRAGMA foreign_keys", &foreignKeys},
			{"PRAGMA synchronous", &synchronous},
			{"PRAGMA busy_timeout", &busyTimeout},
			{"PRAGMA trusted_schema", &trustedSchema},
			{"SELECT MAX(version) FROM schema_migrations", &schemaVersion},
		}
		for _, check := range checks {
			if err := reader.QueryRowContext(context.Background(), check.query).Scan(check.destination); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read database configuration: %v", err)
	}
	if journalMode != "wal" || foreignKeys != 1 || synchronous != 2 || busyTimeout != 5_000 || trustedSchema != 0 {
		t.Fatalf(
			"unexpected pragmas: journal=%q foreign_keys=%d synchronous=%d busy_timeout=%d trusted_schema=%d",
			journalMode,
			foreignKeys,
			synchronous,
			busyTimeout,
			trustedSchema,
		)
	}
	if schemaVersion != len(embeddedMigrations) {
		t.Fatalf("schema version = %d, want %d", schemaVersion, len(embeddedMigrations))
	}

	if runtime.GOOS != "windows" {
		assertPermissions(t, databasePath, 0o600)
		assertPermissions(t, filepath.Dir(databasePath), 0o750)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "stackfort.db")
	first := openTestStore(t, databasePath)
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	second, err := Open(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("reopen migrated store: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	var count int
	if err := second.Read(context.Background(), func(reader Reader) error {
		return reader.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	}); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != len(embeddedMigrations) {
		t.Fatalf("migration count = %d, want %d", count, len(embeddedMigrations))
	}
}

func TestOpenRejectsRelativePathAndDatabaseSymlink(t *testing.T) {
	t.Parallel()

	if _, err := Open(context.Background(), "stackfort.db"); err == nil {
		t.Fatal("Open accepted a relative path")
	}

	directory := t.TempDir()
	target := filepath.Join(directory, "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("create symlink target: %v", err)
	}
	link := filepath.Join(directory, "link.db")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating symlinks requires an unavailable Windows privilege: %v", err)
		}
		t.Fatalf("create database symlink: %v", err)
	}
	if _, err := Open(context.Background(), link); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Open symlink error = %v, want symbolic-link rejection", err)
	}
}

func TestOpenDoesNotRelaxExistingDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix directory permission semantics")
	}

	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0o777); err != nil {
		t.Fatalf("create permissive directory: %v", err)
	}
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatalf("set permissive directory mode: %v", err)
	}
	_, err := Open(context.Background(), filepath.Join(directory, "stackfort.db"))
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("Open error = %v, want permission rejection", err)
	}
	assertPermissions(t, directory, 0o777)
}

func TestForeignKeysAreEnforcedOnWriteConnections(t *testing.T) {
	t.Parallel()

	state := openTestStore(t, filepath.Join(t.TempDir(), "stackfort.db"))
	ctx := context.Background()
	if err := state.Write(ctx, func(executor Executor) error {
		_, err := executor.ExecContext(ctx, `
			CREATE TABLE test_parent (id INTEGER PRIMARY KEY) STRICT;
			CREATE TABLE test_child (
				id INTEGER PRIMARY KEY,
				parent_id INTEGER NOT NULL REFERENCES test_parent(id)
			) STRICT;`)
		return err
	}); err != nil {
		t.Fatalf("create foreign-key fixture: %v", err)
	}

	err := state.Write(ctx, func(executor Executor) error {
		_, err := executor.ExecContext(ctx, "INSERT INTO test_child (id, parent_id) VALUES (1, 404)")
		return err
	})
	if err == nil {
		t.Fatal("invalid foreign key was accepted")
	}
}

func TestOpenRefusesFutureAndDriftedSchemas(t *testing.T) {
	t.Parallel()

	t.Run("future", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "stackfort.db")
		state := openTestStore(t, databasePath)
		if err := state.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		raw := openRawDatabase(t, databasePath)
		_, err := raw.Exec(`
			INSERT INTO schema_migrations (version, name, checksum, applied_at)
			VALUES (?, 'future', ?, '2026-01-01T00:00:00Z')`,
			len(embeddedMigrations)+1,
			strings.Repeat("0", 64),
		)
		if err != nil {
			t.Fatalf("insert future migration: %v", err)
		}
		if err := raw.Close(); err != nil {
			t.Fatalf("close raw database: %v", err)
		}

		_, err = Open(context.Background(), databasePath)
		if !errors.Is(err, ErrFutureSchema) {
			t.Fatalf("Open error = %v, want ErrFutureSchema", err)
		}
	})

	t.Run("drift", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "stackfort.db")
		state := openTestStore(t, databasePath)
		if err := state.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}

		raw := openRawDatabase(t, databasePath)
		if _, err := raw.Exec("UPDATE schema_migrations SET checksum = ? WHERE version = 1", strings.Repeat("f", 64)); err != nil {
			t.Fatalf("change migration checksum: %v", err)
		}
		if err := raw.Close(); err != nil {
			t.Fatalf("close raw database: %v", err)
		}

		_, err := Open(context.Background(), databasePath)
		if !errors.Is(err, ErrMigrationDrift) {
			t.Fatalf("Open error = %v, want ErrMigrationDrift", err)
		}
	})
}

func TestOpenRefusesUnmanagedSchema(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "unmanaged.db")
	raw := openRawDatabase(t, databasePath)
	if _, err := raw.Exec("CREATE TABLE existing_data (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create unmanaged table: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close unmanaged database: %v", err)
	}

	_, err := Open(context.Background(), databasePath)
	if !errors.Is(err, ErrUnmanagedSchema) {
		t.Fatalf("Open error = %v, want ErrUnmanagedSchema", err)
	}
}

func TestFailedMigrationRollsBackCompletely(t *testing.T) {
	t.Parallel()

	state := openTestStore(t, filepath.Join(t.TempDir(), "stackfort.db"))
	failingSQL := "CREATE TABLE migration_partial (id INTEGER PRIMARY KEY) STRICT;\nTHIS IS NOT SQL;\n"
	checksum := sha256.Sum256([]byte(failingSQL))
	failingMigration := migration{
		version:  len(embeddedMigrations) + 1,
		name:     "intentional_failure",
		sql:      failingSQL,
		checksum: fmt.Sprintf("%x", checksum),
	}
	testMigrations := append(append([]migration(nil), embeddedMigrations...), failingMigration)

	err := state.migrate(context.Background(), testMigrations)
	if err == nil {
		t.Fatal("failing migration unexpectedly succeeded")
	}

	var partialTableExists bool
	var migrationCount int
	if err := state.Read(context.Background(), func(reader Reader) error {
		if err := reader.QueryRowContext(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM sqlite_schema
				WHERE type = 'table' AND name = 'migration_partial'
			)`).Scan(&partialTableExists); err != nil {
			return err
		}
		return reader.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount)
	}); err != nil {
		t.Fatalf("inspect rolled-back migration: %v", err)
	}
	if partialTableExists {
		t.Fatal("failed migration left its table behind")
	}
	if migrationCount != len(embeddedMigrations) {
		t.Fatalf("migration count = %d, want %d", migrationCount, len(embeddedMigrations))
	}
	if err := state.migrate(context.Background(), embeddedMigrations); err != nil {
		t.Fatalf("known schema was not recoverable after failure: %v", err)
	}
}

func TestConcurrentReadersAndBoundedWriters(t *testing.T) {
	state := openTestStore(t, filepath.Join(t.TempDir(), "stackfort.db"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := state.Write(ctx, func(executor Executor) error {
		_, err := executor.ExecContext(ctx, "CREATE TABLE counter (id INTEGER PRIMARY KEY, value INTEGER NOT NULL) STRICT")
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, "INSERT INTO counter (id, value) VALUES (1, 0)")
		return err
	}); err != nil {
		t.Fatalf("create concurrency fixture: %v", err)
	}

	const (
		writerCount = 6
		increments  = 25
		readerCount = 4
	)
	errorsChannel := make(chan error, writerCount+readerCount)
	var workers sync.WaitGroup
	for range writerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range increments {
				if err := state.Write(ctx, func(executor Executor) error {
					_, err := executor.ExecContext(ctx, "UPDATE counter SET value = value + 1 WHERE id = 1")
					return err
				}); err != nil {
					errorsChannel <- err
					return
				}
			}
		}()
	}
	for range readerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range increments {
				if err := state.Read(ctx, func(reader Reader) error {
					var value int
					return reader.QueryRowContext(ctx, "SELECT value FROM counter WHERE id = 1").Scan(&value)
				}); err != nil {
					errorsChannel <- err
					return
				}
			}
		}()
	}
	workers.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent database operation: %v", err)
	}

	var finalValue int
	if err := state.Read(ctx, func(reader Reader) error {
		return reader.QueryRowContext(ctx, "SELECT value FROM counter WHERE id = 1").Scan(&finalValue)
	}); err != nil {
		t.Fatalf("read final counter: %v", err)
	}
	if want := writerCount * increments; finalValue != want {
		t.Fatalf("counter = %d, want %d", finalValue, want)
	}
}

func TestBackupIsConsistentRestorableAndNeverOverwritten(t *testing.T) {
	t.Parallel()

	state := openTestStore(t, filepath.Join(t.TempDir(), "source", "stackfort.db"))
	ctx := context.Background()
	if err := state.Write(ctx, func(executor Executor) error {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO stackfort_metadata (key, value, updated_at)
			VALUES ('backup-test', 'preserved', '2026-01-01T00:00:00Z')`)
		return err
	}); err != nil {
		t.Fatalf("insert backup fixture: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backups", "state.db")
	if err := state.Backup(ctx, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if err := verifyBackup(ctx, backupPath, embeddedMigrations); err != nil {
		t.Fatalf("verify backup: %v", err)
	}
	before, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if err := state.Backup(ctx, backupPath); err == nil {
		t.Fatal("Backup overwrote an existing destination")
	}
	after, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("reread backup: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("existing backup changed after overwrite attempt")
	}

	restored := openTestStore(t, backupPath)
	var value string
	if err := restored.Read(ctx, func(reader Reader) error {
		return reader.QueryRowContext(ctx, "SELECT value FROM stackfort_metadata WHERE key = 'backup-test'").Scan(&value)
	}); err != nil {
		t.Fatalf("read restored backup: %v", err)
	}
	if value != "preserved" {
		t.Fatalf("restored value = %q, want preserved", value)
	}
}

func TestMigrationChecksumsNormalizeLineEndings(t *testing.T) {
	t.Parallel()

	withLF := fstest.MapFS{
		"migrations/001_test.sql": {Data: []byte("CREATE TABLE test (id INTEGER);\n")},
	}
	withCRLF := fstest.MapFS{
		"migrations/001_test.sql": {Data: []byte("CREATE TABLE test (id INTEGER);\r\n")},
	}
	left, err := loadMigrations(withLF)
	if err != nil {
		t.Fatalf("load LF migration: %v", err)
	}
	right, err := loadMigrations(withCRLF)
	if err != nil {
		t.Fatalf("load CRLF migration: %v", err)
	}
	if left[0].checksum != right[0].checksum {
		t.Fatalf("checksums differ: %s != %s", left[0].checksum, right[0].checksum)
	}
}

func TestVersionComparison(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left, right string
		want        int
	}{
		{"3.51.2", "3.51.3", -1},
		{"3.51.3", "3.51.3", 0},
		{"3.53.0", "3.51.3", 1},
		{"4.0.0", "3.99.99", 1},
	}
	for _, test := range tests {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	state, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { _ = state.Close() })
	return state
}

func openRawDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", dataSourceName(path, false))
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if err := database.Ping(); err != nil {
		_ = database.Close()
		t.Fatalf("ping raw database: %v", err)
	}
	return database
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %q = %o, want %o", path, got, want)
	}
}
