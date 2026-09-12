# Native installation: whole-disk rescue and restore qualification

Status: **tested on a dedicated Debian 13 lab fixture; not a public restore command**.
This follows [crash-image replay](native-installer-crash-replay.md) and completes
the narrowly scoped whole-system restore/boot experiment. It does not introduce
automatic filesystem repair, automatic rollback or public native activation.

## What was recovered

The complete 50 GiB system disk from the known, offline **pre-conversion** checkpoint
was exported to a standalone backup outside the affected guest disk. This includes
GPT, the ext4 root filesystem, BIOS boot partition and EFI system partition.
The original VM, its current successful state and interrupted checkpoints were
not restored, booted, overwritten or removed.

A separate VM first booted an independent Debian rescue system. Its root UUID
differed from the backup's root UUID, and automount services were masked before
the empty replacement disk was attached. The source backup was inspected through
a read-only loop mapping; neither its root nor EFI filesystem was mounted.

The rescue test restored onto a **new, empty disk**, verified all 50 GiB, inspected
both filesystems offline, then detached the rescue OS and booted the replacement.
A second ordinary reboot passed as well. The restored system returned to its
pre-installation state: no quota conversion, native journal or conversion token.
This is not a migration of an already armed operation to new hardware.

## Safety and evidence chain

1. The host checks the exact original VM/checkpoint and requires it to be off.
   [Convert-VHD](https://learn.microsoft.com/en-us/powershell/module/hyper-v/convert-vhd?view=windowsserver2025-ps)
   exports a new standalone image without deleting the source. The source hash
   is unchanged, and the backup's SHA-256 is retained outside the guest.
2. The transferred backup must match that hash. Its sparse raw conversion must
   compare equal with [QEMU's image comparison](https://www.qemu.org/docs/master/tools/qemu-img.html)
   and match the independently pinned complete raw-disk hash.
3. Restore admission requires the dedicated rescue hostname/DMI, explicit test
   opt-ins, the exact target WWN, reserved SCSI slot, block-device type, 50 GiB
   geometry and 512-byte logical sectors. Mounted disk/partition targets, block
   holders and swap are rejected. No `/dev/sdX` name alone authorizes a write.
4. The target is opened exclusively and its **entire contents** must be zero.
   A one-shot started record is retained before mutation. Existing/partial targets
   cannot be silently zeroed, overwritten, adopted or retried.
5. All logical backup bytes participate in the copied-stream hash. Zero ranges
   may be skipped only because the complete destination was independently proved
   blank. After `fsync`, the complete restored disk must match the expected hash.
6. Offline ext4/FAT checks are read-only. Eight source/replacement file pins and
   complete disk hashes are compared again; the source loop is detached.
7. The VM is shut down before changing attachments. Only the replacement and the
   separately exported original cloud-init seed are present for the controlled
   boot. Rescue disk and authoritative backup are not attached.

Both boots verified systemd as PID 1, EFI Secure Boot, the expected kernel,
stable disk/partition identities, correct root/EFI mounts and all eight file
pins. Device enumeration changed between boots, which the WWN-based lookup
handled. Normal OS boot naturally changes other disk bytes; the full-disk
equality claim is the **offline pre-boot** readback, not an immutable live system.

## Operational boundaries

The authoritative backup is independent of the guest's virtual system disk but
resides on the same Windows host/storage. It is **not** an off-host disaster
backup and cannot establish survival of physical host/storage loss. The test
also does not qualify other providers, firmware implementations, disk sizes,
4Kn devices, Ubuntu/Rocky recovery, or preservation of changes made after backup.
Firmware/NVRAM is configured by the lab; only disk-resident EFI files are backed up.

A full restore reverts everything to the backup's point in time. On a real server,
replacement selection, repair, restore or provider reinstallation needs explicit
operator authorization. Never clear native journals or mark storage ready merely
because fsck returned success. The tested route recovers the whole pre-installation
system; it does not bypass a consumed native operation's identity/proof checks.

The [backup/reinstallation policy](native-installer-recovery-policy.md) now defines
the disposable-fresh-host and externally verified backup routes, and a read-only
operator command distinguishes failure handoffs without authorizing overwrites.
The internal fresh-disposable choice is now bound before preparation; accepting
external backup evidence and public consent transport remain open. The report
is not a backup validator or an authorization receipt. The normal
one-line-installation goal remains unchanged; exceptional recovery is not a
requirement to manually partition a server during onboarding.

## Lab reproduction and retained state

- `infra/host-tests/New-StackfortNativeRestoreLab.ps1` creates the dedicated rescue
  VM, exports the exact checkpoint and creates a blank replacement. It refuses
  an existing rescue VM/name and never overwrites an earlier lab directory.
- Transfer the pinned backup into `/var/lib/stackfort-rescue-lab` on rescue storage,
  **not `/tmp`** (Debian's RAM-backed `/tmp` may be smaller than the backup).
  Convert/compare the backup, bind the observed WWN to the host-created disk and
  write the private lab plan. The integration fixture pins the exact raw backup.
- Run `TestDisposableNativeWholeDiskRestore`, then
  `TestDisposableNativeWholeDiskOffline` from the Linux integration binary with
  `STACKFORT_DISPOSABLE_HOST_TEST=1` and `STACKFORT_NATIVE_DISK_RESTORE=1`.
  The offline phase expects the reviewed read-only source on `/dev/loop0` and
  produces the boot file pins. Failed one-shot state is not reset automatically.
- Preserve the JSON/log evidence, export the original cloud-init seed separately,
  shut down rescue and invoke `Start-StackfortRestoredDisk.ps1 -LabManifest <lab.json>`.
  Run `TestDisposableNativeRestoredBoot` with its private, externally supplied plan.
  Preserve each boot record before a repeat; never overwrite the first result.

These are pinned qualification fixtures, not a general-purpose recovery product.
The clone intentionally retains the original OS/SSH/network identity. **Never run
it alongside `stackfort-native-quota-debian-13`.** The restored clone, detached
rescue disk, backups and all original checkpoints are retained offline.

See the [dated evidence](../infra/host-tests/results/2026-09-11-native-whole-disk-restore.md)
for exact identities, artifact hashes, timings and boot IDs.
