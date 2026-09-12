# Single-disk storage image experiment

Status: experimental, **not a production installer backend**. The current release
candidate still requires a prepared quota-capable hosting filesystem. This work
does not publish a release or relax any existing quota capability gate.

The [Debian experiment results](../infra/host-tests/results/2026-09-08-storage-image-prototype.md)
show functional feasibility but significant random-write/fsync overhead. The
ext4 image is not accepted as the production default. The subsequent
[three-way XFS/ext4/native comparison](../infra/host-tests/results/2026-09-08-storage-xfs-comparison.md)
passes XFS functional/reboot checks but does not demonstrate an XFS performance
advantage: both images retain material random-I/O and fsync overhead.

The [native quota follow-up](native-quota-prototype.md) investigates offline
root preparation with automatic reboot/resume, without a loop image. Its Debian
lab replay passes, but it also remains outside the production installer.

## Product requirement

A fresh supported full-VM VPS with sufficient resources should be installable
with the one-line bootstrap and browser setup, without rescue mode, partition
editing, or manual filesystem-feature changes. The existing three-OS installer
fixtures have a separately prepared quota disk; passing those fixtures did not
prove this ordinary single-disk onboarding path.

The experiment provisions one shared, preallocated ext4 image with project
quotas at `/srv/hosting`, using ordinary ext4 root storage. Account project IDs,
hard byte/inode quotas and descriptor-relative layout checks remain unchanged.
It is not one image or VM per account. Native quota-capable storage remains the
direct-storage option.

## Scope and safeguards

- Runs only with both disposable-host opt-ins and the exact hostname
  `stackfort-storage-prototype-debian-13`.
- Refuses existing state, hosting directories and mount/consumer unit files.
- Allocates an 8-GiB image and requires another 8 GiB of available outer space.
  These are **lab sizes**, not a production capacity-allocation policy.
- Uses exclusive creation, a root-only state directory, a UUID-bound stage
  record and fsynced state transitions. Injected process exits cover allocation
  and completed formatting before the journal update. An uncertain interrupted
  format is not automatically repeated.
- Preserves the root filesystem's feature set, mount options and `/etc/fstab`.
- Formats with `nodiscard` and without lazy journal/inode initialization;
  verifies actual allocated guest filesystem blocks after formatting.
- Uses backing-file direct I/O, and disables discard on the exact loop queue.
  Mount option `nodiscard` alone does **not** prevent explicit `fstrim` from
  punching holes in the backing file. The guard verifies both settings.
- A synthetic systemd consumer is bound to the mount and verifies its backing
  file, UUID, quota mount option and reservation before starting. This is **not
  yet wiring for real NGINX, PHP, OCI, updater or agent services**.

The test-only implementation is not hardened enough to install on a customer's
server. Production adoption would need reviewed descriptor-based provisioning,
locking, complete interruption recovery, fsck/boot ordering, real service
dependencies, safe capacity growth, monitoring, migration/update handling and
qualification across all supported distributions. A loop image does not remove
the need for backups or guarantee an outer filesystem can never become full.

Root integrity is bound to the filesystem UUID, not the unstable `sdX` device
name. The reboot harness also requires a changed kernel boot ID and retransfers
the test executable after reboot because Debian may clear `/tmp`.

## XFS follow-up comparison

The same dedicated Debian VM can additionally hold an 8-GiB XFS image at
`/srv/stackfortxfs`. Exclusive creation and `mkfs.xfs -K` preserve the outer
allocation. Debian's default XFS features remain enabled: CRCs, reflink,
reverse-mapping trees and the internal log. Both image backends use backing
direct I/O and suppress loop-queue discard. No journaling or write barrier is
disabled for a benchmark.

The XFS test consumer checks the exact image/UUID and **both quota accounting
and enforcement**, not just a mount option. For the existing production
reconciler tests, the harness temporarily stops the ext4 mount and binds the XFS
root at `/srv/hosting`. Cleanup restores the original ext4 mount and consumer;
neither image is reformatted or copied. The production installer is not run.

After provisioning the ext4 experiment, run:

```powershell
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage XFSProvision
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage XFSValidate
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage XFSComparison
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage XFSReboot
```

`XFSProvision` is single-use and refuses existing paths. The comparison reruns
**native ext4, ext4 image and XFS image** together, rather than comparing a new
XFS result with an old ext4 run. It retains the five fio workloads, 512-MiB
initialized files, two-second warm-up and ten-second measurements. Three rounds
rotate the order so every backend occupies each position once. Each image's
benchmark directory has an inherited project ID and a 1-GiB limit before the
file is created. Native root remains quota-free.

Reboot stages verify automatic consumer startup **before** invoking safety
tests that manually restart the consumer. This prevents test declaration order
from concealing a boot dependency failure.

```sh
node infra/host-tests/summarize-storage-image.mjs /path/to/comparison/results-UUID --xfs
node --test infra/host-tests/summarize-storage-image.test.mjs
```

Do not infer an exhausted pool solely from an application's `ENOSPC` message:
the XFS container quota test observed that message with ample pool space. The
test now requires an attempted write over the account limit, ample free pool
space, and successful append by the **same subordinate UID after raising only
the account quota**. Also, a full XFS pool can reject a write while a few blocks
remain unavailable for the next allocation's metadata reservation. The full
test requires a real `ENOSPC` write and at most 1 MiB remaining for XFS, while
keeping ext4's observed zero-available-block requirement.

## Reproduce on Windows Hyper-V

Use an elevated PowerShell session in the repository. Existing VMs are not
restored, deleted or repurposed. A separate small NoCloud seed disk only supplies
boot configuration; there is no dedicated hosting/quota data disk.

```powershell
./infra/host-tests/New-StackfortHyperVVm.ps1 -ImageId debian-13 `
  -VmName stackfort-storage-prototype-debian-13 -SingleDisk `
  -SystemDiskSizeBytes 50GB -MemoryStartupBytes 8GB -ProcessorCount 2 `
  -ImageCacheRoot ./infra/host-tests/cache/storage-image-fresh

./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage Provision
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage Benchmark
./infra/host-tests/Test-StackfortStorageImageHyperVVm.ps1 -Stage Reboot
```

`Provision` is intentionally single-use. Other stages collect separate logs,
test-binary hashes and raw JSON archives under ignored `infra/host-tests/work`.
Do not run other workloads on the VM during `Benchmark`. The harness leaves
the VM available for inspection; shut down the disposable guest afterwards.

## Measurement boundary

The fio matrix compares the ordinary **quota-free native root** against the
loop-backed, project-quota-enabled image on the same guest/system disk. It is
not a same-features native-project-quota comparison. Both use initialized
512-MiB files, application direct I/O, one job, a two-second warm-up and ten
seconds per measurement. Three paired repetitions alternate backend order.
Workloads are sequential 1-MiB read/write, random 4-KiB read/write (queue depth
16), and 4-KiB writes with fsync after each write (queue depth 1).

Raw fio JSON retains bandwidth, IOPS and latency distributions (including the
separate fsync latency). Guest-wide CPU busy time is sampled separately because
fio process CPU excludes loop workers. It includes warm-up/startup and any
background guest activity. Hyper-V/host caches remain in the storage path; these
are comparative lab measurements, not advertised VPS throughput or web/WAF
benchmarks. Report repeated-run spread rather than treating the best run as a
performance guarantee.

To derive statistics from the extracted benchmark directory:

```sh
node infra/host-tests/summarize-storage-image.mjs /path/to/extracted/results-UUID
```

This reports all three samples, medians/ranges, paired changes and the correct
separate fsync p99. It does not automatically approve a production backend.

The throttle probes distinguish application direct I/O from buffered writes,
and test both the loop and underlying root device. Fresh files must be dirtied
by the limited cgroup itself. Rewriting an inode dirtied by another cgroup is a
separate attribution problem: Linux tracks writeback ownership per inode and
does not promise correct attribution for simultaneously shared writers.
Bandwidth validation includes the entire process/flush wall time, not just
buffered submission speed. IOPS tests use direct random 4-KiB operations.

## References

- [OpenEBS: loop-backed ext4 project quotas](https://openebs.io/docs/user-guides/local-storage-user-guide/local-pv-hostpath/advanced-operations/ext4-quota/loop-device-ext4-quota)
- [Debian mke2fs: quotas, discard and initialization](https://manpages.debian.org/trixie/e2fsprogs/mke2fs.8.en.html)
- [Debian losetup: backing direct I/O and overlap warnings](https://manpages.debian.org/trixie/mount/losetup.8.en.html)
- [Linux cgroup v2 I/O and writeback ownership](https://docs.kernel.org/admin-guide/cgroup-v2.html)
- [Debian systemd mount units](https://manpages.debian.org/trixie/systemd/systemd.mount.5.en.html)
- [Debian mkfs.xfs options](https://manpages.debian.org/trixie/xfsprogs/mkfs.xfs.8.en.html)
- [XFS quota semantics and allocation reservations](https://manpages.debian.org/trixie/xfsprogs/xfs_quota.8.en.html)
