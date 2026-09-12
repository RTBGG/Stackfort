# Durable release source for native installation

Status: **implemented and Linux-tested internal staging API; public installer
activation remains disabled**. Actual package continuation is a separate
[lab-only follow-up](native-quota-install-continuation.md).

The [journal-bound boot experiment](native-quota-boot-handoff.md) can already
prepare native quotas and resume its synthetic consumer. This follow-up supplies
a prerequisite for the real installer: its release files must outlive the
bootstrap's temporary download directory.

## Implemented boundary

`installapply.OpenSourceStage` takes the same `install.lock` as the existing
installer and `storageprep.FileStore`. Its fixed destination is
`/var/lib/stackfort-installer/resume-source`, never a caller-selected directory.
The stage holds the lock until closed. The staging-only `Prepare` and `Verify`
methods do not run release executables, install packages, write a storage
journal, arm GRUB or request a reboot. The separate origin/manifest methods
described in the follow-up extend this boundary explicitly.

`Prepare` accepts an already trusted, extracted release and a canonical operation
UUID. Any native storage journal or package-installation journal blocks new
staging, including corrupt/incomplete records. This is not full fresh-server
eligibility: that still belongs to the future preparation coordinator.

Preparation performs the existing release inspection: version, architecture,
complete native WAF/Vinyl matrix, component hashes, required files and static
ELF checks. It additionally requires an owner-executable static installer and
scans all entries before copying. Symlinks, hard links, special files, foreign
UID/GID, unsafe permission bits, nested devices and excessive size/count/depth
are rejected. Source ancestors must be root-controlled; the root-owned sticky
`/tmp` and `/var/tmp` directories are allowed only as input ancestors.

The copy is independent, not a link to the original. Data, executable bits and
directory permissions are preserved beneath a private root-owned `0700`
directory. Files and directories are synced, then the copied release is checked
again before a canonical `0600` receipt is written and synced last. No success
receipt is published for a mismatched copy.

The receipt binds these independently checked values:

| Field | Meaning |
| --- | --- |
| Operation UUID and version | One selected preparation/release, not a moving latest channel |
| Source digest | The existing installer's complete release-content identity |
| Tree SHA-256 | Sorted directory/file inventory, relative paths, permission bits, file sizes and content hashes |
| Installer SHA-256 | The exact retained `bin/stackfort-installer` bytes |
| Manifest SHA-256 | The exact retained `RELEASE-MANIFEST.json` bytes |

`Verify` requires an expected pin supplied independently by the caller; it never
treats a receipt read from disk as sufficient authority. It rehashes the entire
tree and re-runs release inspection. `VerifyForPlan` additionally checks the
storage plan's operation, release version and source digest. The pin's canonical
digest is available for inclusion in a validated boot manifest. The subsequent
[release-origin binding](native-quota-release-origin.md) now connects the pin,
actual attestation verification and the laboratory boot manifest.

## Failure and trust boundaries

The stage directory is persisted before copying. A directory without a complete
valid receipt blocks verification and repeated preparation. Partial files and
receipts remain for diagnosis; there is no automatic deletion, recopy, repair,
version substitution or network fallback. A complete matching stage can be
verified again without writes. Existing unsafe entries are never fixed in place.

Receipt parsing rejects unknown/duplicate/case-aliased fields, trailing data,
invalid values and oversized/noncanonical JSON. Verification also catches safe
but changed file modes, added empty directories, missing files and valid-looking
replacement pins that differ from the caller's expected values.

These are **local integrity and persistence guarantees, not new publisher
authentication**. The current bootstrap uses HTTPS GitHub release downloads and
checksums; staging alone neither upgrades that to attestation verification nor
trusts a hash solely because it appears next to the files. The separate
`BindOrigin` method authenticates the archive before manifest sealing; a
production caller must establish origin before authorizing release execution.
The existing updater's attestation route is not silently reused or bypassed.

Root-controlled paths are assumed stable against non-root callers. This is not
protection against a hostile root process rewriting the source, manifest and
journals, nor complete serialization with external kernel/package managers.
Sync calls and simulated incomplete stages do not establish arbitrary power-loss
behavior on every controller/filesystem.

## Qualification

Linux root unit tests cover removal of the original bootstrap files, close/reopen,
tampering, permissions, links, malformed receipts, incomplete stages and bounds.
The fixed-path integration test runs in a private mount namespace and uses the
retained beta.3 candidate as **data only**. It stages the real release, removes
its temporary extraction and verifies it in a new process with no download.
It also checks actual installer/storage lock conflicts and proves that source
verification does not bypass the production native-storage gate.

The actual VM's `/var/lib`, storage journal, boot configuration and services are
not replaced by the namespace fixture. See the [dated evidence](../infra/host-tests/results/2026-09-08-resume-source-staging.md).
This test did not combine source staging with a new conversion/reboot cycle or
execute the retained installer.

```sh
go test ./internal/installapply ./internal/storageprep
# Linux root in a disposable environment includes the private filesystem tests.
```

For the dedicated native Debian VM, place the retained beta.3 archive at
`/tmp/resume-source-beta3.tar.gz` (the integration test pins its checksum), then:

```sh
sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 \
  ./integration.test -test.v -test.run='^TestDisposableResumeSourceStage$'
```

## Next gates

The [origin/manifest follow-up](native-quota-release-origin.md) connects those
components under the shared lock in the laboratory. The separate
[package continuation](native-quota-install-continuation.md) uses the existing
installer stages. Next qualify production interruption/recovery and package/kernel coordination,
and test the full fresh-server browser flow. Capacity reserve and multi-OS
provider-image qualification remain open. No public flag enables this path;
every native storage journal, including `ready`, still blocks production entry.
