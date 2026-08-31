# I-002 Hyper-V installer result — 2026-08-24

Release-shaped build: `0.0.0-dev`, Linux `amd64`

Final release tree digest reported by all three guests:
`ab788da871c427b5f7f9ef949b84ec4f7cdf9fa66aeb3ea9610b909c3f6869ed`.

| Guest | First install | Second run | Firewall | Mandatory access control | Health |
| --- | --- | --- | --- | --- | --- |
| Debian 13 | pass | pass, unchanged journal, `alreadyInstalled=true` | dedicated nftables table, persistent and reloadable | `stackfort-api` AppArmor profile enforcing | API and UID-authenticated agent pass |
| Ubuntu 26.04 LTS | pass | pass, unchanged journal, `alreadyInstalled=true` | dedicated nftables table, persistent and reloadable | `stackfort-api` AppArmor profile enforcing | API and UID-authenticated agent pass |
| Rocky Linux 10 | pass | pass, unchanged journal, `alreadyInstalled=true` | firewalld 80/443 runtime and permanent | SELinux enforcing; both persistent contexts verified | API and UID-authenticated agent pass |

The harness also externally asserted root-only journal metadata, immutable
binary/web ownership and modes, private API configuration/state ownership,
enabled/active services, `NoNewPrivileges`, `PrivateDevices`, NGINX activation,
and the service endpoints.

## Recovery evidence

Before the final matrix, the Debian guest intentionally encountered two
mid-install failures:

1. The old disposable-image login occupied the reserved `stackfort` identity.
   The journal retained `packages=complete` and `service-identity=failed`.
   After moving the test login to `stackfort-test`, rerun skipped package apply
   and completed the identity on attempt 2.
2. The initial root-owned health probe was correctly rejected by the agent's
   kernel UID check. After changing the installer to probe under the actual API
   service UID, rerun verified every prior stage and completed only
   `services-and-health` on attempt 2.

The engine suite separately simulates process loss after an idempotent stage
side effect but before its completion record. The persisted `applying` stage is
retried and verified without reapplying completed predecessors.

## Reusable checkpoints

The three local VMs retain the non-destructive checkpoint
`stackfort-installer-ready-20260824`, created after renaming the disposable SSH
login to `stackfort-test` and before any Stackfort installation. The older
`stackfort-ready-20260816` checkpoints were retained.
