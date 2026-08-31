// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"modernc.org/sqlite"
)

// Backup creates a consistent, compact snapshot without overwriting any
// existing destination. The temporary file and final hard link live on the
// same filesystem, so publication is atomic.
func (s *Store) Backup(ctx context.Context, destination string) error {
	cleanDestination, err := cleanAbsolutePath(destination)
	if err != nil {
		return fmt.Errorf("backup destination: %w", err)
	}
	if samePath(s.path, cleanDestination) {
		return errors.New("backup destination must differ from panel state database")
	}

	directory := filepath.Dir(cleanDestination)
	if err := ensurePrivateDirectory(directory); err != nil {
		return fmt.Errorf("prepare backup directory: %w", err)
	}
	if _, err := os.Lstat(cleanDestination); err == nil {
		return errors.New("backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup destination: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".stackfort-state-*.partial")
	if err != nil {
		return fmt.Errorf("reserve temporary backup: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary backup: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", temporaryPath); err != nil {
		return fmt.Errorf("create consistent panel state backup: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return fmt.Errorf("protect panel state backup: %w", err)
	}
	if err := verifyBackup(ctx, temporaryPath, embeddedMigrations); err != nil {
		return err
	}

	// Link fails if destination appeared after the earlier check and therefore
	// never overwrites an existing backup.
	if err := os.Link(temporaryPath, cleanDestination); err != nil {
		return fmt.Errorf("publish panel state backup: %w", err)
	}
	return nil
}

func verifyBackup(ctx context.Context, path string, migrations []migration) error {
	connector, err := sqlite.NewConnector(dataSourceName(path, true))
	if err != nil {
		return fmt.Errorf("configure backup verifier: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("open panel state backup: %w", err)
	}
	if err := quickCheck(ctx, db); err != nil {
		return fmt.Errorf("verify panel state backup: %w", err)
	}
	violations, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("verify panel state backup foreign keys: %w", err)
	}
	hasViolation := violations.Next()
	iterationErr := violations.Err()
	if err := violations.Close(); err != nil {
		return fmt.Errorf("close panel state backup foreign-key check: %w", err)
	}
	if iterationErr != nil {
		return fmt.Errorf("verify panel state backup foreign keys: %w", iterationErr)
	}
	if hasViolation {
		return errors.New("verify panel state backup: foreign-key violation found")
	}

	hasHistory, err := migrationHistoryExists(ctx, db)
	if err != nil {
		return fmt.Errorf("verify panel state backup: %w", err)
	}
	if !hasHistory {
		return errors.New("verify panel state backup: migration history is missing")
	}
	applied, err := readAppliedMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("verify panel state backup: %w", err)
	}
	if err := validateAppliedMigrations(applied, migrations); err != nil {
		return fmt.Errorf("verify panel state backup: %w", err)
	}
	return nil
}
