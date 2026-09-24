# Beta.12 candidate qualification — 2026-09-24

Status: **exact-candidate host qualification passed**, completed
`2026-09-24T15:06:39Z`, including MBR/BIOS and GPT/UEFI onboarding, resource and
security checks, actual rootless OCI, process-loss containment and same-target
full OS removal. RTBGG authorized experimental fresh-install-only publication;
see the [scope decision](../../../docs/release-evidence/2026-09-24-beta12-fresh-install-scope.md).
Publication and public-download checks are separate, not claimed by this report.
The frozen candidate and published Beta.11 bytes remain unchanged.

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

At this initial stage, the installed MBR fixture remained available for the
checks below. No setup code, password, cookie or raw terminal transcript was
retained. The GPT/public Beta.11 fixture was subsequently preserved before its
separate test; the original public Beta.10 VM remains untouched. This retained
transport is not a public GitHub download test.

| Local receipt/helper | SHA-256 |
| --- | --- |
| `mbr-onboard.json` | `5b1cd6c7c86b9ed8f128703da2bc40e8c03eb4bc1c72ff7a6f6e21d1604efe72` |
| `post-install-optical.txt` | `21f6a537ba0ae487e2fee5c6f178266be1a3229aa2b420184bd81ef678f3138b` |
| Onboarding driver | `e9c256d6e77ca2c5d6e887eecee519ed9cf8f1c1fb668ac00714a41d59cac9fc` |
| Installed API helper | `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196` |

## GPT/UEFI fresh installation

The same exact retained candidate passed the unchanged onboarding driver on VM
`55729cf8-11e3-4744-be5b-ddde3824c0e4`, DMI
`a5db08d7-6c4e-47e0-a381-9bd594b2731a`. The earlier public Beta.11 instance was
preserved as checkpoint `b29e55f5-12d4-42c2-a5e8-4e66f647752b` before restoring
the genuinely fresh baseline. This is not an upgrade test.

- Operation: `8a5d5d5a-bc3b-4990-8944-95448056fbcc`.
- Initial boot: `bf75d40e-9de1-4d80-9d2c-8c49bc5d8898`.
- Conversion boot: `ac89a0cf-e7c1-46a1-bf0a-96c0467226d6`.
- Normal reboot: `be3be9af-73dc-4dfa-a15c-ff7033ad18fe`.
- Setup redemption/replay rejection, admin login, all seven installed API smoke
  groups, same-release rerun and original-session/data persistence after normal
  reboot passed; completion `2026-09-24T14:55:04Z`.
- Receipt `gpt-onboard.json` SHA-256:
  `0a0804bb419ad31a27fbffc1f35b5727c51414236f8aca5f07dd11e093753030`.

## Installed-host security, isolation and real OCI

On **each** variant the read-only collector passed 65 checks, failed none, and
explicitly left ten checks unexercised. The separate tests below cover relevant
runtime/closure/removal items; an independent review, global IPv6 and exhaustive
isolation are not claimed. The collector verifies all eight installed payload
digests, actual service hardening/mount boundaries, AppArmor and private listeners.

Qualification helpers were built from all 1,103 independently verified frozen
Git blobs, with only seven test overlays and their test-server binary added.
No installed product binary was replaced. Final helper ELF SHA-256:
`50a71f108b187023fafa4bbb61796f904208ae6b36f520dbd7b0fac6621284b1`.
Guards pin the independent candidate, payload hashes, actual DMI, boot and operation.

Both resource tests used the **installed broker RPC**, two fresh accounts and
bounded workloads. They verified 25% CPU throttling (about 1.02 CPU seconds over
4.03–4.06 seconds), 32-task ceiling, a 64-MiB memory limit with a real OOM event,
2-MiB/128-inode limits returning `EDQUOT`, unchanged peer limits, own-home positive
access and peer-home denial. Owner and subordinate identities could not open
controller/migration files for writing. No migration write, IO-throughput limit,
container-escape test or aggregate OS reserve guarantee is claimed.

Both OCI tests built/exported a real rootless scratch image with a RUN step,
scanned it with installed Trivy 0.74.0 without bypass, verified identical replay
and changed-source rejection, and deployed USER 1000 with expected subordinate
host UID/GID, loopback response and cgroup membership. Scoped fixture cleanup
passed. This is installed-agent RPC qualification, not public OCI API routing,
database credential/phpMyAdmin coverage or a performance benchmark.

Two helper-only issues are retained transparently: the first MBR OCI log used
an old source-reference **label** despite the correct independently pinned source
and payload; it was corrected and both probes rerun with the final helper. The
first GPT pressure probe refused its root-only helper directory before creating
fixtures. Making only that test directory/executable traversable (root-owned
0755, not tenant-writable) allowed the unchanged probe to run. No product repair,
limit relaxation or discarded failed product result was involved.

| Final receipt | SHA-256 |
| --- | --- |
| `host-security-mbr.json` | `9f33fe09e38f92a3a3c36d742e54c4c25df605ab4f4ea70e8a781ea1a42f3003` |
| `host-security-gpt.json` | `7d998b70f7b34cc0731a461ac8fe3f2cd84150140a9bfdaea2c71b15981fce3f` |
| `resources-mbr-v2.log` | `1b155f437f42f76af010552a106a672127e5284124dbd55383c4356633b88592` |
| `resources-gpt-v2-retry.log` | `d7ee8e99fe062e0dbb88b335c2ecca5b03a9c4b825616b8eb5ae2b0d227100cc` |
| `oci-mbr-v2.log` | `06abc7490e5c8d0bf3ee7b43a3122d15c1d3e7593f55f435fb0c50de38b09b64` |
| `oci-gpt-v2-retry.log` | `4b23b1a8e821dd6de1d0fe008a66f76bf3a324b6661f5ecf56668d30bdc0b251` |

## Real completed-recheck MainPID loss

The reviewed [process-loss plan](../native-completed-recheck-fault-plan.md) ran
on **both** variants, using real pinned-bootstrap completed reruns. Five observer
contract tests passed first. Preserved healthy checkpoints are
`efafd9bd-1b68-46e5-a645-12e6af886ca6` (MBR) and
`354857cd-9086-46e4-a548-65967e68fe94` (GPT).

The observer selected only the exact transient unit's MainPID, verified executable,
full argv, root identity, cgroup, invocation and durable checking attempt, then
used a retained pidfd for STOP/revalidation/KILL. No whole-cgroup kill, product
patch or manual quarantine was used. Both results: `injected-quarantine-verified`.
The real supervisor stopped consumers and closed non-loopback TCP **and UDP**
ports 80/443/8443. Storage stayed ready; package/source/setup records were unchanged;
admission correctly remained checking at attempt + 1. Recovery-plan returned
`admission-review`/exit 2, with public resume disabled. This is containment and
rerun rejection, **not successful automated recovery or a power-loss test**.

Independent host tests verified IPv4 and link-local IPv6 TCP/UDP behavior before
and after loss. UDP used real, privilege-dropped, host-only echo controls on all
three web ports plus control port 49173; control remained reachable after web
ports stopped replying. SSH stayed reachable. No global IPv6 claim is made.
Both interrupted callers returned failure without success/setup output; later
ordinary reruns were rejected without record changes or reboot. Invocation-only
journals were empty (explicitly recorded); separate fixed-unit systemd journals
confirmed MainPID SIGKILL and signal failure, with no successful native result.

| Receipt | MBR SHA-256 | GPT SHA-256 |
| --- | --- | --- |
| Observer JSONL | `e5ae80309f258fbae3bad526b1d18f04890c7f301a4c64c62350805adc6fce25` | `62a254d579750faecd9414ddf7c57ca294873e1411709888d3b0cf8adf8b6716` |
| Independent positive preflight | `ea2bdeb09e81c0e1e3be395ef65dd72b958e79dec39d19535296749c290149c7` | `cb5011270b7e5a084fe6c1ffff35d8fd0f125dfdd693baaecd6ab530793f5dd3` |
| Quarantined TCP | `fea80bef1c4540afa6b8e78add1fdfabe1ee69c343d2c6c7ab44e0fad26b3fd0` | `29d339e73b429a289bddf0b0d91da378b83c3c293faf058a2934217d0a741aec` |
| Quarantined UDP | `9553b63611ef5ef2e687d3ea3ca17b72a5a603d5ab26888fc7a1c4c859096456` | `dd5b5ee78dcae75c853f5e7be99ed801616b3ddffb346f33c4eefa1401cf12fa` |
| Later rerun rejection | `8d477fb2c590c66c5164ac7ca3785bdab042421884179a624d9a3b5e4d7b61e9` | `a127f86fdd3ac8d9a55b4cbde109c9b7c65634b1d5c6346ddedcd4aa6a421609` |
| Systemd summary | `c4264715429d206fed29a65a73339fae1b56516ec8c51c08d70efa4f81a99a71` | `c507066df791fe33c3a52ac27bf8c37af9a5db1e000788f4c06e1395c05144d3` |

Raw private records/journals remain on preserved guest disks. Qualification IDs:
`c9f5ff1ea10e4e508f537c61e1c13a3a` (MBR),
`fb0d2d5d4da7490e8437a839cd9bbd51` (GPT).

## Removal and release boundaries

[Same-target complete OS reprovision](2026-09-24-beta12-os-removal.md) passed after
the active GPT tests. Snapshot restoration was not counted as removal. Public
Beta.10, preserved Beta.11 disks and the user's VPS were untouched.

Fresh native scope remains experimental Debian 13 amd64 only, with the qualified
ext4/GRUB layouts. RTBGG explicitly approved **no upgrades** for Beta.12: no older
upgrade evidence is relabeled, no predecessor is retired, and no passing upgrade
matrix is fabricated. No independent review, production support or important data.
Live CI/security, original-validator readiness, cryptographic provenance and
immutable publication still need enforcement by the bounded publication workflow;
the bootstrap default must not change before verified public assets exist.
