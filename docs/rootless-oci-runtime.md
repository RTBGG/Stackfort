# Rootless OCI account runtime

L-002 establishes the host and account prerequisites for later OCI deployment.
It deliberately does not pull an image, build a Containerfile, create a
network, start a container, or generate an application Quadlet.

## Host capability contract

The bounded host inspection reports Podman as a typed OCI provider with five
independent capabilities:

| Capability | Required evidence |
| --- | --- |
| Rootless | Podman is installed and subordinate-ID helpers are available |
| Quadlet | Podman 4.4 or newer, systemd, and unified cgroup v2 |
| Network | netavark, aardvark-dns, passt/pasta, and slirp4netns are installed |
| Storage | fuse-overlayfs is installed |
| Rootful socket isolation | the rootful socket is masked/inactive and its filesystem socket is absent |

The report never opens an engine socket. Missing, unsupported, or uncertain
evidence remains a typed unavailable/unknown capability and blocks account
runtime mutation with a stable `oci_runtime_unavailable` response.

## Deterministic account contract

Only immutable account identity is accepted across the agent boundary. For UID
`u`, Stackfort derives the subordinate range as:

```text
start = 1000000 + (u - 200000) * 65536
end   = start + 65535
```

UID and GID are equal, and the same start/count is used in `/etc/subuid` and
`/etc/subgid`. Supported account UIDs are `200000–249999`, yielding 50,000
pairwise-disjoint ranges. A different range for the same user, duplicate entry,
foreign overlap, malformed line, overflow, or oversized database fails closed.

The derived paths are also fixed:

| Purpose | Path | Owner/mode |
| --- | --- | --- |
| Rootless storage | `/srv/hosting/accounts/<account>/.local/share/containers` | account UID:GID, `0700` |
| User runtime | `/run/user/<uid>` | account UID:GID, `0700` |
| Future Quadlets | `/etc/containers/systemd/users/<uid>` | root:root, `0755` |

Creation walks directory descriptors without following symlinks. Existing
objects with unexpected type, ownership, link behavior, or writable trusted
parents are conflicts; the agent does not adopt them.

## Engine API exclusion

The fresh-host installer masks both `podman.socket` and `podman.service` with
`--now`, repeats the mask in systemd's global user configuration, and verifies
that the system units are inactive and `/run/podman/podman.sock` does not
exist. The account reconciler separately rejects
`/run/user/<uid>/podman/podman.sock` before accepting the runtime.

Stackfort will use typed agent operations and rootless Quadlets, not Podman's
REST API. No Docker-compatible socket is passed to the API, agent, tenant
configuration, or future application schema.

## Reconcile and removal

Runtime preparation occurs as part of the existing replay-safe Unix identity
operation:

1. inspect all OCI prerequisites without mutation;
2. add and re-read the exact subordinate UID/GID ranges;
3. enable linger and verify its root-owned marker;
4. start and verify the account user manager when necessary;
5. create and verify the fixed storage and Quadlet directories; and
6. reject any per-user Podman API socket before returning success.

The control database records an immutable `oci_runtime_reconciled_at` marker.
Identity, filesystem, and resource reconciliation alone no longer make an
account host-ready.

Removal is also narrow. A non-empty Quadlet directory blocks identity deletion,
the user manager is terminated, linger is disabled, and only the exact derived
subordinate ranges are removed. Account data remains governed by the existing
archive-first lifecycle; no recursive deletion is introduced.

See [ADR 0054](adr/0054-rootless-podman-account-runtime.md) and the
[constrained application foundation](oci-application-foundation.md).
