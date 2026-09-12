# Native write-prefix / tear-image qualification — 2026-09-11

Result: **deterministic scratch-image replay and verified file-image restoration
pass; external rescue and whole-system disk restoration remain open**.
No production installer change, public activation, commit, push or release.

See the [model and recovery boundaries](../../../docs/native-installer-crash-replay.md)
and [machine-readable per-case results](2026-09-11-native-crash-replay.json).

## Fixture and real tools

Only `stackfort-native-quota-debian-13` was operated:
VM ID `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, DMI UUID
`6365bd88-5141-4f15-b3f8-2ba9996baad2`, Debian 13, kernel
`6.12.107+deb13-cloud-amd64`, e2fsprogs 1.47.2, 2 vCPU / 8 GiB RAM.
The normally installed guarded fixture was started without restoring or changing
its system disk. Existing successful/interrupted checkpoints were preserved.

The kernel's existing signed dm-log-writes module was available; no packages,
replacement filesystem tools, initramfs hooks or fault switches were installed.
All four filesystem-tool hashes matched the previously sealed boot intent.
The real dispatcher remained
`204503d56dadb60550d70e6fd7d8561cacae822844958d1b55363a9e5bb79bc2`.

The final run created only new private scratch files below
`/var/tmp/stackfort-crash-lab-4034587384`. The test filesystem was 256 MiB ext4,
4 KiB blocks, 256-byte inodes, two block groups, initially without quota/project.
It contained known text and binary payloads. Its journal/inode tables were fully
initialized before capture; formatting/discard was not part of the write trace.
Neither the test filesystem nor its crash images were mounted.

## Final matrix

The 41-record log includes 16 payload writes, stage marks and flush records.
Actual commands were precheck `e2fsck -f -p`,
`tune2fs -O project,quota -Q prjquota`, then postcheck `e2fsck -f -p`, with raw
device synchronization between stages. Complete replay was byte-identical to
the real converted image and passed read-only fsck and both payload checks.

| Case class | Count |
| --- | ---: |
| Initial baseline | 1 |
| Complete-write boundary | 16 |
| Internal 512-byte sector-prefix boundary | 112 |
| Changed-sector first 256 bytes new | 19 |
| Changed-sector last 256 bytes new | 19 |
| **Total** | **167** |

There were **26 distinct image hashes**, not 167 different corrupt files.
57 cases returned fsck exit 0; 110 returned exit 4. Those 110 cases represent
22 distinct inconsistent byte states. Representative findings included
superblock checksum mismatches and inode-bitmap checksum inconsistencies.
All 167 read-only diagnostics left the full image hash unchanged.

This is expected corruption-detection evidence, not a claim that all images
were repairable or safe to mount. Some partial states appeared clean. No fsck
result was used to rearm the installer or bypass its current-boot proof gate.
The first inconsistent image was retained, never repaired or overwritten.

The final integration run passed in 29.45 seconds. An earlier extended run
also passed: 179 cases / 17 writes / 119 sector cuts / 42 half-sector variants,
28 distinct hashes, under `/var/tmp/stackfort-crash-lab-596817158`.
Recordings may differ because of timestamps and actual tool writes; replay
selection is deterministic for each retained baseline and log. An initial
write-boundary/tear-only run also passed and remains in
`/var/tmp/stackfort-crash-lab-3265732579`.

## Restore rehearsal and negative checks

A complete pre-conversion backup was verified before recording. Restoring it
into a new exclusive regular file reproduced the original full-image checksum,
passed `e2fsck -f -n` and recovered both payloads. The backup and separately saved
damaged evidence retained their original hashes. There was no in-place repair,
feature clearing or automatic conversion retry.

Linux guard tests rejected wrong hashes, actual truncated/corrupted input,
FIFO and symlink input, and existing/symlink destinations. Rejected prevalidation
did not create a destination or modify the backup. Existing files could not be
overwritten by rerunning the restore helper.

All backup and image files were on the **same guest filesystem**. This verifies
a copy/validation procedure, not independent/off-host backup availability,
disaster recovery, whole-disk bootability or an end-user restore command.

## Artifact pins

Final local evidence directory:
`infra/host-tests/work/native-crash-replay-82d486e4afd34024b8f0bcd561da2215/`.
Its `evidence.tar.gz` contains command logs, decoded writes, payloads and raw JSON;
large raw images remain in the private guest directory. The repository JSON is
the same parsed result, normalized with a final newline.

| Artifact | SHA-256 |
| --- | --- |
| Final integration helper | `cdda1314d0abc7c6ff2d77fcb8a7334be3081c531d35f359f33e3f1d156e68a6` |
| Final driver test log | `04ac7951dc3239447867abd7f978fb8ca763ff7c213fc351e1da8a1c531f359e` |
| Final evidence archive | `6b92c8ccd1058beab2a4059b9e65a4402c1a93bb207daea2e8e25a032302206f` |
| Raw guest result JSON | `b94e7b3dca29c3e0529cda715a60dfc9eee2c82672e0660a7fc9560f15c42d60` |
| Normalized repository JSON | `938f60276c0151d075fb9809f986f3d1e2d40a489d4e4b17e3e77a02092037cd` |
| Complete baseline / backup / restored image | `f1dc24f5d534ddc68d81bbbbb0441b6f4eb450656efcd6ba3f8be5ce2f18ded9` |
| Real converted image / full replay | `e11fa73ef110fcf93269b6ab0444641e55f771d14ad5d6561d5fb130eb15824d` |
| Preserved first inconsistent image | `f652d8d74b15e99635cfdfe68a0ec76a48b5a92f99b3ba9ed3e15f959e71b8b6` |
| 64 MiB raw write-log file | `41245fb4c138272351c97959bb0d8b3a30458a0beeac6f8a647142621433227f` |
| Linux decoder/variant unit-test binary | `29429abd70928656230537db00b77433a6c3c08fc459db295b6ea6b330e5d1bd` |
| Linux unit-test log | `b4e5d78f6087de9f8efb16ae27041a2a3eb54e14619f5d619954f67966111c9b` |

## Regression and final state

- Decoder/variant tests pass on Windows and Linux; statement coverage is 93.0%.
  The bounded decoder fuzz run completed 5,299,581 executions without a failure.
- Both final Linux integration tests pass with no skips. Windows `go test ./...`
  and `go vet ./...`, Linux-targeted integration/decoder vet, PowerShell parsing
  and `git diff --check` pass.
- The actual installer, sealed runtime intent, main GRUB configuration and fstab
  retain their before/after hashes. The native install service remains active
  with `Result=success` before shutdown.
- No dm mappings or loop devices remain. Evidence images are intentionally kept;
  the guest retained approximately 40 GiB available space.
- The VM was shut down normally and preserved as
  `native-crash-replay-qualified-success`
  (`cc30c3ba-3468-4af3-8240-4b8e8a35098d`). All VMs are off.

## Remaining gate

This does not enumerate all reorderings, all tear offsets, hardware cache-loss
behavior, 4Kn/other-kernel cases, actual root-size metadata layouts or damaged
bootloader/native-journal combinations. The scratch images are not booted through
the recovery guard; earlier real VM hard-off evidence remains separate.

Point 3 still needs **external rescue, an independently retained whole-system
backup, replacement-disk restore and controlled boot verification**, plus the
operational backup/reprovisioning policy. The public native installer remains
disabled. No unsafe repair/reset shortcut was added to close that gate.
