# SQL boundary and compatibility review — 2026-09-12

Scope: SQL-construction alerts #1–7 and the MariaDB password-verifier alert #28
identified in the repository's CodeQL UI. This is an internal source review,
not an independent security audit, a remote alert dismissal, or candidate
publication approval. Green scanner jobs alone do not resolve open alerts.

## SQL construction

The reviewed sinks are `CreateDatabase`, `CreateUser`, `AlterUserPassword`,
`Grant`, `Revoke`, `DropDatabase` and `DropUser` in
[`internal/hostdatabase/reconciler.go`](../../../internal/hostdatabase/reconciler.go).
No SQL-injection path was demonstrated through their production entry points:

- `Reconcile` checks canonical account/operation identities, exact derived
  database and user names, fixed `localhost`, bounded credentials and a closed
  grant preset before opening the SQL backend.
- `RotatePassword` and `Drop` run complete agent-protocol validation first.
  [`databaseidentity.ValidateDerived`](../../../internal/databaseidentity/identity.go)
  requires the full canonical UUIDv7 prefix and restricted lowercase ASCII alias;
  quotes, backticks, whitespace, `%` and backslashes cannot enter identifiers or
  principals. SQL helpers are private implementation details, not generic SQL APIs.
- Privilege lists are fixed strings. Control-state queries and grant-table
  lookups use bound values. Password SQL receives only the locally generated
  fixed-shape hexadecimal verifier, never plaintext passwords.
- [`sql_boundary_test.go`](../../../internal/hostdatabase/sql_boundary_test.go)
  verifies malicious names, aliases, principals, hosts and presets are rejected
  before any backend is opened. Quoting helpers alone are not general-purpose
  SQL escaping and must not be reused without these entry-point invariants.

## Real adjacent grant-scope defect

Backticks do **not** make database-level GRANT wildcards literal. Previously,
`literal_db` also authorized a matching sibling such as `literalxdb` in the
same account. This is an authorization defect, even though the SQL-construction
inputs do not permit syntax injection. MariaDB documents
[database grant wildcard matching](https://mariadb.com/docs/server/reference/sql-statements/account-management-sql-statements/grant#database-name-wildcard-matching-order).

The fix uses the same escaped literal pattern for GRANT, `mysql.db.Db` lookup
and REVOKE; CREATE/DROP DATABASE retain the original identifier. The escaped
pattern must fit the [64-character grant-table field](https://mariadb.com/docs/server/reference/system-tables/the-mysql-database-tables/mysql-db-table).
The account prefix consumes 38 bytes after escaping, so aliases have a
26-character budget with underscores counted twice. This prevents the
truncation/rejection covered by MariaDB's
[MDEV-39047 regression](https://github.com/MariaDB/server/blob/12.3/mysql-test/main/grant.test).

The existing physical-name derivation is unchanged. Older internal installations
with unescaped grants or over-budget aliases are not migrated by this fix and
are not qualified upgrade sources for the fresh-only experimental native beta.

### Established-session limitation

MariaDB can retain effective privileges in an already established connection.
Stackfort currently does not terminate those sessions during grant revocation,
user deletion or password rotation. In particular, MariaDB 11.8 documents that
[`DROP USER` does not close existing connections](https://mariadb.com/docs/server/reference/sql-statements/account-management-sql-statements/drop-user).
Grant-row removal and denial on a fresh connection therefore do not demonstrate
immediate invalidation of every previously authenticated session. Any future
forced-session revocation needs separate implementation and qualification.

The parent agent's real MariaDB 11.8.6 run reproduced unauthorized same-account
`literal_db` to `literalxdb` access with the superseded `6a74497` implementation.
The first fixed-code run denied sibling access, stored the escaped pattern and
removed the grant row before user deletion, but its subsequent reused-session
assertion failed after database recreation. That failed run is retained; it is
not a complete passing test. The corrected regression closes the old connection
and tests authorization from a new connection, matching the stated guarantee.

## Password-verifier compatibility exception

`nativePasswordHash` deliberately implements MariaDB's `mysql_native_password`
verifier; this is not the panel's human-password hashing scheme. The control
plane generates 24 bytes of random credential material, and principals are
restricted to the local socket. These controls do not make the legacy algorithm
a modern password scheme or eliminate verifier/protocol compromise risks.
MariaDB itself [discourages this plugin for high-password-security installations](https://mariadb.com/docs/server/reference/plugins/authentication-plugins/authentication-plugin-mysql_native_password).
The existing compatibility decision is retained, not silently replaced merely
to clear a scanner alert. A future authentication-plugin change requires PHP,
phpMyAdmin, client-library and credential-migration qualification.

## Verification status

Source review and focused Windows Go tests for `databaseidentity`, `hostdatabase`,
`core`, `operations` and `httpapi` passed, as did the 18 account UI tests, Vue
type checking and translation check. The Linux amd64 integration binary also
cross-compiled successfully.

The parent agent then executed the actual regression on Debian 13 with
`11.8.6-MariaDB-0+deb13u1` and kernel `6.12.107+deb13-cloud-amd64`, on the
preserved disposable VM (DMI UUID `6365bd88-5141-4f15-b3f8-2ba9996baad2`).
The retained logs were read and SHA-256 checked for this report:

| Run | Observed result |
| --- | --- |
| Pre-fix `6a74497` SQL implementation with the new regression | Failed as expected: same-account wildcard sibling SELECT succeeded instead of returning an access-denied error |
| First fixed-code regression | Lifecycle/rotation passed; later reused-session denial assertion failed, retained as evidence of the established-session limitation |
| Fixed v2 | All three tests below passed, with no skips; the new-session authorization assertion replaces the incorrect immediate-session-invalidation assumption |

- `TestInstalledAgentMariaDBLiteralDatabaseGrants`: same-account wildcard sibling
  access denial, exact stored pattern, revocation while the user still exists,
  and fresh-connection denial after recreating the database.
- [`TestInstalledAgentMariaDBMaximumLiteralGrantPattern`](../../../tests/integration/host_database_boundary_linux_test.go):
  exact 64-byte stored grant, successful own-database DDL/DML and revocation
  before principal deletion.
- `TestInstalledAgentMariaDBTenantLifecycle`: normal read/write and read-only
  access, cross-account denial, replay fencing, password rotation and cleanup.

The fixed-v2 binary was built from archived `6a74497` source with the scoped
current SQL/identity changes and two integration-test overlays, deliberately
excluding concurrent OCI changes. It is **not a final candidate archive or
tagged-release qualification**. The parent restored pre-MariaDB checkpoint
`bda31db6-4150-44b8-821c-1136b89bd2cd` after the test; checkpoint restoration is
lab cleanup, not the separately required full-OS reprovision removal test.

### Retained evidence digests

These named files are in ignored `infra/host-tests/work/`; they are local test
evidence, not public release assets. Keep these exact runs without overwriting
them; use a new filename for any later execution.

| File | SHA-256 |
| --- | --- |
| `mariadb-literal-grant-before.test` | `e8158f870c1858e1522e3568b657e252c2850a98400a4dc43ec4b4db81f82b9e` |
| `mariadb-literal-grant-before.test.log` | `2379f1dc6ee6109648bfc14287c4e885747af25ee0aa9157b3fbd225137d019c` |
| `mariadb-literal-grant-fixed.test.log` | `28d3b72a2fff6e79c07dbabee1f747fc6eafc834f866d71f5bcd7ab8441832da` |
| `mariadb-literal-grant-fixed-v2.test` | `2d7dcbc9545f7421658fe7979eb365a22770c8c673dc4d41262b7bb3e39ab9d9` |
| `mariadb-literal-grant-fixed-v2.test.log` | `4f69eaaa5cabf331c5c23bc89ee91714839b58c51ce712b74fce55256aec5c41` |
| `mariadb-literal-grant-host.txt` | `801eaca5b1fd90675ba6d6d7654eaecdd4eea9de247fe7675fe862693b670caa` |

These tests and any subsequent real results must be distinguished from old
rehearsal builds and bound separately to the final candidate before publication.
