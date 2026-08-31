# Persisted operations

Migration `004_persisted_operations.sql`, the `internal/core` operation
repository, and `internal/operations.Runner` implement B-004. They provide the
durable execution substrate; concrete NGINX, account, TLS, and host-agent
handlers are added by their respective vertical slices.

## Logical operation and attempts

An operation is one user-visible logical job. Its immutable request identity
contains:

- optional hosting-account and actor IDs;
- kind, request ID, and optional idempotency key;
- bounded JSON payload;
- retry classification and maximum attempt count;
- creation timestamp.

Mutable summary fields contain status, stage, monotonic percentage, bounded
result, stable error code, attempt count, next eligibility time, cancellation
request, timestamps, and the current lease holder.

Every claim creates a separate immutable attempt with a UUIDv7 attempt ID,
attempt number, UUIDv7 worker-instance ID, claim/heartbeat/lease times, and final
outcome. Only heartbeat and the one transition from `running` to a terminal
attempt outcome are permitted. Attempts and operations cannot be deleted.

## Idempotent creation

Idempotency keys are unique within one hosting account or within the global
administrator scope. A replay returns the existing operation, including its
current or terminal outcome, without creating another event or audit entry.

The replay must match the original actor, kind, canonical payload, retry class,
and maximum-attempt policy. Reusing a key for different input returns a conflict
rather than silently attaching the caller to unrelated work. Request IDs may
differ because a retried HTTP request is a new transport request for the same
logical operation.

Operations without an idempotency key are always distinct. Mutation APIs that
can cause host effects should require a key at the future HTTP/service boundary,
even though the generic repository also supports internal observation jobs
without one.

## Claiming and fencing

A runner declares up to 32 supported operation kinds. Claiming is a serialized
immediate transaction that:

1. recovers all expired leases;
2. selects the oldest eligible supported pending operation;
3. inserts its next attempt;
4. moves the operation to `running` with that attempt and worker ID;
5. appends a `claimed` event.

Leases are between five seconds and five minutes. The default runner uses a
30-second lease and a 10-second heartbeat. Heartbeats, checkpoints, completion,
failure, and cancellation acknowledgement require the exact operation,
attempt, and worker tuple and an unexpired lease. Once recovery clears that
tuple, a delayed old worker receives `ErrOperationLeaseLost` and cannot commit
progress or an outcome.

SQLite's serialized writer prevents two local workers from winning the same
claim. Attempt fencing remains necessary because the actual host effect may run
outside that database transaction.

## States and transitions

| From | Allowed destinations | Cause |
| --- | --- | --- |
| `pending` | `running`, `cancelled` | Worker claim or cancellation before start |
| `running` | `running`, `pending`, `cancelling`, `succeeded`, `failed` | Heartbeat/progress, safe retry, cancellation request, terminal handler result |
| `cancelling` | `cancelling`, `cancelled`, `failed` | Heartbeat, cleanup acknowledgement, or cleanup failure |
| `failed` | `pending` | Explicit allowed manual retry |
| `succeeded`, `cancelled` | none | Terminal and immutable |

Database triggers enforce state shape, immutable request fields, non-decreasing
progress and attempt count, terminal timestamps, current-attempt ownership, and
allowed transitions.

## Retry and restart recovery

Retry classes have deliberately different guarantees:

- `safe`: the handler declares its stages safe to replay. A transient failure
  or expired worker lease can schedule another attempt automatically.
- `manual`: worker loss or failure ends the operation as failed. An authorized,
  audited action may schedule the next attempt after review.
- `none`: exactly one attempt is allowed and no retry API can requeue it.

Automatic retry uses 5, 10, 20, 40 seconds and so on, capped at five minutes,
and never exceeds `max_attempts`. Progress does not decrease across attempts;
handlers must make stage checkpoints represent durable, replay-safe progress.

Recovery runs explicitly at startup and before every claim. A safe expired
attempt becomes `lease_expired` and the logical operation becomes pending after
backoff. Other retry classes become failed with the stable code
`operation.worker_lease_expired`. A cancellation already in progress becomes
cancelled after worker loss so no replacement worker resumes unwanted work.

The in-process runner treats parent-context cancellation as service shutdown: it
does not guess an outcome. The lease expires and recovery makes the decision
from persisted policy. This makes an abrupt process exit and a graceful restart
follow the same durable path.

## Progress and user cancellation

Checkpoints append structured events with stage, percentage, localization-ready
message code, details, attempt, sequence, and UTC time. Progress while running is
limited to 0–99; only successful completion writes 100. Event reads require the
expected account scope and are paginated to at most 200 rows.

A pending operation cancels immediately. A running operation enters
`cancelling`; the next heartbeat or checkpoint returns
`ErrOperationCancellationRequested`. The handler must stop at a defined safe
boundary, clean up or roll back as appropriate, then acknowledge cancellation.
Cleanup failure is recorded as failure rather than falsely claiming a clean
cancellation.

## Error and secret boundary

The durable record stores stable error and message codes, never raw Go errors,
panic values, command output, or exception text. The generic runner converts an
unclassified handler error to `operation.handler_failed` and a panic to
`operation.handler_panic`. Handler-specific safe failures use
`operations.Failure`.

Payload, result, and event-detail JSON is canonicalized, bounded, and checked
for secret-bearing field names. Error text may be recorded only in a separately
bounded and redacted internal diagnostic channel added by a later observability
slice.

Operation events are append-only operational detail. Creation, manual retry,
cancellation request, success, final failure, and final cancellation also append
global hash-chained audit events. Heartbeats and routine percentage updates do
not flood the audit chain.

## Handler contract and deferred integration

Handlers receive typed operation payloads and a progress reporter. They must:

- validate their payload type before host effects;
- use the operation/attempt as idempotency and correlation context when calling
  the privileged agent;
- checkpoint only after a stage is durably complete;
- honor context and cancellation at documented safe boundaries;
- classify retryability conservatively;
- return bounded non-secret result data.

D-004 makes the agent side of that contract enforceable: a privileged RPC must
carry the claimed operation ID and its identity/system actor, with the account
when scoped. The API worker derives this correlation from the persistent record;
browser input is not an independent source. Agent outcome logs can then be
matched to the hash-chained audit record by operation ID without copying the
operation payload. See [Agent audit correlation](agent-audit-correlation.md).

B-004 itself does not start a polling loop in the API binary. D-001 provides the
authenticated versioned transport; D-002 through D-004 add capability data,
safe native execution, and audit correlation. F-003 now supplies the first
concrete durable handler: it replays an immutable typed domain snapshot under
the same operation/revision UUID and records applied state idempotently after
the agent's staged activation and rollback protocol.
