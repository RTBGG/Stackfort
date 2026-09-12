# Journal-bound native quota boot — 2026-09-08

Outcome: **the Debian lab now connects the durable storage protocol to the
offline ext4 helper and resumes successfully across a real reboot**. The same
final integration binary passes the normal subsequent boot and two fail-closed
fault sequences. This is not production installer activation, a release
qualification, a performance benchmark or arbitrary power-loss qualification.

See the [boot handoff design and reproduction steps](../../../docs/native-quota-boot-handoff.md)
and the [preceding state-protocol results](2026-09-08-native-storage-journal.md).

## Environment and scope

- Base commit: `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2`, plus working-tree changes.
- Exact disposable VM: `stackfort-native-quota-debian-13`, Hyper-V ID
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`; 2 vCPU, 8 GiB RAM, 50 GiB system disk
  and a 64 MiB seed disk. No separate hosting disk or loop image.
- Debian 13, kernel `6.12.107+deb13-cloud-amd64`, plain GPT/ext4 root with `/boot`
  on that filesystem; GRUB `2.12-9+deb13u2`.
- Each final sequence began by explicitly restoring the offline
  `native-quota-before-conversion` checkpoint
  (`9e62e0ac-89ad-4851-8ae2-e6b936e40eae`).
- Final integration executable SHA-256:
  `744f1870f9e1d0ee1ee90616ed162fcedf3461f8a77bbf4c71432e5c9b96fcac`.
  The lab source version is `0.0.0-native-boot-lab`, not a published release.

Only this VM was started. The customer VPS and the other four test VMs were not
modified. The harness uses real initramfs/GRUB/systemd behavior but a synthetic
resumer and dependent consumer, not the actual package installation workflow.

## Final results

| Check | Result |
| --- | --- |
| Windows ordinary `go test ./...` | Pass |
| Linux storage preparation and integration build/vet | Pass |
| Storage preparation unit suites on Linux root | 14 top-level tests pass; no skips |
| `Prepare` → `Arm` → real reboot → `Validate` | Pass; journal reaches `ready`, one arm attempt |
| Subsequent normal reboot → `NormalBoot` | Pass; no helper/token, journal remains unchanged |
| Original kernel, normal initrd and main GRUB configuration | SHA-256 unchanged; no `update-grub` used |
| Project quotas, account isolation, private OCI resources and OCI lifecycle | Pass after conversion and after the normal reboot |
| Rootless container subordinate-UID writes | Quota enforced; successful 16 MiB append after raising the quota, on both boots |
| Pre-write rejection → `Recovery` → `RecoveryBoot` | Pass; quota features/fstab unchanged, consumer blocked, terminal recovery |
| Lost success proof after completed conversion → `Recovery` → `RecoveryBoot` | Pass; quota features present but fstab unchanged, consumer blocked, terminal recovery |

The final success operation is `14158ef9-69a5-4142-9efd-67840196116c`, manifest
SHA-256 `4af401a0a0514237df214020df1b10094091efa5b29b680fe5372b81250616e8`.
Its conversion boot ID is `dea3fc92-03db-4ef0-ae98-1db7eeb65885`. The later
normal boot retains that recorded resume ID without repeating mutation or
resume side effects. The supplemental menu and special initrd are retired;
global build hooks are absent from subsequent normal initrd builds.

Pre-write rejection used operation `ae3785c4-91bd-4ce6-bd60-27d2537441c7`.
Lost-proof rejection used `37554ce4-5de8-4204-899f-3c7df77ea58e` and manifest
SHA-256 `ac915226d14c48f9d2d9f6866b94d14ce07fb468b18e9819a2406a7ca4b67bbe`.
Both reached `recovery-required` with `boot-evidence-invalid`. GRUB's armed and
next-entry values were consumed. A second normal boot had no preparation token
or early-helper log; repeated journal advancement did not re-arm or rewrite
terminal state. The intentionally failing early/resume service logs are
expected in these negative cases, not a failed recovery assertion.

The lost-proof injection occurs **after completed conversion**. It does not
simulate power loss midway through filesystem metadata modification.

## Development failures retained separately

The following earlier attempts did not qualify and are not counted as passes:

1. The first prepare attempt rejected the literal warning line in Debian's
   GRUB environment block. The parser now accepts that exact vendor line while
   retaining strict size, key, duplicate and padding checks. Eligibility checks
   run before fixture preparation.
2. An earlier helper created its exclusive RAM mutation marker twice after
   moving protection ahead of the potentially writing pre-conversion fsck.
   The second creation caused an emergency stop before `tune2fs`. The lab boot
   stalled; a running checkpoint, console snapshot and subsequent recovery
   evidence were retained. After a hard power cycle of this lab VM, the original
   boot path returned with quota features absent and hosting blocked by terminal
   recovery. The duplicate marker creation was removed before the final runs.
   This was one real interruption after an unexpected early-boot failure, not a
   controlled power-cut test during metadata writes with the final binary.
3. A subsequent attempt converted successfully, but regenerating GRUB changed
   font setup/device hints, so the strict original-configuration digest check
   rejected resume. The final design instead stages the supplemental
   `/boot/grub/custom.cfg` through the existing verified loader and never
   regenerates the main configuration. A fresh final sequence passed with the
   original boot artifacts byte-identical.

The earlier helper hashes were
`87cce34d7b6acb3ebfc793e486226698cf8c598fb691ca79b91ea9a4f72871a0`
(stalled boot) and
`fb9599b158ce8a87f130ff0621d7fe44f7188bd4a049802cce2c5b13bdd6cfa2`
(GRUB drift). No manual journal unlock was used to manufacture a successful run.

## Retained local evidence

All paths below are relative to ignored `infra/host-tests/work/`. Each final
stage directory retains `tests.log`, `boot.log`, the integration executable and
`evidence.tar.gz`. The archive contains the lab and storage-journal directories,
excluding the test executable and large original/retired initrds. These are
local development evidence, not release assets.

| Final sequence | Stage directories (UTC timestamps) | Archive SHA-256, identical across the two checked boots |
| --- | --- | --- |
| Success | `native-journal-Validate-20260908T184205Z`, `native-journal-NormalBoot-20260908T184237Z` | `4eeb15ab6c0ccd1b6aee70943d9a804c5b6aa7218314a811110af461bb746ef8` |
| Pre-write rejection | `native-journal-Recovery-20260908T184419Z`, `native-journal-RecoveryBoot-20260908T184432Z` | `34020967a3156a4abb9f9e08648ac97cdea832ee09fbb609ea001fdf75828248` |
| Lost success proof | `native-journal-Recovery-20260908T184817Z`, `native-journal-RecoveryBoot-20260908T184829Z` | `66f6a0b974214c3e3c3797275870f75d03d77d716172df26b755967f163cec74` |

Corresponding prepare/arm directories are `184158Z`/`184201Z`,
`184413Z`/`184415Z` and `184811Z`/`184813Z`, all on `20260908` with the
`native-journal-Prepare-`/`native-journal-Arm-` prefixes.

| Additional evidence | SHA-256 |
| --- | --- |
| `native-journal-storageprep.test` | `8ced3480601b073dc51e13ef30bdcaed230d8fb20f913571ec83c2fa315b93b5` |
| `native-journal-unit-linux.log` | `1da874910422673dfb4ca842db65bad4d902f05c96776a5af0314583c384a9dc` |
| `native-journal-Validate-20260908T184205Z/tests.log` | `ac0e5fec5878c7e954be756f5f6feade0d6270627d94ab3bc5754179db6d974d` |
| `native-journal-NormalBoot-20260908T184237Z/tests.log` | `f33108f3fd03721bcbc9a1b532e91cf716cb301c75e8d5277145309918798478` |
| `native-journal-interrupted-recovery.log` | `e21697a62a03761ceff0498547dc28fd0af8509cee105eda387ee18d4eab0eb7` |
| `native-journal-interrupted.tar.gz` | `1aadcdfc0ae07ed97d8746aa962de5e4692fbcdc6b7229785685747b04f57cb1` |
| `native-journal-Validate-20260908T183950Z/evidence.tar.gz` (failed GRUB-drift attempt) | `736695ad96ce524546d6be705e87ea72ea9fcdb17e1bb2d65eb7c37f2fadd5c1` |

The stalled boot also has checkpoint `native-journal-stalled-boot`
(`69bd8034-6108-4129-821c-fd85c08839ba`) and local
`native-journal-console.png`. The screenshot does not itself establish the
marker-error diagnosis; source review and later recovery evidence do.

After preserving the negative-case evidence, the VM was shut down normally and
restored, still off, to `native-journal-qualified-success`
(`e94cd170-e2d1-4a51-b225-c1fc1a1aa564`), saved offline after the final successful
normal boot and Linux unit tests. All five test VMs are off. No commit, push,
public release, installer activation or customer-server change was performed.

## Remaining gates

The public installer still requires prepared hosting storage and rejects every
native preparation journal, including `ready`. Production needs a reviewed
typed backend, verified release staging outside temporary bootstrap paths,
fresh-server/boot eligibility, actual package and service continuation,
updater/kernel/package coordination, operator recovery and controlled metadata
interruption tests. OS capacity reserve/admission, enforcement reporting and
ordinary Debian/Ubuntu/Rocky provider-image onboarding remain open.

The root journal, consumed GRUB latch and RAM marker are not a durable journal
of individual ext4 metadata writes, nor proof of arbitrary crash recovery or
boot-environment write ordering on other hardware. These limitations prevent
promoting this laboratory result directly to a one-line public installer.
