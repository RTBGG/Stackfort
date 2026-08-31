# Panel state persistence

Stackfort keeps control-plane state in a private SQLite database that is
independent from the MariaDB service used by hosted applications.

## Runtime configuration

The control API uses `/var/lib/stackfort/stackfort.db` on Linux. Developers may
set `STACKFORT_STATE_PATH` to another absolute local path; relative paths and
Windows network paths are rejected. The containing directory must not be a
symbolic link or permit group writes or access by other users. Newly created
directories use mode `0750` and the database uses `0600`.

The implementation pins the CGo-free `modernc.org/sqlite` driver at `v1.56.0`.
It embeds SQLite 3.53.3 and supports the project's Linux amd64 and arm64 release
targets without a C toolchain. Startup enforces SQLite 3.51.3 or newer because
that is the first mainline release containing the WAL-reset race fix.

Every physical connection receives these settings:

| Setting | Value | Reason |
| --- | --- | --- |
| `foreign_keys` | `ON` | Enforce relational ownership and lifecycle rules. |
| `busy_timeout` | 5 seconds | Bound ordinary lock contention. |
| `synchronous` | `FULL` | Preserve committed writes across power loss as far as the host stack permits. |
| `trusted_schema` | `OFF` | Do not trust schema objects to invoke unsafe application-defined behavior. |

WAL mode is enabled and verified before migrations. The pool is bounded to four
connections. Application writes pass through one serialized `BEGIN IMMEDIATE`
transaction path, while short reads may use the remaining connections.

References:

- [SQLite write-ahead logging](https://sqlite.org/wal.html)
- [modernc.org/sqlite package](https://pkg.go.dev/modernc.org/sqlite)
- [SQLite PRAGMA reference](https://sqlite.org/pragma.html)

## Migrations

Migration files live in `internal/store/migrations` and use contiguous names
such as `001_initial.sql`. They are embedded in the API binary and applied in
individual immediate transactions.

Migration 002 adds the core records described in
[Core records](core-records.md), including relational account ownership,
immutable package snapshots, desired-state revisions, operations, and chained
audit events.

Migration 005 adds immutable bootstrap-capability history and persistent
direct-source/global attempt buckets. It stores only capability digests; see
[Secure administrator bootstrap](administrator-bootstrap.md).

Migration 006 adds persistent login pressure buckets for global, direct-source,
and SHA-256 identity keys. Raw email addresses and passwords are not stored in
these records; see
[Password authentication and browser sessions](password-authentication-and-sessions.md).

Migration 007 adds session authentication-level metadata, encrypted TOTP setup
and active-factor records, hash-only recovery and MFA challenge records, and
persistent MFA attempt limits. Its TOTP ciphertext depends on the external host
master key described in
[TOTP, recovery codes, and session management](totp-recovery-and-session-management.md).

Migration 016 adds tenant-owned managed database, database-user, grant, and
durable mutation records. Physical names are constrained to the exact account
UUID prefix, ownership foreign keys include `account_id`, credentials use
envelope-encrypted blobs, and applied mutations cannot be changed or removed.
See [Account database lifecycle](account-database-lifecycle.md).

The `schema_migrations` table records each version, stable name, normalized
SHA-256 checksum, and UTC application time. Line endings are normalized before
hashing so Windows and Linux builds agree. Startup refuses:

- an applied version newer than the binary;
- a missing or non-contiguous migration;
- a changed name or checksum for an applied migration; and
- a non-empty database without Stackfort migration history.

A failed migration is rolled back together with its history row. Applied
migration files are immutable; corrections require a new migration.

## Backups and recovery boundary

The store backup primitive uses `VACUUM INTO` to produce a consistent, compact
snapshot of a live database. It writes a mode-`0600` temporary file in the
destination directory, then verifies SQLite `quick_check`, foreign keys, and the
migration chain. A same-filesystem hard link publishes the verified snapshot
atomically and refuses to overwrite an existing destination.

Do not copy only the live `.db` file: a WAL database's `-wal` file can contain
committed state. Use the backup primitive. Automated tests reopen the produced
snapshot and verify persisted content. A privileged, staged service-level
restore workflow and its public API will be added with the backup manager; the
foundation deliberately does not replace a database while it is open.

Backups containing encrypted TOTP records must be paired with a protected copy
of the host `master.key`. Do not place that key inside the SQLite snapshot or a
public release artifact. Restore procedures must verify the key before making
the restored service reachable.

Reference: [SQLite `VACUUM INTO`](https://sqlite.org/lang_vacuum.html#vacuuminto).
