# Native private image and OS kernel lifecycle — 2026-09-12

Result: **a complete Debian native preparation/conversion/installation cycle,
normal reboot and an actual distribution-kernel update/reboot passed** using
the private initramfs builder and operation-bound lifecycle policy.

This is internal qualification: the platform payload was the retained older
`0.1.0-beta.3` candidate authenticated on `main`, with a separately pinned newer
installer. It is **not** qualification of a final `0.1.0-beta.4` tag archive,
its archived installer, public native onboarding or the public README command.
Later installer/setup changes require their own exact-candidate qualification.

## Fixed scope and identity

- Host: the dedicated `stackfort-native-quota-debian-13` disposable Hyper-V VM,
  Debian 13 amd64, qualified plain GPT/ext4 root and GRUB profile.
- Fresh baseline: `native-host-missing-prerequisites`, checkpoint
  `dd956c2c-b555-4d74-8812-06e85a149d39`.
- Operation: `147fcf2d-bcf3-40c8-b6c8-8ad9731e391f`.
- Sealed installer SHA-256:
  `ec44fdb2db9ea0a423db2db0213684b66e3de6aa249f15a2522677c49f0bb866`.
- Native release/boot manifest SHA-256:
  `61961e9c0ff7badf674447a623896964971b5984c499685ef46601fb10b53c26`.

## Complete real-host cycle

The existing driver performed `Prepare`, `Arm`, `Validate` and `NormalBoot`.
Preparation installed the exact approved prerequisites `nftables 1.1.3-1` and
`quota 4.09-1+b1`, retained the reviewed disposable-host decision and retired the
original input. Arm constructed the separate real-installer image; repeating
Arm did not advance the journal or consume a second arming attempt.

| Phase | Observed result | Test execution duration |
| --- | --- | ---: |
| Prepare | Real prerequisites and sealed operation | 40.48 s |
| Arm | Separate image; repeated request unchanged | 3.49 s |
| Conversion and installation | Real finalization, installation and admission | 93.46 s |
| Normal reboot | `alreadyInstalled=true`, `changed=false` | 28.33 s |
| Updated-kernel reboot validation | Same no-reconversion/no-reinstallation result | 17.13 s |

These durations are test execution times, not isolated boot or performance
benchmarks. Validation after conversion, normal reboot and updated-kernel reboot
included project quota/account isolation, OCI private resources, deployment
lifecycle and rootless-container subordinate-ID quota enforcement.

The private build receipt recorded the original kernel
`6.12.107+deb13-cloud-amd64`, source configuration hash
`3677a730f5674f2cee70e9b271a34d7f3cefa25b93526ad5aa98048ff8586aa2`
and private configuration hash
`709ed7bd3639bd7c08c814036c63963d8e277d3b2027e457013bc66500b10558`.
Inspection of the one-shot image found its embedded manifest/runtime, premount
script and real installer. Normal kernel/initrd/GRUB hashes were retained;
Stackfort conversion hooks are installed in the private configuration, not
temporarily added to the normal `/etc/initramfs-tools` tree.

The ordinary-kernel success checkpoint is
`1bebf7c3-a5b7-4c5a-88f0-d4880cfeae73`.

## Authenticated OS update after completed conversion

The actual Debian APT transaction installed the trixie-backports cloud kernel
`7.1.8-1~bpo13+1`, generated its normal initramfs and refreshed GRUB. The next
boot ran `7.1.8+deb13-cloud-amd64`, boot ID
`2c5e31be-c130-4c4a-9f05-6c9be6f488e0`. Native finalization/readiness services
reported ready without another conversion, and the same frozen integration
helper passed the full validation listed above.

The historical storage journal was unchanged across the kernel transition:
SHA-256 `860b44c2293522a1845dc7f898c72a294b0f1bc526a08fada8ed1a145f8ae46c`.
Thus the completed-conversion policy did not rewrite the original plan to the
new kernel or disable live storage/service checks. The updated-kernel success
checkpoint is `0d60f3ae-31a5-46c0-b267-09f7e71698a6`.

This qualifies that particular authenticated Debian kernel transition, not every
future kernel, provider layout, custom root hook or bootloader modification.

## Unit/fault coverage and retained logs

The earlier Linux installapply/storage/CLI suites for this implementation passed.
They included private-config preservation/unsafe-input/replay/drift cases,
bounded offline digest inputs, ordinary descendant cancellation, package-guard
conflicts and existing storage-state/admission boundaries. This report does not
claim a new hard-power-cut, sector-tear or whole-disk-restore run: those earlier
experiments remain associated with their own dated reports. Later reruns must
retain separate evidence for the changed source.

Ignored local driver evidence:

- `native-boot-Prepare-20260912T073722Z`;
- `native-boot-Arm-20260912T073828Z`;
- `native-boot-Validate-20260912T073845Z`;
- `native-boot-NormalBoot-20260912T074051Z`.

The following files are under ignored `infra/host-tests/work/`; hashes refer to
their retained bytes, not public-download availability.

| Log | SHA-256 |
| --- | --- |
| `native-oneline-private-image.log` | `6c8a329797bff82c01466751b97d9457e00209d3155f595e2980f2f67aaf10d7` |
| `native-oneline-kernel-install.log` | `38335efb04e9365c6be4f0ede176a9ad144126d322cf241d8a742ed893414745` |
| `native-oneline-kernel-status.log` | `ba20e9f69608feacf28c9fc9ffee5c2f395adacfcff56c0a2f0e0442add8ecf3` |
| `native-oneline-kernel-qualified.log` | `682460f031237b6587f35943390c00c04f2c94ff20b8ff791076e8e2000d7fb1` |
| `native-oneline-safety-unit.log` | `6dc4a6200cb1db720537dddf821cdb341dd853b733a9fe7363f76b55389aa2fb` |
| `native-oneline-storage-unit.log` | `49155c69bc448fe102b1892c621a2e3f5fac8006321299f23cc425c21e69424d` |
| `native-oneline-cli-unit.log` | `5d20df141cdd6c34a029deb56cb9f99a2fbf10987a2ddc652055ef2a971145a7` |

## Later same-day source and namespace validation

After additional setup-delivery and firewall eligibility changes, the Linux
`internal/installapply`, `internal/core`, `cmd/stackfort-api`,
`cmd/stackfort-installer` and `internal/storageprep` suites passed again.
The installapply suite includes the disposable mount-namespace tests for
canonical/unsafe/tampered setup records, legacy versus tag-origin binding,
orphan rejection and idempotent registration receipts. These fixtures do not
register a real administrator capability. Raw setup tokens are never recorded
in the journals or accepted as report evidence.

The firewall boundary suite also passed real isolated nftables table lifecycle
checks: managed start/reload/stop preserves the separate closed-admission table.
Native eligibility now rejects active/enabled foreign firewall managers and
unexpected existing tables without disabling services or flushing rules. The
new real fresh-host positive eligibility check is still pending at this report's
cutoff; isolated checks do not qualify full installer/reboot ingress.

Windows `go test ./...`, focused vet and the then-current offline documentation
check (223 Markdown files/789 links) also passed. These later source checks do
not retroactively change the exact installer or payload of the earlier boot
cycle above, nor prove the final tagged archive's public setup path.

| Later local log | SHA-256 |
| --- | --- |
| `native-oneline-current-installapply.log` (later same-day rerun) | `d05c4f842e80a5c61cdad1e77eab9cfc7f2faa7948c41920266a7d4e8fea2050` |
| `native-oneline-current-core.log` | `4721e4f9bb5785048a669aabbeb30a4bbc199536c91e44e37a68825d83af795c` |
| `native-oneline-current-api.log` | `eadf8585536960a8318a059cb7ebce26722ae33edfe5004beddb9d20f169ac96` |
| `native-oneline-current-installer.log` | `69e62d3f023ccd335b72dd3caf1dc181c8f80f6682d575dbac26629056682adf` |
| `native-oneline-current-storageprep.log` | `4e6f5fa29a25dbffee2cfe31f14018ad16bb4f5e764c59746f8650f5c3bcf5a5` |
| `native-oneline-firewall-boundary.log` | `0772b1f7b0727ff73ddb81ec26892c27f1fcd700ad3daa064aeb4cd564461a4e` |

The installapply row was refreshed after a later successful source-suite rerun;
its earlier contents are not claimed to remain at that path. All `current-*`
work paths are reusable local outputs, not frozen release artifacts. Their
hashes identify the observed report snapshot only; future reruns require a new
record or separately retained output and cannot inherit exact-candidate approval.

## Remaining release boundary

This closes the previously missing internal real-image/kernel-lifecycle
qualification for the pinned installer above. It does not qualify the later
public dispatcher or create release approval. Listener coverage, setup delivery,
full product/uninstall tests, exact tagged archive
qualification and the [publication contract](../../../packaging/releases/README.md)
remain separately evidenced requirements.

The native initial-capacity check is installation headroom, not a durable OS
disk/inode reserve. Aggregate account admission, unlimited requests and platform
growth remain production blockers. The explicitly disposable/nonproduction
experimental beta must disclose possible shared-root exhaustion and whole-server
unavailability, while retaining real quota/isolation and functional safety tests.

Ordinary prior-OS package writers cannot survive reboot into the unmounted
conversion initramfs; checked drift must reject before metadata writes. A
persistent global package-writer fence is not an established additional
filesystem-safety requirement. Maintenance quiescing can avoid availability
failures, while late-drift/recovery tests and the non-hermetic privileged-hook
boundary remain explicit; see [package coordination](../../../docs/native-installer-package-coordination.md).
