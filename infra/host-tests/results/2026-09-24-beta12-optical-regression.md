# Beta.12 preparation: optional optical fstab regression

Result: **source correction and focused host regression passed**, 2026-09-24.
This is not exact-release qualification or publication approval. Beta.11 and
the public bootstrap remain unchanged.

The user supplied two fstab rows: `/dev/sr1 /media/cdrom0 udf,iso9660 user,noauto
0 0` and `/dev/sr0 /media/cdrom1 udf,iso9660 user,noauto 0 0`.
Only the ext4 root was mounted. No access or changes were made to that VPS.

## Real MBR host reproduction

Dedicated disposable VM `1f5e665a-fddd-4cd2-bc55-44255b01963d`, DMI
`91d127c9-40e6-ab48-9528-5085e8888729`, was first preserved in checkpoint
`beta11-preserved-before-beta12-optical-20260924`. Its existing clean 8 GiB
MBR checkpoint `c9c3f45c-a014-42db-83fa-8111a559c394` was restored for this
regression. No checkpoint was deleted; this is deliberately NOT a removal test.

Boot: `ee962d39-ed81-4bd3-9610-eea9ed6aa95a`. Exact VM/DMI, strict SSH key,
fresh storage/state and the original single-line fstab were verified first.
The old published installer SHA-256 was independently pinned:
`2bdee0efaf811f5eb7a488236f2a021ab865c546a7e486ad52c8743b790740bf`.

| Read-only host inspection | Exit | Eligible | Boot layout |
| --- | --- | --- | --- |
| Original Beta.11, original root-only fstab | 0 | yes | pass |
| Original Beta.11, same root plus both user-supplied optical rows | 2 | no | additional persistent filesystems rejected |
| Corrected development installer, identical augmented fstab | 0 | yes | pass |

The old fstab was retained separately before test-fixture deployment. Both
inspections preserved the augmented fstab exactly, created no installer journal
or hosting directory, and performed no reboot/conversion. The fixture remains
available for subsequent exact-candidate onboarding.

Development installer SHA-256:
`9986a0cd6dcf37673540f06fac336c33c8a2af6c2f9f27af8e94d7fcdd8ab25f`.
This is a locally built diagnostic binary, not a released archive or replacement
for an installed production binary.

## Regression coverage

Pure Go tests cover the exact provider rows, optional absent drives, valid type
orders, root-only quota transformation, and unchanged GPT/EFI/tmpfs/swap policy.
Negative cases cover additional persistent storage, missing/conflicting noauto,
nofail alone, automount/initrd/dependency options, bind/loop mounts, quota options,
ambiguous/duplicate fields, arbitrary sources/targets, traversal/escapes, fsck,
target overlap and malformed/incomplete mount inventories. Diagnostics identify
the fstab line without exposing source credentials.

The Linux suite also ran on the separate installed Debian test VM
`55729cf8-11e3-4744-be5b-ddde3824c0e4`. Synthetic device nodes and parent mounts
were confined to a private mount namespace; no real device was opened or modified.
It accepted missing drives and rejected an actual mounted parent, a device-number
alias of the mounted root, a dangling symlink and a regular file.
The installed platform and original public Beta.10 VM were not modified.
The root namespace test is added to CI; ordinary Go tests do not silently stand
in for it.

Full local `go test ./...`, `go vet ./...`, Linux cross-compilation and the
focused real-Linux tests passed.

Ignored evidence directory: `infra/host-tests/work/beta12-optical-20260924/`.

| Receipt | SHA-256 |
| --- | --- |
| `baseline.json` | `3e408c3972979cf947dd8d0b2d795fa57424b7e702959a1cbc271cbe40d1ffb6` |
| `beta11-provider.json` | `66f971584b0afabbad285282435731e98c22373a379212bf2fb7e89fe8aa9ca6` |
| `fixed-provider.json` | `c37ad4d8f478cb7a221e778e96904f98426530a801f057eadcb7ed29f534c415` |
| `host-reproduction.log` | `9789b035e162829242fdfc1f1a2b02549aec3a7aade5a2468dfc490947a3dc5d` |
| `linux-tests.log` | `421110c8779393bc668c9b7f108310f48ae9e4f0d69a90de5c5fac8e6efab22f` |
