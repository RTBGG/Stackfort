// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostdatabase owns the agent-only MariaDB administrative boundary.
// It connects only through a fixed local Unix socket and exposes no generic SQL
// execution primitive to the control API.
package hostdatabase

import (
	"context"
	"crypto/sha1" // #nosec G505 -- MariaDB mysql_native_password mandates double SHA-1.
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/databaseidentity"
	mysql "github.com/go-sql-driver/mysql"
)

const (
	controlSchema = "stackfort_control"
	controlTable  = "managed_objects"
	lifecycleLock = "stackfort:database-lifecycle"
)

var socketCandidates = []string{
	"/run/mysqld/mysqld.sock",
	"/var/lib/mysql/mysql.sock",
}

type ErrorKind string

const (
	ErrorConflict    ErrorKind = "conflict"
	ErrorUnavailable ErrorKind = "unavailable"
	ErrorValidation  ErrorKind = "validation"
	ErrorMutation    ErrorKind = "mutation"
)

type Error struct{ Kind ErrorKind }

func (err *Error) Error() string { return "managed database " + string(err.Kind) }

type Result struct {
	Changed bool
	Active  bool
}

type marker struct {
	AccountID   string
	OperationID string
	State       string
}

type backend interface {
	Close() error
	Acquire(context.Context) error
	Release(context.Context)
	EnsureControlSchema(context.Context) error
	VerifyControlSchema(context.Context) error
	Marker(context.Context, string, string) (marker, bool, error)
	ObjectExists(context.Context, string, string, string) (bool, error)
	InsertMarker(context.Context, string, string, string, string) error
	ActivateMarker(context.Context, string, string, string, string) error
	AdvanceUserMarker(context.Context, string, string, string) error
	CreateDatabase(context.Context, string) error
	CreateUser(context.Context, string, string, []byte) error
	AlterUserPassword(context.Context, string, string, []byte) error
	Grant(context.Context, string, string, string, agentprotocol.DatabaseGrantPreset) error
	GrantExists(context.Context, string, string, string) (bool, error)
	Revoke(context.Context, string, string, string, agentprotocol.DatabaseGrantPreset) error
	DropDatabase(context.Context, string) error
	DropUser(context.Context, string, string) error
	DeleteMarker(context.Context, string, string, string) error
}

// RotatePassword changes only an existing active Stackfort-owned principal.
// Reapplying the same candidate password after a crash is semantically
// idempotent. Advancing the marker fences every older provisioning/rotation
// operation from restoring a superseded credential.
func (reconciler *Reconciler) RotatePassword(
	ctx context.Context,
	operationID, accountID string,
	request agentprotocol.DatabasePasswordRotateRequest,
) (Result, error) {
	correlation := &agentprotocol.AuditCorrelation{
		OperationID: operationID, AccountID: accountID, ActorKind: agentprotocol.ActorSystem,
	}
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "database-password-rotate-validation",
		IdempotencyKey: "database-password-rotate-validation",
		Operation:      agentprotocol.OperationRotateDatabasePassword,
		Correlation:    correlation, RotateDatabasePassword: &request,
	}
	if agentprotocol.ValidateRequest(protocolRequest) != nil {
		return Result{}, &Error{Kind: ErrorValidation}
	}
	if reconciler == nil || reconciler.open == nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	connection, err := reconciler.open(ctx)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	defer connection.Close()
	if err := connection.Acquire(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	defer connection.Release(context.WithoutCancel(ctx))
	if err := connection.EnsureControlSchema(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	if err := connection.VerifyControlSchema(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorConflict}
	}
	entry, marked, err := connection.Marker(ctx, "user", request.Username)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	present, err := connection.ObjectExists(ctx, "user", request.Username, request.Host)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	if !marked || !present || entry.AccountID != accountID || entry.State != "active" {
		return Result{}, &Error{Kind: ErrorConflict}
	}
	if err := connection.AlterUserPassword(ctx, request.Username, request.Host, request.Password); err != nil {
		return Result{}, &Error{Kind: ErrorMutation}
	}
	if err := connection.AdvanceUserMarker(ctx, request.Username, accountID, operationID); err != nil {
		return Result{}, &Error{Kind: ErrorMutation}
	}
	return Result{Changed: true, Active: true}, nil
}

func (reconciler *Reconciler) Drop(
	ctx context.Context,
	operationID, accountID string,
	request agentprotocol.DatabaseDropRequest,
) (Result, error) {
	correlation := &agentprotocol.AuditCorrelation{
		OperationID: operationID, AccountID: accountID, ActorKind: agentprotocol.ActorSystem,
	}
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "database-drop-validation",
		IdempotencyKey: "database-drop-validation", Operation: agentprotocol.OperationDropDatabase,
		Correlation: correlation, DropDatabase: &request,
	}
	if agentprotocol.ValidateRequest(protocolRequest) != nil {
		return Result{}, &Error{Kind: ErrorValidation}
	}
	if reconciler == nil || reconciler.open == nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	connection, err := reconciler.open(ctx)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	defer connection.Close()
	if err := connection.Acquire(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	defer connection.Release(context.WithoutCancel(ctx))
	if err := connection.EnsureControlSchema(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	if err := connection.VerifyControlSchema(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorConflict}
	}

	entry, marked, err := connection.Marker(ctx, string(request.Kind), request.Name)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	present, err := connection.ObjectExists(ctx, string(request.Kind), request.Name, request.Host)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	if !marked {
		if present {
			return Result{}, &Error{Kind: ErrorConflict}
		}
		return Result{Active: true}, nil
	}
	if entry.AccountID != accountID || entry.State != "active" {
		return Result{}, &Error{Kind: ErrorConflict}
	}

	if request.Kind == agentprotocol.DatabaseDropDatabase {
		for _, grant := range request.Grants {
			userMarker, exists, markerErr := connection.Marker(ctx, "user", grant.Username)
			if markerErr != nil {
				return Result{}, &Error{Kind: ErrorUnavailable}
			}
			if !exists || userMarker.AccountID != accountID || userMarker.State != "active" {
				return Result{}, &Error{Kind: ErrorConflict}
			}
			granted, grantErr := connection.GrantExists(ctx, request.Name, grant.Username, grant.Host)
			if grantErr != nil {
				return Result{}, &Error{Kind: ErrorUnavailable}
			}
			if granted && connection.Revoke(ctx, request.Name, grant.Username, grant.Host, grant.Preset) != nil {
				return Result{}, &Error{Kind: ErrorMutation}
			}
		}
		if present && connection.DropDatabase(ctx, request.Name) != nil {
			return Result{}, &Error{Kind: ErrorMutation}
		}
	} else if present && connection.DropUser(ctx, request.Name, request.Host) != nil {
		return Result{}, &Error{Kind: ErrorMutation}
	}
	if err := connection.DeleteMarker(ctx, string(request.Kind), request.Name, accountID); err != nil {
		return Result{}, &Error{Kind: ErrorMutation}
	}
	return Result{Changed: present, Active: true}, nil
}

type backendFactory func(context.Context) (backend, error)

type Reconciler struct{ open backendFactory }

func NewReconciler() *Reconciler {
	return &Reconciler{open: openLocalMariaDB}
}

func (reconciler *Reconciler) Reconcile(
	ctx context.Context,
	operationID, accountID string,
	request agentprotocol.DatabaseProvisionRequest,
) (Result, error) {
	correlation := &agentprotocol.AuditCorrelation{OperationID: operationID, AccountID: accountID, ActorKind: agentprotocol.ActorSystem}
	if err := agentprotocol.ValidateAuditCorrelation(*correlation); err != nil ||
		databaseidentity.ValidateDerived(accountID, request.DatabaseAlias, request.DatabaseName) != nil ||
		databaseidentity.ValidateDerived(accountID, request.UserAlias, request.Username) != nil ||
		request.Host != databaseidentity.LocalHost ||
		(request.CreateUser && (len(request.Password) < 20 || len(request.Password) > 256)) ||
		(!request.CreateUser && len(request.Password) != 0) ||
		(request.Preset != agentprotocol.DatabaseGrantReadOnly && request.Preset != agentprotocol.DatabaseGrantReadWrite) {
		return Result{}, &Error{Kind: ErrorValidation}
	}
	if reconciler == nil || reconciler.open == nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	connection, err := reconciler.open(ctx)
	if err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	defer connection.Close()
	if err := connection.Acquire(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	defer connection.Release(context.WithoutCancel(ctx))
	if err := connection.EnsureControlSchema(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorUnavailable}
	}
	if err := connection.VerifyControlSchema(ctx); err != nil {
		return Result{}, &Error{Kind: ErrorConflict}
	}

	changed, err := reconcileObject(ctx, connection, "database", request.DatabaseName, accountID, operationID, func() error {
		return connection.CreateDatabase(ctx, request.DatabaseName)
	})
	if err != nil {
		return Result{}, err
	}
	userChanged, err := reconcileUser(ctx, connection, request, accountID, operationID)
	if err != nil {
		return Result{}, err
	}
	changed = changed || userChanged
	if err := connection.Grant(ctx, request.DatabaseName, request.Username, request.Host, request.Preset); err != nil {
		return Result{}, &Error{Kind: ErrorMutation}
	}
	return Result{Changed: changed, Active: true}, nil
}

func reconcileObject(
	ctx context.Context,
	connection backend,
	kind, name, accountID, operationID string,
	create func() error,
) (bool, error) {
	entry, exists, err := connection.Marker(ctx, kind, name)
	if err != nil {
		return false, &Error{Kind: ErrorUnavailable}
	}
	if exists {
		if entry.AccountID != accountID || entry.OperationID != operationID ||
			(entry.State != "creating" && entry.State != "active") {
			return false, &Error{Kind: ErrorConflict}
		}
		objectExists, checkErr := connection.ObjectExists(ctx, kind, name, databaseidentity.LocalHost)
		if checkErr != nil {
			return false, &Error{Kind: ErrorUnavailable}
		}
		if entry.State == "active" {
			if !objectExists {
				return false, &Error{Kind: ErrorConflict}
			}
			return false, nil
		}
		if !objectExists {
			if err := create(); err != nil {
				return false, &Error{Kind: ErrorMutation}
			}
		}
		if err := connection.ActivateMarker(ctx, kind, name, accountID, operationID); err != nil {
			return false, &Error{Kind: ErrorMutation}
		}
		return true, nil
	}
	objectExists, err := connection.ObjectExists(ctx, kind, name, databaseidentity.LocalHost)
	if err != nil {
		return false, &Error{Kind: ErrorUnavailable}
	}
	if objectExists {
		return false, &Error{Kind: ErrorConflict}
	}
	if err := connection.InsertMarker(ctx, kind, name, accountID, operationID); err != nil {
		return false, &Error{Kind: ErrorMutation}
	}
	if err := create(); err != nil {
		return false, &Error{Kind: ErrorMutation}
	}
	if err := connection.ActivateMarker(ctx, kind, name, accountID, operationID); err != nil {
		return false, &Error{Kind: ErrorMutation}
	}
	return true, nil
}

func reconcileUser(
	ctx context.Context,
	connection backend,
	request agentprotocol.DatabaseProvisionRequest,
	accountID, operationID string,
) (bool, error) {
	entry, exists, err := connection.Marker(ctx, "user", request.Username)
	if err != nil {
		return false, &Error{Kind: ErrorUnavailable}
	}
	if exists && !request.CreateUser {
		if entry.AccountID != accountID || entry.State != "active" {
			return false, &Error{Kind: ErrorConflict}
		}
		present, checkErr := connection.ObjectExists(ctx, "user", request.Username, request.Host)
		if checkErr != nil {
			return false, &Error{Kind: ErrorUnavailable}
		}
		if !present {
			return false, &Error{Kind: ErrorConflict}
		}
		return false, nil
	}
	if !request.CreateUser {
		return false, &Error{Kind: ErrorConflict}
	}
	changed, err := reconcileObject(ctx, connection, "user", request.Username, accountID, operationID, func() error {
		return connection.CreateUser(ctx, request.Username, request.Host, request.Password)
	})
	if err != nil {
		return false, err
	}
	// A replay uses the same encrypted control-plane credential. Resetting only
	// a principal owned by the same operation closes a crash window without
	// adopting or rotating an unrelated account user.
	if !changed {
		if err := connection.AlterUserPassword(ctx, request.Username, request.Host, request.Password); err != nil {
			return false, &Error{Kind: ErrorMutation}
		}
	}
	return changed, nil
}

type sqlBackend struct {
	database   *sql.DB
	connection *sql.Conn
}

func openLocalMariaDB(ctx context.Context) (backend, error) {
	socketPath, err := discoverSocket()
	if err != nil {
		return nil, err
	}
	config := mysql.NewConfig()
	config.User = "root"
	config.Net = "unix"
	config.Addr = socketPath
	config.Timeout = 3 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return nil, err
	}
	database := sql.OpenDB(connector)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(0)
	connection, err := database.Conn(ctx)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	return &sqlBackend{database: database, connection: connection}, nil
}

func discoverSocket() (string, error) {
	for _, path := range socketCandidates {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
			return "", errors.New("MariaDB socket path is not a real Unix socket")
		}
		return path, nil
	}
	return "", errors.New("MariaDB Unix socket is unavailable")
}

func (backend *sqlBackend) Close() error {
	return errors.Join(backend.connection.Close(), backend.database.Close())
}

func (backend *sqlBackend) Acquire(ctx context.Context) error {
	var acquired int
	if err := backend.connection.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", lifecycleLock).Scan(&acquired); err != nil {
		return err
	}
	if acquired != 1 {
		return errors.New("MariaDB lifecycle lock is unavailable")
	}
	return nil
}

func (backend *sqlBackend) Release(ctx context.Context) {
	var released sql.NullInt64
	_ = backend.connection.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lifecycleLock).Scan(&released)
}

func (backend *sqlBackend) EnsureControlSchema(ctx context.Context) error {
	statements := []string{
		"CREATE DATABASE IF NOT EXISTS `stackfort_control` CHARACTER SET ascii COLLATE ascii_bin",
		"CREATE TABLE IF NOT EXISTS `stackfort_control`.`managed_objects` (" +
			"object_type ENUM('database','user') NOT NULL," +
			"object_name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL," +
			"account_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL," +
			"operation_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL," +
			"state ENUM('creating','active') NOT NULL," +
			"PRIMARY KEY (object_type, object_name)" +
			") ENGINE=InnoDB",
	}
	for _, statement := range statements {
		if _, err := backend.connection.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (backend *sqlBackend) VerifyControlSchema(ctx context.Context) error {
	var charset, collation, engine string
	if err := backend.connection.QueryRowContext(ctx, `
		SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME
		FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?`, controlSchema).Scan(&charset, &collation); err != nil {
		return err
	}
	if charset != "ascii" || collation != "ascii_bin" {
		return errors.New("managed MariaDB control schema has an unexpected charset")
	}
	if err := backend.connection.QueryRowContext(ctx, `
		SELECT ENGINE FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, controlSchema, controlTable).Scan(&engine); err != nil {
		return err
	}
	if !strings.EqualFold(engine, "InnoDB") {
		return errors.New("managed MariaDB control table has an unexpected engine")
	}
	expected := []struct{ name, columnType string }{
		{"object_type", "enum('database','user')"}, {"object_name", "varchar(64)"},
		{"account_id", "char(36)"}, {"operation_id", "char(36)"},
		{"state", "enum('creating','active')"},
	}
	rows, err := backend.connection.QueryContext(ctx, `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, CHARACTER_SET_NAME, COLLATION_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, controlSchema, controlTable)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var name, columnType, nullable string
		var columnCharset, columnCollation sql.NullString
		if err := rows.Scan(&name, &columnType, &nullable, &columnCharset, &columnCollation); err != nil {
			return err
		}
		if index >= len(expected) || name != expected[index].name ||
			strings.ToLower(columnType) != expected[index].columnType || nullable != "NO" ||
			!columnCharset.Valid || columnCharset.String != "ascii" ||
			!columnCollation.Valid || columnCollation.String != "ascii_bin" {
			return errors.New("managed MariaDB control table has unexpected columns")
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index != len(expected) {
		return errors.New("managed MariaDB control table has an unexpected column count")
	}
	primaryRows, err := backend.connection.QueryContext(ctx, `
		SELECT COLUMN_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND INDEX_NAME = 'PRIMARY'
		ORDER BY SEQ_IN_INDEX`, controlSchema, controlTable)
	if err != nil {
		return err
	}
	defer primaryRows.Close()
	primary := []string{}
	for primaryRows.Next() {
		var name string
		if err := primaryRows.Scan(&name); err != nil {
			return err
		}
		primary = append(primary, name)
	}
	if err := primaryRows.Err(); err != nil {
		return err
	}
	if len(primary) != 2 || primary[0] != "object_type" || primary[1] != "object_name" {
		return errors.New("managed MariaDB control table has an unexpected primary key")
	}
	return nil
}

func (backend *sqlBackend) Marker(ctx context.Context, kind, name string) (marker, bool, error) {
	var result marker
	err := backend.connection.QueryRowContext(ctx, `
		SELECT account_id, operation_id, state
		FROM stackfort_control.managed_objects
		WHERE object_type = ? AND object_name = ?`, kind, name).Scan(
		&result.AccountID, &result.OperationID, &result.State,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return marker{}, false, nil
	}
	return result, err == nil, err
}

func (backend *sqlBackend) ObjectExists(ctx context.Context, kind, name, host string) (bool, error) {
	var exists int
	var err error
	switch kind {
	case "database":
		err = backend.connection.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?)`, name).Scan(&exists)
	case "user":
		err = backend.connection.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM mysql.user WHERE User = ? AND Host = ?)`, name, host).Scan(&exists)
	default:
		return false, errors.New("unsupported managed MariaDB object kind")
	}
	return exists == 1, err
}

func (backend *sqlBackend) InsertMarker(ctx context.Context, kind, name, accountID, operationID string) error {
	_, err := backend.connection.ExecContext(ctx, `
		INSERT INTO stackfort_control.managed_objects
		(object_type, object_name, account_id, operation_id, state)
		VALUES (?, ?, ?, ?, 'creating')`, kind, name, accountID, operationID)
	return err
}

func (backend *sqlBackend) ActivateMarker(ctx context.Context, kind, name, accountID, operationID string) error {
	result, err := backend.connection.ExecContext(ctx, `
		UPDATE stackfort_control.managed_objects SET state = 'active'
		WHERE object_type = ? AND object_name = ? AND account_id = ? AND operation_id = ?`,
		kind, name, accountID, operationID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errors.New("managed MariaDB marker activation was fenced")
	}
	return nil
}

func (backend *sqlBackend) AdvanceUserMarker(ctx context.Context, name, accountID, operationID string) error {
	result, err := backend.connection.ExecContext(ctx, `
		UPDATE stackfort_control.managed_objects SET operation_id = ?
		WHERE object_type = 'user' AND object_name = ? AND account_id = ? AND state = 'active'`,
		operationID, name, accountID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errors.New("managed MariaDB user marker advancement was fenced")
	}
	return nil
}

func (backend *sqlBackend) CreateDatabase(ctx context.Context, name string) error {
	_, err := backend.connection.ExecContext(ctx,
		"CREATE DATABASE "+quoteIdentifier(name)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	return err
}

func (backend *sqlBackend) CreateUser(ctx context.Context, username, host string, password []byte) error {
	passwordHash := nativePasswordHash(password)
	_, err := backend.connection.ExecContext(ctx,
		"CREATE USER "+quotePrincipal(username, host)+" IDENTIFIED BY PASSWORD '"+passwordHash+"'")
	return err
}

func (backend *sqlBackend) AlterUserPassword(ctx context.Context, username, host string, password []byte) error {
	passwordHash := nativePasswordHash(password)
	_, err := backend.connection.ExecContext(ctx,
		"ALTER USER "+quotePrincipal(username, host)+" IDENTIFIED BY PASSWORD '"+passwordHash+"'")
	return err
}

// nativePasswordHash returns MariaDB's documented mysql_native_password
// verifier. The control plane generates 192 bits of random password material;
// only this verifier enters SQL text, never the plaintext credential.
func nativePasswordHash(password []byte) string {
	// #nosec G401,G505 -- this is a wire-compatible database verifier, not a general-purpose password hash.
	first := sha1.Sum(password)
	// #nosec G401,G505 -- MariaDB mysql_native_password requires SHA1(SHA1(password)).
	second := sha1.Sum(first[:])
	clear(first[:])
	return "*" + strings.ToUpper(hex.EncodeToString(second[:]))
}

func (backend *sqlBackend) Grant(
	ctx context.Context,
	databaseName, username, host string,
	preset agentprotocol.DatabaseGrantPreset,
) error {
	privileges := "SELECT, SHOW VIEW"
	if preset == agentprotocol.DatabaseGrantReadWrite {
		privileges = "SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, INDEX, ALTER, " +
			"CREATE TEMPORARY TABLES, LOCK TABLES, REFERENCES, CREATE VIEW, SHOW VIEW, TRIGGER, EXECUTE"
	}
	_, err := backend.connection.ExecContext(ctx,
		"GRANT "+privileges+" ON "+quoteIdentifier(databaseName)+".* TO "+quotePrincipal(username, host))
	return err
}

func (backend *sqlBackend) GrantExists(ctx context.Context, databaseName, username, host string) (bool, error) {
	var exists int
	err := backend.connection.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM mysql.db
		  WHERE Db = ? AND User = ? AND Host = ?
		)`, databaseName, username, host).Scan(&exists)
	return exists == 1, err
}

func (backend *sqlBackend) Revoke(
	ctx context.Context,
	databaseName, username, host string,
	_ agentprotocol.DatabaseGrantPreset,
) error {
	_, err := backend.connection.ExecContext(ctx,
		"REVOKE ALL PRIVILEGES ON "+quoteIdentifier(databaseName)+".* FROM "+quotePrincipal(username, host))
	return err
}

func (backend *sqlBackend) DropDatabase(ctx context.Context, name string) error {
	_, err := backend.connection.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name))
	return err
}

func (backend *sqlBackend) DropUser(ctx context.Context, username, host string) error {
	_, err := backend.connection.ExecContext(ctx, "DROP USER IF EXISTS "+quotePrincipal(username, host))
	return err
}

func (backend *sqlBackend) DeleteMarker(ctx context.Context, kind, name, accountID string) error {
	result, err := backend.connection.ExecContext(ctx, `
		DELETE FROM stackfort_control.managed_objects
		WHERE object_type = ? AND object_name = ? AND account_id = ? AND state = 'active'`,
		kind, name, accountID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errors.New("managed MariaDB marker deletion was fenced")
	}
	return nil
}

func quoteIdentifier(value string) string { return "`" + value + "`" }

func quotePrincipal(username, host string) string {
	return fmt.Sprintf("'%s'@'%s'", username, host)
}
