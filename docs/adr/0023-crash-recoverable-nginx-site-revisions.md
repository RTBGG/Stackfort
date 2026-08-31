# ADR 0023: Crash-recoverable NGINX site revisions

Status: accepted

## Context

Filesystem writes, an NGINX configuration test, a live reload, a health probe,
and SQLite applied-state recording cannot share one atomic transaction. A
process or machine can stop between any two of them. Replacing individual live
account files also exposes mixed revisions and gives a retry no durable way to
decide whether the new configuration became visible.

## Decision

1. Make the durable control-plane operation UUID the host revision UUID and
   persist the complete typed domain snapshot in the operation payload.
2. Render independently inside the agent. Do not accept source text, a path,
   executable arguments, or a service name over RPC.
3. Stage one complete global sites revision and a private complete main
   configuration. Require the fixed vendor `nginx -t` profile before promotion.
4. Expose exactly one revision through an atomically renamed, relative symlink;
   keep the stable active include file independent of account changes.
5. Serialize activation with a root-owned non-blocking `flock` and persist a
   strict, atomically replaced, directory-synced phase journal before and after
   externally visible boundaries.
6. After promotion, gracefully reload through the fixed systemd profile, verify
   service state, and issue a local Host-routed HTTP probe. On failure, restore,
   validate, and reload the previous pointer before cleanup.
7. On restart, recover the journal before accepting new work. Prefer a known
   previous valid revision when commit completion is uncertain; the retried
   operation may then safely activate the same candidate again.
8. Make applied-state recording idempotent and unique by account and operation.
   Exact replay returns the active row; divergent or superseded replay is a
   conflict.

## Consequences

- Invalid candidates never replace the live pointer, and readers observe one
  complete old or new site revision rather than a mixture.
- An agent restart does not depend on its process-local RPC response cache. An
  API restart does not depend on rereading mutable product rows.
- The live reload and database write are still not one transaction; durable
  reconciliation and exact idempotence make the sequence convergent.
- The local probe establishes that NGINX selected a generated host instead of
  the rejecting default. External DNS, TLS, and public-path checks belong to
  later domain/TLS lifecycle work.
- Successful immutable revisions consume storage until a separately specified
  bounded retention policy is implemented.

## Rejected alternatives

- Replacing individual files in `sites-enabled` can expose a mixed global state
  and cannot be rolled back with one atomic operation.
- Treating a successful `nginx -t` as activation success ignores reload and
  runtime-routing failures.
- Always continuing the candidate after a crash requires guessing whether
  NGINX loaded it; restoring the known prior revision and replaying is simpler
  and deterministic.
- Using only the process-local RPC idempotency cache loses evidence on agent
  restart.
- Storing only account/domain IDs in the operation would make retry semantics
  depend on later mutable database state.

