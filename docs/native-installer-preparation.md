# Native installer offline preparation

Status: **internal Debian 13 qualification**, not public native installation.
This follows the [post-ready runtime](native-installer-runtime.md). It moves the
initial preparation, one-shot initramfs conversion, and finalization into the
regular `stackfort-installer` executable. The qualification test only supplies
authenticated inputs, requests the real operations, and checks their results.

## Boot sequence

1. `SourceStage.PrepareNativeBoot` verifies retained release provenance, captures
   the current host/root identity and geometry, and checks the narrow boot layout.
   It now requires the [explicit recovery decision](native-installer-recovery-policy.md)
   for an exactly reviewed fresh disposable host, and durably consumes that
   decision before package or boot preparation. A missing or stale decision stops
   preparation; the backup-preserving mode is not enabled yet.
   The [host/prerequisite follow-up](native-installer-host-eligibility.md) performs
   conservative eligibility checks and any bounded prerequisite installation first.
   It seals the installer, runtime intent, release manifest, and planned journal
   under the shared lock, closes the web gate, and installs service dependencies.
2. Internal `native-boot arm` records the arming attempt before staging anything.
   It builds a **separate** initrd containing the real installer and pinned tools,
   retires build-only hooks, and selects an operation-bound GRUB entry once.
   The normal kernel, initrd and main GRUB configuration are untouched. The
   [power-loss guard](native-installer-power-loss.md) adds a temporary
   recovery-first default in the supplementary menu for new preparation intents.
   Arming does not reboot the machine; the qualification driver requests it.
   [Package guards](native-installer-package-coordination.md) now exclude cooperating
   APT/dpkg writers during preparation's guarded sections and the Arm backend,
   including initramfs construction. Arm also rejects a changed package baseline.
3. Internal `native-boot early` requires an initramfs root and an **unmounted**
   plain GPT/ext4 root partition. It checks UUID/PARTUUID, machine/kernel/boot,
   geometry, installer/tool hashes, the offline journal, consumed GRUB selection,
   release/source/runtime records, the sealed recovery-choice receipt and unchanged
   fstab **before the first write**.
4. Conversion uses `e2fsck -f -p`, then `tune2fs -O project,quota -Q prjquota`, then
   another `e2fsck -f -p`. Only fsck exit codes 0 and 1 are accepted. It never uses
   `-y`, clears features, or repeats a partially/completely converted filesystem.
5. Internal `native-boot finalize` verifies current-boot proof and boot artifact
   hashes before remounting with `prjquota`. It checks real kernel accounting and
   enforcement before persisting fstab, retains completion evidence, and retires
   the selectable menu and one-shot image. Only then can storage become ready.
6. The existing real storage verifier, hosting bind mount, and authenticated
   nine-stage installer/admission run. Subsequent boots only revalidate; they
   cannot rerun offline conversion or silently replay an interrupted operation.

## Files and boundaries

The new `debian-native-offline-qualification-v1` runtime profile includes a typed
offline intent with exact before/after fstab and four executable hashes. It is
bound into the release manifest before storage preparation. The old post-ready
profile and its historical lab driver remain separate regression fixtures.

New boot evidence is retained in `/var/lib/stackfort-installer`, including
`native-boot-artifacts.json`, `native-boot-completed.json`, retired hooks/menu,
and the retired one-shot initrd. The dispatcher remains root-owned mode `0500`;
private JSON is mode `0600`. Existing artifacts, links, partial writes, or foreign
journals cannot be overwritten/adopted. Build hooks do not survive successful
arming and therefore do not enter subsequent normal initrd rebuilds.

The qualification uses the fixed bind source `/srv/stackfort-native-hosting` and
the existing operation-owned TCP/UDP 80/443/8443 gate. No arbitrary device, command,
source, force, reset, or public activation option is introduced. Neither the
one-line bootstrap nor `stackfort-install` forwards `native-boot`.

The initial layout check accepts only the currently exercised Debian 13 plain
GPT/ext4 root with 256-byte inodes, no quota features, GRUB environment on root,
an unused supplementary menu, and at least 8 GiB free. It preserves the cloud
image's `x-systemd.growfs` option but rejects geometry changes at later checks.
This is **not** the final provider-image eligibility or OS-reserve policy.

## Failure behavior and remaining work

A RAM marker is created **before** the first fsck, because even `-p` can repair
metadata. It is removed only after success proof is written and synced. The
[new power-loss guard](native-installer-power-loss.md) additionally retains a
recovery-first boot default, flushes the consumed authorization before metadata
work and stops on every guarded premount error. Pre-write failures no longer
automatically proceed to mount root in this profile. Historical unguarded lab
fixtures retain their old behavior. There is no reset/rearm shortcut.

The marker and proof remain RAM-only; the boot guard does not. **Automatic
metadata repair and full torn-write recovery remain unqualified.**
The [prerequisite follow-up](native-installer-host-eligibility.md)
adds conservative Debian eligibility and prerequisite ordering. Provider-wide
coverage remains open, along with persistent external package/kernel/initrd
coordination beyond the new process-lifetime guards, root capacity admission, externally replaced firewall rules,
arbitrary container-published ports, Ubuntu/Rocky boot variants, or public signed
release qualification. The authenticated beta.3 input archive is unchanged; the
new installer is a separately pinned local qualification artifact, not a newly
published release.

## Reproduce on the disposable VM

Use `infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1` with the explicitly
started `stackfort-native-quota-debian-13` VM restored from its known **offline**
`native-quota-before-conversion` checkpoint. No other VM is in scope. The driver
never restores a checkpoint or starts a VM itself.

Run `Prepare -AcceptDisposableReinstallationRisk`, `Arm`, `Validate` (one-shot reboot), then `NormalBoot`. Each stage
retains logs and evidence locally. Non-Prepare stages compare the current built
installer against the sealed remote executable and refuse version mixing.
If code changes, restart qualification from the clean checkpoint after preserving
failure evidence; never replace an armed executable. `Rejected` and
`RejectedCurrent` only inspect an intentionally rejected disposable boot.

See the [dated qualification record](../infra/host-tests/results/2026-09-11-native-installer-preparation.md)
for the exact verified cases and pins.
