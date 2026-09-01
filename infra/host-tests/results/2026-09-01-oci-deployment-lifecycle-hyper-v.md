# OCI deployment lifecycle qualification — 2026-09-01

## Scope

The focused L-005 test ran the production rootless OCI deployment manager in
the existing disposable `stackfort-debian-13` Hyper-V guest. The Windows
harness cross-built the Linux integration binary, installed the distribution's
Podman rootless helpers, transferred the exact binary over SSH, and ran only
`TestDisposableHostOCIDeploymentLifecycle` as root with the destructive-host
opt-in marker.

The workload fixture was pulled from
`docker.io/nginxinc/nginx-unprivileged:alpine`; the test inspected its local
SHA-256 image ID and supplied only that immutable digest to the production
Quadlet renderer.

## Result

| Guest | Lifecycle | Cleanup |
| --- | --- | --- |
| Debian 13 | passed | passed |

The test proved:

- a root-owned byte-stable Quadlet and digest-pinned `Pull=never` image;
- publication only on the allocated `127.0.0.1` high port;
- real rootless systemd start and HTTP health success before activation;
- transient stdin secret creation and derived secret removal;
- an idempotent deploy replay with metadata-only host evidence;
- bounded journald retrieval;
- suspend with no remaining ingress, healthy resume, and rollback/reconverge;
- removal of the exact container unit and Quadlet; and
- clean network, state, rootless runtime, subordinate-ID, linger, user, and
  group teardown.

Qualification marker:

```text
STACKFORT_QUALIFICATION oci-deployment-lifecycle=passed
```

Reproduction from an elevated PowerShell session:

```powershell
.\infra\host-tests\Test-StackfortOCIDeploymentHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

This is the focused L-005 evidence, not the Phase 5 exit matrix. L-006 still
owns Debian 13, Ubuntu 26.04, and Rocky Linux 10 reboot, exhaustion, malicious
workload, and cross-account isolation qualification.
