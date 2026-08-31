# ADR 0055: Prepare only digest-pinned, bounded, scanned OCI images

- Status: accepted
- Date: 2026-08-31
- Extends: [ADR 0053](0053-constrained-oci-application-drafts.md), [ADR 0054](0054-rootless-podman-account-runtime.md)

## Context

An OCI application draft must become an immutable image before a later
Quadlet lifecycle can deploy it. General Docker build options, tags, engine
sockets, unbounded contexts, or a scan performed after deployment would let
mutable or unsafe workload intent cross the privileged host boundary.

## Decision

1. Queue one retry-safe `oci.image.prepare` operation containing a server-built
   snapshot of the persisted application revision and derived account identity.
2. Pull registry sources only by explicit SHA-256 digest with TLS verification
   and an always-pull policy. Persist both requested source digest and resulting
   local image ID.
3. Snapshot Containerfile inputs descriptor-relatively. Reject links, special
   files, traversal, oversized inputs, mutable base tags, implicit registries,
   remote `ADD`, external-stage copy, volumes, on-build instructions, and
   instruction-level network/security/secret overrides.
4. Build as the account UID/GID through rootless Podman, with fixed no-network,
   no-cache, CPU, memory, process, file-descriptor, output, and time bounds.
   Account project quota bounds engine storage; `RLIMIT_FSIZE` bounds the scan
   archive.
5. Bundle one checksum-pinned Trivy version. Scan an OCI archive without an
   engine socket, fail closed on scan errors, and reject every HIGH or CRITICAL
   finding before persistence.
6. Store a root-owned, create-only replay manifest on the host and append-only
   evidence in SQLite. A retry converges only when the request and result match
   exactly.
7. Do not create networks, secrets, volumes, containers, public ports, or
   Quadlets in this ticket.

## Consequences

- Mutable tags and unrestricted Docker/Compose compatibility are deliberately
  unavailable.
- Containerfile builds cannot download during `RUN`; required inputs must be in
  the bounded context or a digest-pinned build stage.
- A missing vulnerability database or scanner outage prevents deployment. This
  favors a false-negative-resistant security gate over availability.
- Scanner/database freshness and broader supply-chain policy can evolve only by
  versioning the policy contract and its persisted evidence.
- L-004 can add private networking, encrypted secret references, and bounded
  volumes without reopening image acquisition arguments. L-005 can deploy only
  a recorded immutable image ID.

## References

- <https://docs.podman.io/en/stable/markdown/podman-pull.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-build.1.html>
- <https://trivy.dev/docs/latest/guide/references/configuration/cli/trivy_image/>
