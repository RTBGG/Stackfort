# Beta.14 candidate qualification

Status: **publication blocked**. The exact-archive MBR/BIOS onboarding, installed
API smoke, rerun and normal reboot passed. GPT/UEFI stopped safely with
`arm-failed` during a concurrent automatic Debian package update; its preserved
state has confirmed package/tool drift. This is not a complete installation
qualification or public-release approval. The public one-line installer remains
on Beta.13.

## Frozen candidate

| Field | Recorded value |
| --- | --- |
| Version | `0.1.0-beta.14` |
| Source commit | `3681c2fb2bf5053ac2e6eafc6727d62ec1c1ffda` |
| Source PR | [#18](https://github.com/RTBGG/Stackfort/pull/18) |
| Release build | [36257102618, attempt 1](https://github.com/RTBGG/Stackfort/actions/runs/36257102618) |
| Build artifact | `10911073741`, `stackfort-0.1.0-beta.14` |
| Artifact ZIP bytes | `313708616` |
| Artifact ZIP SHA-256 | `6be1f26030db13133385d542dc2881ac461948056f38ba8b51aa38b467e25f92` |
| Release archive | `stackfort-0.1.0-beta.14-linux-amd64.tar.gz` |
| Release archive SHA-256 | `7e6a35ba4223fd8dbb8c485f56eacde43289fe4d5da175f11a0a46538410fe67` |
| Standalone installer SHA-256 | `ff8415e23bc3308e9046b97b87adc909936efd6c7ff7891171795034a1fd8d95` |

The build completed successfully across the native WAF/Vinyl OS matrix and the
three minimal-runtime Vinyl checks, then generated the archive, passive carriers,
SPDX SBOM and build provenance. It is a manual branch build, not tag provenance.
Do not rerun this original workflow attempt or replace its retained artifact.

## Completed checks

- Exact-source [CI 36257129010](https://github.com/RTBGG/Stackfort/actions/runs/36257129010)
  (`workflow_dispatch`, attempt 1): workflow hygiene, race-enabled Go tests,
  Linux privileged integration fixtures, frontend tests/audit/build and
  reproducible release-shaped archives all passed.
- Exact-source [Security 36257158469](https://github.com/RTBGG/Stackfort/actions/runs/36257158469)
  (`workflow_dispatch`, attempt 1): full-history secret scan, Go vulnerability
  analysis/static security checks and both CodeQL languages passed. Dependency
  review is not applicable to a manual dispatch.
- PR [CI 36257057823](https://github.com/RTBGG/Stackfort/actions/runs/36257057823)
  and [Security 36257057816](https://github.com/RTBGG/Stackfort/actions/runs/36257057816)
  also passed; the PR security run includes dependency review. These PR runs do
  not substitute for the exact-source release-readiness CI records above.
- The original artifact was downloaded and its GitHub-reported ZIP digest and
  byte size verified locally. `promote-release-candidate.mjs --mode verify`
  accepted the actual run/artifact inventory and returned
  `publicationAuthorized: false`.
- `extract-release-candidate.py` accepted the fixed ten-file inventory, all
  checksums, carrier sidecars, archive VERSION/COMMIT and SPDX metadata, plus
  equality of the archived and standalone installer. No files were substituted.
- The [development report](2026-09-26-panel-ui-fixes.md) records the regression
  tests and read-only panel RPC through a separately started candidate agent
  using the installed sandbox and service identity on a disposable Debian host.
  This is useful integration evidence, not a full candidate installation.

## Exact-tag provenance without publication

[PR #18](https://github.com/RTBGG/Stackfort/pull/18) was integrated by fast-forward
at `3681c2fb2bf5053ac2e6eafc6727d62ec1c1ffda`. Mechanical metadata was committed
after it as `4b9d8fe`. The annotated tag `v0.1.0-beta.14` points to the frozen
source, not the metadata commit; tag object:
`2e4b9e3ff7b39e806a77bfae7ef5d4660fc0cf30`.

[Tag run 36258110592](https://github.com/RTBGG/Stackfort/actions/runs/36258110592),
attempt 1, successfully verified the retained original build, issued genuine
exact-tag provenance and uploaded the unpublished candidate. It then stopped at
the unchanged release-readiness gate because the Beta.14 publication evidence
file does not exist (HTTP 404). Upgrade/publication steps were skipped. Its
expected overall failure is **not a passing publication qualification**.

| Retained proof | Value |
| --- | --- |
| Tag artifact | `10911382567`, `stackfort-tag-candidate-0.1.0-beta.14-attempt-1` |
| Tag ZIP bytes | `313716802` |
| Tag ZIP SHA-256 | `ac5a3f34fc79d41332389eb040c5e6163ea8b0df502c5ccb922eed7675114c20` |
| Attestation SHA-256 | `c124080fbbf195117bb9304223919dfa91a143cf593ef8840c2dabde3136c537` |
| Cryptographic verification receipt SHA-256 | `a7bb7bb63a88714b19b8ffbc274f02006e365bc509a966ccbe1859e86c9c18b9` |

The bounded tag extractor verified the twelve-member inventory and equality of
all ten original files and the mechanical selection. Independent GitHub CLI
2.99.0 verification passed with exact repository, source ref, source/signer
commit, release-workflow certificate identity, GitHub OIDC issuer and denial of
self-hosted runners. The verifier executable was compared to its retained
official ZIP, SHA-256
`ea040d0ba03176440d13481caf67f742236c3b38ce1d4442a9da37e9bb98d8f2`.
An initial invocation supplied mutually exclusive identity flags and failed
before verification; the successful receipt uses the exact certificate identity,
not a relaxed identity match or trust bypass.

## Disposable fixture preservation

Only the two fixed local Debian test VMs were changed. Existing Beta.13 state,
including the MBR failure fixture, was preserved before restoring reviewed fresh
generic-kernel baselines. No disk/checkpoint was deleted or failed journal reset.

| Variant | Preserved prior checkpoint | Restored fresh baseline |
| --- | --- | --- |
| MBR/BIOS | `92687990-498e-4b0c-a850-5d4adcd0f9de` | `2f452ad6-59fb-435b-814d-e7308660b476` |
| GPT/UEFI | `e74befb9-a665-4d8f-b5c1-18461e549025` | `8f1acd64-e267-47b3-846c-97e2b90c56cf` |

Each fixture was independently identified by DMI, still without installer state
or quota/project features, with fixed 8-GiB RAM and standard Debian kernel
`6.12.107+deb13-amd64`. A checkpoint-inventory propagation delay stopped the
preparation helper before restoration; the newly created checkpoint IDs were
independently re-read and explicitly supplied before continuing. No bypass of
guest installer checks was involved. Rocky and the duplicate-identity rescue
clone remained off; the operator's VPS was untouched.

## MBR/BIOS exact-archive onboarding passed

The unchanged fixture-restricted onboarding driver used the real archived
installer and genuine `tag-release` origin verification. Retained transport
changes only how unpublished bytes reach the bootstrap; **this is not an
anonymous public-download or README-default test**.

- VM: `1f5e665a-fddd-4cd2-bc55-44255b01963d`;
  DMI: `91d127c9-40e6-ab48-9528-5085e8888729`.
- Operation: `b9c1d575-40fe-42a7-a873-c1adebc8660a`.
- Initial/conversion/final boot IDs:
  `c0e01f07-091c-4609-ac05-f1f4677162d0`,
  `32a012bb-bda0-4d68-9196-2335a42ec801`,
  `7535d678-ca0e-46d4-82ec-92993d92b94f`.
- Original setup redemption, rejection of replay and administrator login passed.
- All seven installed API smoke groups passed: package/account provisioning;
  static/PHP upload/download; domain idempotency; database wizard/inventory;
  document-root backup/restore; FastCGI toggle/HIT/bypass/purge; WAF
  off/detection/blocking before cached responses.
- Same-release rerun did not reboot or reissue setup. The subsequent normal
  reboot preserved the same session, objects, file/backup digests, database
  inventory and static/PHP/WAF/FastCGI behavior. Completed
  `2026-09-26T17:24:21Z`.
- A separate read-only client, executed as the real unprivileged `stackfort`
  service identity, successfully inspected the fresh native panel through the
  **installed candidate agent**, with its normal sandbox. No replacement agent,
  public ACME request or hostname mutation was used. It reported disabled custom
  panel state without recovery, global PHP-FPM and Podman socket as present but
  `inactive/dead`, and the actual managed firewall unit
  `stackfort-firewall.service` as `active/exited`.
- Healthy checkpoint: `f0d14b01-8a21-4992-8b95-6a86a89605b9`.

| Retained receipt/helper | SHA-256 |
| --- | --- |
| `mbr-onboard.json` | `e5ebd76603a04801f43cff680092434be31a66be1c8ac610e0b5b718e2d27d4b` |
| `mbr-panel-services.log` | `bb31017db77b384994dd51f82a73c15da682dc6cae1b2aa8e7b5b03416f14d9c` |
| Unchanged onboarding driver | `e9c256d6e77ca2c5d6e887eecee519ed9cf8f1c1fb668ac00714a41d59cac9fc` |
| Beta.14 invocation wrapper | `11ce7763f035deba45ca0af19c29202f4e6ed27058956a59f0d6435870527314` |
| Read-only installed panel client | `91fdc5629e505cf046877907b065c51d444b8b415eb46681d69b1bef2180d864` |

This smoke does not claim exhaustive isolation, SQL credentials/phpMyAdmin,
actual OCI deployment, full-account/database backup, public Let's Encrypt
issuance, performance benchmarks or removal. Credentials and raw terminal
transcripts were not retained.

## GPT/UEFI blocker: concurrent package activity during arming

VM `55729cf8-11e3-4744-be5b-ddde3824c0e4`, DMI
`a5db08d7-6c4e-47e0-a381-9bd594b2731a`, operation
`4d8c3989-dbf9-47b2-83c2-fb74ce4e9079` stopped with storage
`recovery-required` / `arm-failed`. No one-shot image, build directory or custom
GRUB entry was created; GRUB's next-entry selection is empty and no reboot was
requested. Root remains unconverted. Failed checkpoint:
`ac35a35f-3d2f-405e-8b93-151f30dfc532`.

The observed UTC timeline is:

1. Prerequisite receipt completed at `17:19:53.326791443`.
2. Runtime intent was sealed at `17:19:54.110814376`.
3. `apt-daily-upgrade.service` started at `17:19:55`.
4. The terminal storage failure was recorded at `17:19:57.090901406`.
5. Automatic package upgrades continued until `17:20:43`.

A separately compiled **read-only** inspection of the preserved operation ran
the candidate's private pre-arm checks without invoking Arm, Advance, Resume,
repair or recovery approval. Package locks were available by then, but the
sealed package baseline no longer matched and `/usr/sbin/e2fsck` had changed.
Normal kernel/initrd/GRUB artifacts, fstab, unconverted device identity/geometry,
GRUB selection and initramfs configuration checks passed. Storage state remained
unchanged. Inspection log SHA-256:
`d002706c81f0e91669dcf371e30c572644efb8e8976b0f1cc8bbf44d9c94d721`;
read-only helper SHA-256:
`8d4d8a04c8187015f7bb46036cb99292593d9d1b81f0d5ac84b234f6e5a5a9d8`.

This confirms real package/tool drift and is consistent with the existing
process-lifetime coordination gap between preparation and arming. The onboarding
driver deliberately discarded the original terminal transcript, so it does
**not** establish which failing check returned first at `17:19:57`; later
diagnostics are not presented as a recovered original error. No journal was
cleared, native resume attempted, check weakened or timer disabled to turn this
failed cell into a pass. Improve the handoff/diagnostic coverage before claiming
robust public qualification. Any change to the frozen installer code needs a new
candidate build/tag, not replacement of Beta.14's existing tag or artifacts.

## Remaining qualification and public one-line preparation

1. Resolve the package-coordination blocker without modifying a failed
   conversion journal or rebinding the frozen candidate. Keep the
   [approved test preparation](../../../docs/release-evidence/2026-09-26-beta14-test-qualification.md)
   and [mechanical selection](../../../packaging/releases/promotion/0.1.0-beta.14.json)
   distinct from publication approval.
2. Complete exact-archive qualification on both fresh Debian 13 variants,
   including the fixed UI/native panel boundary,
   security/isolation, quotas, WAF/cache, rootless OCI, failure containment and
   same-target OS-reinstallation removal. Preserve quarantined fixtures and
   retained disks/evidence; do not reuse a consumed conversion journal.
3. Record real results and obtain explicit candidate-specific publication and
   support/removal/upgrade-scope decisions. No current report claims a public
   Let's Encrypt issuance: private-CA tests must remain labelled as such.
4. Validate the candidate's publication files and readiness gates. Publish only
   with approval, then verify immutable assets, anonymous download URLs,
   checksums and exact-tag provenance before changing the default selector.
5. Run the unchanged public command on a fresh disposable target and record its
   end-to-end result. Existing Beta.13 installations are not an upgrade fixture.

Preparation must not create another transient 404 for users: the selector is
changed **after**, never before, matching immutable release assets are verified.
After these tests the live `main` bootstrap was independently fetched and still
selected `0.1.0-beta.13`; its release `397220223` remained immutable and public.
The Beta.14 release API correctly returned 404 because no public release exists.
The ordinary user's command therefore does not select the unpublished candidate.
