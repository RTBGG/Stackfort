# Beta.13 retained candidate and qualification — 2026-09-26

Status: **exact-candidate host qualification passed**, completed
`2026-09-26T11:54:13Z`. Large-initrd onboarding, security, resources, real OCI,
process-loss containment and same-target OS removal passed. RTBGG authorized
[fresh-install-only publication](../../../docs/release-evidence/2026-09-26-beta13-publication-approval.md).
This report does not claim publication or public-download validation.

## Frozen candidate

- Version: `0.1.0-beta.13`.
- Source: `991f7df6b27093588a69d0dbea2e3eab7a7ceaae`.
- [Original manual build](https://github.com/RTBGG/Stackfort/actions/runs/36235431432),
  attempt 1, passed: six native packages, three minimal-runtime cells and the
  aggregate build/inventory/attestation job.
- Retained artifact: `10904191959`, `stackfort-0.1.0-beta.13`,
  313,230,822 bytes; expires 2026-10-26.
- Original ZIP SHA-256:
  `38895891e3641466319f17e8b962780134e814d23fc7b60a0420e3cd1f6d8ea7`.
- Release archive SHA-256:
  `b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91`.
- Standalone installer SHA-256:
  `ce214102248c7e5ab8069ef7927542b5b63cf7a6ddea3744afd3d38891ad36d2`.

The actual download matched the GitHub ZIP digest and size. The unchanged
promotion validator accepted the complete live run/artifact metadata. The
bounded extractor verified the exact ten-member inventory, every aggregate
checksum and carrier sidecar, version/source inside the archive, SPDX presence,
and byte equality of the standalone and archived installers. These checks do
not substitute for cryptographic attestation verification or host qualification.
No original bytes were rebuilt or replaced.

Mechanical selection is recorded in
[the candidate manifest](../../../packaging/releases/promotion/0.1.0-beta.13.json);
`publicationAuthorized` remains false in the generated verification receipt.
Ignored working artifacts/receipts are retained in
`infra/host-tests/work/beta13-candidate-20260926/`.

## Source verification and authorization

[PR #17](https://github.com/RTBGG/Stackfort/pull/17) was integrated by fast-forward,
preserving the exact frozen source commit. Its
[PR CI](https://github.com/RTBGG/Stackfort/actions/runs/36233861590) and
[PR security](https://github.com/RTBGG/Stackfort/actions/runs/36233861577)
passed, including the large-inventory regressions and two identical development
archive builds. Those PR builds used a synthetic merge revision and are not the
release archive above.

Fresh push-triggered [exact-source CI](https://github.com/RTBGG/Stackfort/actions/runs/36236508283)
and [exact-source security](https://github.com/RTBGG/Stackfort/actions/runs/36236508227)
both passed on attempt 1, checked before candidate metadata/tag publication.
All four CI jobs and all four applicable security jobs passed; only the
PR-specific dependency-review job was skipped on the push event.
The [explicit test-qualification authorization](../../../docs/release-evidence/2026-09-26-beta13-test-qualification.md)
allows mechanical metadata and an unchanged-source tag, not a public release.

## Exact-tag provenance, without publication

The annotated tag `v0.1.0-beta.13` points to the frozen `991f7df` source, not the
later mechanical metadata commit `050de5e`. Tag object:
`49c8323b27c8de7ac94d0f30be0648c7dd51f2c0`.
[Tag run 36237290259](https://github.com/RTBGG/Stackfort/actions/runs/36237290259),
attempt 1, successfully selected the retained build, attested the exact tag and
uploaded the unpublished candidate. It then stopped at the unchanged public
readiness gate because Beta.13 publication evidence does not exist (HTTP 404).
Publication steps were skipped; the expected failed overall run is **not**
reported as a passing release qualification run.

- Tag artifact: `10905165398`, 313,239,196 bytes, twelve members.
- Tag ZIP SHA-256:
  `1e5bbb2a9904be8cd9837acc22fa5ac33083d08905ed2296ee0653812aabaced`.
- Attestation bundle SHA-256:
  `7c2219d067fe4cd6471ba5b70c7b65e86b61c9c07ca2de41fbc4dba2db06d6ea`.
- Bounded extraction confirmed all ten original files are byte-identical and
  the mechanical selection record is unchanged.
- Separate GitHub CLI 2.99.0 verification passed with the exact repository,
  source ref, source/signer commit, release-workflow certificate identity,
  GitHub OIDC issuer and GitHub-hosted-runner requirement. No trust bypass.
  The official Windows CLI ZIP was SHA-256 pinned to
  `ea040d0ba03176440d13481caf67f742236c3b38ce1d4442a9da37e9bb98d8f2`;
  verification receipt SHA-256:
  `3181c4f08029746a84692d22ac4a4fc1e07a9e518818fc85f8f98acabe556dc3`.

## Host preparation

The fixed local MBR/BIOS and GPT/UEFI Debian 13 test VMs were preserved before
restoring their known fresh baselines:

| Variant | Preserved prior state | Fresh baseline |
| --- | --- | --- |
| MBR/BIOS | `99ea8ec9-21ac-4146-b9ee-7538b207f279` | `1f20fd10-024c-49eb-b6bd-ce94eb32ff35` |
| GPT/UEFI | `1b9d8cd1-c77e-4b0c-85a8-9697feab873e` | `dd9deeed-4ae6-4231-951e-abf8ffac5d14` |

Both fixtures have fixed 8 GiB RAM. Before any Stackfort installation they
received Debian's standard `linux-image-amd64` kernel, version
`6.12.107+deb13-amd64`, to exercise a real generic-kernel inventory, not only the
smaller cloud-kernel fixture. A lab-only GRUB_TOP_LEVEL setting selects that
installed kernel. No installed Stackfort executable, journal or security check
was modified. The GPT reprovisioned SSH key was independently matched to the
retained original reprovision receipt; strict host-key checking was not disabled.
The user's VPS and other test VMs were not changed.

After the fixture reboot, both hosts were independently checked as the intended
DMI identities, still without installer state or ext4 quota features, running
the standard kernel above with `MODULES=most`. Each ordinary Debian initrd
listed 1,337 entries / 90,379 bytes. This is preparation evidence only; the actual
Stackfort one-shot image will be measured separately during qualification.
The resulting fresh generic-kernel checkpoints are
`2f452ad6-59fb-435b-814d-e7308660b476` (MBR) and
`8f1acd64-e267-47b3-846c-97e2b90c56cf` (GPT).

## Actual large-inventory onboarding passed

Both tests used the unchanged fixture-restricted onboarding driver and installed
API helper, genuine tag-origin proof and the exact original release archive.
The retained-fixture transport changes only how unpublished bytes reach the
bootstrap; it does not bypass origin checks. This is **not** a public GitHub
download or README-default installation test.

| Evidence | MBR/BIOS | GPT/UEFI |
| --- | --- | --- |
| Operation | `4ce7c2f6-dbe1-4147-8386-7bb441092a65` | `9cd68871-99f0-4ced-a41b-063fa6fceb21` |
| Initial boot | `c0e01f07-091c-4609-ac05-f1f4677162d0` | `2bd5ffcf-2bcb-4a8d-83b2-5259c1b54009` |
| Conversion boot | `01aca899-9c69-4551-b1e4-dd543a299c20` | `2841da3a-b6df-41ff-a3c7-fd0a62d7daf7` |
| Final normal boot | `62b4ac33-a57f-4333-bd83-3dfcb61b5dfc` | `0a66862e-1890-4845-8c14-9395b876c6ba` |
| One-shot listing | 1,346 entries / 90,649 bytes | 1,346 entries / 90,649 bytes |
| Required members | All five present | All five present |
| Healthy checkpoint | `61e5a344-494e-4e44-93b3-4e9d8f0a8180` | `dd8e9494-e71d-4899-84c5-d3704b40797b` |

The actual one-shot images were read from their normal post-conversion evidence
location, `/var/lib/stackfort-installer/retired-one-shot.img`. Their hashes match
the root-owned arming receipts, whose operation IDs match the onboarding results:

- MBR: `41187bee9d3c3633a2ec02d71b92208d0c0177b69fa08e4f0f74adb3f27acc71`.
- GPT: `b29a6ed16a57f561c9026609bb277a82114f50839f4516c725a425a6c9fb65da`.

Thus both actual inventories exceed the old 65,536-byte limit; successful boot
arming and conversion were followed by full installation. These local VM
fixtures are not the user's image and do not assert identical inventory sizes.

On each host the original setup code was redeemed once, replay was rejected and
administrator login passed. All seven installed API groups passed: account and
hosting-package provisioning; static/PHP delivery and upload/download; domain
idempotency; database wizard/inventory; document-root backup/restore; per-domain
FastCGI enable/disable/HIT/bypass/purge; WAF off/detection/blocking before cached
responses. Same-release rerun did not reboot or reissue setup. A subsequent
normal reboot preserved the original session, package/account/domain records,
file/backup digests, database inventory and static/PHP/WAF/FastCGI behavior.

No installed binary, permission, quota limit or recovery journal was repaired to
obtain a pass. The read-only measurement helper was corrected to inspect the
retired image and use host-side JSON parsing after finding `jq` absent; no guest
dependency was added. Transient SSH reconnects followed the normal DHCP/reboot
changes. Setup secrets, passwords, cookies and raw terminal transcripts were
not retained by the driver.

| Retained receipt/helper | SHA-256 |
| --- | --- |
| `mbr-onboard.json` | `b1ff2bfd57aa25ebd4272c98a08b728390066c9e6101bf3074fff7c6b3ac50a6` |
| `gpt-onboard.json` | `ebf8de231af368a81612b4afac55d87bac3b579547673a1a5518d1b4e257e246` |
| `initrd-mbr.txt` | `9a9ad9a42a67e0454e5fc9dc4be1d86077d54318f895bfe09d982065a0e67061` |
| `initrd-gpt.txt` | `2f581df82a53fa22c22140af9764910b13346a8694b06381aea7b3a52cb5e5b5` |
| Unchanged onboarding driver | `e9c256d6e77ca2c5d6e887eecee519ed9cf8f1c1fb668ac00714a41d59cac9fc` |
| Unchanged API helper | `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196` |
| Fixed-pin invocation wrapper | `0d39eb3cf7f0f1e812714e02ec6de5e03e32eae6237797d6b5c9ac93c06e96f5` |

The API smoke does not claim exhaustive tenant isolation, SQL credential or
phpMyAdmin verification, full-account/database backups, actual OCI deployment,
a performance benchmark or product removal. Those exclusions are not converted
into passing results.

## Installed security, resources and actual OCI

On each host the read-only collector passed **65 checks, zero failures** and
explicitly left ten unexercised. It verified eight installed artifact hashes,
service hardening, actual broker mount boundaries, AppArmor and private listeners.
These observations are not an independent audit or pressure test.

A new helper ELF was built from all **1,122 exact frozen Git blobs**, independently
verified by Git object hashes, plus seven reviewed test overlays and their fixed
test-server ELF. An initial Windows archive export had CRLF conversion; the
source check rejected it before host execution. Re-export with LF yielded exact
blob equality and an identical helper build. No installed payload was replaced.
Helper SHA-256: `b83c9a1c95db7bd458cbbc1eef0a23f84d507ab09e6b817f13f10769559cc1d3`.

Both installed-broker RPC resource probes passed: 25% CPU throttling, 32-task
ceiling, 64-MiB memory limit with actual OOM, 2-MiB/128-inode quotas returning
EDQUOT, unchanged peer limits, positive own-home access and peer-home denial.
Tenant and subordinate identities could not open controller/migration files
for writing. No migration write, throughput limit or OS reserve guarantee is
claimed. Successful probes removed only their guarded fresh test fixtures.

Both OCI probes built/exported a real rootless scratch image with RUN, scanned
with installed Trivy 0.74.0 without bypass, verified exact replay and changed-source
rejection, deployed USER 1000 with correct subordinate identity, loopback reply
and cgroup, and completed scoped fixture cleanup. This is installed-agent RPC
coverage, not public OCI API routing or a performance benchmark.

| Receipt | SHA-256 |
| --- | --- |
| `observe-mbr.json` | `1ad80d8b53cba31ba59121c09950794d79a9e9b6e20bad65b1e70ce3096e1d54` |
| `observe-gpt.json` | `35e2ce0c6f8949e6a45f6704f2642bd2a976ae1950f16572ab0e04483f27e7f4` |
| `resources-mbr.log` | `e13320c5f5c64314dce3a3674cabbaf3c031119ae3c69d80a6c8339460520e09` |
| `resources-gpt.log` | `8daa81fe4f6d23ac79f39f6913ed4622db83d899550f8f42e5b59ae075aa78c6` |
| `oci-mbr.log` | `782bf46ffd21f3a14e1751ef4a0dde38f8e81e8873467d9fd2bad8e3c6864ac5` |
| `oci-gpt.log` | `497d0caff9cd47da0b2a59809291696aee65f416156061ac33d8eaec6427a87c` |

## Real completed-recheck MainPID loss

The [guarded plan](../native-completed-recheck-fault-plan.md) ran on both variants
after five observer contracts passed. Before-fault healthy checkpoints:
MBR `c1454ed6-dfe6-4fef-882c-f712cb308667`;
GPT `968920b6-57b6-47c5-af5a-57e71043b6f9`.

The actual pinned-bootstrap completed rerun created the real transient unit.
Its exact MainPID, executable digest, argv, cgroup, invocation and durable
checking attempt were verified before pidfd STOP/revalidation/KILL. No whole-
cgroup kill, product patch or manual quarantine was used. Both observers returned
`injected-quarantine-verified`: consumers stopped, exact non-loopback TCP/UDP
80/443/8443 gate closed, storage stayed ready, admission stayed checking at
attempt + 1, package/source/setup records unchanged. Recovery-plan returned
admission-review/exit 2 with public resume disabled.

Independent IPv4 and link-local IPv6 TCP/UDP probes verified before/after
behavior. Real privilege-dropped UDP echo controls ran on all three web ports
and control port 49173; the control stayed reachable after web ports closed.
SSH remained reachable. No global IPv6 claim. Interrupted callers failed without
success/setup output; ordinary subsequent reruns rejected unchanged state without
reboot. Invocation-only journals were empty, explicitly recorded; separate
fixed-unit journals confirmed SIGKILL and signal failure. This is containment
and rerun rejection, **not automatic recovery or power-loss qualification**.

| Receipt | MBR SHA-256 | GPT SHA-256 |
| --- | --- | --- |
| `observe` | `4205548755149d2b9c589e7d53600d63c1f75bd3d0d98abde3d1ea230918d07c` | `da015423f758a1a4ba42fdeb7e0af596798f92e977221d84606a3b73b80926c7` |
| `positive-preflight` | `4a8a8ccab755e9ea56f6181f37d6b0b81e4f188b530e41aa2f61976ccc5f218d` | `a1ec0c5af400f0d21e4973a636a6fe1179f1986d9f2696f5a79293d75ea4029e` |
| `quarantined-tcp` | `f4e577d3feb0a94b845e14f7cb587f24bf252b2db7f92c9b89ff37c5c5696a66` | `abc85a395003b1a42fd760a62279d81a8ca887cb0067c8ff7e214c9e92354ddb` |
| `quarantined-udp` | `8af6cbe99d408d61a22d4044c0cd6b813b25154099b18c0130acf74b1da1da27` | `ab686bbbea52df9962bbeaffaa6f41978c204a8e93017d225eebb824757465fb` |
| `interrupted` | `8bdc43c083537653925b8a6faafd17398e1de8f01731d411b28fce3636dc72fb` | `d228380fe9d7b2cf524bd03c1b0109a736b5d41ee5ecf2b270e04ba95ba1836b` |
| `rejection` | `ea5184f1efca7489812e4cbb0f50fc9cb080faed97a31719eab8663470cbbaf1` | `04876a8bff0acf788f22f67f35662c1f6dcd9d7eaae2e557b45127ad5652afee` |
| `supervisor` | `9ed006b48412c92413205346536ed2485ac85235c1c9dec24af919e6699c8103` | `e78797e6a5f917ec0fcb57908399919f50c4b5eec72a48f4ba3b46281a64f90f` |

## Removal and publication boundary

[Same-target full OS reprovision](2026-09-26-beta13-os-removal.md) passed on the
GPT VM at `2026-09-26T11:54:13Z`. Old disks/checkpoints were preserved, not
deleted; the fresh OS independently lacked all platform state/data/services.
The MBR failure fixture remains preserved. The user VPS was untouched.

All listed host gates now have Beta.13-specific evidence. Publication still
requires the unchanged readiness validator with live exact-source CI/security,
the explicit fresh-only scope, genuine tag provenance, immutable original assets
and public-download verification. Never advance the public selector first.
