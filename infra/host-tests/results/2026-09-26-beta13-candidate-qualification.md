# Beta.13 retained candidate and qualification — 2026-09-26

Status: **large-initrd regression and fresh onboarding/API/rerun/reboot tests
passed on MBR/BIOS and GPT/UEFI**; the last persistence test completed at
`2026-09-26T11:06:06Z`. Remaining release gates are listed below. This record does
not approve publication, upgrades or recovery of an interrupted Beta.12
installation. The public one-line default remains Beta.12.

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

## Still required

- Complete the remaining exact-candidate resource/isolation, real OCI,
  installed-host security collection, failure-containment and same-target
  full-OS-removal gates. Do not relabel Beta.12 evidence as Beta.13 results.
- Obtain candidate-specific publication/scope approval after genuine evidence;
  do not advance the public one-line selector before verified public assets exist.
