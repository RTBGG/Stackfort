# Bounded OCI image preparation

L-003 turns one immutable OCI application revision into a scanned local image.
It does not start a container, create a network, publish a port, mount a
volume, or generate a Quadlet. Those remain separate, closed lifecycle steps.

## Control-plane contract

The account workspace accepts only an account ID, application ID, expected
revision, request ID, and idempotency key. It authorizes
`account.resources.manage`, reconstructs the complete source and Unix identity
from persisted state, and queues `oci.image.prepare`. A caller cannot supply an
executable, Podman/build argument, registry option, scanner switch, host path,
network mode, mount, namespace, device, or capability.

The worker rejects a queued snapshot when its semantic digest no longer equals
the current application revision. It calls the local agent with the durable
operation/actor/account correlation and records the result only after the
agent returns a valid policy result.

Successful evidence is append-only and keyed by application plus revision. It
contains the deployed `sha256` image ID, requested registry digest or exact
snapshotted build-context digest, policy/scanner versions, severity counts,
actor, and timestamp. A
replay must produce the same evidence. HIGH or CRITICAL findings, a changed
source digest, a stale revision, or altered host replay manifest fail closed.

## Digest pull

Registry sources already require an explicit registry and lowercase
`@sha256:<64 hex>` digest. The account-owned rootless Podman process uses fixed
`--policy=always` and `--tls-verify=true` settings. Stackfort then independently
reads the resulting local image ID; the requested manifest digest and deployed
image ID are retained as distinct immutable values.

No tag-only reference, implicit registry, transport override, insecure TLS, or
credential argument is accepted.

## Containerfile build

The agent snapshots the declared account-relative context without following
symlinks. Devices, FIFOs, sockets, non-regular files, traversal, more than
20,000 files, more than 512 MiB, or a Containerfile over 1 MiB are rejected.
The snapshot is immutable and readable only by the derived account identity.
Its normalized directory/file structure, executable bits, bytes, and separate
Containerfile bytes form a SHA-256 source digest. A replay after any source
change conflicts instead of silently returning an image built from older bytes.

The closed Containerfile subset requires every external `FROM` to name an
explicit registry and SHA-256 digest. `scratch` and earlier named stages are
allowed. Parser directives, `ADD`, `ONBUILD`, `VOLUME`, external `COPY --from`,
and per-instruction secret, SSH, security, or network overrides are rejected.

Podman builds rootlessly as the account UID/GID with these fixed controls:

| Boundary | Fixed value |
| --- | --- |
| Network during build instructions | `none` |
| Cache/layers | no cache; layers disabled |
| Memory and swap | 1 GiB total |
| CPU | one CPU quota (`100000/100000`) |
| Processes | 512 |
| Open files | 1,024 |
| Build duration | 15 minutes |
| Captured stdout/stderr | 1 MiB each |
| Complete preparation | 35 minutes |

Account project quota remains the hard storage boundary for rootless Podman
storage. The exported OCI archive is additionally limited to 2 GiB with
`RLIMIT_FSIZE` before it is accepted for scanning.

## Vulnerability gate

Stackfort bundles Trivy 0.74.0 from its official release archive. Release
creation pins separate SHA-256 hashes for amd64 and arm64; installation verifies
the complete Stackfort release before placing the scanner at the fixed
root-owned mode-`0755` `/usr/local/libexec/stackfort-trivy` path. Readiness also
requires a regular file with one link and rejects different ownership or mode;
scanner presence/version and readiness are typed host capabilities.

The scanner runs as root against the saved OCI archive, never an engine socket
and never the application process. It enables only the vulnerability scanner,
requests only HIGH and CRITICAL results, writes JSON through a 16-MiB bounded
capture, uses a root-only cache, and has a ten-minute scanner deadline. A
scanner error, database/update failure, malformed or oversized JSON, unknown
host readiness, or any HIGH/CRITICAL finding rejects the image. A rejected
or incompletely verified local image is removed through a fixed rootless
Podman profile. Removal is deliberately non-forcing and therefore never
deletes a container that already uses an identical image.

## Replay and cleanup

Each attempt owns one UUIDv7-derived root transaction directory. Success writes
one root-owned `0600` manifest below the fixed artifact root with `O_EXCL`.
Retries return the stored result only when the complete request digest and
policy result validate. Transaction snapshots and scan archives are removed on
every return path; no broad or caller-derived recursive target is used.

See [ADR 0055](adr/0055-digest-pinned-bounded-oci-image-preparation.md), the
[application foundation](oci-application-foundation.md), and the
[rootless account runtime](rootless-oci-runtime.md).

Upstream references:

- <https://docs.podman.io/en/stable/markdown/podman-pull.1.html>
- <https://docs.podman.io/en/stable/markdown/podman-build.1.html>
- <https://trivy.dev/docs/latest/guide/references/configuration/cli/trivy_image/>
