# Native preparation recovery-choice binding — 2026-09-11

Result: **the fresh-disposable decision is enforced before prerequisites and
bound through real Debian preparation, offline conversion, installation and a
normal reboot**. All quota/OCI checks passed on both boots. This is an internal
qualification; public native activation, public consent transport and external
backup verification remain disabled/unimplemented. No commit, push or release.

See the [policy and implementation boundary](../../../docs/native-installer-recovery-policy.md).

## Implemented gate

- `ReviewNativeBootRecovery` produces a read-only, locked, authenticated
  release/dispatcher/host/package/boot snapshot. A review is not consent.
- `PrepareNativeBoot` requires the exact review SHA-256, `fresh-disposable` mode,
  and separate no-retained-data and provider-reinstallation-risk assertions.
  It repeats review, then exclusively writes and syncs the private receipt before
  APT or boot preparation. A generic yes-only decision or backup mode is refused.
- Receipt presence is consumed preparation, not a pending token. Existing,
  partial, unsafe, foreign or stale state cannot be reset, replaced or re-adopted.
  Presence also blocks public installation/update before a storage journal exists.
- The prerequisite journal cannot change its decision hash. The actual APT hook,
  runtime sealing, arm/finalization/runtime loader and pre-write initramfs path
  verify the corresponding binding. Recovery/admission advice exposes limited
  mode/digest information and never grants restore or reinstallation authority.
- Historical profiles without the field remain readable; no consent or new
  privileges are synthesized for an existing operation.

## Tests

The final three Linux package/CLI binaries ran **130 top-level tests**, with no
skips or failures. They include the existing native boot/admission/storage tests,
the earlier read-only recovery fixtures and these additions:

- Nine explicit-decision cases, including absent assertions, stale/uppercase
  digests, unsupported backup/yes-only modes and schema mismatch.
- Nineteen changed review cases across source/host/boot/kernel/geometry/packages,
  policy and artifact pins; retained consent is rejected.
- Prerequisite and boot receipt drift/downgrade checks, canonical decoding,
  no-authority advice, and unchanged legacy intent encoding.
- Fourteen isolated Linux cases: exclusive valid receipt, existing storage or
  prerequisites, missing/backup consent, cancellation, unsafe file types/links/
  permissions and truncated/duplicate/unknown JSON. Rejected writes preserve
  evidence; replay fails, the public gate stays closed, and receipt-only status
  gives an incomplete-preparation handoff. Fixtures bind private scratch storage
  over `/var/lib` in a separate mount namespace, not the host's actual state.

Windows `go test ./...` and `go vet ./...` passed, as did Linux-targeted vet and
integration compilation. Targeted Windows tests reach 100% statement coverage
for the pure recovery-choice review, decision and binding helpers; this does not
claim all branches or all Linux I/O paths are covered. The PowerShell harness
parses and requires a dedicated explicit acceptance switch for Prepare only.

## Real boot qualification and preserved state

The fixed VM was `stackfort-native-quota-debian-13`, ID
`4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, DMI
`6365bd88-5141-4f15-b3f8-2ba9996baad2`. The previous successful state was preserved
offline as `native-before-recovery-choice-qualification`, checkpoint
`8721a1c9-a459-4366-9f2b-06b0c3755187`, before explicitly restoring the reviewed
fresh `native-host-missing-prerequisites` checkpoint
`dd956c2c-b555-4d74-8812-06e85a149d39`. No checkpoint was deleted.
The duplicate-identity restored comparison VM remained off throughout.

Using the existing authenticated beta.3 candidate and separately pinned current
installer, the driver ran:

```powershell
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage Prepare -AcceptDisposableReinstallationRisk
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage NormalBoot
```

The receipt was accepted for **new operation**
`5d9a878b-d277-448b-a5bf-bd266db521ce`. Its reviewed digest was
`390357fe694b980bcd9587928f5bef8d1c618dceeddc5bf472f0eeb11a86896d`, with both
assertions true. Prerequisites installed exactly `nftables 1.1.3-1` and
`quota 4.09-1+b1`; the temporary service-start policy was removed after success.
The original input directory was retired before boot; retained provenance still
verified. Repeating Arm did not repeat a journal transition.

| Phase | Result | Test duration |
| --- | --- | ---: |
| Prepare | Reviewed decision, actual APT prerequisites and sealed artifacts | 40.28 s |
| Arm | Separate real-installer initramfs and one-shot authorization | 3.25 s |
| Conversion boot + installation validation | Real conversion, nine complete package/service stages, admitted | 78.42 s |
| Normal reboot validation | `alreadyInstalled=true`, `changed=false`; no conversion token/proof | 22.66 s |

Durations are test execution times, not boot-time benchmarks. The conversion boot
ID was `b9c93385-9cb8-435b-b7de-3cf8ddff89ab`; the normal boot ID was
`779ae55b-3acb-47c1-9944-e2467bb3d4db`. Both passed account filesystem isolation,
hard project quotas, OCI private resources/deployment lifecycle and subordinate-ID
container quota enforcement. The consumed boot authorization remained consumed.

After the normal reboot, the final Linux tests plus actual sealed CLI `native
status` and `native recovery-plan --format=json` passed. The report showed
`admission-recorded` with `fresh-disposable` evidence and all backup/live-readiness/
automatic/destructive/public-resume flags false. Ten before/after file hashes
(fstab, main GRUB configuration, storage/admission/package journals, runtime,
manifest, intent, prerequisites and decision receipt) were unchanged across those
tests/inspections. The guest was gracefully shut down and checkpointed as
`native-recovery-choice-qualified-success`
(`cfcae82a-c9a7-460a-b5ea-26db57bcb935`); all six lab VMs are off.
Original checkpoints/backups remain retained.

## Artifact pins

| Artifact | SHA-256 |
| --- | --- |
| Sealed current installer | `b938e8e330650dfb82858e99022a4141709a8b851be098635e86e0c91083b1c6` |
| Real-boot integration helper | `ed13aced1c5c0672f8b5232524d00fa8cf04f9618ecacba6e8d057aa3bfc2b4c` |
| Recovery choice receipt | `955cec54668ac5cd4883160ba80b3f89539bf7dac73954e20d8c7bdf29ee42ea` |
| Completed prerequisite receipt | `8d372d91e4657f8294a5e3fcdcec867fbc14d8dbb91472b8ed7aef3cd9949d6d` |
| Runtime intent | `bfeef7e3f304cb4c83775bc6138cbc8c80b849a3f44de56637ac4174db800ded` |
| Release manifest | `98c2d4ee6a6e634da7698f15aa882de48e04532fb1a948e5ad18d8cd230a4c85` |
| Final installapply tests | `1b0d94723e4e9793253a25f320c31281ffab39c6d4d81773a7896e2871f756d5` |
| Storage protocol tests | `2e7698095db40947fc30316a71e8d189d570787345c274b43cfb0e5822fc7063` |
| Installer CLI tests | `60600e63ab55148666970e72b888016b49dc7c61297c0bb5d191a261ad658176` |
| Final unit/CLI observation log | `18adaad5852a77dae10ebce09ec65d7fb3174f974af108b22e3607b16f13ad91` |

Ignored local evidence directories under `infra/host-tests/work/`:

- `native-boot-Prepare-20260911T111944Z`;
- `native-boot-Arm-20260911T112139Z`;
- `native-boot-Validate-20260911T112158Z`;
- `native-boot-NormalBoot-20260911T112431Z`.

Each retains build artifacts, test/boot logs and a private evidence archive. The
normal-boot archive hash is
`7478e9fa31d7c255dfff7d8ddd5bcf04da49ea607d9c58feb9119fea35e048dc`.
Final package/CLI evidence is `native-recovery-choice-final.log`. These are local
qualification artifacts, not signed release assets or public backup records.

External package/kernel serialization is the next safety gate. General capacity,
listener/firewall coverage, additional OS boot profiles and a signed native
candidate also remain open. The earlier hard-power-off, sector-tear and complete
disk-restore experiments were not repeated in this decision-binding qualification.
