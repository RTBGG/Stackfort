# Rootless OCI Phase 5 exit matrix — 2026-09-01

## Scope

L-006 closes Phase 5 by placing the per-account systemd user manager—and thus
its delegated rootless Quadlet tree—below the same UID-derived slice used by
PHP pools and scheduled jobs. The production resource reconciler writes the
exact root-owned drop-in, reloads PID 1, verifies live placement, and migrates
an already active manager only when its cgroup is wrong.

The Windows Hyper-V harness cross-built one Linux integration binary and ran
it unchanged on the existing Debian 13, Ubuntu 26.04, and Rocky Linux 10
fixtures. Before accepting Podman readiness it masked the rootful API service
and socket, matching the production fresh-host installer. The reboot phase
recorded `/proc/sys/kernel/random/boot_id`, requested a real system reboot,
waited for a different boot ID, and executed post-boot verification from the
persistent `/var/tmp` binary.

## Result

| Guest | Shared cgroup | Exhaustion | Private/cross-account isolation | Hostile policy corpus | Reboot recovery |
| --- | --- | --- | --- | --- | --- |
| Debian 13 | passed | passed | passed | passed | passed |
| Ubuntu 26.04 | passed | passed | passed | passed | passed |
| Rocky Linux 10 | passed | passed | passed | passed | passed |

The matrix proved:

- the live `user@<uid>.service` and rootless container PID are exact
  descendants of `stackfort-accounts-<uid>.slice` before and after reboot;
- OCI forks hit the parent account `pids.max`, while the generic boundary
  corpus also records task rejection and an account-local memory OOM kill;
- Quadlets publish only a stable `127.0.0.1` high port and recover a healthy
  HTTP endpoint automatically through linger/default-target activation;
- account roots and volume trees reject foreign traversal, account rootless
  networks remain invisible to another identity, and a foreign UID cannot
  signal the first account's user manager;
- tag-only/transport image references, traversal build paths, unsafe
  Containerfile instructions/options, reserved mount targets, and an unknown
  `privileged` field fail closed; and
- the rendered runtime retains `Pull=never`, read-only storage, no-new-
  privileges, all capabilities dropped, private networking, and no public or
  engine-socket escape directive.

The hostile policy corpus supplements L-003's vulnerability-scanner tests; it
does not treat a deliberately vulnerable image as safe enough to deploy.

Qualification markers:

```text
STACKFORT_QUALIFICATION filesystem-isolation-and-quota=passed
STACKFORT_QUALIFICATION oci-private-resources=passed
STACKFORT_QUALIFICATION oci-deployment-lifecycle=passed
STACKFORT_QUALIFICATION oci-malicious-policy-corpus=passed
STACKFORT_QUALIFICATION oci-reboot-prepare=passed
STACKFORT_QUALIFICATION oci-reboot-recovery=passed
```

Reproduction from an elevated PowerShell session:

```powershell
.\infra\host-tests\Test-StackfortOCIExitMatrixHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
.\infra\host-tests\Test-StackfortOCIExitMatrixHyperVVm.ps1 `
  -ImageId ubuntu-26.04 -VmName stackfort-ubuntu-26-04-v2
.\infra\host-tests\Test-StackfortOCIExitMatrixHyperVVm.ps1 `
  -ImageId rocky-10 -VmName stackfort-rocky-10
```
