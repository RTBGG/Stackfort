# ADR 0014: Kernel-authenticated typed local agent protocol

Status: Accepted

## Context

The browser-facing API must eventually request narrowly privileged host work.
Socket file permissions alone can be misconfigured, a generic RPC payload would
grow into command execution, and incompatible API/agent upgrades must fail
before applying partial state. Duplicate delivery also needs an explicit
contract.

## Decision

1. Keep the agent Linux-only and expose HTTP/1.1 solely over a protected
   filesystem Unix stream socket.
2. Authenticate every connection before HTTP parsing with kernel-reported
   `SO_PEERCRED`, requiring the exact configured control API UID.
3. Use a versioned, strict JSON tagged union. Every operation needs dedicated
   Go request/response fields and an explicit dispatch case. Never add a
   generic command, shell, arbitrary executable, environment, or path method.
4. Begin with only `protocol.handshake`, negotiating the highest overlapping
   version and returning build provenance plus an explicit operation list.
5. Require bounded request and idempotency IDs, 64-KiB body limits, strict JSON,
   fixed timeouts, response correlation, and stable non-secret error codes.
6. Reject idempotency-key reuse with different semantic input. Use the bounded
   process-local response cache only for duplicate dispatch suppression;
   privileged handlers remain responsible for durable desired-state
   reconciliation and operation fencing.

## Consequences

- Possession of socket filesystem access does not bypass the exact UID check.
- The privileged surface cannot be expanded without a code and protocol review.
- Rolling upgrades can stop safely on incompatibility rather than guessing.
- Future operation types add schema code and tests, which is intentional.
- The local response cache is not durable; future mutations must be naturally
  idempotent and correlated with persisted control-plane operations.
- ADR 0017 now enforces that mutation correlation in the operation registry and
  defines payload-free agent and rejected-peer events.
- Remote-node transport is explicitly out of scope and cannot reuse the local
  peer-authentication assumption unchanged.
