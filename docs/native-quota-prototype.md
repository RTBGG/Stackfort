# Automatic native ext4 quota experiment

Status: Debian laboratory prototype, **not a production installer backend**.
The one-line installer and published release candidate have not gained automatic
root conversion. See the [test report](../infra/host-tests/results/2026-09-08-native-quota-prototype.md).

## Purpose

The preceding [image experiments](storage-image-prototype.md) preserved the root
filesystem configuration but added material random-I/O/fsync overhead. This
alternative keeps hosting data on the existing native ext4 filesystem, with
project quotas covering the complete account tree, including container UIDs.
It requires a controlled reboot to change an **unmounted** root filesystem.

The intended user experience remains installation on a fresh ordinary VPS,
followed by browser setup. A required reboot can be explained and coordinated
by an installer; manual partition editing or rescue-mode preparation should not
be the normal workflow. Exceptional filesystem corruption still needs recovery.

## Lab scope and state transitions

The harness only accepts `stackfort-native-quota-debian-13`, two virtual disks
(system plus NoCloud seed), and a named offline Hyper-V recovery checkpoint.
It neither targets the customer's VPS nor modifies the earlier image-test VM.

1. **Prepare:** reject existing lab paths/services, unsupported OS/topology,
   existing quota features, non-256-byte inodes, conflicting fstab entries or
   insufficient free space. Persist root UUID, partition UUID, VM DMI UUID,
   geometry, stable features, operation/boot IDs, original/desired fstab and a
   file sentinel. Preserve the original initrd locally and use a Hyper-V
   checkpoint for lab recovery. No partition resize/reformat is performed.
2. **Arm:** copy a static test executable and bounded manifest into the current
   Debian initramfs. Verify the expected GRUB entry and initrd contents. No
   arbitrary command line, device or shell fragment is taken from a user.
3. **Early boot:** resolve the boot device, match all identities and geometry,
   and reject any mount of that device, including read-only mounts. Require an
   initramfs root. Run offline `e2fsck -f -p`, enable `project,quota` with project
   quota metadata, and check again. Do not use `tune2fs -f`, `e2fsck -y`, disable
   journaling or alter data geometry. A completed conversion is detected and
   skipped on subsequent boots.
4. **Resume:** require successful evidence bound to the current boot and
   operation. Remount with `prjquota`, explicitly enable project enforcement
   when needed, and read back **both accounting and enforcement** from the
   kernel. Persist the fstab option atomically only after successful activation.
5. **Consumer:** mount the lab's native hosting directory at `/srv/hosting`
   using a bind mount, then start a synthetic dependent service. This adds no
   loop device or second filesystem. The consumer stops when its mount stops.

The bind source is `/srv/stackfort-native-hosting`, on the original root device.
It is a lab path, not a committed production layout choice. The production
account path remains `/srv/hosting/accounts/<account UUID>`.

## Failures and important discoveries

- Guard rejection before conversion allows the regular OS boot while leaving
  the hosting consumer blocked. Wrong-UUID injection exercises this path.
- A failure after the metadata-change marker must not silently continue to a
  writable hosting service. The boot wrapper enters recovery instead; this
  emergency branch and unclean power-loss recovery are not qualified here.
- `needs_recovery` and `orphan_present` are transient mount-state flags.
  Their removal at clean unmount is expected; losing `has_journal` or
  `orphan_file` is not. Feature-delta tests distinguish these cases.
- `setquota` does not accept the native subtree bind mount as its filesystem
  argument. The execution boundary now selects the **literal `/`** only after
  descriptor-based proof that managed hosting storage is on the same native
  ext4 root, with valid mount evidence and kernel-confirmed enforcement.
  Separate hosting filesystems keep `/srv/hosting`. RPC callers cannot select
  a mountpoint/device, and there is no retry with a broader target after errors.
- Mount options and `quotaon -p` alone were insufficient in the first converted
  boot: accounting was on while limits were not enforced. `quotastate` uses
  read-only `quotactl_fd(Q_XGETQSTATV)` to distinguish these states. This ABI
  query is supported by Linux's generic quota layer, including ext4.
- Missing current-boot evidence and accounting-only state must reject hosting
  startup/quota mutations. Repeating resume must not duplicate fstab entries.

## Reproduction and evidence

Use the existing Hyper-V image harness to create a **new** Debian 13 VM with
the exact lab name, `-SingleDisk`, 50 GiB system storage, 8 GiB RAM and two CPUs.
Prepare the same fio/quota/Podman dependencies as the image experiment, mask
the rootful Podman service/socket, gracefully shut down, and create the
`native-quota-before-conversion` checkpoint while off. The native harness
refuses to proceed without that checkpoint and never restores it automatically.

From elevated PowerShell at the repository root:

```powershell
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage Prepare
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage ArmReject
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage CheckReject
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage BenchmarkBefore
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage Safety
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage Reboot
./infra/host-tests/Test-StackfortNativeQuotaHyperVVm.ps1 -Stage BenchmarkAfter
```

`Prepare` and each benchmark phase refuse existing evidence/data paths. Arm
refreshes the embedded executable; immutable hashes are retained per stage.
`CheckReject`, `Validate` and `Reboot` request real reboots and verify a changed
boot ID. `ValidateCurrent` runs tests without reboot, for diagnosis only.
The harness captures logs and raw evidence under ignored `infra/host-tests/work`
even when tests fail. Shut down the lab after testing; retain evidence before
any deliberate restoration of its checkpoint.

```sh
node infra/host-tests/summarize-native-quota.mjs /path/to/extracted/evidence
node --test infra/host-tests/summarize-native-quota.test.mjs
```

The before/after fio phases use initialized 512-MiB files, direct I/O, three
repetitions, two seconds warm-up and ten seconds measurement per workload.
Random 4-KiB read/write use QD16; writes with fsync each operation use QD1.
After conversion, the file inherits a project with a 1-GiB hard limit before
creation. The phases are separated by reboot/validation and are **not randomized
or simultaneous paired runs**. Host caches/state still affect the results.
They do not measure website/WAF performance or prove zero quota overhead.

## Before production adoption

The follow-up [installation state protocol](native-quota-installation-state.md)
now supplies durable control-plane state, a one-shot staging/resume boundary and
production entry-point guards. It is deliberately not wired into this lab's
original boot helper and does not qualify power-loss recovery of offline conversion.
The separate [one-shot boot integration](native-quota-boot-handoff.md) now connects
that protocol to this helper in a dedicated Debian lab flow, without enabling
the public installer.

Require an approved fresh-server policy, unsupported-topology detection,
installation locking, a durable one-shot/retry protocol, kernel/initrd update
coordination, bootloader and storage-encryption variants, capacity admission
control/OS reserve, real service dependencies and failure monitoring. Extend
capability reporting to distinguish accounting from enforcement, not just the
native mutation/startup gates tested here. Qualify ordinary Debian, Ubuntu and
Rocky images, unclean interruption/recovery, and complete installer/browser setup.

The prototype's in-memory boot marker is not a power-loss-safe operation
journal. A hypervisor checkpoint is useful lab protection, not a universally
available VPS backup or automatic rollback mechanism. Native hosting shares OS
space; this test deliberately does **not** fill the root filesystem.

## Primary references

- [Debian initramfs boot stages](https://manpages.debian.org/trixie/initramfs-tools-core/initramfs-tools.7.en.html)
- [tune2fs project/quota features](https://manpages.debian.org/trixie/e2fsprogs/tune2fs.8.en.html)
- [e2fsck checks and exit codes](https://manpages.debian.org/trixie/e2fsprogs/e2fsck.8.en.html)
- [ext4 orphan-file mount-state flag](https://docs.kernel.org/filesystems/ext4/orphan.html)
- [setquota filesystem arguments](https://manpages.debian.org/trixie/quota/setquota.8.en.html)
- [Linux quota status ABI](https://raw.githubusercontent.com/torvalds/linux/v6.12/include/uapi/linux/dqblk_xfs.h)
- [Linux generic quota status implementation](https://raw.githubusercontent.com/torvalds/linux/v6.12/fs/quota/quota.c)
