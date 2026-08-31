// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store owns Stackfort's private SQLite control-plane state.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

const (
	busyTimeoutMilliseconds = 5_000
	maxOpenConnections      = 4
	minimumSQLiteVersion    = "3.51.3"
)

// Store is the bounded connection pool for the panel state database.
type Store struct {
	db      *sql.DB
	path    string
	writeMu sync.Mutex
}

// Executor is the deliberately small database surface available inside a
// serialized write transaction.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Reader is the read-only database surface used by Store.Read.
type Reader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Open creates or opens an absolute, private database path, applies all known
// migrations, and refuses schemas that this binary cannot safely understand.
func Open(ctx context.Context, path string) (_ *Store, returnErr error) {
	cleanPath, err := prepareDatabaseFile(path)
	if err != nil {
		return nil, err
	}

	connector, err := sqlite.NewConnector(dataSourceName(cleanPath, false))
	if err != nil {
		return nil, fmt.Errorf("configure SQLite connector: %w", err)
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db, path: cleanPath}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, db.Close())
		}
	}()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open panel state database: %w", err)
	}
	if err := requireSafeSQLiteVersion(ctx, db); err != nil {
		return nil, err
	}

	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return nil, fmt.Errorf("enable SQLite WAL mode: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return nil, fmt.Errorf("enable SQLite WAL mode: SQLite returned %q", journalMode)
	}

	if err := store.migrate(ctx, embeddedMigrations); err != nil {
		return nil, err
	}
	if err := quickCheck(ctx, db); err != nil {
		return nil, fmt.Errorf("check panel state database: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConnections)
	db.SetMaxIdleConns(maxOpenConnections)
	return store, nil
}

// Path returns the canonical absolute database path.
func (s *Store) Path() string {
	return s.path
}

// Close releases the connection pool cleanly so SQLite can checkpoint WAL
// state during process shutdown.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close panel state database: %w", err)
	}
	return nil
}

// Ping verifies that the database can still serve a connection.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping panel state database: %w", err)
	}
	return nil
}

// Read executes fn against the connection pool. The supplied interface has no
// mutation method; callers must keep result sets and read transactions short.
func (s *Store) Read(ctx context.Context, fn func(Reader) error) error {
	if err := fn(s.db); err != nil {
		return fmt.Errorf("read panel state: %w", err)
	}
	return nil
}

// Write serializes writers and runs fn inside BEGIN IMMEDIATE. This bounds
// lock contention while WAL mode continues to allow concurrent readers.
func (s *Store) Write(ctx context.Context, fn func(Executor) error) (returnErr error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire panel state writer: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, conn.Close())
	}()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin panel state write: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, rollbackErr := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
			returnErr = errors.Join(returnErr, rollbackErr)
		}
	}()

	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit panel state write: %w", err)
	}
	committed = true
	return nil
}

func prepareDatabaseFile(path string) (string, error) {
	cleanPath, err := cleanAbsolutePath(path)
	if err != nil {
		return "", fmt.Errorf("panel state path: %w", err)
	}

	directory := filepath.Dir(cleanPath)
	if err := ensurePrivateDirectory(directory); err != nil {
		return "", fmt.Errorf("prepare panel state directory: %w", err)
	}

	info, err := os.Lstat(cleanPath)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("panel state database must not be a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return "", errors.New("panel state database must be a regular file")
		}
		if err := os.Chmod(cleanPath, 0o600); err != nil {
			return "", fmt.Errorf("protect panel state database: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := createPrivateFile(directory, filepath.Base(cleanPath)); err != nil {
			return "", fmt.Errorf("create panel state database: %w", err)
		}
	default:
		return "", fmt.Errorf("inspect panel state database: %w", err)
	}

	return cleanPath, nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("directory must not be a symbolic link")
		}
		if !info.IsDir() {
			return errors.New("path is not a directory")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o027 != 0 {
			return fmt.Errorf("directory permissions %o allow group writes or access by other users", info.Mode().Perm())
		}
		return nil
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o750); err != nil {
			return err
		}
		// The directory is intentionally group-readable/traversable so the
		// dedicated Stackfort service group can reach private mode-0600 state.
		// #nosec G302 -- this is the documented directory policy, not a regular file.
		if err := os.Chmod(path, 0o750); err != nil {
			return err
		}
		return nil
	default:
		return err
	}
}

func createPrivateFile(directory, name string) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.Join(err, root.Close())
	}
	return errors.Join(file.Close(), root.Close())
}

func cleanAbsolutePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is empty")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(filepath.Clean(path), `\\`) {
		return "", errors.New("network paths are unsupported")
	}
	return filepath.Clean(path), nil
}

func samePath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func dataSourceName(path string, readOnly bool) string {
	uriPath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	query := uri.Query()
	query.Set("_busy_timeout", strconv.Itoa(busyTimeoutMilliseconds))
	query.Set("_foreign_keys", "on")
	query.Set("_pragma", "trusted_schema(off)")
	query.Set("_synchronous", "full")
	if readOnly {
		query.Set("mode", "ro")
		query.Set("immutable", "1")
		query.Set("_query_only", "on")
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func requireSafeSQLiteVersion(ctx context.Context, db *sql.DB) error {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return fmt.Errorf("read SQLite version: %w", err)
	}
	if compareVersions(version, minimumSQLiteVersion) < 0 {
		return fmt.Errorf("SQLite %s is unsupported; version %s or newer is required", version, minimumSQLiteVersion)
	}
	return nil
}

func compareVersions(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := range 3 {
		leftValue := versionPart(leftParts, index)
		rightValue := versionPart(rightParts, index)
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, _ := strconv.Atoi(parts[index])
	return value
}

func quickCheck(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	rows, err := db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return err
	}
	defer rows.Close()

	resultCount := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		resultCount++
		if result != "ok" {
			return fmt.Errorf("SQLite quick check failed: %s", result)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if resultCount != 1 {
		return fmt.Errorf("SQLite quick check returned %d rows", resultCount)
	}
	return nil
}
