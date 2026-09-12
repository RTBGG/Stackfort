# Native whole-system rescue and restore — 2026-09-11

Result: **a complete pre-installation disk was restored from an external-to-guest
backup using a separate rescue OS; offline verification and two normal EFI Secure
Boot starts passed**. The original VM and all its checkpoints were preserved.
No production installer change, public activation, commit, push or release.

See the [recovery model and limitations](../../../docs/native-installer-whole-disk-recovery.md)
and [machine-readable evidence](2026-09-11-native-whole-disk-restore.json).

## Scope and identities

| Role | Identity |
| --- | --- |
| Original VM, kept off throughout | `stackfort-native-quota-debian-13`, `4361f439-15e9-4f9e-a690-9a8e44b6cbd3` |
| Offline pre-conversion source checkpoint | `native-quota-before-conversion`, `9e62e0ac-89ad-4851-8ae2-e6b936e40eae` |
| New rescue / restored-boot VM | `stackfort-native-restore-rescue`, `ba098b0e-82fe-4293-9343-b2584253ccb4` |
| Rescue / restored VM DMI | `3fea2402-cf8a-49af-aede-c1c6eb1159ef` |
| New replacement VHD identifier | `1636e52b-b559-462a-9ac4-2c1db3eef871` |
| Observed replacement WWN | `naa.600224802be5361659b52c1db3eef871` |
| Independent rescue root UUID | `76e5250b-18db-43d8-be18-e4c8b24c0bbb` |
| Recovered original root UUID | `a88eaa57-e875-4855-a3cb-c231758653f8` |

The rescue OS came from the previously qualified Debian image, independently
pinned by SHA-256. It installed its signed-distribution rescue prerequisites and
masked udisks2/autofs before attaching the replacement. The target was not the
rescue root, and the two root filesystems had different UUIDs.

Local lab/evidence root:
`infra/host-tests/work/native-disk-restore-afd7a865f8474ddb8e205af9d82d0aab/`.
Full images and VM files remain ignored local artifacts; they must not be published
because a complete disk backup includes OS/SSH identity and potentially secrets.

## Backup, rescue and restore

1. `Convert-VHD` exported the known offline pre-conversion disk to a standalone
   dynamic VHDX. The source file's before/after hash remained unchanged. No
   checkpoint was applied to the original VM and no source disk was attached to
   rescue. The authoritative Windows-side backup was made read-only and hashed.
2. The first transfer exceeded the rescue guest's 2 GiB RAM-backed `/tmp`.
   The incomplete copy was preserved as `/var/tmp/native-restore-transfer-failed.vhdx`.
   A new transfer to disk-backed storage succeeded and matched the authoritative
   backup hash. The source backup was never changed or replaced.
3. `qemu-img` converted the transferred backup to a sparse raw file and compared
   their complete logical contents successfully. The raw hash was captured before
   restore and pinned into the qualification. The backup was inspected through
   an explicitly read-only loop device, without mounting any source filesystem.
4. The target passed DMI/WWN/SCSI-slot/type/geometry checks and a full 50 GiB zero
   scan. Mounted targets/partitions, holders and active swap were excluded. A
   one-shot started record preceded mutation. A foreign WWN was rejected.
5. Restore processed **53,687,091,200 logical bytes**. It wrote 2,536,505,344 bytes
   and skipped 51,150,585,856 bytes only after proving those target ranges zero.
   The copied-stream digest, synchronized full-target readback and source hash
   matched. Re-admitting the restored target as an empty disk was rejected.
6. Source and replacement root/EFI filesystems passed `e2fsck -f -n` and
   `fsck.fat -n`. Eight files matched independently extracted source hashes.
   Complete disk/source hashes remained unchanged by inspection; the source loop
   was detached. No repair, journal reset, feature clearing or native retry occurred.

The restore test passed in **107.45 seconds**, including full scans/hash checks;
the additional offline inspection passed in **47.65 seconds**. These are test
durations, not a disk-throughput benchmark.

## Partition and boot verification

The complete readback includes both GPT copies and the EFI filesystem.
GPT disk ID: `3840ad31-d156-4923-a9b8-1dab6550cc95`, 512-byte sectors.

| Partition | Start sector | Sector count | PARTUUID |
| --- | ---: | ---: | --- |
| ext4 root, 1 | 262144 | 104595423 | `54964bdf-2add-41b7-b41e-6d483962d021` |
| BIOS boot, 14 | 2048 | 6144 | `80c6f848-7cb1-485e-9670-248386dfbb02` |
| EFI system, 15 | 8192 | 253952 | `1571ce10-020a-40ee-b41a-6cc127239fac` |

After normal rescue shutdown, only the replacement and a separately exported
copy of the original cloud-init seed were attached for boot. The independent
rescue system and authoritative backup remained detached. Hyper-V selected the
replacement as first boot device with the Microsoft UEFI CA Secure Boot template.
The original VM remained off while its restored clone used the same OS/network
identity. The clone carries an explicit warning not to run both concurrently.

| Boot | Boot ID | Root / EFI device |
| --- | --- | --- |
| Initial restored-system start | `7ba93986-258d-41a0-b148-8efcc95210a0` | `/dev/sda1` / `/dev/sda15` |
| Ordinary subsequent reboot | `b821e23d-5daf-41bf-97fa-5e375fe843de` | `/dev/sdb1` / `/dev/sdb15` |

Both boots passed WWN-based target discovery despite the enumeration change,
root/EFI mount and partition identity checks, systemd as PID 1, enabled EFI Secure
Boot, kernel `6.12.107+deb13-cloud-amd64` and all eight file pins. These pins cover
fstab, machine ID, hostname, SSH public host key, main GRUB config/environment,
kernel and initrd. No native journal, supplementary GRUB entry, native kernel
token, quota/project feature or `prjquota` fstab change appeared.

The first boot evidence was archived under a different filename before running
the second assertion; no result was overwritten. This is ordinary pre-installation
Debian recovery, not admission of an old armed operation under a new DMI identity.

## Artifact hashes

| Artifact | SHA-256 |
| --- | --- |
| Original checkpoint source VHDX file | `a30939d0176771ad9d3df8ddeb99c74954b4ed4f443372cbbdd1cd542fa686ff` |
| Standalone external-to-guest backup VHDX | `cdbbffa97c09585277d2c408cf223705a09b98f4472a2191ddc933f5a695aaad` |
| Complete raw backup / restored pre-boot disk | `eb2eea722d0dfd8926e0e017e6eb8ee9c47d350ce61ac544f64220df69b6e32e` |
| Independent rescue base VHDX | `7b44fcab32c87643542d600ba04837dcacfa554414d9bc0983cbb776dc396711` |
| Exported original cloud-init seed | `ebbe29a7c2e117a5282080d57d19a45f6698c27ce980aac5659cf5a1c8dcb1d9` |
| Restore integration helper | `33bbb353744815e5d4c2b52c7206f794969af5b2fb4645a995397530888febcd` |
| Additive offline / boot integration helper | `827229e50baaf78d395bf807fb183656fb0cd3630f2f3397d45a82e44199f398` |
| Linux blank-copy unit helper | `84c4adeb71cfd94f118a5101149bceb11b2be616af14c3cc8c9d877bf5cec828` |
| Restore test log | `9d8fa57a9055300c523344a50d0d1e690c0dd584c19554902050e867902d9254` |
| Offline inspection test log | `40e6648d690d208a0498dcdd029c868bdf8c96e60789e666c71fa5683ea190ab` |
| First restored boot test log | `bb6be83b6a8bcf9b1f0f8b94d23a76f1859fcf927cdda5139ec3b31800454b15` |
| Normal reboot test log | `c70fec23685913d496774eee3fa7240fdf40e1dc73fade1d3d3b498203e936c6` |
| Offline restore evidence archive | `2c2fa89736d613e10af9cf94f03b486008090d1f8094e48abbffdbe884fc3cc6` |
| Both-boot evidence archive | `2a898eb0a7e684d7ad596a8baa1fbd376488ffd4b175c07958d3116d58972d63` |
| Repository evidence JSON | `9d263178411a796b6874fbf1009090dc376318117d91f98320b3f988fe14c7ff` |

The mutating restore function was unchanged when the additional offline test was
linked into the second helper. No production executable was changed in this step.

## Regression and final state

The blank-copy helper's Windows/Linux tests pass, including malformed size/hash,
truncated/growing/changed input, short/failed writes and nonblank targets. Its
statement coverage is 100%. The mount/swap policy test and all three Linux host
test stages pass without skips. Windows `go test ./...` / `go vet ./...`,
Linux-targeted integration/helper vet, both PowerShell parsers and diff whitespace
checks pass. Full native install/OCI suites were not rerun because this step
changed test support/documentation only.

All six VMs are off. The new restored clone is retained as
`native-whole-disk-restore-qualified`, checkpoint
`df9fd73b-486e-41df-805c-f7a317ab2e64`. The detached rescue disk, external backup,
failed-transfer artifact and all original successful/interrupted checkpoints remain.

## Remaining public gate

The narrow Debian whole-system recovery experiment is complete. The backup is
outside the affected guest disk but on the **same Windows host/storage**; physical
host loss, arbitrary providers/firmware, other OS/disk layouts and preservation of
post-backup data are not qualified. Disk contents do not include firmware NVRAM.

Public native activation still requires product integration and a defined safe
backup/reprovisioning policy, plus the other roadmap gates (external package/kernel
coordination, capacity policy, listener/firewall coverage, additional OS variants
and signed release qualification). No automatic destructive recovery policy was
chosen or enabled on the user's behalf.
