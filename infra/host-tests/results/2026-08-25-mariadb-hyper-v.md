# Tenant-scoped MariaDB Hyper-V qualification

Date: 2026-08-25

The same release-shaped `linux-amd64` archive was installed from the clean
`stackfort-installer-ready-20260824` checkpoint and tested on all supported
Hyper-V guests.

- Version: `0.0.0-dev`
- Archive: `stackfort-0.0.0-dev-linux-amd64.tar.gz`
- SHA-256: `51cbfac950e54e102278958f448fe805d15f66560435761e35824891da643a04`
- Installer source digest: `4dd7b2698cd6499c619d5af21fbd74fa062a76701b715e5dfb6d5472e2eef799`
- Guest shape: 2 vCPU, 4 GiB startup RAM, system disk plus dedicated
  project-quota disk

## Matrix

| Evidence | Debian 13 | Ubuntu 26.04 | Rocky Linux 10 |
| --- | --- | --- | --- |
| Fresh install and journaled no-op rerun | Pass | Pass | Pass |
| Native MariaDB package, CLI, active/enabled service | Pass | Pass | Pass |
| Verified root Unix socket and control-schema shape | Pass | Pass | Pass |
| Read/write principal can create and mutate own table | Pass | Pass | Pass |
| Read-only principal can select own data | Pass | Pass | Pass |
| Read-only write escalation denied | Pass | Pass | Pass |
| Cross-account schema access denied | Pass | Pass | Pass |
| Same agent mutation replay is idempotent | Pass | Pass | Pass |
| Drop revokes all schema grants first | Pass | Pass | Pass |
| Database and principals removed without grant residue | Pass | Pass | Pass |
| AppArmor or enforcing SELinux integration | Pass | Pass | Pass |
| Existing NGINX/PHP/TLS/quota/security suite | Pass | Pass | Pass |

Every guest emitted the machine-checked marker
`STACKFORT_QUALIFICATION mariadb-tenant-lifecycle=passed`. The harness also
required `Phase2MariaDBLifecycle : passed`; a missing marker fails the run.

## Baseline measurements

These loopback measurements are regression baselines from the complete suite,
not public capacity claims. Each record used eight concurrent clients in the
disposable guest.

| Guest | NGINX static requests/s | Static p99 | API health requests/s | API p99 |
| --- | ---: | ---: | ---: | ---: |
| Debian 13 | 69,394 | 613 us | 40,441 | 1,043 us |
| Ubuntu 26.04 | 88,558 | 449 us | 44,263 | 1,084 us |
| Rocky Linux 10 | 73,642 | 602 us | 40,178 | 1,919 us |

## Defects found and closed

Live qualification exposed two MariaDB integration faults and one test-boundary
fault that unit-only coverage did not reveal:

1. Passing password bytes through an interpolating SQL driver produced a
   MariaDB `_binary` literal in `CREATE USER`. The agent now computes the
   documented `mysql_native_password` verifier locally and uses only that
   verifier in a fixed statement; plaintext remains out of SQL and arguments.
2. The control-schema verification originally trusted `CREATE TABLE IF NOT
   EXISTS`. The agent now verifies the exact column types, nullability, ASCII
   binary collation, InnoDB engine, and composite primary key before use.
3. Calling the installed production agent from a root-run test was correctly
   rejected by `SO_PEERCRED`, because only the configured API UID may connect.
   The test now starts the same in-process handler with an explicitly root-bound
   disposable socket, preserving the production peer-identity gate while
   exercising the real MariaDB reconciler.

The final three-guest matrix was repeated after these corrections and after
changing deletion to revoke every schema-wide privilege before dropping the
database.

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

