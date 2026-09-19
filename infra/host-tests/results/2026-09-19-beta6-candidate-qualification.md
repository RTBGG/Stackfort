# Beta.6 exact candidate qualification — 2026-09-19

Status: **fresh onboarding failed before reboot; not publishable**.
The prepared, tag-attested bytes below were tested on a fresh disposable
Debian 13 amd64 host. Earlier modified-host diagnostic passes do not qualify
this candidate. Its immutable tag and artifacts remain unchanged.

## Build prerequisite corrected

The first manual build at `c78d73eaf7efcafb32f6868b3f42c126b7c40cf0`
([run 35437461243](https://github.com/RTBGG/Stackfort/actions/runs/35437461243))
correctly stopped before producing a release archive: all three distributions
had updated their NGINX package revisions beyond the reviewed WAF target locks.
No failing build was selected, tagged or published.

The revised source is `d9402971ea282cadcb32dafce425daa066ababfa`. It retains
exact package checks, source checksums and the real worker test, and pins:

| Target | NGINX package |
| --- | --- |
| Debian 13 | `1.26.3-3+deb13u9` |
| Ubuntu 26.04 | `1.28.3-2ubuntu1.11` |
| Rocky Linux 10 | `2:1.26.3-6.el10_2.7` |

Ubuntu's exact distribution source layer was downloaded from official HTTPS,
checked against its published DSC SHA-256, and pinned alongside the DSC. This
does not claim separate OpenPGP signature verification. The existing upstream
source selection for Debian/Rocky was not changed. All three real WAF package
builds/worker checks and all three Vinyl package builds passed. Their existence
does not expand the Debian-only native experimental installation scope.

## Exact retained candidate

- Version: `0.1.0-beta.6`.
- Source: `d9402971ea282cadcb32dafce425daa066ababfa`.
- [Manual build](https://github.com/RTBGG/Stackfort/actions/runs/35437730633):
  `35437730633`, attempt `1`, success.
- Aggregate artifact: `10583041248`, `stackfort-0.1.0-beta.6`, 313,151,064 bytes.
- ZIP SHA-256: `39276c80e870f5d25e10a3bad6f89b4469ae14ee8bc8146092e41ad2d04de5d5`.
- Release TAR: 117,604,668 bytes;
  SHA-256 `6ea7ce25108a9654a9b0de5bd8db45b4828455c5ffaa55284f88fbb8ed4ea767`.
- Standalone and archived installer:
  SHA-256 `e9a7b93a110b14e6a2522c7022bb95263ad3e1ed67acddbcea4bfb5acea5b4b1`.
- Bootstrap source:
  SHA-256 `1b122b9548ec8ed668cc6becfcd2ae1566ddd8d0d40a22d3b7a430b7ba2871f6`.

The successful run and complete seven-artifact GitHub inventory were passed to
the mechanical promotion verifier. Strict extraction passed the fixed ten-file
inventory, ZIP digest/CRC/bounds, all release and carrier sidecar checksums,
SPDX metadata, TAR version/commit and standalone/archived installer equality.
The verified receipt explicitly does not authorize publication.

The exact source's [CI](https://github.com/RTBGG/Stackfort/actions/runs/35437707586)
passed all four jobs, including reproducible development archives.
[Security](https://github.com/RTBGG/Stackfort/actions/runs/35437707613) passed
secret scanning, Go vulnerability analysis and both CodeQL languages;
PR-only dependency review was skipped for the push event. This is automated
evidence, not an independent security review.

## Tag proof, without publication

Selection commit `0e0262fbf5070e660c8d9ff0135a20ee382d4943` records the actual
original artifact. Annotated tag `v0.1.0-beta.6` has object
`76a22a276dfd236a0f3f1f75c284ea5fd54cd660` and points to the frozen source above.

[Tag run 35438359608](https://github.com/RTBGG/Stackfort/actions/runs/35438359608),
attempt `1`, verified and reused the retained files without rebuilding, issued
exact-tag provenance, and uploaded unpublished artifact `10582383395`:

- Name: `stackfort-tag-candidate-0.1.0-beta.6-attempt-1`.
- ZIP: 313,159,273 bytes;
  SHA-256 `bf33da70c01b3a84c83ee9d1c8a9df2fdae670ba5e5ca4168b10e937bc380639`.
- Attestation bundle SHA-256:
  `13661aad734dd7ec232af4d0d7d201c768e7644fbd0e3add67acb9be015bae16`.
- Aggregate checksums SHA-256:
  `48ed881b649672f0f290b4d9a2f7f7136ebcdd0152dd278d53a3ef327e86a5e2`.

The downloaded twelve-file tag artifact passed integrity checks; all ten
original payloads are byte-identical. The local extraction helper checks
integrity, not cryptographic origin; the real installer must verify the tag
attestation under its unchanged origin policy. The tag workflow then stopped
at the absent readiness record (HTTP 404), before publication. This expected
gate failure is not an installation pass or a waiver.

## Fresh host baseline

The exact disposable Hyper-V VM `stackfort-native-quota-debian-13` was off before
its active disk attachments were changed. All old AVHDX chains and diagnostic
checkpoints were retained; no old disk was copied, restored or deleted. The new
independent 50 GiB system disk and 64 MiB cloud-init seed were matched to their
prepared disk IDs and initial hashes before attachment.

The Debian vendor image SHA-256 is
`85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`;
its preparation used official HTTPS and the published SHA512SUMS, not a claimed
detached signature. Cloud-init completed without reported errors. A nonce-,
instance-, image- and VM-bound Hyper-V KVP report authenticated the newly
generated SSH public key before strict SSH access. No private key was read.

Observed: Debian 13.6, kernel `6.12.107+deb13-cloud-amd64`, plain GPT/ext4 root,
256-byte inodes, cgroup v2, about 47 GiB available root space, and more than
3.2 million free inodes. Project quota features and Stackfort state were absent.
The root filesystem/partition UUIDs match the vendor image because conversion
does not randomize them; identical UUIDs are not evidence of copying an
installed disk. Baseline checkpoint:
`380f2377-709e-4b83-8881-e405755759dc` (`native-beta6-vendor-fresh-20260919`).
This baseline provisioning is **not** the required post-install removal test.

## Qualification boundaries

The native-onboarding driver used `retained-fixture` transport and the exact
hashes above. This transport uses authentic unpublished files; it is not the
later required public GitHub download test. The driver exited with a redacted
failure at `native-onboarding`, without observing the arming/reboot boundary.
Raw terminal output, setup codes, passwords and cookies were not logged; the
original terminal exception is therefore not available as evidence.

Read-only inspection found the same boot ID
`37fe578f-32ca-43a5-9dfa-07b0c2fc1ac2`, an original setup commitment and a
prerequisite journal in `checking` with an empty plan and no post-state. No
storage/install journal or native runtime had been created. The current package
inventory digest matches the prerequisite receipt's pre-install digest exactly.
No native retry, reset, repair, setup-code recovery or reissuance was attempted.

An exact read-only APT simulation reproduced a six-package addition plan:
`nftables`, `quota`, `libnftables1`, `libjansson4`, `libnl-3-200` and
`libnl-genl-3-200`; no upgrades or removals. The unmodified parser rejects this
plan because `libjansson4` is absent from its reviewed additions, although it is
a [required Debian libnftables1 dependency](https://packages.debian.org/trixie/libnftables1).
A regression fixture first failed against the candidate source with that exact
parser rejection. Code inspection also found that assigning the parser's nil
error result to the journal plan makes its deferred `recovery-required` update
invalid, explaining the retained `checking` state. This reconstructs the failure
from persisted state and a reproducible parser input, not a recovered transcript.

The next source adds only that reviewed library to the allowlist and assigns a
plan only after successful parsing. Unknown additions, upgrades, removals,
changed transactions and retries remain blocked. Windows `go test ./...` passed.
Seven focused tests also passed on Linux, including the real private mount/network
namespace test for durable `checking` → `recovery-required` after rejected
planning, plus terminal no-reset behavior. The test ELF SHA-256 was
`c333068dbbe80ad63b300301c17a405bbe8ef8f065e1818285b94114e85ae3be`.
All actual installer receipt hashes were unchanged before/after these isolated
tests. This diagnostic fix is not a beta.6 installation pass; beta.7 must be
built and tested separately on another fresh baseline.

Remaining gates include completed fresh onboarding and product smoke, same
release rerun/reboot persistence, host/security/isolation/resource checks,
actual rootless build/scan/deploy, process-loss quarantine, same-target full OS
reprovision removal, and candidate-specific publication approval. No passing
readiness record is created by this report.

Local preparation also passed Go tests, WAF lock validation, 28 Node promotion
tests (one Linux shell case skipped on Windows), four extractor test methods,
16 read-only collector contract tests and the native-onboarding driver contract.
These are not live host qualification. Test-only overlays were compiled from
the raw frozen source: all 1,055 tracked Git blobs match exactly. An initial
Windows `core.autocrlf` export was rejected and retained unused; the corrected
export explicitly disables conversion. No installed product is replaced by
those test helpers.
