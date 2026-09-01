# OCI private networks, environment references, and volumes

L-004 prepares the private host resources for one image-approved application
revision. It still does not start a container, expose a host port, or generate
a Quadlet; those lifecycle actions belong to L-005.

## Encrypted environment values

Environment values are tenant-scoped control-plane records. Stackfort accepts
non-empty UTF-8 values up to 32 KiB, encrypts each value with a fresh AES-256-GCM
data key, and wraps that key with the configured master key. Public models,
application records, operation payloads, audit events, and agent requests carry
only the value UUID, target environment name, and generation.

An application can reference at most 32 values. Targets use the closed
`[A-Z_][A-Z0-9_]*` form, and neither a target nor a value record can occur twice
in one application. Cross-account references and removal of referenced values
are rejected. Rotation replaces the encrypted envelope and increments the
generation without exposing plaintext. Logical removal destroys the retained
envelope bytes.

L-005 will own the narrow deployment-time plaintext boundary. L-004 deliberately
does not place values in Podman storage before a workload exists.

## Account-private network

Every hosting account receives its own rootless Podman network namespace and
the deterministic network name `stackfort-private`. The agent accepts only the
derived account identity and invokes fixed Podman profiles; no network driver,
subnet, port, option, or engine endpoint can be supplied by a caller.

The network is a DNS-enabled bridge with Netavark `isolate=strict`. It is not an
`--internal` network, so applications may make outbound connections, but
strict bridge isolation blocks traffic to other Podman bridge networks. The
agent verifies the driver, DNS state, isolation option, managed/account labels,
and exact name after creation. A foreign object with the same name is a hard
conflict. Public host-port publication remains absent from every schema and
agent profile.

## Descriptor-verified volumes

An application can attach at most 16 account-owned volume records. A caller
selects only a volume UUID, normalized absolute container target, and read-only
flag. Targets below `/proc`, `/sys`, `/dev`, `/run`, or `/boot` are rejected.
Host paths, devices, mount options, namespaces, and capabilities do not exist in
the contract.

The host path is always derived as:

```text
/srv/hosting/accounts/<account-id>/.stackfort-oci-volumes/<volume-uuid>
```

The agent opens each parent descriptor-relatively with `O_NOFOLLOW`, requires
the account filesystem device, and enforces account UID/GID plus mode `0700`.
The hidden volume root is reserved from file-manager mutation/navigation and
ordinary account backup traversal. It inherits the account-wide project quota,
so rootless image storage, application files, and volumes share the package
storage boundary. Host-side retirement and application-consistent backup are
sequenced with the L-005 lifecycle rather than inferred from a database delete.

## Durable evidence and rotation

`oci.resources.reconcile` is retry-safe and revision-fenced behind successful
image evidence. Its semantic digest includes the application revision, secret
generations, and volume mounts. The agent stores a root-owned create-only replay
manifest, and SQLite stores append-only metadata evidence.

Evidence is keyed by application revision plus resource digest. This permits a
rotated environment value to produce a new append-only resource generation
without rebuilding the unchanged image. Replays converge only when the full
metadata intent and policy result match.

Fresh account provisioning now runs in this order so project inheritance is
active before Podman creates any storage:

1. Unix group, user, and empty account root;
2. project assignment, quota, and managed filesystem layout;
3. subordinate IDs, linger, user runtime, Podman storage, and Quadlet root; and
4. account systemd/cgroup limits.

## Qualification

Unit and repository tests cover encryption, plaintext absence, tenant scope,
forbidden fields, stale revisions, rotation, immutable evidence, foreign
networks, symlink roots, exact modes, and replay. The focused disposable-host
test additionally passed on Debian 13 on 2026-09-01 with real Podman 5.4.2,
Netavark 1.14, project quotas, two rootless accounts, and the production host
manager. The complete three-distribution isolation matrix remains the L-006
exit gate.

See [ADR 0057](adr/0057-account-private-oci-resources.md), the
[rootless account runtime](rootless-oci-runtime.md), and
[bounded image preparation](oci-image-preparation.md).

Upstream references:

- <https://docs.podman.io/en/latest/markdown/podman-network-create.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-secret-create.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-volume-create.1.html>
