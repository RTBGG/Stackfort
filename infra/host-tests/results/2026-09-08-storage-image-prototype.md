# Single-disk storage image prototype — 2026-09-08

Outcome: **automatic provisioning is feasible; this ext4 loop configuration is
not accepted as the production default on performance evidence**. No production
installer path, release asset, customer server or existing three-OS test VM was
changed by this experiment.

See the [design/reproduction guide](../../../docs/storage-image-prototype.md)
and [machine-readable results](2026-09-08-storage-image-prototype.json).

## Environment and evidence

- New `stackfort-storage-prototype-debian-13` Hyper-V guest: 2 vCPUs, 8 GiB RAM,
  one 50-GiB system disk. The 64-MiB NoCloud seed is not a hosting data disk.
- Vendor Debian 13 cloud image, kernel `6.12.107+deb13-cloud-amd64`, systemd
  `257.13-1~deb13u1`, fio 3.39, Podman 5.4.2. The image checksum is in the JSON.
- Ordinary ext4 root with **no quota/project features**, unchanged throughout.
  The root UUID is `a88eaa57-e875-4855-a3cb-c231758653f8`.
- An 8-GiB ext4 project-quota image at `/srv/hosting`; actual outer allocation
  8,589,938,688 bytes, mode 0600/root. Backing direct I/O is enabled; discard is
  disabled on the loop queue. This reserves blocks inside the guest, not on the
  underlying provider/hypervisor's potentially thin-provisioned physical disk.
- Base source: `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2` plus this experiment's
  working-tree tests. Per-phase test-binary and evidence hashes are recorded;
  this is not a single immutable candidate-release qualification.
- Complete collected raw evidence:
  `infra/host-tests/work/storage-image-Reboot-20260908T081044Z/evidence.tar.gz`,
  SHA-256 `1688fde037b906cc053d3478e690042c72c4d676a6068889c562dc82dfce38ce`.
  Local ignored evidence remains available; the JSON publishes the measurements.

## Functional results

| Check | Result |
| --- | --- |
| Exclusive image creation, preallocation and resume after two process exits | Passed |
| Existing installer read-only preflight on provisioned image | `ready=true`; all three storage checks pass |
| Account byte/inode quota, directory isolation, CPU/memory/PID regression | Passed |
| Rootless OCI private resources and complete deployment lifecycle | Passed |
| Container write with subordinate UID under 256-MiB account quota | EDQUOT; partial file 202,670,080 bytes, UID 3,273,867,840 vs account UID 249,940 |
| Direct and buffered bandwidth limits with files first dirtied by limited cgroup | Passed: approximately 3.58–3.59 MiB/s including process/flush time at 4-MiB/s limit |
| Direct random read/write IOPS limit | Passed: 499.95 read / 496.48 write IOPS at configured 500 |
| Explicit fstrim does not remove preallocation | Passed; discard rejected |
| Stop mount, remove backing image from expected path, attempt restart | Synthetic consumer stops; restart fails safely; underlying hosting directory remains empty |
| Hosting image completely full | 8,331,350,016 bytes allocated, zero available blocks; write gets ENOSPC, outer filesystem remains writable |
| Actual guest reboot | Automatic mount/consumer startup, sentinel persistence and account quotas pass |

The rootless container image resolved to
`sha256:881a32046cbec149de5f57d15349042946d52f9aeb49e430146b0c6ee714e7a5`.
The consumer is a test service, not the panel/NGINX/PHP/OCI service graph.
The existing package I/O-limit feature has not been implemented by these tests;
the probes directly exercise Linux/systemd enforcement capabilities.

## Performance

Medians of three measurements per backend. The baseline is **native ext4 without
quotas**; the image has project quotas. These are storage microbenchmarks, not
HTTP, PHP, WAF or database transaction benchmarks.

| Workload | Native median IOPS | Image median IOPS | Image change |
| --- | ---: | ---: | ---: |
| Random 4-KiB read, QD16 | 119,963 | 105,755 | −11.8% |
| Random 4-KiB write, QD16 | 216,638 | 177,953 | −17.9% |
| 4-KiB write + fsync each operation, QD1 | 245.35 | 123.85 | −49.5% |

For random writes, median guest-wide CPU busy time increased from 52.3% to
60.3%, despite lower throughput. Median-of-run p99 write completion latency
rose from 0.153 to 0.226 ms. For fsync, the corresponding p99 **fsync** latency
rose from 6.59 to 10.68 ms; do not confuse it with fio's separate write latency.

Sequential medians were close (read: 4,786 vs 4,810 MiB/s; write: 2,966 vs
2,980 MiB/s), but the native read results ranged from 2,255 to 11,752 MiB/s.
Host/hypervisor cache and storage state make these numbers unsuitable for a
claim of equivalent sequential performance. Random-write and fsync throughput
were lower in **every paired run**. No claim is made about absolute VPS speeds.

## Findings during implementation

1. The rolling vendor image had changed since the old fixture download. The
   checksum mismatch was retained as a hard stop; a separate cache fetched and
   verified the current image instead of bypassing verification.
2. The initial buffered throttle probe rewrote an inode dirtied by another
   cgroup and escaped the short limit. A native/fresh-file control and matching
   image probes pass. Shared-inode, cross-cgroup writeback attribution remains
   a real operational constraint, not a universally solved limit.
3. The clean VM lacked the installer's masked rootful Podman socket and agent
   state parent. Adding those explicit **test prerequisites**, without altering
   production reconcilers, made the existing OCI tests pass.
4. Debian clears `/tmp` at reboot; the harness now retransfers its test payload
   after reboot and verifies a changed boot ID before running checks.
5. Hyper-V reordered system/seed device names (`sda1`/`sdb1`). Root integrity
   checks now use the stable filesystem UUID. The original record was migrated
   only after independently checking its original UUID and reconstructing the
   exact previous hash of features/options/fstab with the old device name.

## Decision and remaining work

Keep the simple single-disk installation requirement open. Do not ship this
configuration silently as the new default, disable quotas, disable filesystem
journaling, or treat the existing prepared-disk fixtures as ordinary-VPS proof.

Follow-up: the [completed XFS comparison](2026-09-08-storage-xfs-comparison.md)
reran all three backends and found no useful XFS-over-ext4 image advantage.
XFS functional/reboot checks pass, but both images retain material overhead.
Before any backend
becomes an installer default, require production-grade provisioning/recovery,
service dependency wiring, capacity/growth policy, and full Debian/Ubuntu/Rocky
qualification on ordinary single-disk images. Unclean power loss/fsck recovery
and realistic application benchmarks are still outstanding.
