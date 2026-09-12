# Native installer boot services

Status: **real installer entry points with Debian laboratory qualification**.
The public interactive `onboard` coordinator is implemented, but exact tagged
candidate qualification and release publication remain pending. These services
enforce the sealed boot/admission boundary; their existence does not mean
arbitrary provider images can be converted safely.

## What moved out of the test executable

The current `stackfort-installer` contains an internal `native-service` dispatcher:

| Service action | Responsibility |
| --- | --- |
| `close` | Establish the operation-owned web-port quarantine before networking. |
| `verify-storage` | Recheck an existing ready storage plan before the hosting bind mount. |
| `admit` | Verify the retained release, consume any exact reviewed approval, execute or verify the nine installation stages, then admit web traffic. |
| `quarantine` | Close the gate and stop managed consumers independently of the interrupted process and installer lock. |

The package wrapper deliberately does not forward this internal interface.
The higher-level [interactive onboarding](installer-installation.md#interactive-native-setup)
selects authenticated release inputs and collects fresh-host/reboot consent;
these internal actions still have no public device override, reset, force or
automatic-recovery shortcut.

The deterministic systemd units execute the sealed **real installer**, not a Go
test program. The admission unit's `ExecStopPost` also uses that installer. It is
not ordered after its consumers: cleanup must be able to stop them without
waiting on its own systemd stop transaction. The early gate is ordered after an
already-enabled `nftables.service` and before `network-pre.target`; it does not
enable nftables' distribution service itself.

## Immutable runtime intent

Before creating the storage journal, the internal preparation API seals:

- a separately bounded, root-owned, exclusive runtime executable (mode `0500`);
- a canonical private runtime intent (mode `0600`);
- the executable digest, the offline boot-capsule digest, expected filesystem
  geometry/features, and fstab/kernel/initrd/GRUB digests.

The runtime-intent digest becomes the authenticated release manifest's boot
binding. It cannot be attached retroactively to an existing storage journal.
The source stage retains the same installer/storage lock throughout sealing.
Partial artifacts are preserved and never silently overwritten or adopted.
Even a pre-journal runtime fragment blocks the ordinary installer/updater, and
operator status reports it as orphan evidence rather than an empty installation.

Admission checks both the pinned file and the currently running executable
inode. It verifies exact service/mount files, manager fragment paths, absence of
loaded overrides, reload state, and the required consumer storage dependencies.
The original `debian-native-post-ready-qualification-v1` profile retains its
historical lab handoff. The [offline-preparation follow-up](native-installer-preparation.md)
adds a distinct profile with typed initial preparation and the real installer
inside the one-shot initramfs. Both use `/srv/stackfort-native-hosting`.

## Read-only storage backend

The runtime backend cannot arm or resume storage conversion. It requires the
exact existing `ready` plan and independently checks:

- host DMI identity, current boot, kernel, plain root partition UUID/PARTUUID;
- ext4 identity, unchanged geometry and permitted feature delta;
- root and hosting mount identity, `rw,prjquota`, actual bind-source inode;
- kernel project-quota **accounting and enforcement**, not just mount options;
- unchanged fstab and normal boot artifacts;
- consumed and unarmed one-shot state, with preparation hooks/menu/initrd retired.

The pre-mount verifier checks the root and bind source without mounting anything.
Admission additionally requires the live hosting bind mount. Existing durable
storage/admission recovery rules remain in force; a saved ready/admitted flag is
never sufficient permission to expose web listeners.

## Qualification boundary

Use the dedicated Debian driver with `-Stage Prepare -WithRelease
-WithInstallation -WithRuntime`. The first conversion and its proof-checked
finalization still use the earlier lab helper. Its resumer has a current-boot
proof condition and is skipped on normal boots. The synthetic dependent service
no longer executes a test guard on those boots.

`RuntimeInterrupt` stops admission and interrupts a subsequent **read-only
revalidation of an already complete package journal**. It does not kill apt or
dpkg during installation. Recovery uses the existing real operator CLI and
one-time reviewed approval; the service itself performs the continuation.

The retained beta.3 archive remains unchanged and authenticated. The newer
dispatcher is a separately pinned local qualification artifact, **not** a newly
signed release executable. The [dated evidence](../infra/host-tests/results/2026-09-10-native-installer-runtime.md)
records what was actually exercised.

The [next preparation layer](native-installer-preparation.md) replaces the initial
offline lab backend/handoff with installer code. Still open: prerequisite
ordering and provider-image eligibility,
then qualify external package/kernel coordination, capacity reserve, the full
listener/firewall boundary, additional OS images, and a freshly signed candidate.
Externally initiated firewall flush/reload and arbitrary tenant-published ports
are not covered by this fixed-web-port admission gate.
