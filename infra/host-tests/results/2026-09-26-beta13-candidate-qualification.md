# Beta.13 retained candidate and qualification — 2026-09-26

Status: **original build verified; host qualification pending**. This record
does not approve publication, upgrades or recovery of an interrupted Beta.12
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

## Still required

- Retain and cryptographically verify genuine exact-tag provenance without
  rebuilding the original candidate.
- Measure the actual Stackfort one-shot inventory above 64 KiB and complete
  real fresh onboarding/setup/hosting/rerun/reboot checks on both fixtures.
- Complete the remaining exact-candidate resource/isolation, real OCI,
  failure-containment and same-target full-OS-removal gates.
- Obtain candidate-specific publication/scope approval after genuine evidence;
  do not advance the public one-line selector before verified public assets exist.
