# Native host eligibility and prerequisites

Status: **internal Debian 13 qualification; public activation remains disabled.**
This is the second follow-up to [real installer boot preparation](native-installer-preparation.md).
It supports the conservative `debian-13-plain-ext4-grub-v1` profile, not every
provider image and not Ubuntu/Rocky boot conversion.

## Read-only diagnosis

The current locally built installer provides:

```sh
sudo stackfort-installer native-host --format=json
```

This command does not install packages, create installer state, arm conversion,
change services, or reboot. Exit 0 means the inspected host is eligible for this
internal profile; it does **not** enable public installation. Exit 2 reports
blocking checks with stable identifiers and remediation. Missing `quota` or
`nftables` is reported separately as a prerequisite plan.

The checks cover:

- Debian 13 amd64, systemd PID 1, cgroup controllers, memory/CPU and existing
  mandatory access-control enforcement.
- Full host/root/mount/user namespaces, container/chroot/WSL rejection, observable
  firmware state and completed cloud-init. Secure Boot is never disabled; the
  current Hyper-V qualification uses Secure Boot enabled with Debian's existing
  signed kernel. This does not claim an authenticated custom-initrd trust chain.
- Healthy native-architecture package state, held package-writer exclusion locks, no pending
  reboot or existing `policy-rc.d`, and an existing normal Debian GRUB/initramfs
  toolchain. Unknown/held/broken states require review, not automatic repair.
- Conflicting web/database/panel packages (including residual configuration),
  known hosting-data paths, service identities, existing installer evidence and
  TCP/UDP 80/443/8443 listeners. Unused Podman packages and empty container-storage
  directories are allowed; detected container data is not adopted.
- Plain GPT/ext4 root with 256-byte inodes, a narrow feature allowlist, no existing
  quotas, root-local boot/state/hosting paths, supported fstab/GRUB configuration,
  no conflicting one-shot artifacts, and at least 8 GiB free for qualification.

This is a conservative conflict detector, **not proof that a server has never
hosted workloads**. Unusual service names/data locations, arbitrary rootless home
directories, additional provider layouts and a final capacity-reserve policy
still need broader qualification. LVM/RAID, separate persistent boot/state mounts
and unknown layouts are rejected without conversion.

## Package ordering

Under the shared installer lock, after release authentication but **before**
sealing boot artifacts, `PrepareNativeBoot` now:

1. Inspects eligibility and writes a private, operation/release/installer-bound
   `native-prerequisites.json` record. It captures host/firmware identity,
   filesystem geometry, package-inventory hash and the original fstab, kernel,
   initrd and GRUB configuration hashes.
2. If necessary, refreshes APT indexes and simulates installation of only missing
   `quota`/`nftables`. Only a small explicit new dependency allowlist is accepted;
   a required upgrade, removal or unknown dependency stops preparation.
3. Rechecks the host, records exact planned versions and stages a separately
   pinned, root-only prerequisite guard executable. An APT protocol-v2
   pre-install hook checks the **actual** transaction while APT holds its locks:
   the package inventory and original boot layout must still match, and every unpack/configure action must
   be an exact, planned, newly installed package. Simulation alone is insufficient.
4. Temporarily denies prerequisite service starts through its own exclusive
   `policy-rc.d`; an existing administrator policy is never replaced. Ordinary
   completion/failure removes only the exact operation-owned policy. Process loss
   retains the policy and journal for review.
5. Checks package health, the complete exact package delta and unchanged boot artifacts, records
   completion, then seals the receipt hash into the offline boot intent.

Both the initramfs entry **before its first filesystem write** and subsequent
runtime entry points verify the bound receipt. There is no general package
upgrade, bootloader replacement, automatic conflict removal or automatic reboot.
The original beta.3 release archive remains unchanged; the new dispatcher is a
local qualification build.

The [package-coordination follow-up](native-installer-package-coordination.md)
holds real APT/dpkg-compatible locks through inspection, preparation outside its
own APT handoffs, and boot-image arming. It detects unrelated package changes and
changed lock identities. This does not yet provide a persistent maintenance fence
between process exit and reboot, or exclude direct boot-tool commands.

## Interrupted preparation

Journal phases are `checking`, `applying`, `complete` and `recovery-required`.
Interrupted/non-complete operations cannot be reset, rebound or automatically
retried. A completed receipt can only be rechecked for the same operation and
unchanged host/package state before boot sealing. Even an orphan prerequisite
record or executable blocks the ordinary installer/updater.

`native status` reports valid prerequisite-only state and its digest even before
a storage journal exists. This is recorded evidence, not live readiness. Existing
admission-recovery approval commands do not authorize package-transaction repair.
Preserve the journal, APT/dpkg logs and any retained service policy for review;
do not delete state to bypass the gate.

## Qualification and references

See the [dated evidence](../infra/host-tests/results/2026-09-11-native-host-eligibility.md).
The disposable driver uses the real preparation/boot/runtime executable. An
offline checkpoint with `quota` and `nftables` deliberately absent exercises
actual prerequisite installation; no package is removed from a user server.

The implementation follows Debian's
[APT transaction options](https://manpages.debian.org/trixie/apt/apt-get.8.en.html),
[APT hook protocol](https://manpages.debian.org/trixie/apt/apt.conf.5.en.html),
[service-policy interface](https://manpages.debian.org/trixie/init-system-helpers/deb-systemd-invoke.1p.en.html)
and [virtualization detection](https://manpages.debian.org/trixie/systemd/systemd-detect-virt.1.en.html).
In particular, `--no-upgrade` is not treated as a guarantee for all dependencies;
the actual-action guard establishes the narrower allowed transaction.
