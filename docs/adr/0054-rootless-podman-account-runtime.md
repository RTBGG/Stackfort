# ADR 0054: Prepare a closed rootless Podman runtime per account

- Status: accepted
- Date: 2026-08-31
- Extends: [ADR 0018](0018-stable-hosting-account-unix-identities.md)

## Context

OCI application drafts cannot safely become host workloads until Stackfort can
prove that the selected host supports the same rootless runtime contract and
that every account receives isolated identity, storage, runtime, and Quadlet
state. A generic Docker-compatible API would expose a broad privileged control
surface, while caller-selected subordinate-ID ranges or filesystem paths could
cross tenant boundaries.

Rootless Podman requires subordinate UID/GID mappings, a user systemd manager,
rootless networking and storage helpers, and cgroup v2 for Quadlet. Podman's API
socket grants the effective authority of its owning user, so neither a rootful
nor account-user API socket belongs in Stackfort's control path.

## Decision

1. Podman is the sole Phase 5 OCI provider. The installer adds Podman,
   netavark, aardvark-dns, passt/pasta, slirp4netns, fuse-overlayfs, and the
   distribution's subordinate-ID helpers. Pasta is the modern default;
   slirp4netns remains installed for explicit compatibility profiles.
2. The host capability report exposes a typed `oci` section. Account runtime
   reconciliation fails closed unless rootless execution, Quadlet, networking,
   storage, and rootful-socket isolation are all available.
3. Each account UID from `200000` through `249999` deterministically owns one
   non-overlapping block of 65,536 subordinate UIDs and GIDs beginning at
   `1000000`. New account allocation is restricted to that runtime-capable
   range.
4. The privileged agent derives all mappings and paths from the immutable
   account identity. It accepts no caller-selected mapping, runtime path,
   storage path, unit name, or Podman argument.
5. Account storage is prepared below
   `$HOME/.local/share/containers`; the systemd user runtime is
   `/run/user/<uid>`; and root-owned Quadlets will live below
   `/etc/containers/systemd/users/<uid>`. The latter is root-owned `0755` so
   the account's systemd generator can read but never modify it.
   Descriptor-relative, no-symlink traversal and exact ownership/mode checks
   protect every created directory.
6. Linger is enabled for the no-login account and its user manager is started
   only through fixed `loginctl` and `systemctl` profiles. Every subordinate-ID
   mutation is re-read and verified.
7. The installer masks `podman.socket` and `podman.service` both system-wide
   and in the global user configuration, stops rootful units, and verifies that
   `/run/podman/podman.sock` is absent. Account reconciliation also rejects an
   existing `$XDG_RUNTIME_DIR/podman/podman.sock`.
8. Successful account reconciliation records an immutable OCI-runtime marker.
   An account is not host-ready without it. Deletion requires an empty Quadlet
   directory, terminates the user manager, disables linger, and removes only
   the exact derived subordinate-ID ranges before the Unix identity is removed.

## Consequences

- L-002 prepares no image, network, container, or application unit. Workload
  execution remains impossible until later Phase 5 tickets add bounded image
  acquisition and a closed lifecycle.
- Stackfort does not use the Podman REST API or expose an engine socket to the
  web process, host agent, tenant, or workload.
- The single-host account ceiling is 50,000. This is an explicit safety bound
  imposed by non-overlapping 65,536-ID mappings in Linux's 32-bit ID space.
- A foreign subordinate-ID overlap, altered directory, unmasked rootful API
  unit, socket artifact, malformed local ID database, or missing dependency is
  an actionable conflict rather than something Stackfort silently repairs.
- Rootless containers still share the host kernel; later escape-resistance and
  cross-account qualification remain mandatory Phase 5 exit gates.

## Upstream references

- <https://docs.podman.io/en/stable/markdown/podman.1.html>
- <https://docs.podman.io/en/latest/markdown/podman-system-service.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-systemd.unit.5.html>
