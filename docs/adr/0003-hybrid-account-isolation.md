# ADR 0003: Use Linux accounts for shared hosting and rootless Podman for OCI apps

Status: Accepted

## Context

Putting every static/PHP hosting account in a Docker container makes CPU and
memory flags convenient but increases density cost, image maintenance, and the
privileged daemon surface. Plain Unix accounts alone do not provide a usable
application-container workflow.

## Decision

Give each hosting account a unique Linux UID/GID, PHP-FPM pool, filesystem quota,
and systemd/cgroup-v2 resource subtree. Use rootless Podman under that same
account identity only for OCI applications. Enforce package resources at the
account parent cgroup so all account workloads share its limits.

## Consequences

- Conventional PHP hosting stays dense and fast.
- OCI applications remain available without granting users a rootful daemon.
- All workloads share the host kernel and are not VM-isolated.
- Filesystem permissions, cgroups, quotas, SELinux/AppArmor, and agent path
  handling become critical security controls.
- Shared MariaDB consumption cannot be perfectly charged to account cgroups.

