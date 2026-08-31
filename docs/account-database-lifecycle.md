# Account database lifecycle

Status: implemented, accessibility-tested, and VM-qualified on 2026-08-26

J-003 adds the first managed MariaDB lifecycle: native service installation,
tenant-owned records, a four-step account wizard, one-time credential reveal,
fixed least-privilege grants, and explicit deletion. Secure phpMyAdmin signon
is implemented in J-004. J-005's password-rotation path is implemented and
qualified on all three supported guests; database backup/restore remains
subsequent work.

## Names and ownership

The account owner enters a lowercase alias of at most 28 characters. Stackfort
derives the physical database or principal name as:

```text
sf_<32 lowercase hexadecimal account UUID characters>_<alias>
```

The complete account UUID prevents prefix ambiguity while remaining within
MariaDB's 64-character identifier limit. The browser and API read models see
the friendly alias, record ID, status, timestamps, and grant relationships—not
the physical name. Database principals are fixed to `localhost`.

SQLite migration 016 enforces the derived name, UUIDv7 record shape, live alias
uniqueness, composite tenant ownership, closed statuses, and two grant presets:

- `read_only`: `SELECT` and `SHOW VIEW`;
- `read_write`: reviewed application DDL/DML privileges, without global,
  administrative, grant, file, routine-admin, or replication authority.

Each provisioning or deletion request creates one retained mutation bound to
one durable account operation. Reusing an idempotency key with the same input
returns the same records; different input is rejected.

## Four-step wizard

`POST /api/v1/accounts/{accountID}/databases/wizard` requires an active owner
session, CSRF proof, `account.resources.manage`, a host-ready account, a bounded
request ID, and a required idempotency key. The English/German UI guides the
owner through:

1. choosing the database alias;
2. selecting an existing database user or creating a new alias;
3. selecting read-only or read-write access; and
4. reviewing and starting the durable operation.

Package database and database-user limits are checked in the same immediate
SQLite transaction that creates the records, grant, mutation, operation, and
audit event. The worker later calls the typed agent operation and atomically
marks the logical objects active only after host reconciliation succeeds.

## Credential boundary

A new password is generated from 24 cryptographically random bytes. Its only
persistent form is envelope-encrypted with a new data key wrapped by the host
master key. The worker decrypts it only while the new principal is pending and
clears the byte slice after the agent call.

The agent accepts the password only as a bounded in-memory byte value over its
peer-verified Unix socket. It derives the MariaDB password verifier locally;
plaintext is absent from SQL text, process arguments, responses, structured
logs, audit details, and list models.

After successful provisioning, an owner with recent authentication may call
the one-time reveal endpoint. The reveal and its audit event are committed
together before the response is returned. Later attempts fail closed. The UI
keeps the credential only in page memory and warns that it cannot be shown
again.

## Host reconciliation

`database.provision`, `database.password.rotate`, and `database.drop` are the
fourteenth through sixteenth
version-1 agent operations. The agent opens only a verified non-symlink MariaDB
Unix socket and relies on the local root `unix_socket` authentication boundary.
Before use it verifies the exact private control-schema table shape, storage
engine, and ASCII binary collation.

The fixed `stackfort_control.managed_objects` table binds object kind, physical
name, account ID, logical record ID, and operation ID. An exact existing marker
makes a replay safe; an unmarked object or mismatched marker is a collision and
is never adopted. Provisioning creates the schema, localhost user, and fixed
grant, then records success without returning credential material.

## Explicit deletion

Database and database-user deletion require the destructive account permission,
recent authentication, CSRF proof, a required idempotency key, and a request
body whose confirmation exactly matches the visible alias. The UI states that
an automatic pre-deletion backup is not yet available.

The agent refuses to remove an unmarked or differently owned object. Database
deletion revokes all schema-wide grants before `DROP DATABASE`; MariaDB does not
perform that grant cleanup automatically. Database-user deletion is rejected
while any managed grant remains. Successful principal deletion zeroes its
encrypted credential fields and retains only the tombstoned control record and
audit history.

## Verification

Unit and integration tests cover name derivation, limits, atomic wizard replay,
credential omission and one-time reveal, cross-account denial, exact deletion
confirmation, immutable mutations, agent marker collisions, protocol bounds,
and HTTP CSRF behavior. The same release archive is installed twice and the
full destructive suite runs on clean Debian 13, Ubuntu 26.04, and Rocky Linux
10 guests. It creates real read/write and read-only principals for different
accounts, proves permitted own access and denied write/cross-account access,
replays the agent mutation, deletes both objects, and verifies no grant row is
left behind.

See [ADR 0038](adr/0038-tenant-scoped-mariadb-lifecycle.md), the
[local agent protocol](local-agent-protocol.md), and the
[MariaDB Hyper-V result](../infra/host-tests/results/2026-08-25-mariadb-hyper-v.md).
The separate [phpMyAdmin signon design](phpmyadmin-signon.md) documents how a
selected managed principal is exchanged without returning its password to the
browser. See [Managed database password rotation](database-password-rotation.md)
for J-005's separate-envelope promotion and revocation rules.
