# Native conversion: deterministic crash-image qualification

Status: **internal Debian 13 test evidence, not public activation**.
This complements the [real VM power-cut containment tests](native-installer-power-loss.md).
It adds precise disk-image states which serial-triggered shutdown cannot reliably
select. No production installer code, initramfs fault switch, automatic repair or
public restore command is added.

## Recorded conversion and replay

The fixed disposable VM creates a new 256 MiB ext4 image with 4 KiB blocks,
256-byte inodes, an initialized journal, two block groups and two known payloads.
The unconverted image has no quota/project features and is never mounted.
A complete byte-for-byte backup is made and verified first.

The recording image and write-log file use separately owned loop devices.
The Linux [dm-log-writes target](https://www.kernel.org/doc/html/latest/admin-guide/device-mapper/log-writes.html)
records write payloads and flush/FUA information. The same hashed `e2fsck` and
`tune2fs` executables as the boot-qualified installer run with the same precheck,
quota conversion and postcheck arguments and raw-device `fsync` barriers.
Markers bind the trace to these stages. Formatting happens before recording.

A test-only decoder accepts bounded 512-byte-sector v1 logs and rejects unknown
flags (including discard), out-of-range writes, truncation and missing/conflicting
completion markers. Mapping teardown drains the log before decoding. Complete
replay must be **byte-identical** to the actual converted image or the run fails.

The matrix checks the initial state and, for each recorded write:

- The complete-write boundary.
- Every internal 512-byte sector-prefix boundary, including byte-identical states.
- Both 256-byte old/new half-sector directions for every changed sector. Earlier
  sectors in that request are complete; later sectors retain their old contents.

Every case receives offline `e2fsck -f -n` diagnosis with full-image hashes before
and after. Any diagnostic mutation fails the test. Nonzero filesystem diagnoses
are expected evidence, not successful repair or permission to mount. The first
inconsistent image is retained separately. Reports distinguish case counts from
distinct image hashes; many cut positions produce the same bytes. Exceeding the
case bound fails explicitly rather than silently sampling.

Replay states are deterministic for a given baseline/log. Separate recordings
can contain different writes, for example when fsck updates timestamps.

## Verified image restore, with limits

The rehearsal accepts only a complete regular-file backup matching its expected
length and SHA-256. It creates an exclusive new destination, never overwrites
damaged evidence, checks the copied stream, synchronizes the file and parent,
and verifies the destination again. Restoration must match the pre-conversion
bytes, pass read-only fsck and recover both payloads. Backup and damaged evidence
must remain unchanged.

Negative tests reject wrong hashes, corrupted/truncated backups, non-regular or
symlink sources, and existing/symlink destinations. Prevalidation failures cannot
create or change the destination. Failure during copying would retain a partial
destination as failed evidence, never admit it as recovered.

This is a **file-image restore rehearsal**. Its backups reside on the same guest
filesystem. It proves neither an off-host backup nor a provider snapshot,
external rescue boot, partition-table/EFI restoration or boot from a replacement
system disk. No Hyper-V checkpoint is needed to replay these scratch images.

## External recovery gate

An uncertain native operation still stops under the operation-bound boot guard.
A clean fsck result does not replace lost current-boot proof or authorize
rearming, mounting root or starting hosting. Some intermediate states look clean;
others are inconsistent even at complete-write boundaries.

The remaining whole-system recovery qualification must demonstrate:

1. Rescue boot with automount disabled, an independently identified unmounted
   disk, and retention of the native operation/console evidence.
2. A complete pre-installation backup outside the affected storage with trusted
   geometry/hash records. Metadata-only images are not complete data backups;
   see [e2image](https://manpages.debian.org/trixie/e2fsprogs/e2image.8.en.html).
3. Preservation of the damaged disk before explicitly approved repair or restore.
   A current `/dev/sdX` name alone must never select the destination.
4. Restore onto an explicitly selected replacement target, full readback
   verification and independent filesystem/content checks before mounting.
5. Controlled boot of the restored system, checking EFI/GRUB, disk identity,
   installer state and absence of accidental conversion replay.

This image rehearsal qualifies only part of step 4. The subsequent
[whole-disk rescue qualification](native-installer-whole-disk-recovery.md) now
exercises the complete sequence on a separate Debian lab VM, including two
successful restored-system boots. Product integration and the operational
backup/reprovisioning policy remain public-activation gates.
These are failure-recovery requirements, not a proposal for manual partitioning
during the intended normal one-line installation.

## Model boundaries

Replay follows one recorded completion ordering and its prefixes, not every
possible write reordering. Half-sector tears deliberately model weaker-than-512-
byte atomicity; they do not prove that this Hyper-V storage physically produces
such tears. Not covered: every tear offset, lost/lying flushes, corrupt
controllers, 4Kn devices, full root geometry or other OS/kernel variants.
Scratch images contain no actual GRUB/native installer records and are not
booted through the recovery guard. The real boot-stop evidence is separate.

## Reproduction

Explicitly start `stackfort-native-quota-debian-13` in its normally installed,
guarded-fixture state, then run:

```powershell
.\infra\host-tests\Test-StackfortNativeCrashReplayHyperVVm.ps1
```

The driver verifies the exact Hyper-V ID. The guest also requires the expected
hostname, DMI UUID, root and three opt-ins. It creates a new private scratch
directory and owned mappings, detaches the mappings, and retains image evidence.
JSON/log evidence is copied to an ignored local directory. The wrapper does not
start/stop VMs, restore checkpoints, install packages, reformat root or modify
installer/GRUB/fstab records. Shut the VM down separately when finished.

See the [dated results](../infra/host-tests/results/2026-09-11-native-crash-replay.md)
for artifact hashes, counts and the remaining release gate.
