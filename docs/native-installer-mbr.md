# Native installer: primary MBR support

Status: **implemented in the development source; not in public Beta.10**.
The existing public bootstrap still selects immutable Beta.10, whose native
conversion profile requires GPT. Do not patch that release's binaries or change
partition tables to bypass its check. A new exact-candidate installation and
release qualification is required before advertising public MBR installation.

## Accepted layout

The development installer additionally accepts fresh Debian 13 amd64 hosts with:

- BIOS boot using the existing GRUB installation;
- an active primary DOS/MBR Linux partition (number 1–4, type `0x83`, flag `0x80`);
- a nonzero canonical PARTUUID such as `a7d6a899-01`;
- a plain ext4 root satisfying the existing inode, feature, free-space,
  root-local boot/state, package, security and fresh-host checks.

This does not enable logical/extended partitions, UEFI on MBR, LVM, RAID,
separate persistent boot/state filesystems, retained-data conversion or native
Ubuntu/Rocky conversion. It does not convert MBR to GPT, repartition a server,
replace its bootloader, disable Secure Boot or add a force/reset option.

## Identity and boot safety

GPT keeps its existing canonical UUID and journal format. MBR accepts only the
kernel/blkid form `xxxxxxxx-0N`, with a nonzero eight-digit lowercase disk
signature and primary partition number 1–4. Filesystem, machine, operation and
boot IDs remain full UUIDs. Uppercase/noncanonical aliases, zero signatures,
logical partitions, path traversal and `PARTNROFF` expressions are rejected.

The short disk signature is **not** a unique authorization token. The operation
also binds the filesystem UUID, machine identity, filesystem geometry, boot
artifacts, package snapshot, authenticated release and reviewed recovery
decision. Read-only `blkid -p` checks the actual filesystem, partition scheme,
PARTUUID and, for MBR, partition type/number/active flag. The installer verifies
BIOS mode and repeats these checks during host inspection, preparation,
initramfs authorization before the first filesystem write, and runtime storage
verification. Identity drift is rejected rather than adopted.

Both the one-shot and recovery GRUB entries load `part_msdos` for MBR, retaining
the filesystem-UUID search and exact `root=PARTUUID=...` binding. GPT still uses
`part_gpt`. Existing one-shot consumption, recovery-first default, unmounted-root
requirement, quota conversion and no-replay rules are unchanged. `fstab` handling
accepts either the bound filesystem UUID or the exact primary-MBR PARTUUID.

## Test status and next release

The [dated MBR evidence](../infra/host-tests/results/2026-09-24-native-mbr.md)
separates successful storage/boot tests from an unsuccessful full installation
using the old beta.3 laboratory payload. That payload requires NGINX `deb13u7`,
while the test repository supplies `deb13u9`; the exact-version check correctly
stopped installation and retained closed admission. No package downgrade,
dependency bypass or journal reset was used.

The next candidate must carry the new installer and matching current platform
packages, pass the complete fresh MBR and existing GPT installation flows,
normal reboot, panel/ingress and applicable release/upgrade/removal gates, and
receive publication approval. Internal storage tests do not satisfy those gates.

## Laboratory reproduction

`infra/host-tests/Test-StackfortNativeMBRHyperVVm.ps1` is restricted to the named,
identity-pinned disposable BIOS VM and its pre-established SSH host key. It
requires fixed RAM of at least 4 GiB and never restores a checkpoint itself.

The regular sequence is `Prepare -AcceptDisposableReinstallationRisk`, `Arm`,
`Validate`, then `NormalBoot`. An unsuccessful `Validate` remains unsuccessful.
The explicitly separate `StorageAfterPackageFailure` and
`NormalBootAfterPackageFailure` stages inspect the retained beta.3 WAF failure,
closed admission, completed storage conversion and quota/isolation probes.
Their success is **not** successful platform installation or release approval.
Never run this harness on a customer server or transplant its journals/binaries.
