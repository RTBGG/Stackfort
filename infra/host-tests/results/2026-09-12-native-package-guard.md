# Native process-lifetime package guards — 2026-09-12

Result: **held package-writer exclusion and checked APT handoffs pass isolated
conflict tests and a new complete Debian preparation/conversion/install/normal
boot qualification**. This completes the process-lifetime substep, not the
persistent package/kernel coordination gate. Public native activation remains
disabled. No commit, push, bootstrap change or public release was made.

See [implementation and remaining boundaries](../../../docs/native-installer-package-coordination.md).

## Implemented behavior

- Native inspection holds OFD read locks on the existing dpkg frontend/database
  locks before reading the package inventory and boot snapshot. Preparation
  retains guards outside its own APT commands, through boot sealing/service setup.
- Nonblocking acquisition rejects existing POSIX writers. Trusted-path and
  regular/root-owned/single-link checks reject unsafe lock files. Both inode
  identities are retained across APT handoffs; no lock file is created or removed.
- APT owns its own locks during Stackfort's package commands. Reacquisition must
  succeed before continuing, and the full package inventory must match the exact
  original-plus-approved-additions delta. The actual APT hook also rechecks the
  original boot layout before the reviewed transaction.
- Arm holds a fresh guard through initramfs construction and GRUB selection. It
  first checks the full package baseline against the pinned completed receipt.
  Normal boot artifact pins and existing one-shot/recovery checks remain in force.

## Test evidence

All **134 top-level Linux package/CLI tests** passed, with zero skips/failures.
The binaries cover `internal/installapply`, `internal/storageprep`, and
`cmd/stackfort-installer`. New cases include:

- 15 exact-delta scenarios: approved additions, unchanged state, multiarch and
  residual-config handling; rejection of unrelated additions/removals/upgrades,
  missing or wrong versions, ambiguous architecture, forbidden existing-package
  upgrades and invalid plans. Inputs are not mutated.
- 20 isolated guard scenarios: existing frontend/backend POSIX writer processes,
  partial-acquisition release, nested inspection, unrelated descriptor closure,
  canceled/nil contexts, missing/link/FIFO/directory/permission/owner failures,
  lock replacement while held or during handoff, competing handoff writer, and
  kernel release after guard-owner process exit. Descriptor close-on-exec and
  double-acquisition/released-guard checks are included.
- Actual `/usr/bin/apt-get` and `/usr/bin/dpkg` reject writes while guards are held;
  read-only dpkg audit still works. An intentionally nonexistent package tests
  APT release/reacquisition without installing anything. Busy-writer cases also
  verify the Arm backend stops at the guard before boot artifact access.
- Eight package-baseline receipt cases use a read-only real inventory and private
  synthetic receipts: matching baseline, changed inventory/receipt, foreign
  operation, missing/incomplete receipt, unsafe mode and absent source stage.

Conflict tests use an `unshare --mount --propagation private` child and a scratch
dpkg database bound only inside that namespace. Test writers use real POSIX
fcntl locks and readiness/EOF pipes, not timed sleeps. No real package database
or boot artifact is modified by these fault fixtures. No production fault switch
or configurable package-lock path is introduced.

Twelve before/after content hashes were identical across the final Linux tests:
fstab, normal GRUB configuration, actual dpkg status and both lock files, storage
and admission journals, sealed installer, runtime intent, release manifest,
prerequisite receipt and recovery-choice receipt. This comparison checks content,
not every filesystem metadata field. Both hash-list files have SHA-256
`c03ba4aeacf0d38c39322f4d6dcce582f8bf9ff021834c067216d97a7f20f14e`.

Windows `go test ./...` and `go vet ./...`, Linux-targeted vet for the three
changed/dependent packages, Linux integration compilation, gofmt checks and
`git diff --check` passed. The latter reports only the existing CRLF conversion
warning for `packaging/core/stackfort-install.in`.

## Real installation and normal boot

Only `stackfort-native-quota-debian-13`, VM ID
`4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, DMI
`6365bd88-5141-4f15-b3f8-2ba9996baad2`, was started. The duplicate-identity restored
comparison VM remained off. Before restoring the exact clean
`native-host-missing-prerequisites` checkpoint
`dd956c2c-b555-4d74-8812-06e85a149d39`, the prior success was preserved offline as
`native-before-package-guard-qualification`
(`e0386020-4b5e-40bc-a5cf-07cab3321805`). No checkpoint or backup was deleted.

The existing driver ran:

```powershell
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage Prepare -AcceptDisposableReinstallationRisk
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortNativeBootHyperVVm.ps1 -Stage NormalBoot
```

The new operation is `ed14a1b6-dd5a-443d-9784-269a95b83271`, using the retained
authenticated beta.3 input and a separately pinned current installer. Preparation
installed exactly `nftables 1.1.3-1` and `quota 4.09-1+b1`. The actual APT hook,
post-install delta check and sealed receipt all passed. The temporary service
policy was removed and the original input was retired. Repeated Arm remained
idempotent, with only one recorded arming attempt.

| Phase | Verified result | Test duration |
| --- | --- | ---: |
| Prepare | Explicit decision, real prerequisites, sealed intent | 42.00 s |
| Arm | Guarded construction of separate real-installer initramfs | 3.42 s |
| Conversion boot + installation | Real conversion, nine completed stages, admission | 78.79 s |
| Normal reboot | No repeated conversion/install; `alreadyInstalled=true`, `changed=false` | 23.01 s |

These are test execution durations, not boot-performance measurements. Conversion
boot ID: `e85367c9-48b0-4c2f-a4db-6dd4eb859183`. Normal boot ID:
`95c1ea43-e03e-4900-af20-b338659d5afd`. Both boots passed account isolation/project
quotas, OCI private resources, deployment lifecycle, and rootless container
subordinate-ID quota enforcement. The original signed Debian kernel and Secure
Boot setting were retained.

The actual sealed CLI's `native status` and `native recovery-plan --format=json`
also passed after the final tests. They reported recorded complete admission;
public resume, live readiness, backup verification, destructive authority and
automatic recovery remained false. The guest was shut down normally and saved as
`native-package-guard-qualified-success`
(`e0259d1b-54ae-4432-8e94-9df20044e18c`). All six lab VMs are off.

## Artifact pins

| Artifact | SHA-256 |
| --- | --- |
| Sealed installer | `ca05ef0b57b0ef34f7cc1e63b9736382da08179d701092f6c55216957fc67d2f` |
| Real-boot integration helper | `a31bbd5708e204d6185e708d34459a4823b2531eac48a6e87b2f90b229965ff8` |
| Completed prerequisites | `865ed968171091b980f2912c8752230086e10dda72dacf2b08839b8d6fe62635` |
| Recovery choice | `3a2076c9bebeba4405876a30c3aff82125c97d57af9a51e4a5fd8f9182bd274f` |
| Runtime intent | `1bb0ceadc84a1c45e7a8d234a72e271da94ec50547e710ce46b0aa1ca70e5bfa` |
| Release manifest | `7d57a451bd985439437cca507f187accc241a3b6f6382c47bd0b9a95341f82d7` |
| Final installapply tests | `eaee61b54192dae522da3aad0af7e391e50e793d64b49f64e332142371aff35f` |
| Storage protocol tests | `2e7698095db40947fc30316a71e8d189d570787345c274b43cfb0e5822fc7063` |
| Installer CLI tests | `8f1660d8cd65f968c3877b7ad005588dac78f3cae4c0ee72f2ce65273e3e9a1f` |
| Final Linux test log | `40819ce18b0b81b1a3ff9178871096c5805b44aaa1664d9b5dd6d1d410d3b16e` |
| Actual CLI status log | `14e05a30935554dab44ae6edf2fd1d72fb48cce66dfcf6e6827e25c4362eb38b` |

Ignored local evidence under `infra/host-tests/work/`:

- `native-boot-Prepare-20260912T070125Z`;
- `native-boot-Arm-20260912T070250Z`;
- `native-boot-Validate-20260912T070257Z`;
- `native-boot-NormalBoot-20260912T070547Z`;
- `native-package-guard-final.log`, `native-package-guard-status.log`, and matching
  `native-package-guard-before.sha256` / `native-package-guard-after.sha256`.

The normal-boot evidence archive SHA-256 is
`946ce98b63320d7c1e4b76518f34ddf95a82ee4178014c4c5da798de09f90c57`.

## Still open

Process exit/killing releases these locks. They do not exclude direct
`update-initramfs`/`update-grub` calls, arbitrary root writes, surviving child
processes or package changes after Arm returns and before reboot. The deliberate
APT handoff is validated, not atomically serialized. Durable operation-bound
coordination with crash/shutdown/reboot qualification is the next substep.
Capacity reserve, listener/firewall coverage, additional OS boot variants and a
signed public native candidate also remain open. Earlier hard-power-off,
sector-tear and whole-disk restore experiments were not repeated in this run.

### Later same-day boundary review

The paragraph above records the remaining work at this earlier run. Subsequent
[private-image/offline-guard/kernel-lifecycle qualification](2026-09-12-native-private-image-kernel-lifecycle.md)
passed and refined the maintenance boundary: a persistent global package-writer
fence is not an established filesystem-safety prerequisite for the qualified
plain-root model. Ordinary prior-OS writers cannot survive the reboot into the
unmounted initramfs, where checked drift rejects before metadata writes.
Maintenance quiescing can improve availability; arbitrary privileged hooks and
unsupported layouts remain excluded. This follow-up does not change this
report's original artifact pins or claim another power-cut/restore run.
