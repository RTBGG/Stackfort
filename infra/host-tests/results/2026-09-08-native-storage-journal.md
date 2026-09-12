# Native storage journal and installer guards — 2026-09-08

Outcome: **control-plane state protocol, Linux persistence and production
entry-point guard tests pass**. Automatic native storage preparation remains
disabled. This is not a release qualification, new conversion benchmark or
physical power-loss test.

See the [design and remaining gates](../../../docs/native-quota-installation-state.md).

## Tested scope

Base commit `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2` plus working-tree changes:

- `internal/storageprep`: source/host/manifest-bound phases, one-shot staging
  and resume, terminal recovery, strict transition validation, private Linux
  journal and shared installer lock.
- `internal/installapply`: reject unqualified storage journals when creating
  the installer/updater runner and when reading the installation journal.
  Also avoid dereferencing failed file metadata reads.
- `tests/integration/host_storage_journal_linux_test.go`: actual fixed-path
  installer guards exercised in a private mount namespace, using temporary
  `/var/lib` content instead of modifying the guest's real installation state.

Linux execution used the existing `stackfort-native-quota-debian-13` VM,
Debian 13 / kernel `6.12.107+deb13-cloud-amd64`. Only this VM was started for
these tests. No new quota conversion, checkpoint restore, partition operation
or installation was requested. The existing prototype's resume/consumer services
were both active afterward, and the real production storage journal was absent.
The VM was then shut down normally.

## Results

| Check | Result |
| --- | --- |
| Full ordinary Go suite on Windows | Pass |
| Linux build/vet for storage preparation and installer | Pass |
| Linux integration build/vet | Pass |
| Storage preparation unit suites on Linux root | 11 top-level tests pass, including fault-injection and filesystem subtests; no skips |
| Existing installer unit suites on Linux root | 34 top-level tests pass |
| Actual production entry-point integration | Pass, all six valid journal phases and corrupt state rejected |
| Absent-journal compatibility | Pass; no directory/journal creation by inspection |
| Cross-component installer lock | Pass; actual installer lock acquisition conflicts with the storage store |

All five normal journal-write boundaries were simulated with both possible
outcomes of a failed persistence acknowledgment: unchanged old state and
persisted new state. No callback runs after a failed intent save. Ambiguous
staging/resume enters recovery on the next invocation; successful persisted
states continue without duplicate side effects. Missing/invalid boot proof,
source/host/kernel drift, readiness loss and failed recovery persistence are
also covered.

Linux filesystem tests reject unsafe ancestors, file permissions/owners,
symlinks, hard links, FIFOs, directories, excessive file size, truncated JSON,
unknown/duplicate/case-aliased keys, invalid phases and trailing data. Rejected
targets and link sentinels remain unchanged. Save/close/reopen persistence and
temporary-file cleanup pass.

The integration child checks a separate mount namespace, makes mount
propagation private, and bind-mounts only a temporary directory over its own
view of `/var/lib`. It verifies the real `OpenFileStore`, installer `Load`,
`NewLinuxRunner`, `NewLinuxUpdateRunner` and installer-lock entry points. The
namespace and test files are discarded when the test exits.

## Retained local evidence

Files are under ignored `infra/host-tests/work/`. SHA-256:

| File | SHA-256 |
| --- | --- |
| `storageprep.test` | `9400b0deacd5df63bbe1c79b4a67e6cb63ff44775731a44b75fd29b4bf2195e6` |
| `storage-journal-integration.test` | `2ab54c20fb7e1bc6595ca76f4e8fc503baf896bac25e8206d0bef1f7a416a348` |
| `storage-journal-installapply.test` | `36c5b42981a7c0e9d2fb4b9fa1e1b4871bf552b2608c426346d243469c1767e1` |
| `storageprep-linux-tests.log` | `755de0096003d804f89d2f1823c9b236712e07785d227356523a8f1735198c55` |
| `storage-journal-integration-linux.log` | `7be7638bfffe2decdd5a2ab15f0ed63afa4dfc1cbd18d8cbd590c46725c5ad25` |
| `storage-journal-host-state.log` | `8d3290570062bd44bfb2bd27cb74feeef3a6f6d9ae16d7ea1970e0247ca973f8` |

These identify the final executed test binaries and logs, not a published
installer artifact. No commit, push or release was performed.

## What this does not prove

The backend callbacks in the state-machine tests are fakes. The previously
tested native conversion helper is not yet connected to this protocol. No
production command can arm it, dispatch a reboot or reconcile native storage.
The root-resident journal does not solve journaling of an offline root
filesystem mutation. Physical interruption, one-shot boot artifact execution,
safe recovery and complete updater/kernel/package coordination remain open.

Fresh-server eligibility, pinned release staging, real service dependencies,
capacity admission/OS reserve, enforcement reporting and full Debian/Ubuntu/
Rocky onboarding must still be implemented and qualified. The public installer
continues to require prepared hosting storage; the customer's VPS was not used.
