# ADR 0053: Persist only constrained, tenant-owned OCI application drafts

Status: Accepted

## Context

Phase 5 needs an application record before the control plane can safely pull an
image, build a Containerfile, generate a Quadlet, or route a domain. Accepting
an opaque application ID or a general Docker/Compose document would make
tenant ownership and prohibited host features difficult to enforce.

## Decision

Stackfort starts with a deliberately small, versioned application draft:

- one account-owned name and slug;
- either an explicit-registry image pinned by a lowercase SHA-256 digest or a
  normalized account-relative build context and Containerfile path;
- one internal container port and no caller-selected host port; and
- one bounded HTTP or TCP health check.

The schema has no fields for privileged mode, host namespaces, host mounts,
devices, capabilities, engine sockets, command/entrypoint overrides, or public
port bindings. Draft creation is feature- and package-limit-gated, updates are
revision-fenced, removal is logical, and every mutation is audited.

An OCI domain target is valid only when the referenced application belongs to
the same account and its current revision is active and applied. SQLite repeats
this relation check with a trigger because the older `domain_targets` table
cannot gain a composite foreign key without being rebuilt.

## Consequences

- Phase 5 does not claim full Docker Compose compatibility.
- Tags and implicit registries are rejected; deployed image identity can be
  stable and attestable.
- Host execution remains impossible at this stage: drafts cannot become active
  until a later lifecycle ticket implements verified rootless reconciliation.
- Environment secrets, bounded volumes, builds, networks, logs, and deployment
  operations extend this closed schema in later migrations.
