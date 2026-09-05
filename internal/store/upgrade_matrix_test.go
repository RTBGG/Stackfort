// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// Every historical schema prefix is exercised, even before public releases
// exist. This supplements (and does not replace) the binary/host release matrix.
func TestUpgradeFromEveryHistoricalSchema(t *testing.T) {
	for version := 1; version <= len(embeddedMigrations); version++ {
		t.Run(fmt.Sprintf("schema-%03d", version), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			root := secureTempDir(t)
			path := filepath.Join(root, "stackfort.db")
			clean, err := prepareDatabaseFile(path)
			if err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", dataSourceName(clean, false))
			if err != nil {
				t.Fatal(err)
			}
			old := &Store{db: db, path: clean}
			t.Cleanup(func() { _ = old.Close() })
			if err := old.migrate(ctx, embeddedMigrations[:version]); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO stackfort_metadata(key,value,updated_at) VALUES ('upgrade-preservation','Grüße / 日本語 / <tenant>','2026-01-01T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
			if version >= 2 {
				if _, err := db.ExecContext(ctx, `INSERT INTO identities(id,email,normalized_email,display_name,locale,status,created_at,updated_at)
				VALUES ('01900000-0000-7000-8000-000000000001','upgrade@example.test','upgrade@example.test','Upgrade owner','de','active','2026-01-01','2026-01-01');
				INSERT INTO password_credentials(identity_id,algorithm,password_hash,salt,memory_kib,iterations,parallelism,version,must_rotate,created_at,updated_at)
				VALUES ('01900000-0000-7000-8000-000000000001','argon2id',zeroblob(32),zeroblob(16),8192,1,1,1,0,'2026-01-01','2026-01-01');
				INSERT INTO platform_role_assignments(identity_id,role,granted_at) VALUES ('01900000-0000-7000-8000-000000000001','platform_admin','2026-01-01');`); err != nil {
					t.Fatal(err)
				}
			}
			before, err := readAppliedMigrations(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(root, "before.sqlite")
			if err := old.Backup(ctx, backup); err != nil {
				t.Fatal(err)
			}
			if err := old.Close(); err != nil {
				t.Fatal(err)
			}
			current, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer current.Close()
			var value string
			if err := current.db.QueryRowContext(ctx, `SELECT value FROM stackfort_metadata WHERE key='upgrade-preservation'`).Scan(&value); err != nil || value != "Grüße / 日本語 / <tenant>" {
				t.Fatalf("value=%q error=%v", value, err)
			}
			if version >= 2 {
				var count int
				if err := current.db.QueryRowContext(ctx, `SELECT count(*) FROM identities i JOIN password_credentials p ON p.identity_id=i.id JOIN platform_role_assignments r ON r.identity_id=i.id WHERE i.locale='de' AND p.password_hash=zeroblob(32) AND p.salt=zeroblob(16) AND r.role='platform_admin'`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("identity/credential/role preservation count=%d error=%v", count, err)
				}
			}
			after, err := readAppliedMigrations(ctx, current.db)
			if err != nil || len(after) != len(embeddedMigrations) {
				t.Fatalf("history=%d error=%v", len(after), err)
			}
			for index, item := range before {
				if after[index] != item {
					t.Fatal("historical migration was rewritten")
				}
			}
			if err := verifyBackup(ctx, path, embeddedMigrations); err != nil {
				t.Fatal(err)
			}
			// The old binary's migration list must still accept the exact backup.
			if err := verifyBackup(ctx, backup, embeddedMigrations[:version]); err != nil {
				t.Fatal(err)
			}
			if err := current.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
