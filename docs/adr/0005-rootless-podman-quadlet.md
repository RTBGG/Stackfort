# ADR 0005: Manage OCI applications with rootless Podman and Quadlet

Status: Accepted

## Context

Users need Docker-compatible application hosting and resource limits. A rootful
Docker API is a high-value privilege boundary, and exposing its socket to the
panel or workloads would undermine the hosting isolation model.

## Decision

Use rootless Podman for customer applications and generate systemd Quadlet units
from a constrained panel schema. Do not expose the Podman API to users. Resolve
deployments to image digests and prohibit privileged, host namespace, device,
engine-socket, and arbitrary host-mount features.

## Consequences

- There is no long-running rootful customer container daemon.
- systemd owns service lifecycle and cgroup integration.
- Compose compatibility is intentionally partial and must be documented.
- Rootless networking and storage behavior require distribution-specific tests.
- The account Linux identity becomes part of the container security boundary.

