# J-005 database-password rotation Hyper-V qualification

Date: 2026-08-26  
Result: passed on Debian 13, Ubuntu 26.04, and Rocky Linux 10

## Qualified artifact

- Version: `0.0.0-dev`
- Archive: `dist/stackfort-0.0.0-dev-linux-amd64.tar.gz`
- Archive SHA-256:
  `dedad14d010b2e71d8a313a28f1a91022e4e960b28bd178904e9ee1998c0bcde`
- Immutable source digest reported by every final guest:
  `e53d77fe644303996c83bf44f3b1464259e4728801bb5aab02b48acfb9eaa7b5`
- Guest shape: 2 vCPU, 4 GiB startup RAM, system disk plus dedicated
  managed-hosting disk.

Each VM was restored to the exact `stackfort-installer-ready-20260824`
snapshot before its final run. The archive was built once and all three final
runs used `-SkipBuild`.

## Password-rotation assertions

The destructive installed-agent test created isolated read/write and
read-only account principals and then:

1. proved the original read/write credential and grant worked;
2. replayed initial provisioning safely before a later generation existed;
3. rotated the principal through `database.password.rotate`;
4. proved a fresh connection with the old password was rejected;
5. proved the new password retained the original schema grant;
6. dispatched a cache-cold replay of the old provisioning operation and
   required `database_conflict`; and
7. proved that rejected stale replay did not disturb the new password.

Every guest emitted:

```text
STACKFORT_QUALIFICATION mariadb-tenant-lifecycle=passed password-rotation=passed
```

The installer first run, journal contract, second-run no-op, file metadata,
systemd sandbox, firewall, AppArmor/SELinux, service health, phpMyAdmin signon,
domain/account isolation, PHP pools, quota, injected recovery, and private ACME
lifecycle also passed on every final guest.

## Qualification finding and correction

The first Debian attempt exposed that the integration test replayed the
original provisioning request only after changing its fixture password. The
agent cache correctly rejected that changed semantic request. Reviewing the
sequence identified a deeper generation fence worth enforcing: a cache-cold
old provisioning operation must never be able to restore a credential after a
rotation.

The final implementation advances the existing same-account user ownership
marker to the rotation operation after changing the password. Rotation replay
with that operation remains idempotent, while every older credential mutation
is fenced. Unit tests and the real three-guest test now cover this condition.
The final artifact above contains the correction; all final runs started from
clean snapshots.

## Performance smoke baselines

These measurements detect broad regressions; they are not a comparative
benchmark of the password operation itself.

| Guest | NGINX static requests/s | Static p99 | API health requests/s | API p99 |
| --- | ---: | ---: | ---: | ---: |
| Debian 13 | 71,000.4 | 584 µs | 35,716.0 | 1,604 µs |
| Ubuntu 26.04 | 87,471.8 | 720 µs | 51,543.6 | 1,395 µs |
| Rocky Linux 10 | 71,209.2 | 778 µs | 41,436.0 | 1,404 µs |

## Commands

```powershell
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 debian-13 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 ubuntu-26.04 -VmName stackfort-ubuntu-26-04-v2 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 rocky-10 -SkipBuild -RunPhase1Suite
```
