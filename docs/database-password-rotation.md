# Managed database password rotation

Status: implemented and VM-qualified on 2026-08-26

J-005 rotates one active managed MariaDB principal through the existing durable
`database.lifecycle` worker. The browser never chooses or receives the
candidate password as part of the rotation request. After completion, the new
credential becomes available through the existing recent-authenticated
single-use reveal action.

## Failure-safe state transition

Migration 018 stores each candidate in a separate envelope-encrypted rotation
record. The active envelope in `managed_database_users` remains authoritative
while the operation is pending. Only one unresolved rotation may exist for a
principal, so an older retry cannot overwrite a newer password.

The worker decrypts the candidate only in caller-owned memory and sends this
closed intent over the peer-authenticated local agent socket:

- account-derived user alias and physical name;
- the fixed `localhost` host; and
- a 20–256 byte generated candidate password.

The agent requires an active ownership marker for the exact account and an
existing local principal. It then executes the parameterized password change
and advances that marker to the rotation operation. Replaying the same
operation applies the same candidate and is semantically idempotent; a delayed
older provisioning operation is fenced and cannot restore an obsolete
password.

After the host reports success, one SQLite transaction:

1. copies the candidate envelope into the managed user;
2. advances its monotonic credential generation;
3. resets the single-use reveal marker;
4. revokes all still-issued phpMyAdmin handoffs for the user;
5. marks the rotation applied; and
6. appends a secret-free audit event.

If the agent fails before success, none of those steps occur and the old
control-plane envelope remains active. A failed unresolved operation must be
retried rather than bypassed with a newer rotation. User deletion is blocked
until it is resolved.

## Browser and phpMyAdmin behavior

`POST /api/v1/accounts/{accountID}/database-users/{userID}/credential/rotate`
requires a valid browser session, same-origin CSRF proof, recent
authentication, `account.credentials.manage`, host readiness, and an
idempotency key. The English/German account UI warns that applications using
the old password will lose access and queues the operation only after explicit
confirmation.

The MariaDB change invalidates credentials held by existing phpMyAdmin
sessions. Stackfort also configures phpMyAdmin with persistent connections
disabled, so a later request must authenticate again rather than retaining a
server connection established with the old password. Any handoff that was
issued but not consumed is explicitly revoked during promotion. A new handoff
can only decrypt the promoted envelope.

## Verification

Local tests cover separate-envelope staging, idempotent preparation and
completion, overlap rejection, exact promotion, generation advancement,
single-use reset, handoff revocation, deletion fencing, protocol bounds,
same-account host ownership, repeated host application, worker ordering, HTTP
CSRF/idempotency, stale-provisioning fencing, bilingual UI wiring, and
production web compilation.

The same final release archive changes a real MariaDB principal on Debian 13,
Ubuntu 26.04, and Rocky Linux 10. Every guest rejects the old password, accepts
the new password with the original grant, rejects a cache-cold stale
provisioning operation, and confirms the rejected replay did not disturb the
new generation. See the
[three-guest result](../infra/host-tests/results/2026-08-26-database-password-rotation-hyper-v.md).

See [ADR 0040](adr/0040-failure-safe-database-password-rotation.md), the
[account database lifecycle](account-database-lifecycle.md), and the
[local agent protocol](local-agent-protocol.md).
