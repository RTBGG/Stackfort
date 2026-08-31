# Constrained OCI application foundation

L-001 establishes the tenant and input boundary for Phase 5. It intentionally
does not execute Podman, build an image, create a network, or generate a
Quadlet. Those actions require the later privileged lifecycle boundary.

## Draft contract

Each live draft has a stable UUIDv7, account parent, human name, account-unique
slug, revision, source, internal port, health check, and lifecycle state.
Creation requires both the package feature and a positive
`maxOciApplications` limit. The absolute implementation ceiling is 64 live
applications per account.

Draft updates require the exact current revision. Draft removal is logical so
audit and foreign-key history remain intact. Cross-account lookup returns the
same not-found result as an unknown ID.

## Closed source schema

| Source | Accepted input | Rejected input |
| --- | --- | --- |
| Digest image | Explicit registry/repository plus `@sha256:` and 64 lowercase hex characters | Tags, URL syntax, implicit registries, uppercase or ambiguous references |
| Containerfile | Normalized account-relative build context and `Containerfile` or `*.Containerfile` path | Absolute paths, traversal, backslashes, repeated separators, arbitrary filenames |

The initial runtime contract exposes exactly one internal port. There is no
host-port field. A health check is mandatory and is either:

- a literal queryless HTTP path with a 10, 30, or 60 second interval; or
- a TCP connection check without a path.

Timeout and retry values are bounded. The public API will reuse the same
validator rather than accepting free-form Compose, Podman, systemd, or NGINX
configuration.

## Explicitly absent capabilities

The persistent type and SQL schema have no representation for:

- privileged containers or additional capabilities;
- host PID, network, IPC, or user namespaces;
- devices, arbitrary host mounts, or container-engine sockets;
- caller-selected host ports; or
- command, entrypoint, systemd, Quadlet, or proxy directive overrides.

Later tickets may add reviewed secret references and bounded account-owned
volumes. They will not add generic passthrough fields.

## Domain relationship

The domain core now verifies that an OCI target references the same account,
has status `active`, has no removal marker, and has an applied revision equal to
its desired revision. A database trigger repeats those checks for every new OCI
domain-target revision.

L-001 creates drafts only, so no application can satisfy this active-state gate
yet. This is deliberate: routing cannot precede successful rootless deployment
and health verification.

## Verification

Tests cover both source forms, digest and path ambiguity, health bounds, feature
and count limits, stale revisions, logical retention, audit-chain integrity,
cross-account reads, inactive targets, and cross-account domain targeting.
See [ADR 0053](adr/0053-constrained-oci-application-drafts.md).
