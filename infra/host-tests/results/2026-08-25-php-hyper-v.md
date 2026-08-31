# Account-scoped PHP Hyper-V qualification

Date: 2026-08-25

The same release-shaped `linux-amd64` archive was installed from the clean
`stackfort-installer-ready-20260824` checkpoint and tested on all supported
Hyper-V guests.

- Version: `0.0.0-dev`
- Archive: `stackfort-0.0.0-dev-linux-amd64.tar.gz`
- SHA-256: `0c63f3e46bffc4da1f74a09bb17ea9cbe7c125d36bd376470b8fa2dab9250001`
- Guest shape: 2 vCPU, 4 GiB startup RAM, system disk plus dedicated
  project-quota disk

## Matrix

| Evidence | Debian 13 / PHP 8.4 | Ubuntu 26.04 / PHP 8.5 | Rocky Linux 10 / PHP 8.3 |
| --- | --- | --- | --- |
| Fresh install and journaled no-op rerun | Pass | Pass | Pass |
| Native package present; vendor pool inactive/disabled | Pass | Pass | Pass |
| PHP configuration/runtime directory metadata | Pass | Pass | Pass |
| AppArmor or enforcing SELinux integration | Pass | Pass | Pass |
| Real PHP response through NGINX/FPM | Pass | Pass | Pass |
| FPM worker runs as hosting-account UID | Pass | Pass | Pass |
| Socket is mode `0600` and owned by NGINX identity | Pass | Pass | Pass |
| Service is active/enabled in account cgroup slice | Pass | Pass | Pass |
| Tenant view reports bounded memory/CPU/task aggregates | Pass | Pass | Pass |
| Foreign account fixture is unreadable from PHP | Pass | Pass | Pass |
| PHP can write within its own document root | Pass | Pass | Pass |
| Domain removal retires config, unit, socket, and service | Pass | Pass | Pass |
| Retired pool reports `missing` without residual metrics | Pass | Pass | Pass |
| Existing Phase 1 lifecycle/security suite | Pass | Pass | Pass |

Rocky additionally passed persistent context resolution for
`/etc/stackfort/php`, `/run/stackfort-php`, and PHP document roots while SELinux
remained enforcing. The harness machine-checked the marker
`STACKFORT_QUALIFICATION php-account-pool-isolation=passed` and
`STACKFORT_QUALIFICATION php-account-pool-observability=passed` on every guest.

## Baseline measurements

These loopback measurements are regression baselines, not public capacity
claims. Each record used eight concurrent clients in the disposable guest.

| Guest | NGINX static requests/s | Static p99 | API health requests/s | API p99 |
| --- | ---: | ---: | ---: | ---: |
| Debian 13 | 62,010 | 855 us | 29,574 | 1,255 us |
| Ubuntu 26.04 | 84,564 | 913 us | 38,953 | 1,394 us |
| Rocky Linux 10 | 67,978 | 937 us | 42,271 | 1,619 us |

## Defects found and closed

Live qualification exposed four runtime integration faults that unit-only
coverage did not reveal:

1. `Type=simple` let systemd return before FPM had validated configuration and
   opened the socket. Managed units now use FPM's `Type=notify` readiness.
2. `/proc/self/fd/2` could not be opened as the FPM error log under journald.
   Managed pools now log through syslog with an account-specific identity.
3. The FPM master could start account workers but could not terminate them
   because its capability set omitted `CAP_KILL`. The fixed minimum capability
   set now covers worker lifecycle and no more.
4. Rocky's PHP package installed an NGINX drop-in that coupled NGINX to the
   disabled global pool. The installer removes only the exact RPM-owned form
   and rejects any modified/foreign replacement.

The complete three-guest matrix was repeated after these corrections.

## Reproduction

From an elevated PowerShell session, restore the named clean checkpoint and
run:

```powershell
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 debian-13 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 ubuntu-26.04 -VmName stackfort-ubuntu-26-04-v2 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 rocky-10 -SkipBuild -RunPhase1Suite
```

The suite is destructive and is valid only for the disposable fixtures named
in the [host-test guide](../README.md).
