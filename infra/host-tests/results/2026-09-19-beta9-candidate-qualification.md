# Beta.9 candidate qualification — 2026-09-19

Status: **native installation and original setup passed; installed product
smoke failed at uploaded static-file delivery. Not qualified for publication.**

The [beta.8 attempt](2026-09-19-beta8-candidate-qualification.md) completed native
installation and original setup, then failed its first domain activation because
the NGINX server-name hash bucket could not hold the valid managed hostname.
Fix source `10627d77baa5ff139f5ce54f9ba8fea09d81bb81` passed
[CI](https://github.com/RTBGG/Stackfort/actions/runs/35443533379) and
[Security](https://github.com/RTBGG/Stackfort/actions/runs/35443534674), both on
attempt 1. CI includes the new real-NGINX syntax checks and reproducible builds.
This does not qualify an archive built from subsequent source.

Beta.9 retains that fix and updates the bootstrap/support/quick-start version
references together. The user independently reproduced the public beta.8
`SHA256SUMS` HTTP 404; the Releases API inventory was empty. No public download
or successful installation is inferred from our retained-fixture tests.

The bootstrap now explains which release asset is unavailable on HTTP 404 and
that installation has not started. Other transfer failures retain curl's exit
status and HTTP status. There is no fallback version, unverified transport or
verification bypass. The default selection still requires published assets.

The extended shell regression tests use a private mount namespace, fixed-path
synthetic state and a private PATH download shim; they perform no network call
or installed-host mutation. Missing checksum/archive/attestation, HTTP 500 and
DNS/transport failure all abort before the fixture installer. Successful mocked
downloads still exercise the ordinary checksum and handler path. All eight
qualification markers passed on the existing Debian VM without repairing its
failed product state. The exact tested bootstrap SHA-256 is
`582b1c4124444200f6148b333e977696a3ad2fee88b457d5fe387313f4ca00e8`;
shell-test SHA-256 is
`5fffcf29525abe7bde431268c3a91a8360c4f0892cd013de673a281f701e663b`.
Documentation contracts and 238 Markdown files / 860 local links also passed
after adding this report.

## Prepared target and remaining work

A new independent vendor-image system/seed pair was attached to the exact
disposable VM `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, after guarded graceful
shutdown of the unchanged failed beta.8 installation. It is separate from the
reserved final-removal pair. Its 50 GiB system disk ID
is `02a6e8b8-3664-b34c-9d7b-a1d094b76340`, initial SHA-256
`1977b77523b37cb3266b9c660c968da5dd0ec3b34c23e1747b93b9e5ad3ee115`.
The 64 MiB seed ID is `71c313ff-749f-45cb-9b92-6187963080af`, SHA-256
`43c418361d7e14a66078483fc52876ea377b60c5d6a3c2de34e28426e6afa188`.
The failed beta.8 disk chain and checkpoint remain intact, with no reset or
credential recovery. No candidate installation/removal result is claimed here.

The fresh cloud-init completed without reported errors. GPT/ext4 root has no
quota/project feature enabled, and all Stackfort installation/state/hosting
paths are absent. The fresh checkpoint is
`28064f82-47f0-48b9-9480-5e60de1c2c27` (`native-beta9-fresh-20260919`),
boot `88d5a724-493e-43ce-af14-089a980cfdea`.
The new SSH public key was authenticated by the VM-bound Hyper-V KVP report,
fresh instance/nonce and independent disk graph before updating only its exact
known-host entry. Its public fingerprint is
`SHA256:bSHpRfiHfroGw3lYsrIyAyoucjd/gTBKAVXtJduM/TE`.

Candidate source is `68d6bd39bd025c776ce8c834ab5197c88a55df4f`;
the original manual build is
[35448968370](https://github.com/RTBGG/Stackfort/actions/runs/35448968370),
attempt 1, completed successfully. All six native-package jobs, all three
compiler-free Vinyl runtime jobs and aggregate packaging passed.
[CI 35448910741](https://github.com/RTBGG/Stackfort/actions/runs/35448910741) and
[Security 35448910781](https://github.com/RTBGG/Stackfort/actions/runs/35448910781)
also completed successfully on attempt 1. Their complete latest-attempt job
inventories passed the readiness validator's exact-source workflow contract.

The selected original artifact is `10585907718`, 313,166,196 bytes,
ZIP SHA-256 `c6aa98d52aa1dbf039841a30b1bf6219f7d070e3b59da4743ca83a3204d2af37`.
Its ten-file inventory, checksums, source/version and standalone-versus-archived
installer equality passed the strict extractor. The selected TAR SHA-256 is
`d0c866050d4f3e8998a41bae6f5e8d50b2c792c48a36cf24d8ca5b323aa27ebe`.
Installer/API/agent SHA-256 values independently read from that archive are,
respectively:

- `09cd7db3e89a891d8ad898f668afc54ec0c30453689a1339b930590518a796b8`
- `f05438628ee2ce46e9e9d2d2bc1a284b59fec0e35860a11dd51f38b5b3093e91`
- `3088aa559311c30e88d91787a6054d808b74607b49fa87301fed48bb861fc81b`

This mechanical selection does not authorize publication or bypass the real
tag-origin verification required during installation.

The annotated `v0.1.0-beta.9` tag points to that unchanged candidate source.
Tag run [35449891634](https://github.com/RTBGG/Stackfort/actions/runs/35449891634),
attempt 1, downloaded the original candidate without rebuilding, attested it
and retained artifact `10586039426` before stopping at the deliberately absent
readiness evidence (HTTP 404). The public-release step was skipped.
The tag ZIP is 313,174,325 bytes, SHA-256
`cbdf8320f4de7a091b980f2301a9b8fc3207698b79320b38b31c0244a9c61c56`.
All ten original payload files are byte-identical; the additional attestation
SHA-256 is `af21c71d4467f8c01660e68f4eccbeef20da0bc2343e4270e5e722c8897e3c34`,
and `SHA256SUMS` SHA-256 is
`c78abc38fd5852a6a34fbc1413d711ddd4cec3dbc53bfcb0147ccfc5dd7a23e8`.
The local tag extractor verified integrity/equality only; the real installer
must independently verify the cryptographic tag origin.

A separate external test executable was built from all 1,065 independently
verified raw Git blobs of that source plus seven named qualification-only Go
overlays and the fixed embedded OCI fixture. The raw source TAR SHA-256 is
`0948057c0bc86192cb48d869987f47ae1123baa3a45f875a0f37c04288704491`;
the Linux integration-tag test ELF SHA-256 is
`2b354dfbb7a8106caece3876d3bf5185c96463a4251b29551f76c67fbcf8cb52`.
All 13 selected pure helper contract tests passed unprivileged on the fresh
guest; the read-only security collector's 16 local contracts also passed.
These are helper checks, not actual OCI/resource pressure or host-security
qualification. No installed payload was replaced and no managed account was
created by these checks.

## Actual installed result and blocker

The exact retained tag installer verified its origin, completed prerequisites,
performed the acknowledged automatic storage conversion/reboot and completed
all nine installation stages with admission `admitted`. Operation:
`ddbbaf39-e1a3-485a-89bd-12545586f420`; conversion boot:
`23ea0f5f-0c59-4f02-ac91-a251d3c38039`.
Original setup redemption/replay rejection and administrator login/session
passed, followed by account provisioning and two active static/PHP domains.
All three queued account/domain operations succeeded on their first attempt.
The prior server-name hash activation failure did not recur.

The API upload and digest-verified download succeeded, but the first public
static request returned HTTP 404. Fixture account
`01a0ba2a-5ac7-7b9c-b239-2fc986adea6e` contains the expected 41-byte
`public_html/index.html`, SHA-256
`c2a19de4cc56fbd370b26c1196836d964ee7f848f37be98551c6c50ca940ba16`.
The activation response header identified the correct current NGINX revision.
Read-only ACL inspection showed account traversal and document-root access/default
ACLs for `www-data`, but no named-user ACL on the uploaded file. An actual
`www-data` read-permission check failed. The upload completion renames its private
staging inode into the public directory; rename does not inherit its default ACL.
See the primary [Linux ACL semantics](https://man7.org/linux/man-pages/man5/acl.5.html).

The failed installed fixture is preserved under checkpoint
`80911e5f-bbab-4726-8dba-445bc8c4d9f1` (`native-beta9-web-404-20260919`).
All three installed binary hashes still match the selected archive. No live ACL
repair, binary replacement, setup-code recovery or onboarding retry was used.
The driver exited 1 and disposed its in-memory credentials; it emitted no success
receipt. SQL/database, backup, WAF/cache, rerun/reboot and subsequent enforcement
checks were not reached. No complete beta.9 qualification is claimed.

The subsequent source correction applies destination default ACLs before upload
publication and seeds private copy/archive staging defaults without making staging
traversable. Archive extraction also finalizes its top-level mode, which previously
remained `0700`. Restore retains live directory ACLs/modes at matching paths and
uses bounded no-follow traversal to inherit policy for newly restored descendants.
No archive ACL import, world-readable fallback or account-wide worker grant is
introduced. Normal rename/move/trash semantics remain unchanged.

Five focused Linux regression tests (plus five negative subcases) passed both
unprivileged and as root against temporary fixtures. The root run additionally
executes real reads as UID/GID 65534: the old rename-only path denies reading;
the corrected published file, copied tree and extracted nested file are readable;
unfinished staging and private restore trees remain denied. These are source
regressions, not repair or requalification of the installed beta.9 payload.

The complete Linux hostfiles test executable also passed unprivileged. Its final
SHA-256 is `e620f1be6c014f2f91010258d5122cca7b86bd076ca374868d1947cc1c568fbf`.
Linux vet and the Windows Go suite passed. The privileged regression invocation
is now part of CI; an exact-source CI result is still required for the new candidate.

Required next: regression-tested staging/restore permission correction, a new
immutable candidate and fresh full onboarding/product smoke; then rerun/reboot,
host/security/isolation/OCI/failure/removal qualification,
actual candidate-specific publication approval and the publication gates.
Only then can the public README download path be verified on a fresh host.
