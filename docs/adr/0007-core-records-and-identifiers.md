# ADR 0007: Version core records and use UUIDv7 identifiers

Status: Accepted

## Context

Stackfort needs durable identities, accounts, package assignments, desired
state, operations, and audit evidence before HTTP authentication or host
reconciliation can be implemented. User-chosen identifiers would leak mutable
names into authorization checks and filesystem paths. Mutable package rows
alone would also make it impossible to explain which limits applied to an
account at an earlier time.

## Decision

Generate canonical lowercase UUIDv7 values for every externally referencable
core record. UUIDv7 follows RFC 9562, provides globally unique opaque values,
and retains useful insertion locality in SQLite. Only canonical UUIDv7 strings
are accepted by the repository and schema checks.

Keep identities separate from hosting accounts. Platform roles attach to an
identity; account roles attach through explicit membership records.

Represent package changes as immutable numbered revisions. An account package
assignment copies the complete resolved limit document and remains immutable
after it is superseded. A hosting account always references one current
assignment through a deferred relational constraint.

Store desired state as immutable, per-account numbered revisions. Store base
operation records durably. ADR 0009 extends those records with the B-004 worker
transition, fencing, retry, event, and recovery model.

Append every repository mutation and security event to an immutable SHA-256
hash chain in the same transaction as the affected records. Reject secret-like
audit detail keys before persistence. External audit-head checkpointing remains
a later operational integration.

## Consequences

- Names, emails, and slugs can change without changing ownership identifiers.
- Account-owned rows cannot reference a missing account.
- Package history can explain the exact effective limits of every assignment.
- Failed audit creation rolls back the associated mutation.
- UUIDv7 reveals approximate creation time; IDs are opaque identifiers, not
  authentication secrets.
- Tail deletion by a privileged database attacker requires an external chain
  checkpoint to detect; the local chain detects record edits and interior
  deletion.

See [Core records](../core-records.md) for the implemented schema boundaries.
