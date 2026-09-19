# Installed-broker resource pressure and isolation diagnostic

Date: 2026-09-19. **PASS on the modified Debian diagnostic host only.**
This complements the [OCI build/scan diagnostic](2026-09-19-oci-layout-scan-diagnostic.md);
it is not an exact-release fresh-install, reboot, removal or independent security
qualification. No product executable, broker policy, UID mapping or vulnerability
exception was changed during this test session.

## Verified implementation and host

- Installed agent source: `b57ff51c14cc69e21e46bda78e81f0c89fbe8574`.
  SHA-256: `285191173c195291bd1f1c6b0a3b387edc8635f83842e248af26496e013780b2`,
  rechecked after the tests. Its diagnostic version label and earlier broker
  drop-ins remain; this is not an authenticated beta.6 release payload.
- The source export's 1,053 tracked raw Git blobs matched their object IDs,
  including after preparing the final test overlay. Only ignored test-helper
  files were added. Test binaries were hash-verified before root-owned staging
  under `/usr/local/libexec`, with no tenant-writable ancestors.
- VM `stackfort-native-quota-debian-13`, ID
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`; DMI UUID
  `6365bd88-5141-4f15-b3f8-2ba9996baad2`; boot ID
  `b706db26-5a50-4db0-8af6-afcd541c4c6b`.
- Checkpoint `native-b57ff51-before-resource-pressure-20260919`, ID
  `675f609f-4852-4ef0-9bfe-f11129241a9f`, was independently confirmed before
  starting the initially powered-off VM. The immediate checkpoint lookup had
  lagged, so startup waited for a subsequent inventory. The rescue clone and
  unrelated VMs stayed off; strict SSH host-key verification remained enabled.
- The agent was started through its existing diagnostic unit. The control API
  was not started, nor were packages updated or the shared parent units repaired.
  Account setup and resource changes used the actual agent socket with the
  installed API service's kernel peer UID. Shared parent unit bytes had to match
  the production renderer before the first account mutation.

## Final complete run

The final `resource-pressure-b57ff51-v2` overlay passed six pure contract groups,
Linux/amd64 cross-compilation, integration `go vet`, and the real workflow in
8.80 seconds. Its fresh account pair was:

- UID/GID249956: `01a0b926-e600-79dd-a877-d4d7f95d12c5`.
- UID/GID249957: `01a0b926-e602-716c-928c-4a6a5d243ba0`.

Both initially absent accounts were provisioned through the installed broker
with CPU25%, memory256MiB, swap0, tasks128 and disk64MiB/1024inodes. Account A's
limits were subsequently changed through the same broker for pressure tests;
account B's CPU/memory/PID ceilings remained byte-identical.

| Test | Actual evidence |
| --- | --- |
| CPU pressure at 25% of one CPU | 4,020,189 microseconds wall time; 1,011,415 microseconds account CPU usage; 40 additional throttle events; 3,103,130 additional throttled microseconds. The assertion includes a bounded launch/accounting margin, not a throughput benchmark. |
| Controller and migration-file permissions | 35 fixed cgroup files rejected no-write `O_WRONLY` opens for the hosting UID and mapped USER1000 host UID3274917415. A legitimate delegated `cgroup.procs` open succeeded for the account UID, while the subordinate UID could not open it. |
| Kernel-sensitive interfaces | Both identities were denied no-write opens of `/proc/sys/kernel/hostname`, `/proc/sys/kernel/kptr_restrict`, `/proc/sysrq-trigger` and `/dev/kmsg`. Existence/type/ownership were checked; neither settings nor log contents were read or written. |
| Filesystem account isolation | Account A could open its own home; opening B's existing home returned `EACCES`. |
| Process pressure | At TasksMax32, creation of bounded child sleepers hit `EAGAIN` and the account's `pids.events:max` increased by1. Children completed inside the test service. |
| Memory pressure | At MemoryMax64MiB and swap0, a child touched a128MiB allocation. The exact transient service reported `Result=oom-kill`, `ExecMainCode=2`, `ExecMainStatus=9`, `MainPID=0`; account `memory.events:oom_kill` increased by1. |
| Byte quota | At a2MiB project hard limit, a bounded4MiB write attempt partially wrote data then returned the exact `EDQUOT` error. This was not merely a nonzero command exit. |
| Inode quota | At a128-inode project hard limit, exclusive creation of at most256 empty files returned `EDQUOT` after creating a positive number fewer than128. The existing account files also consume inodes. |

All pressure children ran in the exact account slice under the actual account
UID/GID, without extra supplementary privileges. Services had RuntimeMaxSec15s,
TimeoutStopSec3s, KillMode=control-group, a22-second launcher deadline and
a12-second child-test deadline. Captured output and attempted allocations,
forks and file creation were bounded independently. Unprivileged CPU/permission
children verified cgroup membership before and after; memory pressure necessarily
ends at the observed OOM kill rather than a post-exit child assertion.

The successful cleanup used only the two initially absent fixtures. It confirmed
stopped processes and exact rendered units before removing their private runtime,
home and identities. Quota reset used the existing closed production execution
profile from the root test operator, not a quota-reset RPC. An additional
post-check confirmed both homes, runtime directories, account slice files and
passwd entries were absent. The exact OOM transient was confirmed failed and
inactive before resetting only its failed state; afterward it was not loaded.

## Failed harness attempts retained honestly

1. `cgroup-pressure-b57ff51-v1` passed CPU throttling and the hosting-UID open
   checks, but PID1 rejected direct `User=` startup for subordinate UID3274524199
   with217/USER before the test executable ran. Its exact-unit diagnostic log
   reported failure to resolve user credentials. The revised test uses only the
   trusted fixed `setpriv` executable to drop to the exact numeric subordinate
   UID/GID, clear groups and set no-new-privileges before test code starts.
   It creates no synthetic passwd entry and changes no product mapping. The
   `cgroup-pressure-b57ff51-v2` run passed with fresh UIDs249952/249953, then
   removed those successful fixtures.
2. `resource-pressure-b57ff51-v1` passed CPU, access and PID checks and actually
   triggered the intended account OOM kill, but incorrectly expected the
   `systemd-run` process to return shell-style137. A bounded reproduction and
   [systemd257 source](https://github.com/systemd/systemd/blob/v257/src/run/run.c)
   confirmed that `oom-kill` yields1. The final helper now requires1 **plus** the
   child's pre-allocation marker, exact retained transient OOM/SIGKILL properties,
   clean bounded output completion and the kernel account event increase. It
   does not accept an arbitrary exit1 as a successful memory test.

The failed fixtures249950/249951 and249954/249955 remain for diagnosis; the final
successful test never adopted or removed them. This is not a clean-host result.
Historical failures and older OCI fixtures remain intact. No product protection
was weakened to obtain a pass.
After the final post-check, the identified VM was shut down gracefully and
verified off, restoring its initial power state and preserving its diagnostic disk.

## Limits of this evidence

No controller value or PID migration was written during the permission checks.
They demonstrate open denial, not an attempted migration syscall. The mapped-UID
child is a real unprivileged numeric host identity, **not an actual container
namespace**. The preceding separate OCI diagnostic covered actual rootless
container execution; neither test is a broad container-escape audit.

These checks do not qualify I/O throughput/IOPS limits, monthly traffic limits,
aggregate disk reservations, public API/browser workflows, external IPv6/WAF/cache,
an authenticated candidate's normal reboot, failure recovery or full OS removal.
Refer to the [cgroup-v2 delegation and CPU interface contract](https://docs.kernel.org/admin-guide/cgroup-v2.html)
for the distinction between parent limit enforcement and writable delegation.

## Artifact identity

Diagnostic sources, ELFs and raw logs are retained in ignored local work
directories; they are not published release assets.

| Artifact | SHA-256 |
| --- | --- |
| Exact source TAR | `289d455739aa365cf82a4faa010fa813a9af19202d26e9a69f7d455672d5ebcf` |
| Initial cgroup test ELF | `28bf068183c7ed3ed9ba03a6dd72e801678bea489399bafd69e4d8f31a7f6cc2` |
| Initial cgroup failure log | `34d061c7cf4848c6a35abc22be2f3d1f88dd3603dd3a30965736b648564b7314` |
| Corrected cgroup test ELF | `15fff3f2672c4255e0236c0015824787431281a8c7be4b8cee814a5ea8a28559` |
| Corrected cgroup pass log | `a268631ffda7dbd75b5cd1197c1d167b3c7f5a630067226642eccf7d6c606066` |
| Initial resource test ELF | `1bed0cebd5d95a8ac04ad7165da08a3894be3a6986e103350b47d767f786147c` |
| Initial resource failure log | `a4815bda62a6296b283cb55e9c05b3bf152edfac5d3fd29f4b24d178869c8149` |
| Final resource test ELF | `b12375f4632dbeb3ba4616a1bad0dc847dc20efd48dfbbef7334bee47236e28e` |
| Final resource pass log | `e91348236f0379745692e05d40764bf9aac4a07d955773aa6d697e4356c39011` |
| Final cgroup pressure overlay | `87436e7e585a40d5fa68df08af8b8a3b9b7918b44e3b089229e47340b8956263` |
| Final cgroup contract overlay | `fa2d9a44135350a4ef5a18d6c63280221a0a23d34afbc788e97c4a9db0cb26e2` |
| Final resource pressure overlay | `ba28c7c722d1d4e669cf8122f5ed4ef5321407737101d36028b15c9f36cb5f23` |

GitHub [CI run35436043183](https://github.com/RTBGG/Stackfort/actions/runs/35436043183)
and [Security run35436043190](https://github.com/RTBGG/Stackfort/actions/runs/35436043190)
completed successfully for `dfdb896b9da4e6a9b173b4d024ca43baa0e26eea`, whose
product code is identical to the tested implementation. This is not a claim
that all repository security alerts or final release qualification are complete.

The next step is to freeze/build the corrected immutable candidate, then repeat
these checks alongside the full native install/rerun/reboot/security/removal
sequence on fresh OS media. Exact-candidate publication approval remains separate.
