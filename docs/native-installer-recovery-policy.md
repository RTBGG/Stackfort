# Native installation recovery policy

Status: **read-only recovery handoff, fresh-disposable decision binding and public
interactive `onboard` are implemented; exact-candidate publication qualification
is pending**. Current source accepts only an authenticated `tag-release` on an
eligible fresh, disposable Debian 13 `amd64` host, with explicit controlling-terminal
consent. It does not enable automatic recovery, generic public resume or data
erasure. This is not a claim that a matching public release has been qualified
and published. See the [current installation guide](installer-installation.md#interactive-native-setup).
The external-backup route remains unaccepted until its independent verification
mechanism is qualified.

The ordinary goal remains: rent a supported fresh server, run one command, finish
setup in the browser. Manual partitioning is not part of that flow. Exceptional
failures must not turn a convenient installer into an automatic data-erasure tool.

## Backup and reinstallation boundaries

The following policy governs current fresh-disposable native preparation and
records the requirements of the still-unavailable external-backup route.
The public path implements terminal consent; it does **not** provide a backup
service, backup-verification receipt, provider API or generic restore command.

| Situation | Required decision before conversion | Exceptional failure route |
| --- | --- | --- |
| Eligible, freshly provisioned server with no data to retain | Owner explicitly accepts that provider reinstallation may be necessary if conversion fails; no mandatory full-disk backup for this disposable case | Preserve evidence; owner separately authorizes the provider's reinstallation of the exact affected server |
| Otherwise eligible fresh server whose pre-installation state must be retained — not accepted by the current native path | Future route requires independently verified, recoverable whole-system backup outside the affected disk, plus owner review of its scope and rollback point | Independent rescue environment; operator explicitly authorizes the exact backup and replacement target |
| Existing hosting, valuable data of unknown scope, unsupported layout, or unclear backup/reinstallation decision | Do not start native conversion; a backup does not override fresh-host eligibility restrictions | Preserve the host and obtain an explicit migration/recovery plan |

Acceptance of possible reinstallation is **not** advance permission for Stackfort
to trigger it. No timeout, `--yes`, failed health check, successful fsck, pending
admission approval or local `backupAvailable` flag may authorize disk overwrite,
filesystem repair, provider reimage or a repeated conversion.

The preparation API enforces the fresh-disposable decision before package or
boot preparation can mutate the host. The registered public `onboard` flow
authenticates the exact release/host review and obtains that decision through
a verified root controlling terminal. It separately confirms reboot and requires
acknowledgement that the one-use setup code was saved. Piped stdin or a generic
`--yes` cannot supply those confirmations. The read-only recovery report is
never an authorization receipt.

## Enforced preparation decision

`SourceStage.ReviewNativeBootRecovery` verifies the authenticated retained release,
fresh-host eligibility and dispatcher, and returns a versioned review under the
shared installation lock. Its digest covers the operation/release identity,
installer hash, DMI/root/partition/boot/kernel identity, package inventory digest,
Secure Boot observation, filesystem geometry/features and four boot-file pins.
Reviewing does not create consent, install packages or arm a conversion.

`PrepareNativeBoot` now requires a `NativeRecoveryDecision` containing the exact
review digest, mode `fresh-disposable`, and **both** explicit assertions:
`noDataToRetain` and `acceptProviderReinstallationRisk`. It repeats live review;
host/source/binary drift invalidates the old decision. Unsupported backup modes,
missing assertions and a generic yes-only decision are rejected. All callers
must display the policy and obtain those assertions; they must not derive consent
from `native recovery-plan`, a timeout or unattended defaults. The public
interactive transport is implemented in `onboard`; no public unattended consent
transport is supported. Its exact-candidate end-to-end qualification remains a
publication requirement, separate from implementation and focused tests.

After validation, preparation exclusively creates root-private mode-0600
`/var/lib/stackfort-installer/native-recovery-choice.json`, syncing both file and
parent directory **before prerequisites**. Source download/authentication may
precede this decision; package and boot preparation may not. Receipt presence
means the decision has been consumed for one preparation attempt, not queued for
future use. Existing/partial/unsafe receipts, prerequisites and storage operations
cannot be overwritten, re-reviewed, adopted or reset. Even an interruption before
the first prerequisite record leaves a public-install/update blocker and a
`preparation-incomplete` operator handoff.

The receipt hash is immutable in the prerequisite journal and is sealed into the
offline intent and release manifest. The actual APT guard checks the corresponding
decision. Runtime sealing, arm/finalization/runtime verification and initramfs
conversion recheck the binding; initramfs reads the receipt from the unmounted root
before filesystem mutation. No user-selected rescue path or command is introduced.

Historical intents without this field remain readable with their original
semantics and hashes; no receipt is synthesized for them. **New preparation always
requires the decision.** Existing sealed binaries are not replaced in place.
The [package-coordination follow-up](native-installer-package-coordination.md)
adds process-lifetime APT/dpkg guards and exact delta checks. Private initramfs
construction and unmounted pre-write boot-file checks reject relevant drift;
this is not a claim of hermetic coordination with arbitrary privileged writers.
See that follow-up for the remaining maintenance-availability boundaries and
their qualification status.

The lab driver requires `-Stage Prepare -AcceptDisposableReinstallationRisk` and
passes a dedicated explicit test opt-in. The flag is rejected on later stages;
it cannot add consent to an existing armed operation. This is lab-only acceptance
for a disposable fixture, not public UI or permission to reinstall a real server.

## What qualifies as a recovery backup

The backup route must establish scope, integrity **and** provenance, not merely
accept a checksum supplied next to an arbitrary image:

- Cover the whole pre-installation system required to boot and recover data,
  including partition layout and disk-resident boot/EFI files. Account backups,
  filesystem metadata-only images and undo logs are not equivalent.
- Keep the authoritative backup and trusted inventory outside the affected
  system disk. A copy on the same disk is not a conversion-failure recovery route.
  Survival of physical host/storage failure additionally needs an independent
  failure domain; a provider snapshot must not be assumed to provide that.
- Bind owner-approved source identity, capture time, consistency method, complete
  image size/hash and boot requirements through an independently trusted record.
  A matching hash alone proves neither source ownership nor recoverability.
- Establish that credentials/decryption material and an independent rescue path
  will remain available. Keep them private, not in the diagnostic report.
- Verify a restore on an independently selected replacement, including full
  readback, appropriate offline checks and bootability. Retain the original and
  backup until verification and explicit owner acceptance.

A full restore discards changes after the capture time. Firmware/NVRAM, external
volumes and provider configuration need separately established coverage. The
[Debian lab restore](native-installer-whole-disk-recovery.md) proves a scoped
pre-installation disk/boot route; it does not certify arbitrary provider backups
or off-host disaster recovery.

## Read-only operator handoff

```sh
sudo stackfort-installer native recovery-plan
sudo stackfort-installer native recovery-plan --format=json
# With a package built from current source:
sudo /usr/sbin/stackfort-install native recovery-plan
```

The command uses the existing locked `native status` reader, never a second
unlocked path. Missing state is not created. It does not modify the installation,
collect an archive, contact the provider, check a backup, approve anything, start
services or reboot. It accepts only an output format, not target paths or consent.

It distinguishes prerequisite review, incomplete preparation, unverified storage,
storage recovery, ready-but-unadmitted storage, admission review, pending/stale
approval, recorded completed admission and absence of a recorded native operation.
An in-progress phase is **not** automatically called a failure. A stored `ready`
or `admitted` state is **not** a live health assertion.

The report exports only allowlisted operation IDs, phases, failure codes and
journal digests. It excludes raw records/errors, host/network inventory, fstab,
tokens and key material. Review even this limited metadata before sharing it.
If inspection is busy, incomplete, unsafe or inconsistent, all partial evidence
is omitted and the result is `inspection-unavailable`. Inspect `native status`
locally for the underlying error; do not publish its raw output without review.

Exit codes differ intentionally from plain `native status`:

- **0:** inspection succeeded and no recorded recovery action was identified
  (absent native state or complete recorded admission), not permission to install.
- **2:** coherent recorded state needs review; no corrective action was performed.
- **1:** inspection/validation or output failed. An inspection failure still
  produces safe advice when output is writable, never usable partial evidence.

When the machine is stopped in the guarded initramfs and normal userspace is
unavailable, this CLI is not a rescue-disk reader. Leave the uncertain root
unmounted, preserve console evidence and use an independent rescue environment.
Do not mount the affected disk just to make this command work.

`approve-recovery` remains limited to **installation admission after recorded
storage readiness**. It cannot approve storage recovery, restore a disk or reset
the consumed boot latch. The qualified supervisor still rechecks the source,
storage, live gate and exact journal snapshots when consuming an approval.

See [operator commands](native-installer-operator.md) and
[diagnostic qualification](../infra/host-tests/results/2026-09-11-native-recovery-policy.md),
plus the [decision-binding and real-boot qualification](../infra/host-tests/results/2026-09-11-native-recovery-choice.md).
