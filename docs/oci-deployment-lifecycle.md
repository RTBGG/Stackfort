# Rootless OCI deployment lifecycle

L-005 turns one image- and resource-approved application revision into a fixed
rootless Podman workload. The public model still has no command, engine option,
host path, capability, device, namespace, or public host-port field.

## Immutable deployment intent

The control plane atomically reserves one stable port from `20000–29999` for
each application. The durable operation contains the derived hosting identity,
UUIDv7 application and revision, approved image/resource digests, one internal
port, the loopback port, health policy, secret references, and managed volume
references. Environment plaintext is not durable.

The pure renderer emits a byte-stable root-owned Quadlet below
`/etc/containers/systemd/users/<uid>`. It fixes:

- the approved local SHA-256 image with `Pull=never`;
- the account-private `stackfort-private` network;
- `127.0.0.1:<allocated>:<internal>/tcp` as the only published port;
- `NoNewPrivileges`, all capabilities dropped, a read-only image filesystem,
  init, a 512-process ceiling, journald, and Stackfort ownership labels;
- only Podman secrets and descriptor-verified managed volumes; and
- restart-on-failure plus activation in the user's `default.target`.

The caller cannot add arbitrary Quadlet, Podman, or systemd directives.

## Secret boundary

Only deploy and rollback decrypt the exact, generation-fenced values. Plaintext
exists briefly in the API worker, the authenticated local Unix-socket request,
and the agent's fixed stdin buffer for `podman secret create --replace`. It is
excluded from operation rows, audit events, replay caches/manifests, structured
agent logs, command arguments, and Quadlets; buffers are cleared after use.
Remove uses a fixed derived `podman secret rm --ignore` profile so application
secrets do not survive workload retirement.

## Health-gated routing

The agent atomically replaces the exact Quadlet, reloads the account user
manager, restarts the unit, and probes only its loopback endpoint. HTTP probes
require a 2xx/3xx response; TCP probes require a successful connection. A
failed probe stops the candidate and restores the previous Quadlet and active
state.

Only after a healthy result does SQLite append immutable deployment evidence
and move the exact revision to `active` with `applied_revision = revision`.
Database triggers reject OCI domain targets that do not meet that invariant.
The NGINX renderer resolves active applications to their persisted loopback
allocation, emits a fixed keepalive upstream, and uses the existing candidate
validation, atomic activation, local probe, and rollback pipeline. Suspend and
remove are rejected while any active domain still targets the application.

## Replay and lifecycle

Deploy, suspend, resume, rollback, and remove are revision-fenced durable
actions. Host replay manifests contain only the semantic deployment digest and
sanitized result metadata. Repeating the same deploy converges without a new
artifact; conflicting host content fails closed. Rollback re-converges the
last control-plane-approved spec. Remove stops the unit, deletes the exact
Quadlet and derived Podman secrets, reloads systemd, and retains managed volume
data for an explicit later data-retirement policy.

Application logs come only from the derived systemd user unit. The request is
limited to 500 entries, command output to 256 KiB, and each returned message to
8 KiB. Invalid records are skipped and control characters are replaced before
the account receives the result.

## Qualification

Unit/repository tests cover the closed schema, deterministic renderer,
plaintext durability fence, typed protocol, stable loopback allocation,
immutable evidence, state transitions, active-route fence, OCI NGINX upstream,
and operation replay. On 2026-09-01 the focused production manager test passed
on the disposable Debian 13 Hyper-V guest with real rootless Podman and systemd:
deploy and health, loopback-only ingress, replay, bounded logs, suspend, resume,
rollback, removal, secret retirement, and clean identity teardown all passed.
The L-006 exit matrix now passes on Debian 13, Ubuntu 26.04, and Rocky Linux
10. It verifies the exact account/user-manager/container cgroup ancestry,
parent PID exhaustion from inside the container, generic memory OOM behavior,
loopback-only ingress, hostile policy inputs, cross-account filesystem/network/
process isolation, and healthy automatic recovery after a real guest reboot.

See [ADR 0058](adr/0058-health-gated-rootless-quadlet-lifecycle.md) and the
[qualification record](../infra/host-tests/results/2026-09-01-oci-deployment-lifecycle-hyper-v.md).
The complete Phase 5 evidence is in the
[exit-matrix record](../infra/host-tests/results/2026-09-01-oci-phase5-exit-matrix-hyper-v.md).

Upstream references:

- <https://docs.podman.io/en/stable/markdown/podman-systemd.unit.5.html>
- <https://docs.podman.io/en/stable/markdown/podman-secret-create.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-secret-rm.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-run.1.html>
