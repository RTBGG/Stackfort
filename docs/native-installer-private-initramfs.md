# Private native initramfs configuration

Status: **implemented and qualified in a complete internal Debian boot cycle on
2026-09-12**. The retained beta.3 payload plus separately pinned installer is not
the final beta.4 tag archive; public exact-candidate qualification remains required.

The one-shot quota-conversion image no longer requires temporarily installing
Stackfort hooks into the ordinary `/etc/initramfs-tools` tree. Arming now uses
`mkinitramfs -d /var/lib/stackfort-installer/native-boot-build/config` with the
already selected, validated kernel and dedicated one-shot output image.

This prevents an unrelated normal `update-initramfs` invocation from picking up
Stackfort's temporary conversion scripts. It does **not** establish a persistent
package/kernel maintenance fence by itself.

## Configuration and evidence

- The original local configuration is read through root-controlled, no-follow
  directory/file descriptors. Symbolic links, hard-linked files, special files,
  foreign ownership, group/other-writable entries, special mode bits, cross-device
  children and unsafe names are rejected rather than followed or normalized.
- Scanning is bounded to 128 entries, eight nested directory levels, 4 MiB per
  file and 16 MiB total. The two added scripts and required directories also count
  toward the final entry limit. Unsupported local layouts fail before a build is
  created; there is no silent fallback to global hooks.
- Existing safe contents and permission modes are copied unchanged. The default
  `initramfs.conf` must exist. Required local hook/script directories are added
  only when absent. Existing Stackfort-named hooks cannot be adopted or replaced.
- Only the private tree receives the real sealed installer hook and the
  operation-bound premount script. The guarded profile keeps its fail-closed
  initramfs behavior. No test executable or test environment flag is introduced.
- A new root-owned `0700` build directory contains a canonical, exclusive `0600`
  `intent.json` receipt. It binds the operation, manifest, kernel and source/private
  configuration tree digests. The receipt and copied files/directories are synced.
  Partial builds remain as evidence; existing builds are never overwritten,
  reused or silently removed.
- Both source and private configuration are re-read and compared before and after
  `mkinitramfs`. Drift stops arming. The existing one-shot image inspection and
  artifact-digest receipt remain in force.

## Deliberately unchanged boundary

Debian's alternate configuration option redirects local configuration, local
hooks, local boot scripts, module lists and DSDT input. It still includes the
normal package-provided `/usr/share/initramfs-tools` defaults and hooks and reads
installed kernel modules and other system dependencies. Stackfort therefore does
not substitute a minimal homemade initramfs for Debian's normal hardware support.

This is **local-configuration isolation, not a hermetic build**. Package-provided
inputs remain subject to the existing process-lifetime package guards. Arbitrary
administrator-owned shell hooks retain their normal privileges; copying them is
not sandboxing them. The operation-bound journal/boot authorization and offline
normal kernel/initrd/GRUB hash checks reject incomplete arming or checked drift
before metadata writes. Ordinary prior-OS package writers cannot survive reboot
into the unmounted conversion initramfs. A persistent global maintenance fence is
not an established additional filesystem-safety prerequisite; maintenance
quiescing concerns availability and does not justify indefinitely disabling
updates. Process-loss and late-drift behavior still require qualification, and
custom privileged hooks/background block writers are not covered by that claim.
See the [coordination boundary](native-installer-package-coordination.md).

The implementation was checked against the official Debian
[initramfs-tools 0.148.4 source archive](https://deb.debian.org/debian/pool/main/i/initramfs-tools/initramfs-tools_0.148.4.tar.xz)
and the documented
[`mkinitramfs -d` option](https://manpages.debian.org/trixie/initramfs-tools-core/mkinitramfs.8.en.html).
Tests cover preserved defaults/modes, generated-script binding, unsafe input,
exclusive creation, replay rejection, and source/private/receipt drift in a
private mount namespace. Actual separate-image generation, conversion,
installation, normal reboot and an authenticated OS kernel update/reboot passed
in the [combined internal qualification](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md).
The public archived installer, late-drift/failure cases for changed source and
release gates must still be qualified; this is not public beta.4 evidence.
