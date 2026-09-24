# Beta.11 candidate qualification — 2026-09-24

Status: **qualification in progress; not published or approved for use.**

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
Original setup credentials remain in process memory only. Full installation,
setup/login, installed API smoke, same-release rerun and normal reboot are still
required for both profiles; starting a driver is not evidence of completion.

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

No exact-candidate installed-host, resource/isolation, OCI, failure containment,
full OS reprovision removal or upgrade result is claimed yet. Technical gates,
candidate-specific RTBGG approval, publication and public-download qualification
remain separate requirements. The user VPS has not been accessed or modified.
