# Native quota installation state protocol

Status: **implemented control-plane foundation; native installer activation is
still disabled**. This builds on the [Debian native quota experiment](native-quota-prototype.md),
not a new public release or a completed automatic-storage installer.

## Implemented boundary

`internal/storageprep` provides a strict state machine and a Linux file store.
There is no production backend, public preparation command, automatic reboot,
boot hook or filesystem conversion in this package. A subsequent
[one-shot boot experiment](native-quota-boot-handoff.md) now connects the protocol
to the offline helper through a laboratory backend. This does not register a
production backend or enable the public installer.

The [durable release staging follow-up](native-quota-release-staging.md) adds a
shared-lock, integrity-checked local copy for future continuation. The
[release-origin follow-up](native-quota-release-origin.md) binds its authenticated
pin into the laboratory boot manifest; the real installer coordinator remains open.

The private journal is `/var/lib/stackfort-installer/storage-state.json`. Its
state directory is root:root `0700`, files root:root `0600`. Descriptor-relative
access rejects symlink ancestors/targets, hard-linked files, special files,
foreign ownership, unsafe permissions and oversized/corrupt JSON. Canonical JSON
also rejects duplicate keys, case aliases, unknown fields and trailing data.
Saves use an exclusive temporary file, file sync, atomic rename and directory
sync. Existing invalid state is retained, not overwritten.

The store holds the same `install.lock` used by the normal installer. Callers
must retain that lock throughout observation, state changes and callbacks.
The old `install-state.json` schema and package-stage sequence are unchanged.

The plan binds operation ID, release version/source digest, Debian distribution,
DMI VM UUID, filesystem/partition UUIDs, previous boot ID, kernel and the digest
of a future independently validated offline manifest. That manifest must pin
topology, geometry, fstab and staged boot/release artifacts. The envelope alone
does not prove fresh-server eligibility. Arbitrary device paths and commands
are not accepted. Ubuntu and Rocky plans are deliberately rejected for now.

## State transitions

| State | Permitted next action |
| --- | --- |
| `planned` | Persist `arming` and attempt artifact staging once, on the original boot only |
| `arming` | A new invocation cannot know how far staging progressed: require recovery |
| `awaiting-reboot` | Same boot: wait without another staging attempt; new boot: validate proof |
| `verifying` | Persisted before post-boot reconciliation; interruption requires recovery |
| `ready` | Recheck live readiness; never infer current enforcement from the saved flag |
| `recovery-required` | Terminal; no automatic reset, rollback, retry or journal deletion |

The normal sequence is `planned → arming → awaiting-reboot → verifying → ready`.
An error or identity drift may instead enter `recovery-required`. The store
itself rejects skipped/backward transitions and rebinding, not just the engine.

Only successful current-boot proof allows post-boot reconciliation. The future
backend must revalidate identities, geometry and the pinned manifest; then check
both kernel quota accounting and enforcement, managed mounts and persistent
configuration. Reconciliation must not re-run offline conversion. A kernel
change during this transaction currently requires recovery, not implicit re-arming.

`Waiting` is a status, **not** permission for a scheduler to repeatedly reboot.
No production reboot-dispatch implementation exists yet. The one-attempt latch bounds the
artifact-staging callback, not the execution count of an initramfs helper.

## Integration and recovery limits

The production installer checks the new journal before constructing its Linux
runner and again when loading its installation journal, including under the
installer lock. The updater's Linux-runner construction also checks it. **Any
native preparation journal, even `ready`, currently blocks these entry points**:
there is no qualified native resume backend that could safely admit it yet.
Absence preserves the previous prepared-filesystem workflow; inspection creates
no state. No public flag can bypass this gate.

This is an entry-point guard, not complete mutual exclusion with an already
running updater or a kernel/package manager. Cross-process boot/updater/package
coordination must be completed before any production preparation writer ships.

Do not delete or hand-edit a recovery journal to force installation. Retain the
journal, original/pinned boot artifacts and console logs for diagnosis. There
is no automatic root-filesystem rollback or supported recovery command yet.
In particular, attempting to remove quota features is not a rollback strategy.

The journal is stored on the root filesystem: it is not a writable journal for
an offline initramfs conversion of that same filesystem. The earlier RAM-only
mutation marker remains insufficient for power-loss recovery. This change
therefore **does not claim crash-safe filesystem conversion** or make mounting
the target root during offline repair acceptable.

## Verification

- Unit tests cover the normal before/after-boot protocol, live-readiness loss,
  source/host/kernel drift, one-shot staging/resume, terminal recovery and invalid
  transitions. All five normal save boundaries are tested with both ambiguous
  persistence outcomes: old state survives or new state persists despite error.
- Linux root tests use temporary directories for real descriptor access,
  locking, atomic persistence/reopen, permissions and malformed-file rejection.
- A dedicated Debian integration test runs the actual installer and updater
  entry-point guards in a private mount namespace. Only that child sees a
  temporary `/var/lib`; the real host journal is not changed. All valid phases
  and corrupt state block; an absent journal remains a read-only no-op.

These are protocol/error-injection and filesystem API tests, **not physical
power-cut tests**, repeated native conversion tests or performance benchmarks.
See the [dated evidence](../infra/host-tests/results/2026-09-08-native-storage-journal.md).

Reproduce unit tests:

```sh
go test ./internal/storageprep ./internal/installapply ./cmd/stackfort-installer
# Run as root only in a disposable Linux environment to include private-file tests.
```

The integration test requires the existing dedicated Debian lab and opt-ins:

```sh
sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 \
  ./integration.test -test.v -test.run='^TestDisposableNativeStorageJournalGate$'
```

## Next implementation gates

1. Fresh-server/topology eligibility, typed immutable manifest and pinned release
   staging outside the bootstrap's temporary directory.
2. One-shot boot handoff, interruption-safe early-boot recovery, verified cleanup
   of hooks and original boot artifacts; no automatic retry of uncertain repair.
3. Wire post-boot continuation into the package installer and real service graph;
   serialize against updates and package/kernel/initrd changes.
4. Account capacity admission and OS reserve on shared native storage; expose
   actual enforcement in capabilities and the setup UI.
5. Full installer/browser flow, interruption/recovery and ordinary provider-image
   qualification on Debian 13, Ubuntu 26.04 and Rocky Linux 10 before activation.
