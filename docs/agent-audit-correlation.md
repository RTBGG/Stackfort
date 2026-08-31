# Agent audit correlation

D-004 connects privileged host mutations to Stackfort's existing durable
operation and audit records. It also defines the safe structured events emitted
by the host-agent boundary.

## Correlation model

The RPC envelope distinguishes three different identities:

| Field | Purpose | Replay behavior |
| --- | --- | --- |
| `requestId` | One API-to-agent transport attempt | Changes for a new transport attempt |
| `idempotencyKey` | Duplicate-dispatch suppression | Reused only for identical semantic input |
| `correlation.operationId` | Durable control-plane operation | Stable across retries and transport attempts |

Every operation in the compiled-in protocol registry has an explicit access
class: protocol, read-only, or privileged mutation. Protocol and read-only
requests must not carry an audit correlation. Every privileged mutation must
carry one before its typed payload can dispatch.

The correlation contains:

- the canonical UUIDv7 durable operation ID;
- actor kind `identity` plus its canonical UUIDv7 identity ID, or actor kind
  `system` without an identity ID; and
- an optional canonical UUIDv7 hosting-account ID at the generic envelope
  layer. Account-scoped mutations require it and bind it to their typed payload.

The API worker derives these values from the claimed persistent operation. They
are never accepted from a browser as independent authority. The local agent
trusts the already authenticated control API to have authorized the actor, but
strictly validates the shape before mutation. The audit correlation remains in
the semantic request digest, so an idempotency key cannot be silently rebound to
another operation, actor, or account.

The present wire version exposes a handshake and read-only host inspection,
which reject correlations, plus E-001 reconciliation and deletion mutations,
which require them. Both account mutations require the correlation account ID
to exactly match the immutable identity payload account ID.

## Structured agent events

Completed RPC calls emit `agent.rpc.completed` with only:

- event kind and code;
- request ID and typed operation name;
- HTTP outcome and whether the response was replayed; and
- for a mutation, the validated operation, actor, and optional account IDs.

An idempotency conflict emits `agent.rpc.rejected` with the same safe
correlation metadata and a stable reason code. Idempotency keys, typed payloads,
raw request bodies, command output, native errors, credentials, and secret
values are never event attributes.

Go's unstructured `net/http` server error hook is adapted to
`agent.http.internal_error`. The adapter intentionally discards the original
text, including panic values and possible request fragments. Detailed failures
must be represented by separately reviewed stable fields, not copied from raw
errors.

These agent events normally enter journald. They complement rather than replace
the control database's append-only hash-chained audit events. The shared durable
operation ID lets an operator correlate both sides without putting user payloads
in agent logs.

## Unexpected local peers

Peer authentication still occurs before HTTP parsing. Credential lookup failure
or a kernel-reported UID other than the configured API service UID closes the
connection and emits:

```text
event_kind=security
event_code=agent.peer.rejected
reason_code=credential_lookup_failed | unexpected_uid
```

For an unexpected UID, the event may include only the kernel-provided numeric
PID and UID. It does not include connection bytes, HTTP fields, request bodies,
credential lookup errors, or filesystem-derived identity names. Failure to
close a rejected connection produces the separate stable event
`agent.peer.close_failed` without raw error text.

## Verification

Tests enforce canonical UUIDv7 correlation, identity/system actor rules,
required-versus-forbidden correlation, semantic-digest binding, immutable
operation-registry copies, safe replay metadata, and omission of payloads,
idempotency keys, and injected secret markers from logs. Listener tests verify
both peer-rejection reason codes. The Linux socket integration test verifies an
actual wrong-UID connection is rejected before the handler and produces the
structured security event.
