# ADR 0040: Failure-safe managed database password rotation

- Status: accepted and implemented
- Date: 2026-08-26

## Context

Replacing the active encrypted envelope before MariaDB accepts the new
password can make Stackfort forget the only working credential. Allowing
several queued candidates can instead let a delayed retry restore an older
password. Rotation also has to invalidate browser handoffs without placing a
password in the browser, audit log, process arguments, or SQL text.

## Decision

1. Generate the password server-side and persist it in a separate
   envelope-encrypted rotation row bound one-to-one to a durable operation.
2. Permit only one unresolved rotation per account principal.
3. Keep the existing managed-user envelope authoritative until a closed local
   agent operation has successfully changed the exact owned `localhost`
   principal.
4. Treat reapplying the same generated candidate as the restart-safe host
   reconciliation behavior and advance the principal's host marker to the
   rotation operation, fencing all older credential mutations.
5. Promote the envelope, increment a credential generation, reset one-time
   reveal, revoke issued phpMyAdmin handoffs, and record the audit event in one
   SQLite transaction.
6. Disable phpMyAdmin persistent connections. Existing sessions retain only
   the old password and therefore fail MariaDB authentication after rotation.
7. Block principal deletion and newer rotation requests while a rotation is
   unresolved.

## Consequences

- A host failure cannot prematurely discard the previous usable envelope.
- A control-plane failure after MariaDB mutation can safely replay the same
  candidate and then finish promotion.
- Failed or cancelled unresolved rotations require resolution/retry; an
  explicit abandon workflow may be added later, but silently superseding them
  is forbidden.
- Rotating a shared application principal intentionally interrupts clients
  until they receive the new one-time credential.
- Per-phpMyAdmin-session bookkeeping is unnecessary for credential safety:
  non-persistent requests must reauthenticate against MariaDB, while unconsumed
  Stackfort handoffs are revoked explicitly.
