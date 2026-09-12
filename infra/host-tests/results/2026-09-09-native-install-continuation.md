# Debian native quota: real post-boot installation

Date: 2026-09-09. Scope: internal coordinator and the dedicated disposable
Debian VM only. This is not a new release or public native-storage enablement.
It follows the [origin/boot qualification](2026-09-09-native-release-origin.md)
and implements the [post-boot continuation boundary](../../../docs/native-quota-install-continuation.md).

## Implementation and evidence boundary

The current `SourceStage.ContinueInstallation` drives the existing nine-stage
Linux installation engine after an exact manifest-bound storage `ready` check.
It holds the installer/storage lock, rechecks retained source and live storage
before engine entry points, and binds the separate package journal to the
complete native operation. Ordinary installer/updater constructors and public
journal loading still reject native storage state.

The current integration executable installs the unchanged, authenticated
`0.1.0-beta.3` candidate payload from CI run `34182191221`. The payload archive
SHA-256 is `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0`;
attestation bundle SHA-256 is
`b929c9d6a8e15726f53381bb35e2bc1912ab13908b2fadffe5c73e45178d8344`.
Its source commit is `5282946bec1f865de7222128a6a5d0d8a656f34c`, signed by the
release workflow at `refs/heads/main`, not a release tag. The pinned GitHub CLI
verifier and origin policy are unchanged from the preceding qualification.

The candidate installer executable is copied but never launched for this
continuation. Bundled candidate binaries predate the new native-storage code;
quota/OCI probes exercise current test-linked code. These results do not prove
that every native-root agent RPC works through the older installed agent, and
do not qualify that candidate as a public native-storage release.

## Host and replay

- VM: `stackfort-native-quota-debian-13`, ID
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`.
- Debian 13, kernel `6.12.107+deb13-cloud-amd64`, 2 vCPU, 8 GiB RAM,
  50 GiB root disk and separate NoCloud seed.
- Root UUID: `a88eaa57-e875-4855-a3cb-c231758653f8`; partition UUID:
  `54964bdf-2add-41b7-b41e-6d483962d021`. Device letters are not assumed stable.
- Every complete installation replay restores the offline
  `native-quota-before-conversion` checkpoint,
  `9e62e0ac-89ad-4851-8ae2-e6b936e40eae`.

The final helper SHA-256 is
`1e7940c39ddd8f054dcc6cd3cd14f1fb3999670b0e40b7ab114427db7026e41c`.
The final operation is `73a3d88f-6485-46b1-bbae-61511dbc861c`; its manifest
SHA-256 is `6bd0f927ab25e90ca41d458e6f41914471b2e0573684033f8744033a13a9a9b3`.
Source digest remains
`48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8`.

After preparation, the original fixture directory was renamed to
`/var/tmp/stackfort-origin-lab-retired`. The expected bootstrap path remains
absent during actual arming, conversion and installation. The separate
namespace-only origin suite maps that preserved fixture privately; it does not
restore the original host path or overwrite the host's installation journals.

## Successful installation and normal reboot

The final fresh installation runs all nine real stages in 62.25 seconds in this
lab run, including 113 newly installed distribution packages and no upgrades,
followed by the retained native Coraza and Vinyl packages. This wall time is
not a performance benchmark: repository caches and network conditions vary.
Stage attempts are one each. The shared-lock contention probe fails to acquire
the same lock while real package installation is active, as required.

The conversion boot ID is `740a9fbb-030a-4422-825e-feaa80bdfecd`. After the quota/
OCI probes the package journal SHA-256 is
`ed5d2ff9d4365127764306405cb1d16cf1cbf39bbdba257d65bceef5fe37bba1`,
and storage journal SHA-256 is
`ba9a8cca27efb5fb690cac0459b34247aebc1f82121983e64fb16655540cac1b`.
Both journals remain byte-identical after normal boot
`4d22f206-26b7-4289-9ba5-f1dc260c7fcb`, with one attempt per package stage and
one storage arm attempt. The rerun reports `alreadyInstalled=true`,
`changed=false`, `resumed=false`, and verifies installation in 2.47 seconds.

| Check | Conversion and normal boot |
| --- | --- |
| All nine package/configuration/security/service stages | Complete, no stage replay on normal boot |
| NGINX, MariaDB, Vinyl, agent, API and phpMyAdmin | Active, storage-bound, installation health checks pass |
| API sandbox/AppArmor, panel static/API and phpMyAdmin launch checks | Pass |
| Project hard quotas and cross-account filesystem isolation | Pass |
| OCI private resources and deployment lifecycle | Pass |
| Rootless subordinate-UID writes | Hard quota enforced; 16 MiB append succeeds after increasing quota |
| Normal kernel, normal initrd and main GRUB configuration | Byte-identical to the sealed baseline |
| Public installer constructor/journal gates | Still reject native storage state |

The normal boot has no conversion token or early-helper execution; its final
systemd failed-unit list is empty. The original bootstrap fixture path remains
absent. Rootless quota probes use image
`881a32046cbec149de5f57d15349042946d52f9aeb49e430146b0c6ee714e7a5`, with
202,670,080 bytes written before rejection and recovery append 16,777,216 bytes.

The final Linux installer unit executable passes 53 top-level tests, no skips,
including source/binding metadata conflicts, canonical journal reopening,
closed-stage rejection, phase/plan guards, completed-install rechecks and the
runtime/shared-template regressions. Its SHA-256 is
`712092ce30f65a8227867a0d831624d5280e9340207861622c6228a75373a821`.
The exact final boot helper separately passes all eight actual signature/origin
scenarios in private namespaces plus the offline-reader output-bound test.
Ordinary Windows `go test ./...`, `go vet ./...`, and Linux integration vet for
the touched packages pass as well.

## Actual fail-closed boots with the final helper

Both tests start from the separately saved successful installation checkpoint.
They change only the dedicated disposable VM, preserving the original journal
or checkpoint; they do not simulate a package-manager crash or power cut.

1. Change only the package journal's top-level status from `complete` to
   `applying`. On reboot the package-journal gate rejects the incomplete state.
   Storage remains `ready`; the package journal is not repaired or retried.
2. Restore success, then change the retained source `VERSION` file's mode from
   `0644` to `0640`, leaving its content intact. On reboot origin/source checks
   detect the metadata drift and persist terminal `recovery-required` /
   `release-invalid`, before storage admission or package continuation.

Both pass the same `TestDisposableNativeInstallBlockedBoot`: the installer
service, hosting mount, synthetic consumer, NGINX, MariaDB, Vinyl, agent, API,
phpMyAdmin and panel-renewal service are inactive. Neither boot carries a new
conversion token or rearms filesystem preparation. This supplements the earlier
real partial-package failure caused by the rejected NGINX drop-in.

## Retained evidence

Paths below are relative to ignored `infra/host-tests/work/`. Each boot directory
contains the pinned helper, `tests.log`, `boot.log`, and an archive retaining
source/origin/manifest/journal and lab evidence. Large original/retired initrds
and the helper are excluded from archives; the separate helper and checkpoints
remain available. No raw payload/archive is added to Git.

| Evidence archive or log | SHA-256 |
| --- | --- |
| `native-journal-Prepare-20260909T120509Z/evidence.tar.gz` | `cdc6d35ce334c276af6d3904d51521233539ec0176b76a50e0f48b7875a0114f` |
| `native-journal-Arm-20260909T120611Z/evidence.tar.gz` | `ba46fdfe20e2e0d12dc68f6b64c9bca62402a0a330baca7f11eec0d2ebe56b5b` |
| `native-journal-Validate-20260909T120627Z/evidence.tar.gz` | `6e8adb20eeaa5d0ecebd9a14fa8f6055aac404895a2c340188ded16c8ae31c03` |
| `native-journal-NormalBoot-20260909T120829Z/evidence.tar.gz` | `4ab694a9afea006b5db605eec09adb6f0b6c32ab82ce068bbefc1b8bb6fd6d90` |
| `native-journal-BlockedInstallBoot-20260909T121025Z/evidence.tar.gz` (incomplete journal) | `83042739a60d2297535ae0fcf982570878247eaddb746cebbeab4f6405f74c05` |
| `native-journal-BlockedInstallBoot-20260909T121140Z/evidence.tar.gz` (source metadata) | `d04540bfa2bf794fb41b5782aa4495b38f3fe44c5ec9300d2e7bebae3c8cc928` |
| `continuation-v4-unit-linux.log` | `b776563c68cf208b48a25f7f916aa0101c596e6bc3fac0cf272b2830ab3473ce` |
| `continuation-v4-windows.log` | `6f1c75d3bdad464a6999f90a12372ad947d78657175597ff9917886ba44ea059` |
| `continuation-v4-origin-linux.log` | `452eb8272fc709dbe50ebcd011b8cafc8a67bd2df734d3462317c185115b8556` |
| `continuation-v4-live-lock.log` | `14c9e200e3b38bdb6bcce80fceeeb9ded3b2e7d028c71ce4dca951a6998b237b` |
| `continuation-v4-before-normal.log` | `00784fa46f6e0518440f4163651e179c94e0fa50a297e7754fe24a09d78668ae` |
| `continuation-v4-after-normal.log` | `cd6449b9c80d873eb99559b5ae0fde88f2d7933da12e4bbda2b3ea5a0fb0acbe` |

Conversion/normal validation logs have SHA-256
`c9e525b9097827cbe2458bd705b82e5e9743762cd8ab9711189a410d69ef4046`
and `a23beac68c10b06bb48feb905d42c1aefebe39e90a6b90a046f428b6281d807c`.
Both negative test logs have SHA-256
`de064d09b82d8355aba847faa9f07aadbb03a24ff7f352ee828f38a5d3f980e8`;
their separate boot logs/archives record the distinct rejection causes.

## Earlier, nonqualifying runs

Three earlier real runs exposed defects; none is counted as a successful
normal-boot qualification:

1. The first lab dependency used a foreign NGINX `.conf` drop-in. The existing
   strict NGINX baseline correctly rejected it after seven completed stages.
   Its next boot left the installer, mount and all checked consumers inactive
   because the package journal was incomplete. The fix preserves strict NGINX
   rejection and uses the mount's `RequiredBy=nginx.service` link and ordering.
2. The next run completed all nine installation stages, but its normal boot
   failed installed-payload verification: `/run/stackfort-php` was missing.
   The installer now writes/verifies a root-owned tmpfiles rule that recreates
   only that shared socket parent at boot, without deleting pool sockets or
   tying its lifetime to any one pool. The lab coordinator also orders after
   already-starting consumers to avoid racing completed-install verification.
3. The subsequent run restored the runtime directory correctly, but account/OCI
   reconciliation had rewritten the shared slices using older accounting
   switches absent from the installer template. Its normal boot correctly
   rejected that byte-level drift. Installer and live reconciliation now share
   the same platform-slice renderers, keeping CPU/memory reserves unchanged.

The first partial state is preserved in offline checkpoint
`native-install-partial-nginx-conflict`, ID
`7c1d8626-2b98-492d-9e99-5999bc43d8b2`. The missing-runtime boot is preserved in
`native-install-runtime-missing`, ID `db6c112f-5f30-4dad-868a-b1740842b660`.
The slice drift is preserved in `native-install-slice-drift`, ID
`a978bf45-fdd4-45e6-9f41-894e22345170`.
In the latter two cases storage/package journals remained complete:
installed-payload verification failure is not the same as rejection by the
earlier package-journal boot gate.
No journal was manually repaired to obtain a passing installation; each fix
was followed by another fresh-checkpoint replay.

The offline debugfs output buffer also now uses a named buffer field rather
than inheriting unbounded writer methods; an explicit oversized-string test
checks its 64 KiB boundary.

## Final state

The successful normal-boot state was shut down gracefully and saved in offline
checkpoint `native-install-qualified-success`, ID
`5f2c59bf-f2b0-4963-a20f-5fd7c972404f`. After both deliberate fault tests, that
successful checkpoint was restored and left powered off. All five test VMs are
off; older checkpoints and local test evidence are preserved. No customer VPS
changes, commit, push, release publication or public native-storage activation
occurred.

## Remaining gates

This lab graph admits consumers through verified storage and a complete package
journal. It is not atomic package installation or a production admission and
recovery implementation. Package scripts may start vendor services before the
final installation stage; a later installed-payload verification failure is not
currently wired to stop every already-started consumer. NGINX has `Requires`
rather than `BindsTo` semantics for unexpected mount-unit deactivation.

Next: production boot/recovery coordination and explicit operator recovery for
partial installation; a newly signed candidate containing the current native
runtime and guards; tag-release selection; external updater/package/kernel
coordination; OS capacity reserve; ordinary Debian/Ubuntu/Rocky provider-image
qualification. The updater has a separate lock, so this shared installer lock
does not serialize older/already-created updaters or unrelated root package
managers. No test here qualifies arbitrary power loss inside ext4 metadata
writes or a kill inside a package-manager transaction.
