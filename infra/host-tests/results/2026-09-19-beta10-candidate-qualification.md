# Beta.10 candidate qualification — 2026-09-19

Status: **second fresh native onboarding, product smoke, rerun/reboot persistence,
host security, resource enforcement, rootless OCI, supervised failure quarantine
and same-VM full OS removal passed. Candidate-specific approval and public
release publication/download qualification remain pending.**

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

## Second independent fresh baseline

The preserved first installation was gracefully stopped, without a reset or
checkpoint restore. A second independent vendor-image pair was attached:

- System disk `6797686c-c86a-4b46-9404-1db00b0dd11b`, initial SHA-256
  `9196c0e15016deb554a53b49532d0362473a37bf24c77a154d76b366d71d6de9`.
- Seed disk `d86dae2d-c994-47ce-a36f-7ba8bc2fc69f`, initial SHA-256
  `eb11de7cfcf4cfa76847728a1a82af18edfe86539780893eaaed2979aa78d8f4`.

Cloud-init completed without reported errors. The 50 GiB GPT root was plain
ext4, without project/quota features, and Stackfort paths were absent. Linux
enumerated this independent system disk as `/dev/sdb`; no `/dev/sda` assumption
was used to install or modify storage. Fresh boot:
`f94ca882-91bd-445c-90c2-7578d02a7aba`; checkpoint
`a4551bca-9c15-4a20-a1fb-445ee4803c99`
(`native-beta10-second-fresh-20260919`). The new public SSH fingerprint was
authenticated through the exact VM/nonce/instance/disk KVP contract before
replacing only this VM's known-host entry:
`SHA256:6LfIjiLHtoVVMlEDgAPvNgtPbHm217/LyUXH52HXjsU`.

Corrected external driver/smoke hashes:
`48f7d3ec51ee43c55b29bbf6d186ca407549955266592963c7d0e33b05febbc9`
and `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196`.
Exact-VM second wrapper:
`33bf5fa4b5c19581a39311ad64164e6d75d681e06c4bb5aa8ca5cbbc5df4b664`.
The selected archive, original build and annotated tag are unchanged.

A bounded off-host observation during installation found TCP 22 reachable,
80/443/8443 and 8080/6081/6082/3306/9000 unreachable over both the private IPv4
and link-local IPv6 lab paths. This is a snapshot, not continuous boot coverage,
global IPv6 or UDP qualification. Retained receipt:
`work/candidates/35452066167-attempt1/installing-v2-tcp.json`.

## Complete second onboarding and persistence

The driver exited 0. All nine installation stages completed and native admission
was granted. The original setup code was redeemed, reuse rejected, and the
administrator logged in. The seven actual installed-API checks were:

- account provisioning and domain-create idempotency;
- static/PHP upload, authenticated download and actual public execution;
- database/user wizard;
- authenticated document-root backup download/restore and restored public bytes;
- FastCGI enable/disable, MISS/HIT, cookie bypass and purge;
- WAF off/detection/blocking, including enforcement before warm-cache delivery.

Both an unchanged pinned-bootstrap rerun and a normal reboot preserved the
original authenticated session, account/package/domain references, file and
backup digests, database inventory, static/PHP delivery, WAF and cache behavior.
No password reset, replacement session, direct database edit or fixture repair
was used. The revised transport successfully followed the same VM's DHCP change
from `172.29.253.77` to `172.29.253.15`, preserving TLS name/pin validation.

Operation: `1a2eb789-c922-4cfc-a9d3-372295d2ae6d`.
Conversion boot: `f2d9cb5a-797a-4364-a129-0bdb0efdd8dc`.
Normal reboot: `7c543173-b02b-4cc9-9f29-c8a14f4c308f`.
Completion: `2026-09-19T15:56:21Z`; checkpoint
`c01cff2e-d75d-4541-9ee5-6780981de73b`
(`native-beta10-onboard-passed-20260919`). Raw terminal output, passwords, setup
code and cookies were not retained. This used genuine tag-provenance verification
with **retained-fixture asset transport**, not yet public GitHub release download.

## Installed-host security, quotas and rootless execution

The read-only collector passed 65 observations, with zero failures and ten
explicitly not-exercised observations. Installed API/agent/installer/sealed
runtime, Trivy and three WAF artifacts match independently extracted archive
hashes. Actual broker mount boundaries, API enforcing AppArmor, ext4 project
quota, service identities/protections, rootful/rootless Podman API socket masks,
private listener scope and account cgroup placement passed. External tests are
explicitly not inferred from this collector; independent review remains absent.

The API-created account is UID/project `200000`, account
`01a0ba61-5356-7e08-bf3b-871537b9b2d5`. Its live kernel limits matched the smoke's
API package: CPU `50000 100000`, RAM 268435456, swap 0 and pids 128. Read-only
`quota -P -v -w -p --filesystem=/ 200000` showed 131072 KiB soft/hard block
limits and 4096 soft/hard inodes; account and `public_html` carry project 200000
and inheritance. This is the requested 128 MiB/4096-inode package, not an
unlimited quota mistaken for enforcement.

The external frozen-source qualification ELF, guarded against the exact
installed archive, DMI, final boot and operation, passed actual installed-broker
resource tests in 8.75 s. New disposable UID249956/249957 fixtures demonstrated:

- 25% CPU: 4069232 us wall, 1028666 us account CPU, 41 throttle events and
  3068603 us additional throttled time;
- process limit 32 with a real kernel pids-limit event;
- 64 MiB RAM limit with a real OOM kill of the bounded 128 MiB pressure child;
- 2 MiB byte and 128-inode limits with exact `EDQUOT` failures;
- own-home access and peer-home denial, unchanged sibling ceilings;
- denied no-write opens of protected cgroup/kernel interfaces as account UID
  and its subordinate UID, with a positive delegated-file control.

The separate installed-broker OCI test passed in 19.87 s. Podman5.4.2 performed
a real `FROM scratch` build including `RUN`, export, Trivy0.74.0 database-backed
scan without bypass, immutable replay and changed-source rejection. The scanned
image ran as container UID/GID1000, mapped to this account's subordinate IDs,
inside its delegated cgroup, with real loopback health and log retrieval. Exact
test application/image/network/account cleanup passed. Only generated disposable
test fixtures were removed; the API smoke account and platform remained intact.
No installed product binary was replaced.

Bounds: the OCI operations crossed the actual peer-UID-authenticated agent RPC,
but were synthetic system operations, not persisted public-control-API OCI
operations. No application public routing, tenant-session authorization,
cross-namespace exploit, migration write or I/O-rate enforcement is claimed.
The smoke package did not request I/O limits. These are functional/enforcement
checks, not a performance benchmark or independent security audit.

## Real completed-recheck process loss

Checkpoint `85e1eb6d-824f-445a-834c-4f7a97ced635`
(`native-beta10-before-fault-20260919`) and hashed positive host evidence preceded
injection. The reviewed observer verified the exact transient unit, executable,
argv, root identity, invocation, pidfd and `checking` attempt4, then stopped and
killed **only** the same MainPID5238. The cgroup, VM and package manager were not
killed. Qualification ID: `d2330453a7c54c3eaa01d56f885cc153`.

The real supervisor quarantined the platform without a repair command: all loaded
consumers stopped and the exact native-admission nft chain dropped non-loopback
TCP/UDP 80/443/8443. Independent private-IPv4 and link-local-IPv6 TCP probes
confirmed closure while SSH stayed reachable. Separately, a ten-minute, fixed
host-only UDP echo helper dropped to UID65534 after binding; all three web-port
controls responded before injection, then stopped responding, while UDP49173
responded before and after each family's blocked probes. This demonstrates a
real UDP drop with live positive controls, not absence of a listener. No global
IPv6 or continuous every-millisecond boot coverage is claimed.

Storage stayed `ready`, package/source/setup record hashes stayed identical,
and admission remained `checking` at attempt4. The interrupted bootstrap exited1
without a success marker or setup code. Another ordinary pinned-bootstrap rerun
also exited1, did not reboot or alter any bound record, and did not reopen access.
`native recovery-plan` classified the state as `admission-review`, exit2.
The invocation-filtered journal was empty (explicitly recorded); a separate fixed
unit journal observation contained five entries and confirmed the MainPID's
SIGKILL and unit result `signal`, with no successful native result. Raw records
and journal content remain private in the preserved guest disk.

This passes process-loss containment and operator recovery classification,
**not successful in-place recovery**. Public resume remains disabled. A final
extra checkpoint was refused at Hyper-V's 50-checkpoint limit; no checkpoint was
deleted. The pre-fault checkpoint and exact post-fault active disk chain are
preserved, and removal uses independent OS disks, never snapshot restoration.

### Retained second-attempt receipts

All paths are below `work/candidates/35452066167-attempt1/`; these ignored raw
receipts are retained locally. This committed report states their actual results
and limits; no recipe or mock is recorded as an executed host test.

| Receipt | SHA-256 |
| --- | --- |
| `onboarding-v2-result.json` | `db6d768963aab993096925cad85161096d0c6b5ac46073c598b16e7f2d691b11` |
| `resource-v2.log` | `7772ef98363a97434cf7d979cc3c58afe90987e3fc5d233fbf596783ff1a7720` |
| `oci-v2.log` | `19bcea2074d03cff7f150d72352398d51ef7b6e5803e4c238715341873c7e481` |
| `host-security-v2.json` | `cc39862ccee15df3b14c34c814ca60ad49da9d4d06d6c044b79f36882afc9ba8` |
| `healthy-v2-tcp.json` | `d9d1acee708f0f7c936b514a4db5fca87f27d873c438aa71e4aef0dc65605d61` |
| `healthy-v2-udp.json` | `ff940e5997c0a7b8cc0c89cc0bd1116d94b2a534451690088cdced5c26c4c03b` |
| `positive-preflight.json` | `c07ad33c852f47e5e4fcee9302413d9f63d218eb2f1412ab359cdbd272615c0a` |
| `recheck-observer-v2.jsonl` | `db860f340b9f26362c30e4f485501b6c2b0d6a4ce3ef9cc9a05b3286f082fadc` |
| `recheck-caller-v2.json` | `50837e40a844c378a4298405166757fa40612d07669cb5550429a03468544981` |
| `recheck-rejection-v2.json` | `7df1822ae60f7852bef4d2cf306359c6a29d24f9dc31770cba835d8f5843ca06` |
| `quarantined-v2-tcp.json` | `e74d17ea296c76c9b281dbf7a3c2538feccb0d7f40f3be75bb3cf29e655e091b` |
| `quarantined-v2-udp.json` | `2a725fbb5ffa17ede982c686c446ece42663675f8124d73dfc371848f36cee23` |
| `supervisor-outcome-v2.json` | `80a18f0b760318caba792d8a600bb3813ccdb21e87d47db71f11d4be434fa4d0` |

## Full-system removal and first-release inventory

The [same-target removal test](2026-09-19-beta10-os-removal.md) passed at
`2026-09-19T16:15:51Z`, completing the ten technical experimental-beta checks.
The same VM now runs an independently deployed fresh Debian13 OS with no
Stackfort state, services, platform listeners or hosting data. The old disk graph
and all50 checkpoints remain preserved and detached; no checkpoint rollback or
secure erasure is claimed. Independent review is still **not performed**.

A fresh public GitHub Releases inventory returned HTTP200, an empty array and
no pagination Link. The actual upgrade-matrix verifier accepted beta.10 with
zero predecessor cells. The tag publication job will fetch the live inventory
again; this result must not conceal a subsequently published predecessor.
Public release assets do not yet exist. Publication still requires the actual
candidate-specific RTBGG approval and current automated gates, followed by a
real public-GitHub bootstrap download/install verification.
