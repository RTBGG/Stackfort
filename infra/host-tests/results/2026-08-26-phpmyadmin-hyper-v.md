# Secure phpMyAdmin signon Hyper-V qualification

Date: 2026-08-26

The same final `linux-amd64` archive was installed from the clean
`stackfort-installer-ready-20260824` checkpoint on every supported Hyper-V
guest.

- Version: `0.0.0-dev`
- Archive: `stackfort-0.0.0-dev-linux-amd64.tar.gz`
- SHA-256: `8f95388db946796f39f8c7e0220c5099985ef22e1d0088d3e35f5a3ea52bac01`
- Installer source digest: `e5d777dbfa1678d0ab515efadf7f751c11f48774a475360a95fbc2ba3c103507`
- Guest shape: 2 vCPU, 4 GiB startup RAM, system disk plus dedicated
  project-quota disk

## Matrix

| Evidence | Debian 13 | Ubuntu 26.04 | Rocky Linux 10 |
| --- | --- | --- | --- |
| Fresh install and journaled no-op rerun | Pass | Pass | Pass |
| Native/bundled phpMyAdmin source contract | Pass | Pass | Pass |
| Dedicated unprivileged FPM service | Pass | Pass | Pass |
| Capability-free `NoNewPrivileges` sandbox | Pass | Pass | Pass |
| Exact socket, secret, state, and config metadata | Pass | Pass | Pass |
| Vendor global PHP-FPM disabled | Pass | Pass | Pass |
| Broker bound only to `127.0.0.1:8081` | Pass | Pass | Pass |
| Launcher syntax and fail-closed GET response | Pass | Pass | Pass |
| AppArmor or enforcing SELinux integration | Pass | Pass | Pass |
| Existing MariaDB/PHP/TLS/quota/security suite | Pass | Pass | Pass |

Every harness result required `Phase2PHPMyAdminSignon : passed`. Core, broker,
HTTP, and browser unit tests separately prove one-time redemption, session and
tenant binding, HMAC authentication, CSRF enforcement, and secret omission.

## Baseline measurements

These loopback records are regression baselines from the complete suite, not
public capacity claims. Each used eight concurrent clients.

| Guest | NGINX static requests/s | Static p99 | API health requests/s | API p99 |
| --- | ---: | ---: | ---: | ---: |
| Debian 13 | 63,996 | 633 us | 39,378 | 915 us |
| Ubuntu 26.04 | 84,437 | 885 us | 39,965 | 1,103 us |
| Rocky Linux 10 | 65,920 | 619 us | 39,298 | 1,948 us |

## Defects found and closed

Live qualification found four issues not visible in unit-only tests:

1. Reopening `/proc/self/fd/2` as the FPM error log failed inside the hardened
   service; the master now logs through Syslog/Journald.
2. A root FPM master needed a privilege transition incompatible with the
   desired `NoNewPrivileges` boundary. The service now starts directly as
   `stackfort-pma`, has no capabilities, and shares only its socket group with
   NGINX.
3. Rocky provides PHP cURL through `php-common`, not a `php-curl` package, and
   needs an explicit `php-cli` package. All distributions now explicitly
   install their CLI while Rocky uses its actual extension packaging.
4. The harness reused a same-version extraction directory and invoked the FPM
   binary as a PHP linter. It now always re-extracts the hash-verified archive
   and uses `/usr/bin/php -l`.

## Reproduction

From an elevated PowerShell session, restore the named clean checkpoint and
run:

```powershell
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 debian-13 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 ubuntu-26.04 -VmName stackfort-ubuntu-26-04-v2 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 rocky-10 -SkipBuild -RunPhase1Suite
```

The suite is destructive and valid only for the disposable fixtures in the
[host-test guide](../README.md).

