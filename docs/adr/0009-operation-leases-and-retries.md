# ADR 0009: Fenced operation leases and explicit retry safety

Status: Accepted

## Context

Stackfort operations cross database, filesystem, service-manager, web-server,
certificate, and future agent boundaries. A control API restart must not lose a
job, while two workers must not complete the same logical job concurrently.
Holding a SQLite transaction for the duration of host work would block state
writes and still would not make external side effects transactional. Blindly
replaying every interrupted command is unsafe.

## Decision

1. Persist one logical operation and a new immutable attempt for every claim.
2. Fence all worker mutations by operation ID, attempt ID, worker-instance ID,
   and an expiring renewable lease.
3. Recover expired attempts before claims and at process startup.
4. Permit automatic retry only for operations explicitly classified `safe` and
   still below their attempt bound. Require an audited action for `manual`; never
   retry `none`.
5. Return an existing live or terminal operation for identical scoped
   idempotency input, and reject key reuse with different semantic input.
6. Model cancellation as a request followed by handler acknowledgement at a
   safe boundary, except pending work which cancels immediately.
7. Store append-only structured progress events and stable error codes. Do not
   persist raw handler errors, panic text, command output, or secret-bearing
   payload fields.
8. On service shutdown, leave in-flight work leased. Recovery after expiry uses
   persisted policy instead of guessing whether an external effect completed.

## Consequences

- SQLite transactions remain short while claim ownership is unambiguous.
- An expired worker is prevented from writing state, but external agent methods
  must independently honor the same idempotency/fencing context; leases alone
  cannot undo an already-issued host effect.
- Safe handlers require deliberate stage design and idempotent reconciliation.
- Interrupted `manual` and `none` operations may need administrator review, which
  is slower but avoids unsafe duplicate destructive work.
- Logical operations retain one user-visible identity across retries and expose
  complete attempt/event history.
- Cooperative cancellation cannot stop a handler that ignores its context;
  handler reviews and timeout-bounded agent methods remain required.
