# Beta.12 candidate qualification — 2026-09-24

Status: **exact-candidate MBR installation with the provider's optical rows,
setup/login, installed API smoke, same-release rerun and normal reboot passed**.
The candidate remains unpublished; full release qualification and publication
approval are pending. No Beta.12 release or default-bootstrap change is
authorized by this record. Beta.11 is unchanged.

This candidate corrects the rejection of inactive optional CD-ROM fstab rows.
See the [source correction and exact provider-row reproduction](2026-09-24-beta12-optical-regression.md).
The user VPS was not accessed or changed.

## Frozen source and retained build

- Version: `0.1.0-beta.12`.
- Source: `f4d1a7947af4ffbdc2cbf28d2f618c10833cefec`.
- [CI run 36004640915](https://github.com/RTBGG/Stackfort/actions/runs/36004640915), attempt 1: all four required jobs passed.
- [Security run 36004640826](https://github.com/RTBGG/Stackfort/actions/runs/36004640826), attempt 1: all four required security jobs passed; only PR-specific dependency review was skipped.
- [Manual build 36005061649](https://github.com/RTBGG/Stackfort/actions/runs/36005061649), attempt 1: successful, including six native packages, three minimal-runtime cells and aggregate inventory/attestation.
- Original artifact: `10811230131`, `stackfort-0.1.0-beta.12`, 313,199,834 bytes.
- Original ZIP SHA-256: `8cf13cd7084cc1a21497a1017bbd45225cdf924347fa4ee95fc66e6906dca485`.
- Release archive SHA-256: `446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9`.

The downloaded ZIP matches the live GitHub artifact digest. Mechanical
selection is not a security audit, readiness approval or publication decision.
Original build retention expires on 2026-10-24; do not rerun or substitute its
pinned build attempt. Ignored working evidence is retained under
`infra/host-tests/work/beta12-candidate-20260924/`.

The strict promotion validator checked the live complete run/artifact metadata.
The bounded extractor verified the ten-file inventory, all aggregate checksums
and carrier sidecars, archive version/source and byte equality of standalone
and archived installers.

## Unpublished tag provenance

Mechanical selection was committed as `c52d25c`. Annotated `v0.1.0-beta.12`
points to the frozen `f4d1a79` source, not that later metadata commit.
[Tag run 36007265703](https://github.com/RTBGG/Stackfort/actions/runs/36007265703),
attempt 1, verified and promoted the retained bytes without rebuilding, issued
the genuine tag attestation, and retained artifact `10810852574`
(`stackfort-tag-candidate-0.1.0-beta.12-attempt-1`, 313,208,244 bytes).
It then stopped at missing public-readiness evidence; the publication step
was not executed. This expected gate failure is not a passed readiness test.

Tag ZIP SHA-256:
`110088185a8d53af35572453d46b984867aed194fe5e1ef13d4da2bc08326eba`.
Strict extraction verified all ten original files remain byte-identical.
Extraction alone does not verify cryptographic origin; the ordinary native
installer's strict tag-origin admission remains mandatory during onboarding.

| Input | SHA-256 |
| --- | --- |
| Tag attestation | `67307840e6be9d9ff659fa22d6cb965098f4edc3742b4929a956448ef0659555` |
| `SHA256SUMS` | `2a8c1ec7926eafecef293846e64ab585a1a035228fc51e82f1d899ab0d5e70a4` |
| Archived/standalone installer | `9a5ff2e3f6db4916c063fd37903cfb30a3975b1d4c3b1f8ca05e74d0e14c8650` |
| Candidate bootstrap | `d5bea20804856b6e5e92006dbaa81ca41042a2029fa1e3a13a9ff40b309677ef` |

The candidate bootstrap retains the public Beta.11 default. Qualification uses
an explicit Beta.12 version and exact retained transport, not a public download
claim or a relaxed branch-origin admission policy.

## Exact-candidate MBR optical installation

Completed at `2026-09-24T13:49:01Z` on the same dedicated disposable VM
`1f5e665a-fddd-4cd2-bc55-44255b01963d`, DMI
`91d127c9-40e6-ab48-9528-5085e8888729`, fixed 8 GiB RAM, BIOS/primary MBR,
root PARTUUID `7c92ab10-01`. The retained fresh optical fixture and preservation
checkpoints are identified in the source-regression report. No snapshot was
restored after starting this candidate's onboarding; no manual repair was used.

The unchanged driver ran with the exact archive, checksums, tag bundle,
bootstrap and source pins above. It acknowledged the live machine/root/boot and
installer-bound review through the real controlling terminal, retained the
original one-use setup code only in process memory, then let the ordinary native
installer perform its strict tag-origin admission, one-shot quota preparation,
reboot and platform installation.

| Observed result | Evidence |
| --- | --- |
| Native operation | `b8803e17-9f81-4422-a1dc-d70de26032bc` |
| Initial boot | `ee962d39-ed81-4bd3-9610-eea9ed6aa95a` |
| Conversion/install boot | `fbe70025-79dc-4dd4-b169-08ea9cf17508` |
| Subsequent normal boot | `10d200c5-2548-4608-9c39-0370cc64f110` |
| Original setup redemption / replay rejection / admin login | all passed |
| Same-release rerun | passed, no reboot or setup reissue |
| Post-normal-reboot persistence | passed using the original session and certificate pin |
| Final native install service | active/exited, successful |
| Final root mount | ext4 with `prjquota` |

Both optical fstab rows remain **byte-identical** through quota conversion,
installation, rerun and normal reboot. Their before/after SHA-256 is
`ea3e4d0aac86201401840ab360d3afee48a414a953e78e0435b082849c90cb1a`.
Only the root row gained its managed quota option; no optical entry was removed.

The real installed API smoke passed account provisioning, static/PHP serving,
file upload/download, domain idempotency, database wizard/inventory,
document-root backup/download/restore, per-domain FastCGI enable/disable,
MISS/HIT/cookie bypass/purge, and WAF off/detection/blocking before cache.
The same fixture (`sf-candidate-847a94bdacbe`) and content/backup hashes remained
valid after rerun and reboot. These are functional checks, not performance
benchmarks, SQL/phpMyAdmin qualification or exhaustive tenant-isolation tests.

The installed MBR fixture remains available for further exact-candidate checks.
No setup code, password, cookie or raw terminal transcript was retained. The
existing GPT/public Beta.11 and original public Beta.10 installations were not
replaced. This retained-artifact run is not a public GitHub download test.

| Local receipt/helper | SHA-256 |
| --- | --- |
| `mbr-onboard.json` | `5b1cd6c7c86b9ed8f128703da2bc40e8c03eb4bc1c72ff7a6f6e21d1604efe72` |
| `post-install-optical.txt` | `21f6a537ba0ae487e2fee5c6f178266be1a3229aa2b420184bd81ef678f3138b` |
| Onboarding driver | `e9c256d6e77ca2c5d6e887eecee519ed9cf8f1c1fb668ac00714a41d59cac9fc` |
| Installed API helper | `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196` |

## Remaining release boundaries

Fresh native installation scope remains Debian 13 amd64 only. The Beta.11-only
fresh-install publication exception does not automatically apply to Beta.12.
No old upgrade, security, resource-isolation, OCI, failure-recovery or destructive
OS-reprovision removal evidence is relabeled as a Beta.12 result. Those
exact-candidate tests, the upgrade-support decision, candidate-specific approval,
publication and real public-download verification remain separate gates.
