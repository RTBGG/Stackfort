# Release-origin binding for native quota preparation

Status: **internal API and Debian laboratory integration; public installation
remains disabled**. The separate [installation follow-up](native-quota-install-continuation.md)
adds an explicit lab-only real package/service continuation path.

This connects [durable release staging](native-quota-release-staging.md) to the
[one-shot boot experiment](native-quota-boot-handoff.md). Local checksums alone
cannot establish a publisher: an archive, its extracted files and its boot plan
must describe the same authenticated release.

The [dated Debian results](../infra/host-tests/results/2026-09-09-native-release-origin.md)
record the successful conversion and normal reboot, all eight origin scenarios,
exact artifacts and remaining qualification limits.

## Authentication and retained evidence

`SourceStage.BindOrigin` runs while holding the existing shared installer lock.
It copies the archive and attestation bundle into fixed private
`/var/lib/stackfort-installer/resume-origin` paths, with exclusive creation,
bounded input, root ownership, safe metadata and file/directory syncs. It then:

1. Checks the retained GitHub CLI executable against an independent upstream
   binary SHA-256 pin, before executing it. A hash declared by the candidate
   itself cannot authorize that verifier.
2. Runs actual `gh attestation verify`, requiring the exact repository,
   workflow certificate identity, GitHub OIDC issuer, source ref, source commit
   and signer commit, and rejecting self-hosted signing runners.
3. Compares every file in the signed archive with the retained extraction and
   rejects additional source files. Archive paths, types, duplicate entries and
   size/count bounds are checked without extracting or executing members.
4. Requires the source's `COMMIT` to match the selected attested commit.
5. Publishes the canonical private success receipt last. It binds the source
   pin, policy, archive digest, bundle digest and approved verifier digest.

The verifier runs without a shell, inherited GitHub credentials, configuration
or caller-selected flags. Its timeout is 45 seconds and each output stream is
limited to 64 KiB. The pinned verifier is GitHub CLI 2.99.0, Linux amd64. Updating
that pin is a reviewed code change, not automatic acceptance of a candidate's
bundled tool. See the [official verification options](https://cli.github.com/manual/gh_attestation_verify).

Initial verification may fetch Sigstore trust roots over HTTPS. It is **not an
offline initial-verification implementation**. Post-boot checks use the sealed
receipt and rehash the retained evidence/source; they do not rediscover trust
roots, download replacement artifacts or rerun the signature verifier.

## Closed origin policies

| Class | Required origin |
| --- | --- |
| `tag-release` | Exact `refs/tags/v<VERSION>`, release workflow identity for that tag, and the independently selected commit |
| `lab-candidate` | Only retained `0.1.0-beta.3`, commit `5282946bec1f865de7222128a6a5d0d8a656f34c`, signed by the release workflow at `refs/heads/main` |

The existing candidate was signed from `main`, not from a release tag. Its
successful laboratory verification cannot qualify the tag-release path. The
candidate exception is exact and explicit; there is no general branch fallback
or operator-supplied verification bypass. Public release selection, tag/commit
resolution and replay/rollback policy still need production orchestration.

## One manifest and one lock

`NativeReleaseManifest` binds the authenticated release to the host identities,
previous boot, kernel and digest of the complete typed laboratory boot intent.
That intent includes filesystem geometry, fstab, helper, normal kernel/initrd,
main GRUB configuration and the explicit laboratory fault mode. Its canonical
digest becomes the storage plan's manifest identity. The source version/digest
now refer to the retained release, not to a synthetic helper version.

`SealManifest` verifies source and origin evidence, exclusively persists the
canonical manifest, then saves `planned`, under the same lock. It cannot replace
a different manifest, rebind an existing journal or reset an armed operation.
A complete matching manifest left before journal creation can be reverified;
partial or conflicting bytes are retained and rejected, not overwritten.

`AdvanceManifest` rechecks the manifest, source tree and evidence under that
lock before calling the storage backend. Changes produce terminal
`recovery-required` / `release-invalid`, before any backend arm/resume call.
An existing recovery state is never automatically retried or repaired.

The envelope is not itself proof of a safe host. The backend must derive it
from the actual boot intent and retain all live host, boot, offline filesystem,
one-shot and readiness checks. No production backend is registered by this API.

In the release-bound lab, the early helper additionally reads the root's
canonical release manifest and source/origin receipts through fixed read-only
`debugfs` requests, without mounting the target. They must match the embedded
intent before the first potentially writing filesystem check. The early helper
does not hash the full release archive offline; complete content checks occur
before arming and in the post-boot resumer before hosting admission.

## Reproduction boundary

Use only the dedicated Debian VM and an explicit restore of the offline
`native-quota-before-conversion` checkpoint. The retained fixture must exist at
these root-controlled paths; the harness does not download or create it:

```text
/var/tmp/stackfort-origin-lab/
  archive.tar.gz
  attestations.jsonl
  stackfort-0.1.0-beta.3-linux-amd64/
```

The namespace-only signature/negative suite uses that fixture:

```sh
sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 STACKFORT_NATIVE_QUOTA_PROTOTYPE=1 \
  ./integration.test -test.v -test.timeout=12m \
  -test.run='^TestDisposableResumeOriginBinding$'
```

After a fresh checkpoint restore and fixture preparation, the actual boot path is:

```powershell
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Prepare -WithRelease
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage NormalBoot
```

Use unchanged Go sources/compiler settings throughout the sequence: the helper
pins the integration executable. `Validate` and `NormalBoot` request real
reboots. Only `Prepare` selects the release fixture; later stages read the
sealed manifest. Without the separate `-WithInstallation` opt-in, package
installation remains synthetic. Unlike the older source-only test, this flow
executes the independently pinned verifier, never the candidate installer.

## Remaining gates

Root-controlled state is a trust boundary, not protection against a hostile
root process rewriting all manifests, receipts and boot artifacts. Sync calls
and ordinary reboot tests do not qualify arbitrary power loss or recovery of
partially modified ext4 metadata. The laboratory fixture is not a production
fresh-server coordinator and does not serialize external package managers.

The real [lab continuation coordinator](native-quota-install-continuation.md)
does not close the production gates: capacity reserve/admission, operator
recovery, interruption qualification, package/kernel coordination and ordinary
Debian/Ubuntu/Rocky onboarding still need implementation and qualification.
Every native journal, including `ready`, continues to block the public
installer/updater. No public flag enables this experimental path.
