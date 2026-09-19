# Beta.10 candidate qualification — 2026-09-19

Status: **first fresh native installation, product smoke and same-release rerun
passed; final reboot/session test stopped at a lab DHCP-address guard.
Not yet fully qualified for publication.**

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

The annotated `v0.1.0-beta.10` tag peels to that unchanged candidate source.
Tag run [35452066167](https://github.com/RTBGG/Stackfort/actions/runs/35452066167),
attempt 1, downloaded the selected original without rebuilding, attested it
and retained artifact `10586079932` before stopping at missing readiness evidence.
The publication step was skipped. Tag ZIP: 313,177,928 bytes, SHA-256
`4e0a0d47f89e82bec1baedd79c726805b5e45d99fe5a406ae52ad830e8c1e803`.
All ten original files are byte-identical. The added attestation SHA-256 is
`0443dbab381d0fff736239be2e88a2408ec1cb8b0188bdeb8ddb97c748e0eddc`;
`SHA256SUMS` SHA-256 is
`2f46582e7f806aff11f0d1706df4f35e9fe98c42af9528160fdfa46e470c5b2e`.
The local extractor checks equality/integrity, not cryptographic origin; the
ordinary archived installer must independently verify the genuine tag origin.

The unchanged onboarding driver and API smoke hashes are respectively
`7cdfb3ebc072358f5625aa515c82b03ae3fa9e0f8560d67ee970d0b948f6bcd8` and
`6900ba286445b92e47da419563b4616760bae686ddbc4e7d75ed2d860af9a727`.
The exact-VM wrapper SHA-256 is
`2d97b557a12a37aa9fd81b747df62c56cf7add4e3e3d127d53558470c3940a36`;
bootstrap SHA-256:
`21977a1a0cb79b0768a23da3e669697f8451a51179643e49b17e270dfbffbe65`.
Onboarding has started with retained-fixture transport and ordinary strict
tag-release verification. No completion or public GitHub download success is
claimed merely from starting this driver.

## First fresh installation: DHCP test-harness interruption

The original archived installer passed genuine tag-origin validation, prerequisites,
automatic offline storage preparation and the acknowledged conversion reboot.
All nine native stages completed with admission `admitted`. Original setup
redemption, replay rejection and administrator login/session passed. The installed
API product smoke completed, including uploaded static/PHP delivery, database
wizard, authenticated document-root backup/download/restore, FastCGI
enable/disable/MISS/HIT/cookie-bypass/purge, and WAF off/detection/blocking before
warm-cache delivery. The same-release installer rerun and original-fixture
persistence checks then passed.

The driver requested a normal reboot, but Hyper-V DHCP changed the lab IPv4
address from `172.29.249.231` to `172.29.241.100`. The old driver explicitly
rejects a different address before its post-reboot authenticated persistence
check. It exited 1 at `normal-reboot`, disposed the in-memory credentials, and
emitted no completed qualification receipt. No setup-code/session recovery,
password reset or direct credential/database repair was attempted.

Read-only checks found all native stages complete, admission `admitted`, and
NGINX/API/agent/native-install active after reboot. All three installed binary
hashes still match the exact archive. Conversion boot:
`b29305f0-6302-4cd3-a84b-36b6e25abdeb`; normal reboot:
`4e32ce5f-5a25-41f4-96a5-fa4bf98a8baa`. Checkpoint
`b881ad51-7e27-4ddb-8b57-ebeaccbd4f41`
(`native-beta10-dhcp-reboot-20260919`) preserves this state. A concurrent off-host
TCP snapshot overlapped completion/reboot and produced no completed report; it
is not installation-gate or network qualification.

The external test driver now retains its original TLS authority, certificate
fingerprint/name/validity checks and original host-only session cookies while
routing fresh TCP connections to the same SSH-authenticated VM's newly observed
address. Public HTTP fixture checks separately use that verified address.
No cookie export, credential recreation, DNS change or certificate-validation
bypass is introduced. Local contracts cover the changed address and continued
rejection of changed setup identity/expiry and unregistered transport rebinding.
This uses the supported
[SocketsHttpHandler connection callback](https://learn.microsoft.com/en-us/dotnet/api/system.net.http.socketshttphandler.connectcallback).

This is a qualification-harness change, not a change to the selected production
archive. The exact beta.10 candidate/tag remains unchanged. A second independent
fresh baseline and full original setup/login/product/rerun/reboot sequence are
required; the first attempt is not relabeled as a complete pass.
