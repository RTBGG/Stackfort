// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrFutureSchema means that the database was written by a newer Stackfort
	// binary and must not be opened by this one.
	ErrFutureSchema = errors.New("panel state schema is newer than this binary")
	// ErrMigrationDrift means that an already-applied migration no longer
	// matches the binary's embedded, normalized SQL.
	ErrMigrationDrift = errors.New("panel state migration checksum mismatch")
	// ErrUnmanagedSchema means that a non-empty SQLite database has no Stackfort
	// migration history and cannot be adopted implicitly.
	ErrUnmanagedSchema = errors.New("database contains an unmanaged schema")
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int
	name     string
	sql      string
	checksum string
}

type appliedMigration struct {
	version  int
	name     string
	checksum string
}

var (
	migrationNamePattern = regexp.MustCompile(`^(\d{3})_([a-z0-9_]+)\.sql$`)
	embeddedMigrations   = mustLoadMigrations(migrationFiles)
)

func mustLoadMigrations(source fs.FS) []migration {
	migrations, err := loadMigrations(source)
	if err != nil {
		panic(err)
	}
	return migrations
}

func loadMigrations(source fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(source, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("migration directory contains subdirectory %q", entry.Name())
		}
		matches := migrationNamePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		contents, err := fs.ReadFile(source, path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		normalizedSQL := normalizeSQL(string(contents))
		checksum := sha256.Sum256([]byte(normalizedSQL))
		migrations = append(migrations, migration{
			version:  version,
			name:     matches[2],
			sql:      normalizedSQL,
			checksum: fmt.Sprintf("%x", checksum),
		})
	}

	slices.SortFunc(migrations, func(left, right migration) int {
		return left.version - right.version
	})
	for index, item := range migrations {
		expected := index + 1
		if item.version != expected {
			return nil, fmt.Errorf("migration sequence has version %d, expected %d", item.version, expected)
		}
	}
	if len(migrations) == 0 {
		return nil, errors.New("no embedded migrations")
	}
	return migrations, nil
}

func normalizeSQL(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func (s *Store) migrate(ctx context.Context, migrations []migration) error {
	hasHistory, err := migrationHistoryExists(ctx, s.db)
	if err != nil {
		return err
	}
	if !hasHistory {
		hasTables, err := unmanagedTablesExist(ctx, s.db)
		if err != nil {
			return err
		}
		if hasTables {
			return ErrUnmanagedSchema
		}
		if err := s.createMigrationHistory(ctx); err != nil {
			return err
		}
	}

	applied, err := readAppliedMigrations(ctx, s.db)
	if err != nil {
		return err
	}
	if err := validateAppliedMigrations(applied, migrations); err != nil {
		return err
	}

	for _, item := range migrations[len(applied):] {
		if err := s.applyMigration(ctx, item); err != nil {
			return fmt.Errorf("apply migration %03d_%s: %w", item.version, item.name, err)
		}
	}
	return nil
}

func migrationHistoryExists(ctx context.Context, reader Reader) (bool, error) {
	var exists bool
	err := reader.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM sqlite_schema
			WHERE type = 'table' AND name = 'schema_migrations'
		)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect migration history: %w", err)
	}
	return exists, nil
}

func unmanagedTablesExist(ctx context.Context, reader Reader) (bool, error) {
	var exists bool
	err := reader.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM sqlite_schema
			WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		)`).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect existing schema: %w", err)
	}
	return exists, nil
}

func (s *Store) createMigrationHistory(ctx context.Context) error {
	return s.Write(ctx, func(executor Executor) error {
		_, err := executor.ExecContext(ctx, `
			CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY CHECK (version > 0),
				name TEXT NOT NULL UNIQUE,
				checksum TEXT NOT NULL CHECK (length(checksum) = 64),
				applied_at TEXT NOT NULL
			) STRICT`)
		if err != nil {
			return fmt.Errorf("create migration history: %w", err)
		}
		return nil
	})
}

func readAppliedMigrations(ctx context.Context, reader Reader) ([]appliedMigration, error) {
	rows, err := reader.QueryContext(ctx, `
		SELECT version, name, checksum
		FROM schema_migrations
		ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}
	defer rows.Close()

	var applied []appliedMigration
	for rows.Next() {
		var item appliedMigration
		if err := rows.Scan(&item.version, &item.name, &item.checksum); err != nil {
			return nil, fmt.Errorf("scan migration history: %w", err)
		}
		applied = append(applied, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration history: %w", err)
	}
	return applied, nil
}

func validateAppliedMigrations(applied []appliedMigration, known []migration) error {
	if len(applied) > len(known) {
		return fmt.Errorf("%w: database version %d, binary version %d", ErrFutureSchema, applied[len(applied)-1].version, len(known))
	}
	for index, item := range applied {
		expectedVersion := index + 1
		if item.version != expectedVersion {
			if item.version > len(known) {
				return fmt.Errorf("%w: database version %d, binary version %d", ErrFutureSchema, item.version, len(known))
			}
			return fmt.Errorf("migration history is not contiguous at version %d", expectedVersion)
		}
		knownItem := known[index]
		if item.name != knownItem.name || item.checksum != knownItem.checksum {
			return fmt.Errorf("%w at version %d", ErrMigrationDrift, item.version)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, item migration) error {
	return s.Write(ctx, func(executor Executor) error {
		if _, err := executor.ExecContext(ctx, item.sql); err != nil {
			return fmt.Errorf("execute migration SQL: %w", err)
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO schema_migrations (version, name, checksum, applied_at)
			VALUES (?, ?, ?, ?)`,
			item.version,
			item.name,
			item.checksum,
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
		return nil
	})
}
