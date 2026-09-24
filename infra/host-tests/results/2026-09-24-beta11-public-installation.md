# Beta.11 immutable publication and public GitHub installation

Result: **passed**, 2026-09-24. Experimental **fresh installations only**,
not production approval. RTBGG explicitly authorized publication of the frozen
candidate without incoming upgrade support; all remaining technical gates were
retained. See the [scope decision](../../../docs/release-evidence/2026-09-24-beta11-fresh-install-scope.md),
[approval](../../../docs/release-evidence/2026-09-24-beta11-publication-approval.md)
and [exact-candidate tests](2026-09-24-beta11-candidate-qualification.md).

## Publication and unchanged bytes

- [Public release](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.11):
  ID395668933, published `2026-09-24T12:36:47Z`, draft=false,
  prerelease=true, immutable=true.
- [Verification-only workflow35999911723](https://github.com/RTBGG/Stackfort/actions/runs/35999911723)
  passed before [publication workflow36000083795](https://github.com/RTBGG/Stackfort/actions/runs/36000083795),
  attempt1, job107634318079, passed all steps including immutability and
  `gh release verify`.
- Publication/evidence commit: `aee23e4bec086ebbf3faf38407e12f1a88bf9d22`.
  Frozen source/tag remains `ec117da052b18e051870a224b89bee4b0ddf64e7`.
  Neither the candidate nor its tag-origin attestation was rebuilt, replaced,
  re-signed under another source, or moved.
- The original build35991617289 attempt1/artifact10804961581 and retained tag
  run35993227841 attempt1/artifact10805515238 were independently checked against
  current GitHub inventories and exact ZIP digests. The original ten package
  files matched byte-for-byte. Cryptographic verification required the original
  repository, release workflow, tag ref, source/signer commit and hosted runner.
- The frozen candidate's readiness validator and unchanged technical policy
  checked all digest-bound reports and complete live CI35991564656 and
  Security35991564722 attempt1 job inventories.
- The published `upgrade-support.json` explicitly records **no supported incoming
  predecessors** and `upgradeQualificationPassed=false`. The historical
  Beta.10 upgrade failure remains a failure, not an empty successful matrix.
  Beta.10 is not globally retired.

All **14 public assets** were downloaded over HTTPS and checked against GitHub's
exact sizes and SHA-256 digests. All ten original build files were also compared
with retained bytes. GitHub normalizes the six carrier package/sidecar names
from `~beta.11` to `.beta.11`; bytes and package metadata remain unchanged.
The one-line archive is unaffected. The manual guide documents original-name
restoration before checksum verification.

| Input | SHA-256 |
| --- | --- |
| Public amd64 archive | `d6fec6d5a894d0c120aef5d83829b335a8875b68a181c45a7ad2bb9d139d1304` |
| Public `SHA256SUMS` | `5e4919e4bdbfd359ac6c3f62dfe04d7808fbeec411185d20eaa9ea283c69fef7` |
| Original public tag attestation | `16570b5f6f4f8e1bc35f5ac31c80b5194a924956e6337df67953236470a37d1f` |
| Archived installer | `2bdee0efaf811f5eb7a488236f2a021ab865c546a7e486ad52c8743b790740bf` |
| Commit-pinned bootstrap used for fresh public test | `21977a1a0cb79b0768a23da3e669697f8451a51179643e49b17e270dfbffbe65` |

## Actual fresh public installation

The independent Debian13 OS disks from the
[same-target removal test](2026-09-24-beta11-os-removal.md) remained attached to
VM `55729cf8-11e3-4744-be5b-ddde3824c0e4`. Exact VM/disk IDs, attachments,
MAC and strict SSH identity were checked. No old disk or checkpoint was
restored. Read-only fresh preflight passed at `2026-09-24T12:36:54Z`: clean
cloud-init, Debian13 amd64, plain GPT/ext4 without quota/project features,
no platform files/state/identities/units/hosting data or listeners.
The original public Beta.10 VM was not touched.

The onboarding driver was copied solely to select the fresh reprovisioned VM's
new pre-established SSH host key and local helper paths. Its consent, receipt,
provenance, API and persistence checks were unchanged.
Driver SHA-256: `6fdeeb95337da81cf454a0410cb3f72bbe99e4c077ed9823bddcfe15d3a1188d`;
unchanged API helper: `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196`.

Transport was `public-github`: a real HTTPS pipe fetched the bootstrap from the
frozen source commit, explicitly selecting Beta.11. That historical script's
bare default is Beta.10; the explicit selection was not hidden. The bootstrap
downloaded the actual public archive, checksum manifest and genuine tag
attestation. An empty environment excluded test-fixture overrides. Staged
reference files were not used as public transport fallback.

Real controlling-terminal disposable/reinstallation-risk, reboot and `SAVED`
acknowledgements completed. The original setup code was redeemed, rejected on
replay, and allowed the original administrator login. Secrets stayed only in
process memory; no raw terminal transcript, passwords or cookies were retained.

Operation: `d31bacc1-8c97-428f-af1a-dce7a635a694`.

| Boot | Identity |
| --- | --- |
| Fresh OS | `30f9566a-acca-4204-8a6b-d272a9653739` |
| Automatic quota conversion and installation | `f5925b77-d194-4e80-8789-b4e9dd8a76e0` |
| Ordinary subsequent reboot | `79adda7d-d0a6-442a-801c-058e00a4c11d` |

Installed API smoke passed at `2026-09-24T12:42:42Z`: account/package provision,
domain idempotency, static/PHP upload/download/serving, database wizard,
document-root backup/download/restore, FastCGI enable/disable/MISS/HIT/cookie
bypass/purge and WAF off/detection/blocking before a warmed cache.

Same-release public-bootstrap rerun passed without new setup code, conversion
or reboot. The original session, account/domains, file and backup digests,
database inventory and static/PHP/WAF/cache behavior persisted after both rerun
and normal reboot. SSH reverified the same VM when DHCP changed; the driver
preserved the original TLS authority/cookie scope. Full result passed at
`2026-09-24T12:43:26Z`.

## Retained receipts and limits

Ignored receipts: `infra/host-tests/work/publication-beta11-20260924/`.

| Receipt | SHA-256 |
| --- | --- |
| `release.json` | `eb013cf78e7ab1657edff076d0d27262f4fe867d710c56e5251c1a5f27bf2617` |
| `original-byte-comparison.json` | `bd1f1e49e81c73e6f998ee34385eac97e3dba05544e85d82688a213852ebc11d` |
| `fresh-preflight.json` | `5d03caf69eca4700c4ba148f47dc71c0d75f10bc64603762d71bb46d48e3ab9f` |
| `public-onboarding-result.json` | `46be190d305299af85b7a83e0f657207c9d22a2737fc8633caa21a2a8c72c782` |

This public transport test uses GPT/UEFI; the separate prepublication exact-byte
MBR/BIOS qualification remains authoritative for that profile. It does not
repeat the resource/isolation, OCI, quarantine or removal tests, and claims no
new performance benchmark, EN/DE visual review, SQL credential/phpMyAdmin test,
full-account/database backup, public-control-API OCI flow, global IPv6 or
I/O-rate enforcement proof. Ubuntu/Rocky native conversion and arbitrary provider
images remain unqualified. No independent review, production suitability or
upgrade support is implied. Shared-root capacity exhaustion remains possible;
removal requires complete OS reinstallation.
