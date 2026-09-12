// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration && linux

package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/databaseidentity"
	"github.com/google/uuid"
)

func TestInstalledAgentMariaDBMaximumLiteralGrantPattern(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	client := startDisposableAgentRPC(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fixture := newDatabaseFixture(t, "grantmax", agentprotocol.DatabaseGrantReadWrite)
	fixture.databaseAlias = strings.Repeat("a", 26)
	var err error
	fixture.databaseName, err = databaseidentity.Derive(fixture.accountID, fixture.databaseAlias)
	if err != nil {
		t.Fatal(err)
	}
	pattern := strings.ReplaceAll(fixture.databaseName, "_", `\_`)
	if len(fixture.databaseName) != 62 || len(pattern) != 64 {
		t.Fatal("fixture does not exercise the exact mysql.db.Db boundary")
	}
	provisionDatabaseFixture(t, ctx, client, fixture)
	defer dropDatabaseFixture(t, context.WithoutCancel(ctx), client, fixture)
	root := openMariaDBForIntegration(t, "root", "", "")
	defer root.Close()
	var storedPattern string
	var storedBytes int
	if err := root.QueryRowContext(ctx,
		`SELECT Db, OCTET_LENGTH(Db) FROM mysql.db WHERE User = ? AND Host = 'localhost'`,
		fixture.username).Scan(&storedPattern, &storedBytes); err != nil {
		t.Fatalf("read maximum-size grant: %v", err)
	}
	if storedPattern != pattern || storedBytes != 64 {
		t.Fatalf("maximum-size grant was changed or truncated: bytes=%d pattern=%q", storedBytes, storedPattern)
	}
	writer := openMariaDBForIntegration(t, fixture.username, fixture.password, fixture.databaseName)
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, "CREATE TABLE boundary_probe (value INT NOT NULL)"); err != nil {
		t.Fatalf("maximum-size grant cannot create its own table: %v", err)
	}
	if _, err := writer.ExecContext(ctx, "INSERT INTO boundary_probe VALUES (64)"); err != nil {
		t.Fatalf("maximum-size grant cannot insert its own row: %v", err)
	}
	var value int
	if err := writer.QueryRowContext(ctx, "SELECT value FROM boundary_probe").Scan(&value); err != nil || value != 64 {
		t.Fatalf("maximum-size grant cannot read its own row: value=%d error=%v", value, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	// Retain the principal while checking revocation. DROP USER would otherwise
	// erase its grant rows and could hide an incorrect escaped-pattern lookup.
	operation := uuid.Must(uuid.NewV7()).String()
	correlation := fixture.correlation()
	correlation.OperationID = operation
	response, err := client.DropDatabase(ctx, "db-boundary-drop-"+operation, correlation,
		agentprotocol.DatabaseDropRequest{
			Kind: agentprotocol.DatabaseDropDatabase, Alias: fixture.databaseAlias, Name: fixture.databaseName,
			Grants: []agentprotocol.DatabaseDropGrant{{
				UserAlias: fixture.userAlias, Username: fixture.username,
				Host: databaseidentity.LocalHost, Preset: fixture.preset,
			}},
		})
	if err != nil || !response.Deleted {
		t.Fatalf("revoke maximum-size grant: %#v, %v", response, err)
	}
	var remaining int
	if err := root.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mysql.db WHERE User = ? AND Host = 'localhost'`,
		fixture.username).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("maximum-size grant survives revocation: count=%d error=%v", remaining, err)
	}
	if err := pingMariaDBForIntegration(ctx, fixture.username, fixture.password, ""); err != nil {
		t.Fatalf("principal disappeared before its revocation was checked: %v", err)
	}
	t.Log("STACKFORT_QUALIFICATION mariadb-literal-grant-64-byte-boundary=passed revoke-before-user-drop=passed")
}
