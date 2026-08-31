# ADR 0038: Derive and mark tenant-scoped MariaDB objects

- Status: Accepted
- Date: 2026-08-25

## Context

Stackfort must let account owners create application databases without giving
the web API administrative SQL authority. MariaDB database names are global to
the host, grants can survive `DROP DATABASE`, and a retried control-plane job
must not adopt an object created by another actor. Passwords also cross more
trust boundaries than ordinary desired-state data.

## Decision

1. Install the supported distribution's native MariaDB server and let only the
   privileged host agent connect as the local root principal through a verified
   Unix socket.
2. Derive every physical database and user name as
   `sf_<complete account UUID without hyphens>_<restricted alias>`. The browser
   submits only the alias; the fixed `localhost` host is not selectable.
3. Keep authoritative tenant records in the panel SQLite store with composite
   account foreign keys. Keep a second, minimal ownership marker in the fixed
   `stackfort_control.managed_objects` MariaDB table before adopting or deleting
   an object.
4. Expose only two closed privileged agent operations: `database.provision`
   and `database.drop`. Allow only reviewed `read_only` and `read_write` grant
   presets.
5. Generate credentials in the control process, envelope-encrypt them at rest,
   transport plaintext only in memory over the peer-authenticated Unix socket,
   and convert it to a `mysql_native_password` verifier before constructing the
   fixed SQL statement. Never pass a password in process arguments or SQL text.
6. Reveal a newly generated credential once and only after recent
   authentication. List and operation responses contain no credential field.
7. Couple each lifecycle transition to a durable operation and immutable
   mutation record. Replays reconcile the same marker and object rather than
   allocating another identity.
8. Require an exact visible alias confirmation for deletion. Revoke all
   schema-wide privileges before dropping a database, reject deletion of a user
   with managed grants, and erase its encrypted credential after retirement.

## Consequences

- Accounts cannot select another tenant's SQL namespace, user host, or grant
  text, and a marker mismatch fails closed.
- Physical names are longer but remain within MariaDB's 64-character identifier
  limit by restricting aliases to 28 lowercase ASCII characters.
- MariaDB root authority stays outside the API and browser processes.
- One-time reveal prevents later recovery of the original password. Password
  rotation is a separate future lifecycle rather than a hidden re-reveal.
- A manual database drop does not silently remove MariaDB grant rows, so the
  agent explicitly revokes them before removal.
- Shared MariaDB remains an authorization boundary, not a hard per-tenant CPU,
  memory, or I/O isolation boundary.

