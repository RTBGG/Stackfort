# Journal-bound native quota boot handoff

Status: **Debian laboratory integration; no production installer activation**.
The [durable state protocol](native-quota-installation-state.md) is now connected
to the offline quota helper through a separate, one-shot GRUB entry. The public
installer still requires prepared hosting storage.

## Implemented lab flow

1. Require the exact disposable Debian VM, the native experiment's fresh-ext4
   guards, GRUB environment storage on the plain root partition, the expected
   one-time selection policy and an unused `/boot/grub/custom.cfg` path.
2. Pin the native intent, helper digest, kernel/initrd digests and main GRUB
   configuration digest in a manifest. By default, the source is explicitly
   `0.0.0-native-boot-lab`, with the test executable as source digest, not an
   attested release archive. The opt-in [release-bound flow](native-quota-release-origin.md)
   instead authenticates and retains the exact candidate, then binds its pin
   and origin to the boot intent. Journal and embedded intent must agree.
3. Persist `arming` under the shared installer lock. Build a separate initrd,
   without regenerating the normal initrd or `grub.cfg`. Stage a supplemental
   menu through the original GRUB configuration's verified loader. Retire the
   build-only hooks so later normal kernel builds cannot embed them.
4. Set an operation-specific GRUB latch and `next_entry`, then persist
   `awaiting-reboot`. The harness requests the reboot explicitly. An interrupted
   `arming` journal cannot authorize conversion even if boot selection was saved.
5. GRUB consumes the selection and records the consumed operation before loading
   the special initrd. The helper independently verifies VM/kernel/root/partition
   identity, geometry, manifest/intent agreement and its executable digest.
   It requires an initramfs root and rejects any mount of the target device,
   including read-only mounts.
6. Fixed read-only `debugfs -D -R 'cat …'` requests read the canonical root
   journal and GRUB environment **without mounting the root filesystem**.
   Require `awaiting-reboot`, the exact plan, a new boot ID, exactly one matching
   kernel token, empty armed/next-entry values and the matching consumed operation.
   These checks precede the first potentially writing filesystem check.
7. Put the RAM emergency marker before `e2fsck -f -p`, which can itself repair
   metadata. Run the existing offline quota conversion and final verification.
   Publish an operation/manifest/current-boot-bound proof only after completion.
   An already quota-ready target is rejected by this one-shot helper.
8. The synthetic systemd resumer advances the journal, verifies proof/artifact
   digests, enables and reads back actual quota enforcement, persists fstab and
   retires the supplemental menu/initrd into lab evidence. The original kernel,
   normal initrd and main GRUB configuration remain byte-identical. The dependent
   bind mount and synthetic hosting consumer start afterward and check native
   hosting placement and actual quota enforcement.
9. A later normal boot uses the original initrd, has no preparation token and
   does not execute the helper. The resumer sees `ready` and checks live state
   rather than reconverting/reconciling storage again.

The consumed GRUB operation remains as evidence. If the consumed supplemental
entry is manually selected while still present, its fallback loads the original
initrd without the preparation token. Successful operations retire the selectable
entry; failed operations keep artifacts for diagnosis.

## Failure boundaries

Invalid journal/manifest/GRUB evidence blocks conversion. Missing success proof
enters terminal `recovery-required`, with hosting blocked. There is no reset or
automatic re-arm operation. A failure after the RAM mutation marker enters the
initramfs emergency path. A later normal boot cannot infer success from quota
superblock flags alone and therefore cannot admit hosting without the protocol.

GRUB's one-time selection requires writable environment storage: Debian notes
that on arrangements such as MDRAID/LVM it may remain selected after reboot.
The lab checks actual consumed state, not merely a successful `grub-reboot`
command. See [Debian's grub-reboot manual](https://manpages.debian.org/trixie/grub2-common/grub-reboot.8.en.html).
The offline reader uses no `-w`, `-n` or mutating request; see the
[debugfs read-only/direct-I/O options](https://manpages.debian.org/trixie/e2fsprogs/debugfs.8.en.html).

This does **not** prove arbitrary power-loss recovery during metadata writes,
durable GRUB write ordering on every controller, rollback of partial filesystem
changes, or support for provider-controlled boot chains. The RAM marker is not
durable. The normal original initrd may run its own filesystem checks; returning
to that boot path is not a guarantee of repairing corruption.

## Reproduction

Use only `stackfort-native-quota-debian-13` and the offline
`native-quota-before-conversion` checkpoint. Preserve evidence before explicitly
restoring that checkpoint; the harness never restores it automatically.

```powershell
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Prepare
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage NormalBoot
```

For each fault case, restore the fresh checkpoint explicitly and use
`Prepare -Mode reject` or `Prepare -Mode lost-proof`, then `Arm`, `Recovery`,
and `RecoveryBoot`. Use one unchanged integration binary throughout a sequence.
The helper refuses a caller that differs from its pinned executable.

The harness retains logs and archives under ignored `infra/host-tests/work`.
Large original/retired initrds are omitted; digests and VM checkpoints remain.
A boot timeout stops the harness: inspect the console instead of re-arming.
See the [dated results](../infra/host-tests/results/2026-09-08-native-journal-boot.md).
This adds no benchmark and does not supersede the earlier fio measurements.

## Before public installation

The subsequent [durable source staging API](native-quota-release-staging.md)
now preserves and rechecks release files independently of bootstrap cleanup.
The [release-origin follow-up](native-quota-release-origin.md) connects it to
this lab's manifest. An explicit [installation follow-up](native-quota-install-continuation.md)
adds a separate real package/service continuation unit after the hosting mount;
the default boot experiment still uses only synthetic consumers.

Replace the testing backend with a reviewed typed implementation and pinned
release staging outside bootstrap temporary paths. Qualify fresh-server/boot
eligibility, interruption during metadata writes, updater/kernel/package locking,
real installer/service continuation and operator recovery. Capacity admission/OS
reserve, enforcement reporting, browser setup and ordinary Debian/Ubuntu/Rocky
provider-image qualification remain open. Production entry points still reject
every native preparation journal, including `ready`.
