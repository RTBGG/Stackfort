# Native conversion: power-loss containment

Status: **internal Debian 13 qualification, not a public recovery guarantee**.
This follows [host eligibility and prerequisites](native-installer-host-eligibility.md).
The new guard prevents an uncertain conversion from automatically proceeding
to root mounting or another conversion attempt after a VM power cut.
It does **not** repair damaged metadata or prove every possible torn write safe.

## Persistent boot guard

The new preparation intent seals `powerLossGuard=true` into its digest.
Historical lab fixtures remain separate; there is no in-place adoption or
upgrade of an already armed operation.

During arming, the supplementary GRUB configuration temporarily makes a
recovery-only entry the default. Only the exact armed, not-yet-consumed operation
selects conversion. The entry consumes that authorization before starting Linux.
Missing, conflicting or consumed authorization selects recovery instead.
Re-selecting the consumed conversion entry also enters recovery; it never falls
back to the normal initrd.

The normal kernel, normal initrd and main GRUB configuration are unchanged.
The temporary default lives in the operation-owned `custom.cfg`, alongside both
entries, and uses the separately sealed initrd. Finalization retires these
temporary artifacts after successful current-boot proof and filesystem checks.

Inside initramfs, before the first `e2fsck -p` or `tune2fs`:

1. The installer verifies the existing release/host/device/boot/receipt bindings,
   an unmounted target and the exact recovery GRUB script.
2. It synchronizes the raw block device and rereads the consumed GRUB environment.
   Failure stops preparation. The same raw-device flush follows each metadata
   command before subsequent progress is acknowledged.
3. A RAM marker additionally guards the current boot, but is no longer the only
   protection. Any error in the guarded premount script remains in initramfs,
   including errors before metadata mutation.

On a recovery-only boot the installer verifies that the target is unmounted,
emits the recovery event and stops. It does not fsck, repair, mount root, start
hosting or issue a new conversion authorization. Losing the RAM success proof
after an otherwise complete conversion is deliberately treated as uncertain.

## Scope of the durability claim

Raw-device synchronization uses Linux block-device `fsync`; see the
[Linux 6.12 block implementation](https://github.com/torvalds/linux/blob/v6.12/block/fops.c).
This relies on the kernel, hypervisor, controller and media honoring their flush
contract. It does not protect against lost/lying flushes, corrupted boot media,
all sector-tear positions, firmware choosing a different boot route, or an
administrator manually bypassing the selected recovery entry.

The GRUB files, kernel and initrd still reside on the root filesystem. If damage
prevents the bootloader from reading them, recovery requires external rescue
media; the installer does not claim that its local recovery image remains
available after arbitrary corruption.

`tune2fs -z`/`e2undo` is intentionally **not** offered as a crash rollback:
the [Debian tune2fs documentation](https://manpages.debian.org/trixie/e2fsprogs/tune2fs.8.en.html)
explicitly excludes power/system crashes from that undo mechanism's guarantees.

## Operator response

The recovery screen is a stop, not a successful installation. Preserve console
output and the operation identity. Do not remove the native journals, re-arm,
force conversion, clear quota features, or automatically replay filesystem tools.

Use an external rescue environment with automount disabled for investigation.
Preserve a block-level copy or a verified provider backup before any repair.
Read-only observations can include device identity, superblock information,
GRUB environment and the sealed installer records; even a nominally read-only
filesystem mount can replay a journal, so it is not the inspection mechanism here.
Any repair/restore/reinstallation decision remains an explicit operator action.
A fresh-server provisioning failure may be resolved by restoring a verified
pre-installation image, but this is not an authorization to erase an existing
server or its data.

The [whole-disk rescue follow-up](native-installer-whole-disk-recovery.md) now
verifies an externally retained pre-conversion backup, replacement-disk restore
and normal boot on a separate Debian fixture. Product integration and an
operational backup/reprovisioning policy are still needed before public activation.
The checkpoints and rescue helpers are lab evidence, not a built-in rollback facility.

## Qualification

`Test-StackfortNativePowerCutHyperVVm.ps1` operates only the fixed disposable
Debian VM. It observes real installer events on a dedicated serial pipe and uses
Hyper-V hard power-off. It never inserts test hooks, pause switches or replacement
filesystem tools into the boot image.

The driver records the trigger and host timestamps, then a separate run verifies
the automatic recovery boot using read-only initramfs diagnostics. It rejects
an ext4 root mount or another conversion-start event and requires the matching
recovery token and consumed operation. Each test restores only the explicitly
identified offline **armed lab checkpoint**, outside the driver.

Serial observation and the hard-stop command have latency. A request following
`quota-start` is **not evidence of a particular interrupted write syscall**.
The observed cut cases had quota features present at recovery, and do not prove
a deliberately torn/partially written quota inode. The separate
[deterministic crash-image qualification](native-installer-crash-replay.md) now
exercises recorded write/sector boundaries and synthetic half-sector tears on
scratch ext4 images. It does not turn these VM cuts into proof of a particular
physical torn write. The separate whole-disk follow-up qualifies one controlled
rescue/restore route, not automatic metadata repair or arbitrary hardware recovery.

See the [dated results and exact artifact pins](../infra/host-tests/results/2026-09-11-native-power-loss.md).
