# ADR 0017: Agent audit correlation and safe events

Status: Accepted

## Context

The control database already records durable operations, initiating actors,
accounts, request IDs, attempts, and hash-chained audit events. A privileged
agent mutation without the same durable identity would leave an operator unable
to distinguish a real execution, a retry, or unrelated local activity.

Logging a complete RPC request or native error would make that correlation easy
but would also copy customer paths, configuration, credentials, or future secret
fields into journald. Rejected socket peers cannot provide trusted application
metadata because they are rejected before HTTP parsing.

## Decision

1. Give every supported agent operation an explicit compiled-in access class:
   protocol, read-only, or privileged mutation.
2. Require every privileged mutation to carry a typed audit correlation before
   dispatch. Forbid correlations on protocol and read-only operations.
3. Require a canonical UUIDv7 durable operation ID and either a canonical
   UUIDv7 identity actor or the explicit `system` actor. Allow one optional
   canonical UUIDv7 account ID.
4. Include audit correlation in the semantic idempotency digest. Continue to
   exclude transport request IDs and idempotency keys themselves.
5. Log RPC outcomes through fixed structured event builders. Include only
   validated identifiers, operation, status, replay state, and stable reason
   codes; never serialize the typed payload or request body.
6. Adapt `net/http`'s unstructured server-error logger to a stable event that
   discards original text.
7. Emit `agent.peer.rejected` for credential lookup failure and unexpected UID
   before HTTP parsing. Include kernel numeric PID/UID only when credential
   lookup succeeded. Do not include raw lookup or close errors.
8. Treat journald agent events as correlated operational/security evidence, not
   a replacement for the database's append-only audit chain.

## Consequences

- The first privileged mutation cannot be added as a valid protocol operation
  without selecting its audit policy.
- An idempotency replay remains attributable to the original durable operation
  and actor; changing that context creates a semantic conflict.
- Background work uses an explicit system actor instead of inventing an
  identity or omitting attribution.
- Agent logs intentionally sacrifice arbitrary error detail. New safe diagnostic
  fields require explicit schema and redaction review.
- The agent authenticates the API service, not the human identity. It records
  the validated correlation supplied by that trusted service; control-plane
  authorization and durable audit persistence remain API responsibilities.
