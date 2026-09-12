# XFS versus ext4 storage images — 2026-09-08

Outcome: **XFS does not demonstrate a useful performance improvement over the
ext4 image in this lab. Neither image configuration is accepted as the silent
production default.** XFS passes the functional checks, but does not remove the
random-I/O and fsync cost seen with the ext4 image. This is not a claim that
native XFS is slower than native ext4.

See the [machine-readable measurements](2026-09-08-storage-xfs-comparison.json),
[reproduction/design guide](../../../docs/storage-image-prototype.md) and
[preceding ext4 experiment](2026-09-08-storage-image-prototype.md).
The production installer, release candidate and customer's VPS are unchanged.

## Method and environment

- Same dedicated Debian 13 Hyper-V VM: 2 vCPUs, 8 GiB RAM, one 50-GiB system
  disk plus a 64-MiB NoCloud seed. No separately prepared hosting disk.
- Kernel `6.12.107+deb13-cloud-amd64`, fio 3.39, Podman 5.4.2. Root UUID,
  filesystem features, mount options and fstab remain unchanged.
- All three backends measured **again in the same run**: native quota-free
  ext4 root, an 8-GiB ext4 image and an 8-GiB XFS image. Image comparisons with
  native root include quota and filesystem/layer differences.
- Both images reserve 8,589,938,688 outer guest bytes, with root-only 0600
  backing files, direct backing I/O and loop discard disabled. Preallocation
  does not reserve physical blocks on a thin-provisioned hypervisor/provider.
- XFS uses `mkfs.xfs -K` and Debian defaults, including CRCs, reflink,
  reverse-mapping trees and a 64-MiB internal log. No journal/barrier is disabled.
- Each initialized 512-MiB benchmark file uses application direct I/O,
  libaio and one job. Image directories inherit project ID 249580 with a 1-GiB
  quota **before** file creation; the native baseline has no project quota.
- Five workloads × three backends × three rounds = **45 measurements**.
  Each has a two-second warm-up and ten-second measured period. Backend order
  rotates native/ext4/XFS, ext4/XFS/native, XFS/native/ext4 across rounds.
- No other tests ran on the VM during the benchmark. The other three Stackfort
  VMs stayed off. The dedicated VM was gracefully powered off after final
  checks; all four VMs were verified off. Images and evidence are retained.

## Random I/O and durable writes

Median IOPS across three runs. Changes use ratios of medians against native
ext4; they are not the median of paired percentage changes.

| Workload | Native ext4 | ext4 image | XFS image | ext4 vs native | XFS vs native |
| --- | ---: | ---: | ---: | ---: | ---: |
| Random 4-KiB read, QD16 | 122,683 | 103,223 | 103,334 | −15.9% | −15.8% |
| Random 4-KiB write, QD16 | 203,230 | 177,171 | 175,275 | −12.8% | −13.8% |
| 4-KiB write + fsync each, QD1 | 263.72 | 131.76 | 123.34 | −50.0% | −53.2% |

Compared directly with the ext4 image, XFS median IOPS change by **+0.1% for
random reads, −1.1% for random writes and −6.4% for fsync writes**. The paired
XFS/ext4 fsync changes range from −25.8% to +14.7%, so the exact −6.4% median
difference is not a stable ranking. Both images are below native throughput
in **every paired random-read, random-write and fsync run**.

The small sample and spread do not support a statistical-significance claim:

| Workload | Native IOPS range | ext4 image IOPS range | XFS image IOPS range |
| --- | ---: | ---: | ---: |
| Random 4-KiB read, QD16 | 114,069–131,439 | 102,239–118,342 | 102,665–104,974 |
| Random 4-KiB write, QD16 | 145,914–238,227 | 138,553–178,969 | 128,002–181,069 |
| 4-KiB write + fsync each, QD1 | 250–282 | 119–139 | 103–137 |

Median-of-run p99 **fsync-only** latency is 5.14 ms native, 10.55 ms ext4 image
and 15.66 ms XFS image. These values come from fio's `sync.lat_ns`, not the
separate write-completion latency printed in the test log.

For random writes, median guest-wide CPU busy time is 51.5% native, 57.4%
ext4 image and 57.4% XFS image despite lower image throughput. This CPU sample
includes warm-up/startup and guest background activity; it is not a per-process
CPU measure. Median-of-run p99 random-write completion latency is 0.165,
0.214 and 0.198 ms respectively.

## Sequential results: too variable for a ranking

MiB/s, median followed by observed minimum–maximum:

| Workload | Native ext4 | ext4 image | XFS image |
| --- | ---: | ---: | ---: |
| Sequential 1-MiB read | 2,368 (1,581–11,715) | 2,479 (1,453–11,703) | 2,574 (2,320–11,720) |
| Sequential 1-MiB write | 3,058 (2,307–9,080) | 2,992 (640–9,102) | 2,919 (2,087–3,117) |

For example, XFS/ext4 paired sequential-write differences span −77.1% to
+356.8%. Backend order rotation reduces order bias but does not eliminate
changing host/cache/storage state. Do not interpret the similar medians or
multi-GiB/s peaks as equivalent performance or a VPS throughput guarantee.

## Functional results

| Check | Result |
| --- | --- |
| Exclusive XFS creation, reservation, exact backing file/UUID | Passed |
| Project quota accounting **and enforcement** | Passed |
| Account byte/inode limits, isolation, CPU/memory/PID regressions | Passed |
| Rootless OCI private resources and complete deployment lifecycle | Passed |
| Container writes with subordinate UID charged to account project | Passed, including causal recovery after raising only the account limit |
| Bandwidth limits, direct and buffered fresh files, loop/outer device | Passed: 3.57–3.65 MiB/s wall-inclusive at 4-MiB/s limit |
| Direct random IOPS limit | Passed: 499.80 read / 499.60 write at configured 500 |
| Explicit trim, missing mount/image, safe restart refusal | Passed before and after reboot |
| Full XFS image | Actual write ENOSPC with 6 blocks (24 KiB) reported available; outer root remains writable |
| Actual reboot and persisted sentinel | Passed; both consumers checked before manual safety-test restarts, XFS account quotas revalidated |
| Updated shared tests rerun on ext4 | Passed: full image, subordinate-UID quota/recovery, sentinel/root integrity after reboot |

The first XFS validation failed two **ext4-specific test assumptions**. The
container write returned “No space left on device” at its project limit despite
ample pool space; the full-pool test retained six reported available blocks.
The failure log is retained, not discarded. Tests were strengthened to prove:

1. The pool still has at least 1 GiB free at account-limit failure; after raising
   only that account's quota from 256 to 320 MiB, the same subordinate UID can
   append exactly 16 MiB. XFS's partial file was 203,423,744 bytes, UID
   3,273,867,840 versus account UID 249,940.
2. Full-pool failure is an actual ENOSPC write with at most 1 MiB remaining for
   XFS allocation reservations. ext4 retains its zero-available-block check.
   XFS filled 8,325,681,152 bytes; ext4 regression filled 8,331,350,016 bytes.

XFS's documented allocation reservations explain why apparent free blocks do
not ensure another allocation can succeed; the exact container error above is
an experimental observation, not a universal errno guarantee.
See [Debian's XFS quota caveats](https://manpages.debian.org/trixie/xfsprogs/xfs_quota.8.en.html).

Production reconcilers were not modified. Existing tests temporarily use an XFS
bind mount at `/srv/hosting`, then restore the ext4 mount. Both original mounts,
active consumers and backing DIO=1 were verified before shutdown.

## Evidence and verification

Source base: `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2` plus working-tree
experimental tests. Final validation, benchmark, reboot and ext4 regression
used binary SHA-256
`edd5589ddf754c313e92c86745c2f3099adcedf850ed318ce2a9b1d8a3abe4e0`.
The persistent XFS guard comes from the earlier provision binary; its guard
logic did not change. This is not one immutable candidate-release qualification.

The JSON records all samples, medians/ranges, paired changes, environment facts
and per-phase binary/log/archive hashes, including the failed first validation.
Benchmark raw JSON is in `results-f91a905b-8cef-4423-8eef-25fe7b18e71c` inside:

`infra/host-tests/work/storage-image-XFSComparison-20260908T083939Z/evidence.tar.gz`

Archive SHA-256:
`ef7dbe490282b6c7e81c79c18c41291471e6e965f460c31fda83cd926079c1f8`.

Raw archives remain local and ignored; no image files or tenant data are
published. Summarizer tests (including legacy format, separate fsync latency,
missing/error measurements), Linux integration `go vet`, cross-compilation
and `go test ./...` pass.

## Decision and open work

Do not switch to XFS as a fix for loop-image overhead. Keep the native
project-quota path as the performance-oriented option, but **ordinary single-disk
VPS onboarding remains unresolved**. This experiment does not justify requiring
the user to do rescue-mode work or silently disabling quotas.

The next storage-design decision must address automatic safe native quota
provisioning versus an explicitly accepted image-backed compatibility tradeoff.
Representative web/PHP/database workloads are needed to establish user-visible
impact; a 53% fsync microbenchmark reduction does **not** mean 53% slower websites.

Before any image backend ships, production-safe provisioning/recovery, real
NGINX/PHP/OCI/agent dependencies, capacity/growth policy, power-loss recovery,
migration/update handling and Ubuntu/Rocky qualification remain open. XFS did
not undergo the preceding ext4 worker-interruption injection. The test consumer
is synthetic, and the I/O probes do not implement the package I/O-limit feature
or qualify shared-inode cross-cgroup writeback.
