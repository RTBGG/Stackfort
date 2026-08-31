# Hosting-account Unix identity

E-001 assigns one immutable local Linux identity to every hosting account. The
identity is control-plane state, not a transformation of an owner's email,
display name, or mutable account slug.

## Allocation

Migration 008 adds `hosting_account_unix_identities` with a restrictive
one-to-one account foreign key. Account creation allocates all of the following
in the same immediate transaction as the account, owner membership, package
snapshot, and audit event:

| Field | Rule |
| --- | --- |
| Username | `sf_` plus the final 26 hexadecimal digits of the canonical account UUIDv7 |
| UID/GID | Equal values, allocated monotonically from the reserved `200000–599999` range |
| Home | `/srv/hosting/accounts/<canonical-account-uuid>` |
| Initial state | `allocated` |

The username retains 104 UUID bits, including UUIDv7's complete random portion,
and is also unique in SQLite. Numeric
IDs are never reclaimed because deleted identities remain as tombstones. An
upgrade backfills existing pre-E-001 accounts deterministically.

The installer must reserve the numeric range and detect pre-existing host use.
If a local name or numeric ID is already occupied, reconciliation fails with
`identity_conflict`; Stackfort does not silently select a different identity,
rename the other account, or adopt it.

## Reconciliation

The privileged operation `hosting.identity.reconcile` accepts only the complete
persisted identity and requires a UUIDv7 operation/actor/account correlation.
The protocol recomputes the expected username and home from the account ID and
requires the correlation account to match.

The agent reads bounded local `/etc/passwd` and `/etc/group` snapshots and
checks both name and numeric-ID indexes before changing anything:

- a missing exact group is created with fixed `groupadd -g` arguments;
- a missing exact user is created with fixed UID, primary GID, home, and
  `/usr/sbin/nologin`, without creating a home or user-private group;
- an identity whose name, UID, and GID are exact may have only its managed home,
  primary group, locked-password state, and no-login shell repaired;
- any name/UID/GID collision with another local identity fails before mutation.

Every command runs through a fixed no-shell profile with a ten-second deadline,
bounded output, a sanitized environment, and process-group cleanup. The agent
reloads the account database after each command and verifies the postcondition.

The managed directory walk uses directory descriptors plus `O_NOFOLLOW`. The
`/srv/hosting/accounts` ancestors must be root-owned and not group/world
writable. The exact account root is created or repaired to UID:GID and mode
`0750`; reconciliation does not recursively adopt descendant files. E-002
defines the descendant layout, safe document-root traversal, and project quota
behavior in [Hosting filesystem layout and project quota](hosting-filesystem-layout-and-quota.md).

## Archive and deletion

Removal is a durable, forward-only state machine:

```text
allocated | reconciled
  -> archive_requested
  -> archived (non-empty archive reference required)
  -> deletion_requested
  -> deleted (retained tombstone)
```

Requesting archive first changes the hosting account to `archived`, disabling
normal account mutation. Archive confirmation records the durable artifact or
manifest reference; it does not claim that arbitrary files were copied by this
identity component. A future backup/archive worker must produce that artifact
before calling confirmation.

Only the `deletion_requested` operation may dispatch
`hosting.identity.delete`. The agent independently requires the managed account
directory to be absent, rejects altered or conflicting identities, then invokes
plain `userdel <managed-name>` followed by `groupdel <managed-name>`. It never
passes `userdel -r` or `-f`, so identity deletion neither recursively removes
the home nor forces removal around running processes. Non-zero tool outcomes
fail closed for explicit remediation and retry.

Every lifecycle transition shares a transaction with its hash-chained audit
event. SQLite triggers reject identity-key changes, invalid state transitions,
and physical deletion of identity tombstones.

## Verification boundary

Automated tests cover deterministic allocation, sequential numeric IDs,
immutable database fields, every required lifecycle stage, retained tombstones,
typed correlation, RPC idempotent replay, stable redacted failures, command
templates, local account parsing, create/repair convergence, collision
rejection before mutation, and the archive prerequisite for deletion.

Real `shadow-utils` mutation and ownership behavior must additionally run on
the disposable Debian 13, Ubuntu 26.04, and Rocky Linux 10 hosts from A-004
before the production gate is cleared.

References:

- [`useradd(8)`](https://man7.org/linux/man-pages/man8/useradd.8.html)
- [`usermod(8)`](https://man7.org/linux/man-pages/man8/usermod.8.html)
- [`userdel(8)`](https://man7.org/linux/man-pages/man8/userdel.8.html)
- [`groupadd(8)`](https://man7.org/linux/man-pages/man8/groupadd.8.html)
- [`groupdel(8)`](https://man7.org/linux/man-pages/man8/groupdel.8.html)
