# Beta.10 candidate qualification — 2026-09-19

Status: **original build and integrity checks passed; no fresh-host installation qualification
claimed. Not qualified for publication.**

This candidate retains the native installation and NGINX activation fixes, and
corrects the staged-file ACL defect found by the
[beta.9 installed product smoke](2026-09-19-beta9-candidate-qualification.md).
Uploaded files now receive the destination's default ACL before publication.
Copy/archive staging remains private while descendants inherit the destination
policy. Backup restoration retains live directory policies, including private
directories and nested web roots. Normal move/trash inode permissions are unchanged.

Five focused Linux tests and five negative subcases passed unprivileged and as
root. The root pass executes real UID/GID 65534 reads, verifying web readability
and private/staging denial. The complete Linux hostfiles suite and Linux vet also
passed. CI now includes the privileged ACL regression pass. These local results
are source tests, not qualification of a release archive.

Required next: exact-source CI/security, an original manual build, mechanical
candidate selection, genuine tag provenance, and fresh full onboarding/product
smoke. Rerun/reboot, host/security/isolation/OCI/failure/removal tests, actual
candidate-specific publication approval and publication gates remain mandatory.
No beta.9 tag, installed binary, credential, ACL or failed fixture is repaired or
reused as successful qualification. The reserved final-removal disk pair remains
unconsumed.

## Prepared exact source and disposable baseline

Candidate source is `0096faed0ae77aef3326ce629db7591a433bb37f`. Original manual
build [35451346082](https://github.com/RTBGG/Stackfort/actions/runs/35451346082)
completed successfully on attempt 1. All six native-package jobs, the three
compiler-free Vinyl runtime jobs and aggregate packaging passed.

[CI 35451311504](https://github.com/RTBGG/Stackfort/actions/runs/35451311504)
and [Security 35451311533](https://github.com/RTBGG/Stackfort/actions/runs/35451311533)
passed on attempt 1 at the same source. Their complete latest-attempt job
inventories passed the exact-source readiness validator. CI includes the real
privileged ACL tests and reproducible archive comparison.

The exact disposable VM `4361f439-15e9-4f9e-a690-9a8e44b6cbd3` was gracefully
stopped only after checking beta.9's DMI/boot, completed installer version,
fixed fixture operation and all three archived binary hashes. Its failed disk
chain/checkpoints remain intact. A new independent vendor-image pair was attached:

- 50 GiB system disk `d1e8bc6e-645a-3646-ac8b-e7afd3222339`, initial SHA-256
  `51962514d96059b7a815959c0c467f90770a0bfbd49f13b38a1df73994480163`.
- 64 MiB seed `5251c055-20ea-4306-9aea-c72feb8a67f8`, initial SHA-256
  `8d87bd2106fba97be1c6839c07c0ea688adb45594bb48ce749e4a991ddc8575d`.

The retained Debian vendor image was checked against official HTTPS/SHA512SUMS;
no detached signature verification is claimed. Fresh cloud-init completed with
no reported errors, GPT/ext4 lacked project/quota features, and Stackfort's
installation/state/hosting paths were absent. Baseline boot:
`18176a5c-bc65-43aa-8255-989f63f07d4c`; fresh checkpoint:
`0c8a99a0-cb09-439b-96ab-88a5eb577b7e` (`native-beta10-fresh-20260919`).
VM-bound KVP/instance/nonce/disk validation authenticated the new public SSH key
before updating only that VM's known-host entry. Its fingerprint is
`SHA256:nz5E7ezW1aGWqe8ncXj3Bz9jfwScpoPvf2BCaVN4/aQ`.

A separate external Linux integration-tag test executable uses all 1,069
independently verified raw Git blobs of the frozen source plus the same seven
named qualification overlays and fixed OCI ELF fixture. Source TAR SHA-256:
`3ad987205480156597677b9f01936b76a2c5ed9c381bd60e1c3cc5653aaf596b`.
Test ELF SHA-256:
`15d2eaa811e741f90a58028ce395aa031b3f3149222c3aad24c049ef9baf692b`.
All 13 pure helper contracts passed unprivileged on the fresh VM. The read-only
collector's 16 and process-loss observer's five local contracts also passed.
These do not constitute actual OCI, resource-enforcement or failure qualification.

## Original candidate selection

Artifact `10586044694` contains 313,169,813 bytes, ZIP SHA-256
`4d68a705d86a58df3d1c8519c01e0d4c39cc8142bb3b184859c0a0a4ff061896`.
Its complete ten-file inventory, checksums, source/version and standalone-versus-
archived installer equality passed the strict extractor. The selected TAR SHA-256
is `8e7ce111d6cefda172e74bd7a71380c181e66a726d8f157f6bd1af8944e93263`.
Installer/API/agent hashes independently read from the archive are:

- `40331c7ee783aba3f4ca9597f0d2881afa45a83c92ee0359d7583c41064f30b2`
- `5a6bae25339af3c29fd208b3f269437cc2dbb00aa0da7c816c52ca957b671865`
- `4c55198781babe723c92451fa62af790aa610465a266467dc19966e2c8af4b49`

The canonical promotion record is a mechanical selection, not publication
approval. Real tag-origin verification and full installed-host qualification
remain required. The original build must not be rerun or substituted.
