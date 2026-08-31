// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration && linux

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/databaseidentity"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestInstalledAgentMariaDBTenantLifecycle(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	// Use the same full RPC handler and peer-verification boundary as the
	// installed service while admitting this root-owned destructive test
	// process. The production socket correctly accepts only stackfort-api's UID.
	client := startDisposableAgentRPC(t)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()

	first := newDatabaseFixture(t, "writer", agentprotocol.DatabaseGrantReadWrite)
	second := newDatabaseFixture(t, "reader", agentprotocol.DatabaseGrantReadOnly)
	provisionDatabaseFixture(t, ctx, client, first)
	provisionDatabaseFixture(t, ctx, client, second)
	defer dropDatabaseFixture(t, context.WithoutCancel(ctx), client, second)
	defer dropDatabaseFixture(t, context.WithoutCancel(ctx), client, first)

	root := openMariaDBForIntegration(t, "root", "", "")
	defer root.Close()
	if _, err := root.ExecContext(ctx, "CREATE TABLE `"+second.databaseName+"`.`probe` (value INT NOT NULL)"); err != nil {
		t.Fatalf("create read-only fixture table: %v", err)
	}
	if _, err := root.ExecContext(ctx, "INSERT INTO `"+second.databaseName+"`.`probe` (value) VALUES (7)"); err != nil {
		t.Fatalf("seed read-only fixture table: %v", err)
	}

	writer := openMariaDBForIntegration(t, first.username, first.password, first.databaseName)
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, "CREATE TABLE own_probe (value INT NOT NULL)"); err != nil {
		t.Fatalf("writer cannot create own table: %v", err)
	}
	if _, err := writer.ExecContext(ctx, "INSERT INTO own_probe (value) VALUES (1)"); err != nil {
		t.Fatalf("writer cannot insert own row: %v", err)
	}
	if _, err := writer.ExecContext(ctx, "SELECT value FROM `"+second.databaseName+"`.`probe`"); err == nil {
		t.Fatal("writer unexpectedly read another account database")
	}
	originalProvisionRequest := first.provisionRequest()
	// A provisioning retry before any later credential generation is safe and
	// must retain the original principal.
	replayed, err := client.ProvisionDatabase(ctx, "db-provision-"+first.operationID,
		first.correlation(), originalProvisionRequest)
	if err != nil || !replayed.Active {
		t.Fatalf("provision replay = %#v, %v", replayed, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer before password rotation: %v", err)
	}
	oldPassword := first.password
	newPassword := "Sft-rotated-" + uuid.NewString() + "-db"
	rotationOperation := uuid.Must(uuid.NewV7()).String()
	rotationCorrelation := first.correlation()
	rotationCorrelation.OperationID = rotationOperation
	rotationRequest := agentprotocol.DatabasePasswordRotateRequest{
		UserAlias: first.userAlias, Username: first.username,
		Host: databaseidentity.LocalHost, Password: []byte(newPassword),
	}
	rotated, err := client.RotateDatabasePassword(
		ctx, "db-password-rotate-"+rotationOperation, rotationCorrelation, rotationRequest,
	)
	if err != nil || !rotated.Active || !rotated.Changed {
		t.Fatalf("rotate database password = %#v, %v", rotated, err)
	}
	if err := pingMariaDBForIntegration(ctx, first.username, oldPassword, first.databaseName); err == nil {
		t.Fatal("old database password remained usable after rotation")
	}
	first.password = newPassword
	writer = openMariaDBForIntegration(t, first.username, first.password, first.databaseName)
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, "INSERT INTO own_probe (value) VALUES (2)"); err != nil {
		t.Fatalf("rotated database password cannot access the original grant: %v", err)
	}
	// After rotation advances the durable host marker, even a cache-cold retry
	// of the original provisioning operation must be fenced rather than restore
	// the old password.
	_, err = client.ProvisionDatabase(ctx, "db-provision-stale-"+first.operationID,
		first.correlation(), originalProvisionRequest)
	var remoteError *agentclient.RemoteError
	if !errors.As(err, &remoteError) || remoteError.Code != agentprotocol.ErrorDatabaseConflict {
		t.Fatalf("stale provisioning replay error = %v", err)
	}
	if err := pingMariaDBForIntegration(ctx, first.username, first.password, first.databaseName); err != nil {
		t.Fatalf("stale provisioning replay disturbed rotated password: %v", err)
	}

	reader := openMariaDBForIntegration(t, second.username, second.password, second.databaseName)
	defer reader.Close()
	var value int
	if err := reader.QueryRowContext(ctx, "SELECT value FROM probe").Scan(&value); err != nil || value != 7 {
		t.Fatalf("read-only SELECT = %d, %v", value, err)
	}
	if _, err := reader.ExecContext(ctx, "INSERT INTO probe (value) VALUES (8)"); err == nil {
		t.Fatal("read-only database user unexpectedly inserted a row")
	}

	dropDatabaseFixture(t, ctx, client, first)
	first.dropped = true
	var remaining int
	if err := root.QueryRowContext(ctx, `SELECT COUNT(*) FROM mysql.db WHERE Db = ?`, first.databaseName).Scan(&remaining); err != nil {
		t.Fatalf("inspect retained database grants: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("database privileges retained after drop: %d", remaining)
	}
	t.Log("STACKFORT_QUALIFICATION mariadb-tenant-lifecycle=passed password-rotation=passed")
}

type databaseFixture struct {
	accountID, operationID        string
	databaseAlias, databaseName   string
	userAlias, username, password string
	preset                        agentprotocol.DatabaseGrantPreset
	dropped                       bool
}

func newDatabaseFixture(t *testing.T, alias string, preset agentprotocol.DatabaseGrantPreset) *databaseFixture {
	t.Helper()
	accountID := uuid.Must(uuid.NewV7()).String()
	operationID := uuid.Must(uuid.NewV7()).String()
	databaseName, err := databaseidentity.Derive(accountID, alias+"_db")
	if err != nil {
		t.Fatal(err)
	}
	username, err := databaseidentity.Derive(accountID, alias+"_user")
	if err != nil {
		t.Fatal(err)
	}
	return &databaseFixture{
		accountID: accountID, operationID: operationID,
		databaseAlias: alias + "_db", databaseName: databaseName,
		userAlias: alias + "_user", username: username,
		password: "Sft-" + uuid.NewString() + "-db", preset: preset,
	}
}

func (fixture *databaseFixture) correlation() agentprotocol.AuditCorrelation {
	return agentprotocol.AuditCorrelation{
		OperationID: fixture.operationID, ActorKind: agentprotocol.ActorSystem, AccountID: fixture.accountID,
	}
}

func (fixture *databaseFixture) provisionRequest() agentprotocol.DatabaseProvisionRequest {
	return agentprotocol.DatabaseProvisionRequest{
		DatabaseAlias: fixture.databaseAlias, DatabaseName: fixture.databaseName,
		UserAlias: fixture.userAlias, Username: fixture.username, Host: databaseidentity.LocalHost,
		Password: []byte(fixture.password), CreateUser: true, Preset: fixture.preset,
	}
}

func provisionDatabaseFixture(t *testing.T, ctx context.Context, client *agentclient.Client, fixture *databaseFixture) {
	t.Helper()
	response, err := client.ProvisionDatabase(ctx, "db-provision-"+fixture.operationID,
		fixture.correlation(), fixture.provisionRequest())
	if err != nil || !response.Active || !response.Changed {
		t.Fatalf("provision %s = %#v, %v", fixture.databaseAlias, response, err)
	}
}

func dropDatabaseFixture(t *testing.T, ctx context.Context, client *agentclient.Client, fixture *databaseFixture) {
	t.Helper()
	if fixture.dropped {
		return
	}
	databaseOperation := uuid.Must(uuid.NewV7()).String()
	correlation := agentprotocol.AuditCorrelation{
		OperationID: databaseOperation, ActorKind: agentprotocol.ActorSystem, AccountID: fixture.accountID,
	}
	databaseResponse, err := client.DropDatabase(ctx, "db-drop-"+databaseOperation, correlation,
		agentprotocol.DatabaseDropRequest{
			Kind: agentprotocol.DatabaseDropDatabase, Alias: fixture.databaseAlias, Name: fixture.databaseName,
			Grants: []agentprotocol.DatabaseDropGrant{{
				UserAlias: fixture.userAlias, Username: fixture.username,
				Host: databaseidentity.LocalHost, Preset: fixture.preset,
			}},
		})
	if err != nil || !databaseResponse.Deleted {
		t.Fatalf("drop database %s = %#v, %v", fixture.databaseAlias, databaseResponse, err)
	}
	userOperation := uuid.Must(uuid.NewV7()).String()
	correlation.OperationID = userOperation
	userResponse, err := client.DropDatabase(ctx, "db-drop-"+userOperation, correlation,
		agentprotocol.DatabaseDropRequest{
			Kind: agentprotocol.DatabaseDropUser, Alias: fixture.userAlias, Name: fixture.username,
			Host: databaseidentity.LocalHost, Grants: []agentprotocol.DatabaseDropGrant{},
		})
	if err != nil || !userResponse.Deleted {
		t.Fatalf("drop user %s = %#v, %v", fixture.userAlias, userResponse, err)
	}
	fixture.dropped = true
}

func openMariaDBForIntegration(t *testing.T, username, password, databaseName string) *sql.DB {
	t.Helper()
	socket := ""
	for _, candidate := range []string{"/run/mysqld/mysqld.sock", "/var/lib/mysql/mysql.sock"} {
		if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSocket != 0 {
			socket = candidate
			break
		}
	}
	if socket == "" {
		t.Fatal("MariaDB Unix socket is unavailable")
	}
	config := mysql.NewConfig()
	config.User, config.Passwd, config.DBName = username, password, databaseName
	config.Net, config.Addr = "unix", socket
	config.Timeout, config.ReadTimeout, config.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	database := sql.OpenDB(connector)
	if err := database.PingContext(t.Context()); err != nil {
		_ = database.Close()
		t.Fatal(fmt.Errorf("connect to MariaDB as %s: %w", username, err))
	}
	return database
}

func pingMariaDBForIntegration(ctx context.Context, username, password, databaseName string) error {
	socket := ""
	for _, candidate := range []string{"/run/mysqld/mysqld.sock", "/var/lib/mysql/mysql.sock"} {
		if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSocket != 0 {
			socket = candidate
			break
		}
	}
	if socket == "" {
		return fmt.Errorf("MariaDB Unix socket is unavailable")
	}
	config := mysql.NewConfig()
	config.User, config.Passwd, config.DBName = username, password, databaseName
	config.Net, config.Addr = "unix", socket
	config.Timeout, config.ReadTimeout, config.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return err
	}
	database := sql.OpenDB(connector)
	defer database.Close()
	return database.PingContext(ctx)
}
