# Native conversion and normal OS kernel updates

Status: **internal lifecycle policy and a real updated-kernel reboot qualified
on 2026-09-12**. This used retained beta.3 platform files with a separately pinned
installer, not the final beta.4 tag archive or public onboarding path.

The kernel and normal boot-file digests selected for offline quota conversion
must not permanently prevent Debian from applying normal kernel/initramfs/GRUB
updates after conversion has completed. The native runtime now distinguishes
these two lifecycles without changing the historical conversion plan.

## Before the first verified Ready state

The original kernel identity and normal kernel/initrd/GRUB hashes remain exact
requirements throughout preparation, arming, offline conversion and finalization.
A changed kernel before the first verified `Ready` transition is rejected by the
storage engine before another backend action.

Finalization writes `native-boot-completed.json` while the journal is still
`verifying`. That receipt alone does not relax anything: the subsequent first
live readiness check still uses all conversion-era pins. Only after it succeeds
can the durable journal transition to `ready`.

## After an already recorded, verified conversion

Every readiness check obtains policy from the still-locked source stage, not a
caller-provided `ready` boolean. Normal OS boot-artifact handling requires all of:

- an existing, valid `ready` journal with the exact immutable plan;
- the sealed runtime intent and specification bound to that manifest;
- reverified retained release/source provenance and prerequisite/decision receipts;
- a canonical, private completion receipt matching operation, manifest and the
  journal's recorded conversion boot ID.

Missing, inconsistent, unsafe or unreadable evidence remains an error. A direct
backend caller without the verified locked stage retains the strict original
pin policy. Historical post-ready-only qualification fixtures also retain their
original policy; they do not acquire conversion authority from this change.

With that past success established, a syntactically valid new running kernel
may be used without rewriting the conversion journal or requiring old kernel
packages to stay installed. The current kernel image, current initrd and normal
GRUB configuration must still be nonempty, bounded, root-owned regular files
with trusted ancestors, safe modes and no hard/symbolic links. Their contents
are now owned by the normal OS update lifecycle, not compared forever against
the old conversion-era digests.

This trusts the administrator and the operating system's package/update trust
chain for later boot-software changes; it is not cryptographic attestation of a
newly installed kernel. An administrator deliberately replacing trusted boot
software is outside this storage-conversion guarantee.

## Checks that remain mandatory on every boot

Live verification still checks exact machine/root/partition identity, a valid
new boot ID, the original filesystem geometry, acceptable feature changes,
project quota inode/accounting/enforcement, root and hosting mount properties,
the correct hosting bind source, and the managed `fstab` digest. The temporary
conversion image, custom GRUB script and global conversion hooks must remain
absent. The one-shot GRUB selection must be retired and its operation-bound
consumed latch must remain intact.

Source/manifest/runtime verification, unit/dependency integrity, public listener
quarantine and installation-admission policy also remain in place. A historical
`ready` journal is not unconditional permission to serve hosted data.

The storage engine only delegates changed-kernel handling to the backend for an
already existing `ready` state; it still calls live verification and records a
terminal recovery state if it fails. It never rearms, reconverts or rebinds the
historical plan to the new kernel.

## Qualification boundary

Policy tests cover pre-ready kernel drift, later syntactically valid kernel
changes, mandatory live verification, unchanged historical plan, missing and
mismatched completion records, invalid state/intent, source-stage/lock gating,
and unsafe current boot files. The [combined Debian qualification](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md)
passed real conversion, normal subsequent boot, and an authenticated Debian
kernel update from `6.12.107+deb13-cloud-amd64` to `7.1.8+deb13-cloud-amd64` followed
by reboot. Quotas/isolation and OCI lifecycle/subordinate-ID checks passed with
an unchanged historical storage journal and no reconversion/reinstallation.
The exact final tag archive still needs its own qualification; later source
changes and arbitrary future kernels do not inherit this result automatically.

This fixes the permanent conversion-pin lifecycle problem. Earlier handoff safety
still requires exact operation authorization and pre-write checks in the unmounted
conversion initramfs. Ordinary prior-OS package writers cannot survive that reboot;
checked drift rejects rather than authorizing a changed conversion. An additional
persistent global writer fence is not an established filesystem-safety prerequisite.
Maintenance quiescing can reduce availability failures, while late-drift/recovery
tests remain required. This does not claim hermetic construction or safety against
arbitrary privileged hooks; see the [coordination boundary](native-installer-package-coordination.md).
