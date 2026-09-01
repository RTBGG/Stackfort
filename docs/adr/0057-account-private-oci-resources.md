# ADR 0057: Derive account-private OCI resources and keep plaintext out of durable work

- Status: accepted
- Date: 2026-09-01
- Extends: [ADR 0053](0053-constrained-oci-application-drafts.md), [ADR 0054](0054-rootless-podman-account-runtime.md), [ADR 0055](0055-digest-pinned-bounded-oci-image-preparation.md)

## Context

OCI applications need network reachability, environment values, and persistent
data, but general Docker/Compose fields would reopen public ports, host mounts,
devices, namespace sharing, engine sockets, and arbitrary runtime options.
Putting plaintext into durable jobs or early Podman state would also widen the
credential exposure and replay surface.

## Decision

1. Store tenant environment values as versioned AES-256-GCM envelopes and
   expose only UUID/name/generation metadata. Never serialize plaintext into an
   application, operation, audit event, agent request, or host replay manifest.
2. Create one deterministic network inside each account's independent rootless
   Podman namespace. Require a DNS-enabled bridge, strict Netavark isolation,
   and exact Stackfort/account labels. Do not accept or publish host ports.
3. Persist only account-owned volume UUIDs and container targets. Derive every
   host path below the hidden account volume root and create it descriptor-
   relatively without following links. Rely on the already assigned account
   project quota as the shared hard storage boundary.
4. Reserve the volume root from normal file-manager and backup traversal. Leave
   host retirement and application-consistent backup to the workload lifecycle.
5. Reconcile only after immutable image evidence. Store append-only evidence by
   application revision and resource digest so secret rotation creates a new
   resource generation without forcing an image rebuild.
6. Split fresh-account reconciliation into base identity, quota/layout, OCI
   runtime, and cgroup stages. Podman storage must never precede project
   inheritance.

## Consequences

- L-005 receives a closed, verified network/volume foundation and must define
  the only transient secret-injection boundary when it generates Quadlets.
- Applications retain outbound network access; isolation is between Podman
  bridge networks rather than a blanket offline policy.
- Account quota, not caller-selected per-mount options, bounds volume storage.
- Secret rotation is append-only and deployable without rescanning an unchanged
  image, while stale queued generations fail their semantic-digest fence.
- Database removal does not guess that a host volume is safe to erase. Exact
  retirement remains coupled to the later workload removal flow.

## References

- <https://docs.podman.io/en/latest/markdown/podman-network-create.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-secret-create.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-volume-create.1.html>
