# Beta.15 exact-candidate qualification — 2026-09-26

Status: **all required fresh-install host checks passed**, completed
`2026-09-26T18:22:18Z`. This report does not claim public release or anonymous
download verification. RTBGG authorized the [fresh-only experimental scope](../../../docs/release-evidence/2026-09-26-beta15-fresh-install-scope.md).
No upgrade or independent security review is claimed.

## Frozen artifact and provenance

- Source: `d93c4bfb9fb527e80a648eeb8482b159d0076b29`, [PR #19](https://github.com/RTBGG/Stackfort/pull/19), merged by fast-forward.
- [Original build 36260323807](https://github.com/RTBGG/Stackfort/actions/runs/36260323807), attempt 1: all six native package cells, three minimal-runtime cells and aggregate build passed.
- Original artifact `10911968743`, `stackfort-0.1.0-beta.15`, 313,709,871 bytes, expires 2026-10-26.
- Original ZIP SHA-256: `309ae1aa1cb4711eaa0f39c4a4730df871e0d3d6a6a280427fceb80144ea620b`.
- Runtime archive SHA-256: `b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`.
- Installer SHA-256: `0050f649a384e3daedcab2d27a1cfdb123167f63f44236e1abbf7575e8eb3fff`.
- `SHA256SUMS` SHA-256: `698dfb44e4f022d55c074515dbcfd33455b2ebd06d3f4d1688ce882b47539a4c`.
- Exact-source [CI 36260359106](https://github.com/RTBGG/Stackfort/actions/runs/36260359106) and [security 36260378988](https://github.com/RTBGG/Stackfort/actions/runs/36260378988): successful manual attempt 1, all applicable jobs passed. PR dependency review also passed; PR CI is not substituted for exact-source CI.
- Annotated tag `v0.1.0-beta.15` targets this source; tag object `ec44994b4f9a88092f5930d0bbabfac90beb5f5e`.
- [Tag run 36261277141](https://github.com/RTBGG/Stackfort/actions/runs/36261277141), attempt 1, retained genuine tag provenance, then intentionally failed the missing-readiness gate. Its overall failure is not a passing publication test.
- Tag artifact `10912416486`, 313,717,993 bytes; ZIP SHA-256 `ca73005c33d62da0cff958e8a8e240a9b0c48858fa65f9b4b9ef025342aeefd3`.
- Tag attestation SHA-256: `22f3fb7ecacfa333bf1441633490ddb49dac527b560bb106da43b2bb43114172`.

Bounded extraction checked the original ten-file inventory, all checksums,
passive carrier sidecars, archive source/version and archived/standalone installer
equality. The tag-qualified twelve-file artifact retained all ten original files
byte-identically and the same mechanical selection. Separate GitHub CLI 2.99.0
verification required the exact repository, tag ref, source/signer digest,
release-workflow certificate identity, GitHub OIDC issuer and hosted runner.
Receipt SHA-256: `b2a3502277392b1fb88b40c3323bb348a44bd43edcb0043be16f0d2d126b769c`.
No rebuild, moved tag or trust bypass was used. Beta.14 remains frozen and unpublished.

## Disposable fixtures and real onboarding

Prior states were preserved before restoring the known fresh generic-kernel
baselines. Both exact VMs had 8 GiB RAM, Debian 13 amd64, standard kernel
`6.12.107+deb13-amd64`, plain ext4 without project quotas and no installer state.
APT timers remained enabled. The user's VPS and other VMs were not modified.

| Identity | MBR/BIOS | GPT/UEFI |
| --- | --- | --- |
| Hyper-V VM | `1f5e665a-fddd-4cd2-bc55-44255b01963d` | `55729cf8-11e3-4744-be5b-ddde3824c0e4` |
| Preserved previous state | `d3b2353c-f26c-489c-bd03-3973630487dc` | `50f0ce33-049e-466a-bcff-967274fffbb9` |
| Fresh baseline | `2f452ad6-59fb-435b-814d-e7308660b476` | `8f1acd64-e267-47b3-846c-97e2b90c56cf` |
| Installation operation | `4de3c58a-2bb5-4752-aef3-c73568490dd5` | `8f0f0893-9f44-425f-ad08-07d696e74ae4` |
| Conversion boot | `c17fe507-0841-4c0c-a4e1-b658d76dd471` | `7a9637b5-33ab-45f6-be06-494fb17e9995` |
| Final ordinary boot | `dfb275c8-e5c7-448a-ae92-d4000d17a317` | `2cb02918-ffcc-4ced-badb-2cb0c130bfb6` |
| Healthy pre-fault checkpoint | `01f4c2e2-8401-421a-a766-1fc8bb149994` | `b6e5950b-8a23-44a3-8995-9836cdf971df` |

The pinned driver used retained-fixture transport only to deliver unpublished
original bytes. The real installer still verified strict tag-release origin.
This is not yet a public GitHub download or README-default test. Bootstrap
SHA-256: `a2a1f6ff2c3d79f11815b1d46d01e6ee0d5cae3929f893256daa903738bfe783`;
driver SHA-256: `7d4d988ae2dd232457f1ce74c6c5738cf1bee921bd703185e8e796e37a7b4bab`.

Both actual one-shot images contained **1,346 entries / 90,649 bytes**, all five
required members, with their digests matching the operation-bound arming receipts:
MBR `71f69cc1ef0e4064670b04342141eb8ee5b734343439685b314548cb9d4d71d9`,
GPT `f53d725eed68c168a81a988b03ca4ed3508ce31db5964e1cb1894ee61d3052d8`.

Original setup redemption, replay rejection and administrator login passed on
both. Seven installed API groups passed: account/package provisioning; static/PHP
upload and delivery; domain idempotency; database wizard/inventory; document-root
backup/restore; FastCGI toggle/HIT/bypass/purge; WAF off/detection/blocking before
cached replies. Same-release rerun issued no new setup and did not reboot. A
subsequent normal reboot preserved the session and all tested account, domain,
file, backup, database and WAF/cache behavior. No installed binary, journal,
permission or quota was repaired to obtain a pass. Secrets and raw interactive
transcripts were not retained.

### Targeted package-updater collision

An independent root-owned, exact-source/VM-bound watcher observed the GPT sealed
manifest at `18:09:08.453674Z`. A real APT client with an impossible package,
no-download and assume-no returned exit 100 due to the frontend package lock at
`18:09:08.461884Z`. It could not install anything. The watcher then started the
ordinary `apt-daily-upgrade.service` at `18:09:08.473176Z`; no update timer,
package policy or lock file was disabled/deleted. Native arming, conversion,
installation and rerun/reboot all passed. This directly exercises the formerly
vulnerable handoff, alongside the 24 kernel-level guard scenarios recorded in the
[development report](2026-09-26-beta15-package-handoff-development.md).
It is not a persistent shutdown fence or protection against arbitrary root writes.

### Installed panel and service semantics

Read-only calls through the real unprivileged API service identity and installed
agent accepted the native panel-management inspection. Both hosts reported
global `php8.4-fpm.service` and `podman.socket` as available but inactive/dead,
and managed `stackfort-firewall.service` as active/exited. Global inactivity does
not mean account PHP pools or rootless OCI are unavailable; both were exercised.
The panel helper SHA-256 is `595fe703e317647816cd37bd9d3eaedf44ed19d0aa4622e45ed37feeadb6d182`.
ACME/UI/panel-transaction regressions, including real NGINX/private-CA checks,
passed exact-source CI. No public Let's Encrypt issuance or user's domain/DNS
change is claimed by these local tests.

## Security, resources and real rootless OCI

The read-only collector passed **65 checks, zero failures**, with ten explicitly
unexercised checks on each host: installed artifact hashes, service hardening,
actual broker mounts, AppArmor and private listeners. This is not an independent
security audit.

The separate helper was built from **1,162 independently verified frozen Git
blobs**, seven reviewed qualification overlays and their fixed server ELF.
Helper SHA-256: `b6129c47b907e90fc7c08d9d2c0104066ea7784c39f9aaf05dacc9ecff2ccdee`.
It did not replace installed product code. Both actual broker-RPC resource tests
passed 25% CPU throttling, 32-task pressure, 64-MiB memory pressure with actual
OOM, 2-MiB/128-inode EDQUOT, own-home access/peer-home denial and unchanged peer
ceilings. Tenant/subordinate identities could not open control/migration files
for writing. No migration-write, throughput or OS reserve guarantee is claimed.

Both OCI tests built/exported a real rootless scratch image with RUN, scanned
using installed Trivy 0.74.0 without a bypass, accepted identical replay and
rejected changed-source replay, deployed USER 1000 with correct subordinate
identity, loopback response and cgroup, and completed scoped fixture cleanup.
These are actual installed-agent RPC tests, not public OCI API routing or a
performance benchmark. API smoke does not assert full-account/database backups
or phpMyAdmin/SQL credential qualification.

## Completed-recheck process loss and external containment

After five observer contracts passed on each host, the [guarded process-loss plan](../native-completed-recheck-fault-plan.md)
targeted only the actual completed-recheck MainPID, pinned executable, argv,
cgroup, invocation, operation and checking attempt. Pidfd STOP/revalidation/KILL
was used; no whole-cgroup kill, modified product, inserted sleep or manual
quarantine. Both returned `injected-quarantine-verified`. Consumers stopped,
the exact non-loopback TCP/UDP 80/443/8443 gate closed, storage stayed ready,
admission stayed checking at attempt 4, and package/source/setup hashes remained
unchanged. Recovery-plan returned admission-review/exit 2, public resume disabled.

Independent Windows-host TCP and positive-control UDP tests passed over private
IPv4 and link-local IPv6 before/after injection. All three web ports closed;
SSH and the independent UDP control port 49173 remained reachable. No global
IPv6 assertion is made. Interrupted callers and subsequent ordinary reruns
failed without success/setup output, record changes or reboot. Invocation-only
journals were empty; separately retained fixed-unit journals confirmed SIGKILL
and signal failure. This is containment and rerun rejection, not automatic
recovery or power-loss qualification.

## Retained receipts

Private working files are under `infra/host-tests/work/beta15-candidate-20260926/`.
Only nonsecret summaries/hashes are published here.

| Receipt | MBR SHA-256 | GPT SHA-256 |
| --- | --- | --- |
| onboarding | `9b6be5908f8a21bd684c666f8db66a0a512bb05e270b5b81d5e30bcd7be24870` | `14278949625d3fd4cbad11b9052f7de7572aac7357fac8d451104d12aa03d20f` |
| host security | `28dcad3ea7566df4f67ea388e9868baf593b7d118edeb6c84baaa8aba70493a9` | `c947b09439435b45cc369120fc5d4a9dbc93c3fbcb897102bac75b8c01add153` |
| resource pressure | `fc3f8a45adf2c4ed1030e0745d5015418a7f31811d84a4f94361bd6f09563b03` | `17aeb8ede5d3ffadc9069b434412093ae4f6e2203815fa8a6386b6a9ce44dd87` |
| rootless OCI | `c2e516d14a7d2c0e749dde0747e6286d5a19948b6cf98fea0668e1349d6e529a` | `3fe2499cd1b33c48e8c52fbcfc3b33ffff9adab64c51509646b6c0bdc68d977c` |
| fault observer | `f681b15d559a9e53c5af683ce49dbc53a81bd17bb9bcb503b7124fc6c5a9103c` | `1bf11ee3754282671a563b38d8d39d59d3f73cf430052a3a0557024a8ee99d19` |
| quarantined TCP | `8c1504984cabb2f4042570bc1178cdb27fac810a4c48c93d2faff3a8f10cc464` | `325b982284c8eff4b7f7dc5363f30c958c0207255a51b8e21be52b6780c75a59` |
| quarantined UDP | `2e1a50ab45ba3033d6ef583e83fbb6e5b0965871427ad984f8e76359e8d6e674` | `6f0d104c125ee34c4d49592fa4c113f3332a9e00fb7a79f2385b4f250b0cb6e9` |
| later rerun rejection | `74b96bee1437338441aacc38ab4e4c8231236b9b1d840d05a02bdb74da5b09a9` | `921521f5d4595294758c47cb181d90a67d2b51bcf0e5e70b2bda8d9bea65a0df` |

GPT package-race receipt: `1e000eace71b87ebdf1707daa340f00adaeb512c61f90ad168377ad5821d8fd2`.
[Same-target whole-OS removal](2026-09-26-beta15-os-removal.md) passed at
`18:22:18Z`, retaining old disk chains/checkpoints. The MBR failure fixture remains
quarantined. Publication still requires validated evidence, immutable original
assets and anonymous download checks before advancing the public selector.
