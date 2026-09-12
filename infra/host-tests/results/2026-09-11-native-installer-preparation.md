# Native installer preparation qualification — 2026-09-11

Scope: move the initial preparation, one-shot offline quota conversion, and
finalization out of the Go test executable into ordinary installer components.
**Public native activation and release publication remain disabled.**

## Environment and artifacts

- Only `stackfort-native-quota-debian-13`, VM ID
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, was started.
- Debian 13, kernel `6.12.107+deb13-cloud-amd64`, plain GPT/ext4 root,
  256-byte inodes, 2 vCPUs, nominal 8 GiB RAM / 50 GiB disk.
- Clean baseline: `native-quota-before-conversion`, snapshot ID
  `9e62e0ac-89ad-4851-8ae2-e6b936e40eae`.
- Preserved successful offline checkpoint: `native-boot-qualified-success`, ID
  `7a5ed1ae-8b62-4e6b-8074-19f01eb62588`.
- Retained authenticated beta.3 release archive: SHA256
  `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0`;
  source digest `48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8`.
- Current installer is a separately trusted local qualification executable,
  **not** an altered or newly published signed beta.3 release.

Final executable SHA256 values (unchanged across the verified boot sequence):

| Artifact | SHA256 |
| --- | --- |
| Real installer | `325958e4d370005c625d07fc36f7eadf6587a375272fe48b8d70b70ecc2f6fa0` |
| Integration test observer/fixture | `6a6fc3f87594ef4b16e3c59751d42d21fd2423466dec740e171f3cbee3e1103c` |

## Successful real installation and normal boot

Operation: `61cc1cb9-4788-47e2-8097-11e5b123eec7`.

| Evidence | SHA256 / identity |
| --- | --- |
| Release/boot manifest | `e1df1a2cdc2789d2b250fb9cb7e0d2e54c1da363a089a96fc0e55a0ebc3af543` |
| Runtime intent | `320979916ddc6812a9835d08cc46c82b1872ffed7603c4644df52092f00e6d25` |
| Completed package journal | `4a4f38d9063c96e9d34b61fee16a50ebba54d835c1058a0ba887a36718a85c89` |
| Retired one-shot initrd | `9f20688f1ab5f181329e8de70fe0cd08bd6d98c112b1cc0947447d35c7ad555e` |
| Original boot | `6e5c0ba7-e555-4bc4-ac91-f154f7014097` |
| Conversion / first admission boot | `6389743b-8b21-45ee-bcbf-355b92cee8b5` |
| Normal / second admission boot | `2cf575de-be63-438e-a224-547e3b7e6534` |

Verified:

- Regular `SourceStage.PrepareNativeBoot` authenticated and retained the release,
  sealed the new runtime/boot intent, and installed all service dependencies.
  The original input directory was retired before arming.
- Real `native-boot arm` built the separate initrd and selected one operation-bound
  entry. A second arm request in the same boot left the journal unchanged.
- Initrd inventory contained the real installer, **no `.test` executable**.
  No legacy test resumer or lab state directory was installed.
- The real early dispatcher verified the offline bindings and unmounted target,
  ran both fsck passes and quota conversion, and produced current-boot proof.
- Real finalization verified live quota accounting/enforcement, persisted fstab,
  retained evidence, and retired the one-shot artifacts. Storage reached `ready`.
- All nine authenticated installation stages completed and web admission opened.
- Normal reboot did not rerun offline code or package installation:
  `alreadyInstalled=true`, `changed=false`; storage stayed ready and admission
  advanced to attempt 2. The normal kernel/initrd/GRUB digests remained pinned.
- Account quota/isolation, private OCI resources, OCI deployment lifecycle, and
  real rootless subordinate-UID project quota tests passed on both boots.
  The rootless write hit quota after 202,670,080 partial bytes and recovered
  after raising the limit; the follow-up append was 16,777,216 bytes.

Ignored local evidence directories under `infra/host-tests/work`:

- `native-boot-Prepare-20260911T071747Z`
- `native-boot-Arm-20260911T071854Z`
- `native-boot-Validate-20260911T071900Z`
- `native-boot-NormalBoot-20260911T072102Z`

First-install test-log SHA256:
`3bea49fd46bffc89c9dfad94ad7253ac847182eefd716b2ebe817f6d2af02add`.
Normal-boot test-log SHA256:
`204306e28b06b73264a3dc0968c5a1298309095cddb96444d7fd4a4f559308f7`.

## Post-arm configuration drift and blocked follow-up reboot

A separate clean-baseline operation, using the **same two executable hashes**,
was prepared and armed: `b2532365-b30d-4426-a00f-cebc8ee9fcc3`, manifest
`c3c09d6a921ff2f909e39f93189011dd9e021df310a34a87bda3131539952e20`.
After arming, a qualification-only comment was added to `/etc/fstab`; root mount
options and identifiers were unchanged. The original file was retained as
`qualification-fstab.before` in installer evidence. This is an external fixture
mutation, not a fault-injection switch in installer code.

- Original fstab SHA256:
  `1e34e36ed0bf1837b172fa48094c5425520ac92bb3f641c38084e1171b185f4a`.
- Deliberately changed fstab SHA256:
  `fc166a379d7a251e31a6c5aca2a17aaa77a1327a9328b744d8becbf6f04ca33f`.
- Offline boot reported `offline binding drift: /etc/fstab` before fsck/tune2fs.
  No mutation marker or success proof existed, and quota/project features
  remained absent.
- Finalization durably recorded `recovery-required / boot-evidence-invalid`.
  Hosting remained unmounted, the web gate stayed closed, and no package
  installation journal was created.
- A further normal reboot consumed no new entry and performed no conversion:
  the original operation remained recovery-required with `armAttempts=1`.
  No native-quota kernel token or early conversion log appeared on that boot.

Evidence directories: `native-boot-Prepare-20260911T072237Z`,
`native-boot-Arm-20260911T072432Z`, `native-boot-Rejected-20260911T072440Z`, and
`native-boot-Rejected-20260911T072538Z`. Both rejection test logs have SHA256
`0554ee6ed69715ba39602dca7f2fbc7911a903183b5725582d203d0aee410043`.
Boot-log hashes are respectively
`6127710cc782043615c48fee267b6560af824de1e3fc5bdf99715d6123fdbd96`
and `3984364f78a1ea020ed8a78b0fae14181628f19a73724d9f9dd87b0f4385c8bf`.

After preserving failure evidence, the successful offline checkpoint was
restored. **All five VMs are off.** No checkpoint was deleted, no other VM was
started, and no customer server, GitHub repository, or release was modified.

## Regression checks

- Windows: `go test ./...` and `go vet ./...` passed.
- Linux: integration-aware `go vet` passed for `internal/installapply`,
  `internal/storageprep`, `cmd/stackfort-installer`, and `tests/integration`.
- All **108 top-level Linux tests**, no skips, passed in installapply,
  storageprep and installer CLI; namespace tests used the explicit disposable
  host opt-in. Log SHA256:
  `a47561918891e88ba6448fd07c8982f80bc500271a991da989a8a510654355d2`.
- New tests cover canonical boot intents, fstab ambiguity/conflicts, exact tool
  binding, absence of test code/repair/retry in boot templates, mounted targets
  including read-only/bind mounts, exclusive artifact writes/links, shared-lock
  requirements, live-root rejection, and internal CLI input boundaries.

Two earlier development attempts were rejected before quota mutation: the first
over-restricted Debian's existing `x-systemd.growfs` option; the next placed
embedded JSON directly under `/`, which the secure directory reader deliberately
rejects. The option is now explicitly preserved and embedded JSON lives under
`/etc/stackfort-native`. Failure evidence was retained and every changed installer
was requalified from the clean checkpoint; no armed executable was replaced.

## Not established by this work

The real code path replaces the initial test backend, but it does **not** qualify
power failure during ext4 metadata writes, general provider images, prerequisite
package installation/order, external apt/dpkg/kernel/initrd coordination, root
capacity admission, every firewall/listener interaction, Ubuntu/Rocky boot
variants, or a newly signed public candidate. There is no public activation,
automatic reboot, automatic filesystem repair/reset, or release upload.
