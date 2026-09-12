# Native ext4 quota provisioning experiment — 2026-09-08

Outcome: **native project quotas can be prepared automatically across a
controlled reboot in the dedicated Debian lab, without manual rescue steps in
the successful path.** A fresh-checkpoint replay of the final prototype passes.
This is the more promising direction after the image experiments, but it is
**not a production installer feature or a release qualification**.

The one-line installer, published release candidate and customer's VPS are
unchanged. The working-tree host execution boundary now supports verified
native-root quota targets and rejects accounting-only native quota state.
See the [design/reproduction guide](../../../docs/native-quota-prototype.md)
and [machine-readable measurements and hashes](2026-09-08-native-quota-prototype.json).

## What passed

| Check | Result |
| --- | --- |
| Wrong-UUID boot injection | OS boots normally; root features/fstab unchanged; hosting consumer blocked |
| Offline native root conversion | Unmounted device, VM/root/partition identities and geometry verified; pre/post checks pass |
| Data/config preservation | Sentinel, root/partition UUIDs, geometry and existing stable features retained; only quota/project features added |
| Automatic post-boot continuation | Matching current-boot evidence, project enforcement, fstab persistence, bind mount and synthetic consumer startup pass |
| Account hard byte/inode quotas and isolation | Passed, including existing CPU/memory/PID regression |
| Rootless OCI resources and deployment lifecycle | Passed |
| Subordinate container UID charged to account quota | Passed; 256-MiB limit stops the write, changing only limit to 320 MiB permits a 16-MiB append |
| Accounting enabled but enforcement disabled | Native quota mutation rejected before account data creation; resume enables enforcement |
| Missing boot evidence, mount loss and repeat resume | Fail-closed behavior and idempotent fstab updates pass |
| Subsequent real reboot | Reports `already-ready`; skips conversion; consumer, quotas and OCI tests pass |
| Final fresh replay without manual fixes | Prepare → arm → reboot → automatic resume → all four account/OCI subtests pass |
| Existing separate hosting-filesystem path | Ext4-image regression passes with the same final binary, including quotas, OCI, bandwidth and IOPS |

The container probe used immutable local image
`881a32046cbec149de5f57d15349042946d52f9aeb49e430146b0c6ee714e7a5`.
Its partial file was 202,670,080 bytes, UID 3,273,867,840 versus account UID
249,940. This is project-tree quota enforcement, not merely a user quota.

The final clean replay used operation
`c7090a1e-752b-43e0-9143-609d91250a59`. Its offline conversion/check test took
about 0.24 seconds and post-boot resume about 0.04 seconds on this mostly empty
lab disk. These exclude reboot/initrd-build time and are not large-disk timing
guarantees. No partition was resized or reformatted.

## Storage measurements

Same new Hyper-V Debian 13 VM: two vCPUs, 8 GiB RAM, 50-GiB system disk and
64-MiB NoCloud seed; no prepared quota disk or loop image. Kernel
`6.12.107+deb13-cloud-amd64`, fio 3.39. Root ext4 was initially quota-free.

Three repetitions per workload before conversion and three afterwards:
18 measurements, initialized 512-MiB files, direct I/O, two-second warm-up,
ten-second measured period. After conversion, the benchmark file inherits a
project with a 1-GiB hard quota. Phases are separated by reboot and validation;
they are **not randomized or simultaneous paired runs**.

Median IOPS:

| Workload | Before native quotas | After native quotas | Change |
| --- | ---: | ---: | ---: |
| Random 4-KiB read, QD16 | 131,814 | 130,897 | −0.7% |
| Random 4-KiB write, QD16 | 241,444 | 222,706 | −7.8% |
| 4-KiB write + fsync each, QD1 | 224.58 | 236.18 | +5.2% |

Observed IOPS ranges:

| Workload | Before | After |
| --- | ---: | ---: |
| Random 4-KiB read, QD16 | 127,226–166,850 | 128,242–169,065 |
| Random 4-KiB write, QD16 | 232,275–243,575 | 210,849–229,495 |
| 4-KiB write + fsync each, QD1 | 220–241 | 233–237 |

Median-of-run fsync-only p99 latency is **6.19 ms before / 6.00 ms after**.
Random-write p99 completion latency is **0.132 / 0.173 ms**. Random-write
guest-wide CPU busy medians are 54.7% / 54.2%; guest CPU includes warm-up,
process startup and background activity.

The roughly halved fsync throughput seen in the prior image experiments is
**not observed in this native before/after run**. The +5.2% fsync median is not
proof that quotas make storage faster; the sample is small and the ranges
overlap. Random-write median remains 7.8% lower, with non-overlapping observed
ranges in this short run. Host/cache/state and before/after ordering prevent
assigning an exact causal overhead from these data alone.

This is not a new simultaneous comparison against the earlier ext4/XFS images
and does not measure HTTP, PHP, WAF or database transactions. No claims about
customer VPS throughput or proportionally faster websites follow.

## Problems found and corrected

1. Debian's exported `ROOT` value retained a `PARTUUID=` specifier. The first
   injected-rejection run stopped before comparing identities. The helper now
   resolves only validated UUID/PARTUUID specifiers; the repeat proves the
   intended wrong-UUID rejection.
2. The initial feature guard rejected the disappearance of `orphan_present`
   during clean unmount. Kernel documentation identifies this as a transient
   mount-state flag. The guard now permits only the known transient changes;
   loss of journaling or `orphan_file` still fails. See the
   [kernel's orphan-file description](https://docs.kernel.org/filesystems/ext4/orphan.html).
3. `setquota /srv/hosting` failed because quota-tools excludes the subtree
   bind mount from its filesystem targets. Host-observed, descriptor-verified
   placement now selects the literal `/` only for eligible native ext4 root
   storage. Separate hosting filesystems retain the old target, and no RPC
   caller can supply a mountpoint or device.
4. Merely remounting with `prjquota` was insufficient in the first converted
   boot: quota accounting was active, but byte and container limits failed.
   Explicit `quotaon -P /` made the same tests pass. The final resumer checks
   the separate kernel accounting/enforcement bits, enables enforcement if
   needed and checks again. The native execution boundary rejects the
   accounting-only state. The generic kernel quota layer exposes both states;
   see its [implementation](https://raw.githubusercontent.com/torvalds/linux/v6.12/fs/quota/quota.c).

Failed stages and their logs remain retained. Their manual diagnostic fixes are
not presented as an unattended success: **the final code was replayed from the
offline pre-conversion checkpoint**, and passed without post-boot intervention.

## Source and evidence

Base commit `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2` plus working-tree
changes. Final replay, resume-safety, reboot, after-benchmark and separate-image
regression test binary SHA-256:

`6ade1cee7a9481837d1b9801e2f1e705895fdfe1eaa4b771560845b6b0f631cc`

The broader debugging sequence used earlier binaries, recorded individually
in the JSON; it is not one immutable release-candidate qualification.

Final clean-conversion evidence:

`infra/host-tests/work/native-quota-Validate-20260908T092818Z/evidence.tar.gz`

SHA-256 `fa3e4c2810f1ac2a7d4cb6bbc2247b75d6908cbd4208f4228bc4f012cb735523`.

Combined before/after raw measurements:

`infra/host-tests/work/native-quota-BenchmarkAfter-20260908T092508Z/evidence.tar.gz`

SHA-256 `abb054ce0a4c06ffd66f5c65cabe49cccc09705ca5ab69a392977d2aeaf78d6e`.

The JSON publishes all samples, medians/ranges, superblock/identity facts,
stage outcomes and binary/log/archive hashes. Raw archives are local and
ignored. Evidence was copied before checkpoint replay; both pre-conversion and
measured-state checkpoints are retained. No unrelated VM was restored.

Linux agent execution tests, quota status ABI/state tests, integration
cross-compilation/vet, the normal Go tests and both summarizer test suites pass.

## Decision and remaining work

Continue with **production design for automatic native quota preparation**,
rather than adopting a loop image as the default. The promising lab outcome
does not justify enabling root conversion silently on existing servers.

Next work: durable installation/reboot state and bounded retries, explicit
fresh-server eligibility, safe kernel/initrd coordination, real service
dependencies, capability/UI reporting of enforcement, and qualification across
ordinary Debian/Ubuntu/Rocky provider images. Native XFS root handling,
LVM/RAID/encryption and provider-controlled boot chains are not covered.

Power-loss recovery, emergency-boot handling and capacity admission control/OS
reserve remain open. A RAM-resident conversion marker is not a crash-safe
journal, and a Hyper-V checkpoint is not a generally available VPS rollback.
Unlike a fixed-size hosting image, native hosting shares OS space; the root
filesystem was deliberately **not** filled during this test.
