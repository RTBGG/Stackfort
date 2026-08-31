# ADR 0034: Provision account host boundaries through one durable snapshot

Status: Accepted

## Context

Account creation transactionally persisted Linux identity, filesystem quota,
and resource intent, but each host primitive previously required an independent
caller. A browser-created account could therefore exist in SQLite without a
corresponding usable Linux boundary, and domain work had no single readiness
condition.

## Decision

After account persistence, queue one retry-safe `hosting.account.reconcile`
operation containing the exact identity plus filesystem and resource revisions.
Apply identity, filesystem, and resource control in that order and confirm the
captured revisions only after every host call succeeds.

Repair the narrow transaction-to-queue crash gap with a periodic scan and a
deterministic account/revision idempotency key. Define host readiness as an
active account with reconciled identity and available applied filesystem and
resource state. Refuse domain mutations until that predicate is true.

## Consequences

- Browser account creation starts real host provisioning automatically.
- Worker restarts and duplicate scans converge on the same operation.
- A superseded package/resource revision cannot be confirmed accidentally.
- The API exposes a non-sensitive readiness boolean for honest UI gating.
- Account creation and operation insertion are not one database transaction;
  the repair scan is therefore a required control-plane component.
