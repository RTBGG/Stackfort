# Host Agent

This directory will contain the minimal privileged Go host agent. Its module
path is `github.com/RTBGG/stackfort` and its installed executable is
`stackfort-agent`.

The agent exposes only typed, allowlisted, idempotent operations through a
protected local Unix socket. It validates native service configuration before
atomic activation and records enough progress for safe retries and rollback.

The foundation implementation is Linux-only and exposes `GET /v1/health` plus
the typed `protocol.handshake` and `host.capabilities.inspect` RPCs. D-001
implements the protected listener, exact Linux peer-UID verification, transport
bounds, and control API client. D-002 adds typed read-only distribution,
systemd, cgroup, quota, security, port, package, and service inspection;
and D-004 enforces mutation audit correlation plus payload-free structured
agent/security events. Privileged mutation operations will be added individually
with typed request models, authorization context, validation, audit coverage,
and failure-injection tests. See
[`docs/local-agent-protocol.md`](../../docs/local-agent-protocol.md).
