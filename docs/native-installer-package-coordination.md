# Native package coordination

Status: **process-lifetime package guards and fail-closed boot-drift checks are
implemented. Combined qualification and public release gates remain separate.**

## What is protected

Native host inspection, preparation outside its own APT commands, and the entire
boot backend's `Arm` operation now hold Linux OFD read locks on the existing
`/var/lib/dpkg/lock-frontend` and `/var/lib/dpkg/lock` files. These exclude the
POSIX write locks used by APT/dpkg. Nested read-only inspections remain compatible;
Stackfort's separate installer lock still serializes its own mutations.

The guard uses nonblocking acquisition, trusted directory traversal, read-only
opens, no-follow/close-on-exec descriptors, and regular-file/root-owner/single-link
checks. Missing, writable, unsafe or replaced locks stop the operation. It never
creates, truncates, deletes or replaces package-manager lock files. A failed
second acquisition releases the first lock. File identity is rechecked after
acquisition, at important handoff points, and before returning success.

OFD locks are deliberate: unlike process-associated POSIX locks, opening and
closing the same file elsewhere in a Go process does not drop them. They still
conflict with ordinary POSIX write locks. This follows the Linux
[fcntl locking semantics](https://man7.org/linux/man-pages/man2/f_ofd_getlk.2const.html)
and Debian's [dpkg locking guidance](https://wiki.debian.org/Teams/Dpkg/FAQ#db-lock).

## Stackfort's own APT transactions

APT must acquire its own locks. The guard releases its descriptors before each
private APT invocation and reacquires both afterward, including on ordinary APT
failure. It does not disable APT locking, pass a privileged lock-bypass environment
variable, or leak lock descriptors into child executables. No automatic-update
service or timer is stopped, masked or permanently disabled.

This creates a deliberate handoff gap, not atomic lock ownership transfer. A
competing writer may win it. Failed reacquisition stops preparation and preserves
the prerequisite recovery journal. A successful reacquisition alone is not
enough to accept the result:

- Planning repeats the host/package/boot snapshot under the reacquired guard.
- The actual protocol-v2 APT hook checks both the package inventory and original
  boot layout while APT holds its own locks, before the reviewed transaction.
- After installation, the **entire** package inventory must equal the original
  inventory plus the exact approved new packages. An unrelated addition, removal
  or upgrade is rejected, even if all requested prerequisite versions are right.
- Final boot identity, geometry, artifacts and package receipt are rechecked
  before sealing. The guard remains held through preparation's remaining work.
- Arming acquires a new guard and checks the current full package inventory
  against the sealed completed-prerequisite receipt before touching boot artifacts.
  It retains the guard through initramfs construction and GRUB selection.

A failed consumed preparation or arming attempt is not reset or silently retried.
Use the existing read-only recovery handoff and preserve evidence.

## Boot handoff and remaining boundary

These locks coordinate **cooperating package-manager writers during the guarded
process lifetime**. They are not a durable maintenance fence:

- They are released when the owning process exits or is killed and do not span
  the interval between successful preparation, a later Arm call and reboot.
- Direct administrator commands such as `update-initramfs`/`update-grub`, manual
  boot-file edits and tools that ignore dpkg locks are not excluded.
- The APT handoff gap is checked, not prevented. Concurrent APT index/cache refresh
  is not globally serialized by these two dpkg locks.

The filesystem-safety boundary does not depend on those locks surviving reboot.
Arming uses a [private initramfs configuration](native-installer-private-initramfs.md),
so an unrelated ordinary rebuild cannot copy Stackfort conversion hooks into its
normal image. The journal records `arming` before boot work starts; only a fully
sealed `awaiting-reboot` operation can authorize the conversion initramfs. Death
of an arming process therefore cannot turn an incomplete build into authorization.

Before its first filesystem check/write, the early boot path requires the exact
operation/source/tool receipts, an unmounted target, current machine/disk identity,
normal kernel/initrd/GRUB hashes, and durable consumed boot authorization. Ordinary
writers from the prior running OS do not survive that reboot into the unmounted
initramfs. Earlier boot-artifact drift is rejected before metadata writes, without
falling through to mount the root. Later non-boot package-inventory drift can
instead quarantine finalization after safe offline conversion: an availability
failure, not permission to replay conversion. Completion evidence is synced before
the recovery boot selection is retired.

No concrete unsafe conversion interleaving from ordinary package maintenance has
been established that requires an additional persistent global writer fence in
this qualified single-host plain-root model. Maintenance quiescing can improve
availability, but should not disable security updates indefinitely. Late drift,
process loss, recovery and the [normal kernel lifecycle](native-installer-kernel-lifecycle.md)
need retained qualification for each relevant change. The
[later internal private-image/kernel cycle](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md)
passed with a separately pinned installer and older beta.3 payload, not the
final beta.4 tag archive. This does not serialize every possible writer or
qualify arbitrary privileged hooks and provider layouts.

Arbitrary root changes, custom privileged initramfs hooks/background block writers
and unsupported provider storage/boot layouts remain outside this guarantee.
Private configuration is not a hermetic build or a sandbox for root-owned hooks.

See the [dated qualification](../infra/host-tests/results/2026-09-12-native-package-guard.md).
