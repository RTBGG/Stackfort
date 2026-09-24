# Beta.11 candidate qualification — 2026-09-24

Status: **fresh MBR/BIOS and GPT/UEFI onboarding, installed API smoke, rerun and
normal reboot passed. Upgrade qualification failed at the published Beta.10
inventory. Not published or approved for use.**

This candidate adds canonical primary MBR/BIOS partition support to the native
Debian 13 installer. It does not convert MBR to GPT or replace the bootloader.
The earlier [MBR source/lab checks](2026-09-24-native-mbr.md) are not release
qualification: their old beta.3 payload failed the full installation at a stale
NGINX dependency. This run must use the complete new candidate unchanged.

## Frozen source and build

- Version: `0.1.0-beta.11`.
- Source: `ec117da052b18e051870a224b89bee4b0ddf64e7`.
- Original manual build: [35991617289](https://github.com/RTBGG/Stackfort/actions/runs/35991617289), attempt 1.
- Exact-source CI: [35991564656](https://github.com/RTBGG/Stackfort/actions/runs/35991564656), attempt 1.
- Exact-source security: [35991564722](https://github.com/RTBGG/Stackfort/actions/runs/35991564722), attempt 1.

Local Windows `go test ./...`, documentation links, seven documentation contract
tests, and the onboarding driver's pure fixture/partition contracts passed.
These are source/helper checks, not installed-candidate or independent security
review results. All four required CI and all four required security jobs passed
at the frozen source. The manual build passed all six native-package jobs,
three compiler-free Vinyl runtime jobs, and aggregate build/inventory/attestation.

The public bootstrap remains pinned to Beta.10. The candidate security policy
explicitly labels Beta.11 unpublished; no previous human publication approval
applies to this candidate. Selecting an artifact and generating tag provenance
do not authorize publication.

## Fresh disposable installation targets

Both targets were independently checked through their pinned SSH host keys at
11:17 UTC. Each has fixed 8 GiB RAM and a 50 GiB ext4 root without quota/project
features. Stackfort state and the installed installer were absent.

| Profile | Hyper-V VM ID | Guest DMI identity | Root PARTUUID |
| --- | --- | --- | --- |
| MBR / BIOS | `1f5e665a-fddd-4cd2-bc55-44255b01963d` | `91d127c9-40e6-ab48-9528-5085e8888729` | `7c92ab10-01` |
| GPT / UEFI | `55729cf8-11e3-4744-be5b-ddde3824c0e4` | `a5db08d7-6c4e-47e0-a381-9bd594b2731a` | `54964bdf-2add-41b7-b41e-6d483962d021` |

The MBR lab was restored from its own fresh static-memory checkpoint
`c9c3f45c-a014-42db-83fa-8111a559c394`. Its prior failed-installation checkpoint
`7ff4b24d-9e97-4cd8-a75f-36e2a8156655` and disk chain remain retained. Restoring
a test baseline is not a removal qualification. The separate GPT lab was still
fresh and received its own `beta11-gpt-fresh-static8g-before-install` checkpoint.
The existing installed public Beta.10 VM and rescue VM were not modified.

The exact-tag onboarding driver now has fixed `mbr` and `gpt-candidate` profiles.
It validates the Hyper-V and guest identities, live partition identity, candidate
hashes, and original interactive review. MBR syntax validation includes rejection
of trailing newlines, zero disk signatures and non-primary partition numbers.
Original setup credentials remain in process memory only. The complete results
below cover installation, setup/login, installed API smoke, same-release rerun
and normal reboot on both profiles; the helper contract alone proves none of them.

## Upgrade baseline and remaining gates

The immutable published Beta.10 archive was independently checked against
`8e7ce111d6cefda172e74bd7a71380c181e66a726d8f157f6bd1af8944e93263`
and is now the supported predecessor catalog entry. The matrix requires nine
cells: success, health rollback and interrupted recovery on each of Debian 13,
Ubuntu 26.04 and Rocky Linux 10. This prepared-quota upgrade route does not
advertise fresh-default native installation on Ubuntu or Rocky.

The prior-source Linux test driver was built from Beta.10 source
`0096faed0ae77aef3326ce629db7591a433bb37f`, plus the current upgrade matrix test.
The selected driver uses explicitly LF-exported source, verified against all
1,068 unchanged predecessor Git blobs and the one allowed test overlay.
Driver SHA-256:
`1bf1011eb6e1e985a15bbb565e74dd58c756f7777481059f8e077087ea514e8b`.
Earlier Windows-native-EOL exports and their test binaries were retained but
are not used as exact-source qualification drivers.
Before any upgrade reset, the three dedicated, powered-off fixtures received
`before-beta11-upgrade-qualification-20260924` preservation checkpoints:

- Debian: `1421daaa-e81d-4383-a719-16a4cb426374`.
- Ubuntu: `a3fb0c29-acd0-4064-a2d6-ea35a5601c1d`.
- Rocky: `2d63b726-5d29-482e-95c5-f83e419fba91`.

No separate exact-candidate host-security, resource/isolation, OCI, failure
containment, full OS reprovision removal or successful upgrade result is claimed.
Those technical gates,
candidate-specific RTBGG approval, publication and public-download qualification
remain separate requirements. The user VPS has not been accessed or modified.

## Exact retained artifacts

The original artifact `10804961581` is 313,165,990 bytes, ZIP SHA-256
`97c6acdb9aaa6695c880ab9a54c26724d3182394989ef38b7ae580d09ebe2c9e`.
Its live workflow/artifact metadata, fixed ten-file inventory, checksum sidecars,
source/version and standalone-versus-archived installer equality passed the
strict promotion validator and extractor. Archive SHA-256:
`d6fec6d5a894d0c120aef5d83829b335a8875b68a181c45a7ad2bb9d139d1304`.

The mechanical selection was committed on descendant `0c85eb5`, without a
readiness approval. Annotated `v0.1.0-beta.11` remains at `ec117da`. Tag run
[35993227841](https://github.com/RTBGG/Stackfort/actions/runs/35993227841),
attempt 1, promoted those bytes without rebuilding and created unpublished
artifact `10805515238` before stopping at the still-missing readiness evidence.
The public release step was not run. Tag ZIP SHA-256:
`fe4ad0a2656207e0af5e8696138d0b4e9c00c34d899fe319513653e5fe3e2c00`.
All ten original files are byte-identical. Additional qualification hashes:

- Tag attestation: `16570b5f6f4f8e1bc35f5ac31c80b5194a924956e6337df67953236470a37d1f`.
- `SHA256SUMS`: `5e4919e4bdbfd359ac6c3f62dfe04d7808fbeec411185d20eaa9ea283c69fef7`.
- Archived installer: `2bdee0efaf811f5eb7a488236f2a021ab865c546a7e486ad52c8743b790740bf`.
- Bootstrap: `21977a1a0cb79b0768a23da3e669697f8451a51179643e49b17e270dfbffbe65`.
- Onboarding driver: `e9c256d6e77ca2c5d6e887eecee519ed9cf8f1c1fb668ac00714a41d59cac9fc`.
- Installed API smoke: `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196`.

An external Linux test driver was built with all 1,084 candidate Git blobs
individually verified and the same seven bounded qualification overlays plus
the embedded test server. Its ELF SHA-256 is
`a3372fd9da89feb2ab99b46b6a1c06bd04a015f22f85c6923724449703bc5ec7`.
All 13 pure helper contracts passed unprivileged on the MBR guest. This neither
replaces the installed binaries nor counts as actual resource/OCI qualification.

## Upgrade attempt: published predecessor naming mismatch

All three prepared-quota fixtures installed the genuine Beta.10 baseline and
reached its services/health stage. Each then failed **before the three upgrade
scenarios** when the unchanged predecessor stager verified published provenance:
it requires carrier names containing `0.1.0~beta.10`, but GitHub's actual
immutable release inventory contains `0.1.0.beta.10` for the DEB/RPM and sidecars.
The public TAR name and digest are unchanged. This is a discovered updater
compatibility defect, not an MBR storage failure or a successful upgrade.

Baseline/verification durations were 104.04 s (Debian), 70.59 s (Ubuntu) and
183.07 s (Rocky). The wrapper retained complete local logs and powered the three
dedicated guests off. Their installed/failed states and pre-test checkpoints
remain intact. No archive rename, immutable asset replacement, patched old
production driver, `rehearsal` relabeling, retirement of the predecessor, or
synthetic pass was used to open the publication gate. All nine upgrade cells
remain unqualified. A compatible, verified transition must be resolved before
publication; previous Beta.10 approval does not waive it.

## Complete fresh installations on both partition schemes

Both full onboarding runs passed without repairing or replacing the installed
candidate. They used **retained-fixture asset transport with genuine tag-release
origin verification**, not a public Beta.11 release download. The archived
installer completed prerequisite planning, acknowledged one-shot reboot,
automatic offline quota preparation and all nine installation stages. Storage
reached `ready` with exactly one arm attempt in each VM.

Original one-hour setup redemption, rejection of setup-code reuse, administrator
login, seven installed API smoke checks, same-release rerun without setup reissue,
and normal reboot with the original session all passed. The product checks cover
account provisioning, uploaded static/PHP delivery and download, domain
idempotency, database wizard, document-root backup/download/restore, per-domain
FastCGI toggle/MISS/HIT/cookie-bypass/purge, and WAF off/detection/blocking before
warm-cache delivery. The original account/domain/file/database-inventory/backup
state persisted across rerun and reboot. No setup code, password, cookie or raw
terminal transcript was retained.

| Result | MBR / BIOS | GPT / UEFI |
| --- | --- | --- |
| Operation | `30d72acc-5142-4bcf-a682-c7a460331c84` | `a46df87b-325b-4197-b997-3ebc75279dda` |
| Conversion boot | `5005336f-1828-4a06-bbc1-309251c5eaf4` | `4783f670-d2c8-475a-94c0-90e7a082b26b` |
| Normal boot | `6183a990-f0c8-4be3-91c1-1ae2c0dcf99b` | `60ca4b6c-96ba-411f-8ede-c96b181f7cd4` |
| Completion (UTC) | `2026-09-24T11:37:12Z` | `2026-09-24T11:37:06Z` |
| Redacted receipt SHA-256 | `3459d3b1a60ce96330556894385a3077b67762f1c4450a7c72d188f6f1bd3f6b` | `9f6ff43f99ee776e9453439c61aeb99eb13afe0dcbb1e109b32add9a9de3b0d2` |

Receipts remain under `work/candidates/35993227841-attempt1/` as
`onboarding-mbr-result.json` and `onboarding-gpt-candidate-result.json`.
This is not browser-rendering, SQL/phpMyAdmin credential, full-account backup,
performance, tenant-isolation, OCI or complete release-readiness qualification.

After verifying the final DMI/boot identity, exact installed-installer SHA and
active platform services, both successful labs were gracefully powered off.
`beta11-exact-onboarding-passed-20260924` checkpoints preserve MBR
`ac786bc1-090b-4877-97a5-66d264ea0720` and GPT
`c0ff2092-0fe6-498b-b23d-081b8ed9ef1e`. The three failed upgrade baselines also
remain off, with additional `beta11-upgrade-predecessor-name-failure-20260924`
checkpoints: Debian `b8a75b6c-764a-4b4f-af84-5bb2d8510f27`, Ubuntu
`a921e37c-a3aa-402a-a086-3ee03b6b4dc5`, Rocky
`643d4a01-d88e-4808-991e-9283677a3df8`. No disk, prior checkpoint or installation
was removed. The original Beta.10 public-installation VM remained running.

## Follow-up source correction, outside the frozen candidate

The public-release stager now requires the actual GitHub-normalized carrier
names while preserving native internal package versions and the archive's
checksum/tag-attestation checks. A literal Beta.10-shaped inventory regression
is independent of the production name generator; it rejects missing, duplicate,
wrong-version, wrong-URL, undigested, not-uploaded and original-tilde metadata.
A complete mocked beta staging regression also passes. Stable-release names
remain unchanged. The focused updater tests, full Windows `go test ./...`, all
seven documentation contracts, and 895 local documentation links passed.

This correction is **not** in the retained Beta.11 artifact and does not repair
the already-published Beta.10 updater. No old production-source driver was patched
and called an upgrade pass. A verified migration path for that predecessor,
a new candidate source/artifact, and fresh technical/publication qualification
are required. The existing tag and public one-line selection remain unchanged.
