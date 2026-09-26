# Beta.14 candidate qualification

Status: **build and extraction verified; exact-archive host qualification is
pending**. This is not public-release approval or a passing installation report.
The public one-line installer remains on Beta.13.

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

## Remaining qualification and public one-line preparation

1. Use the [approved test preparation](../../../docs/release-evidence/2026-09-26-beta14-test-qualification.md)
   and [mechanical selection](../../../packaging/releases/promotion/0.1.0-beta.14.json)
   to obtain genuine tag provenance for these unchanged bytes. Missing readiness
   evidence must still stop the tag workflow before publication.
2. Install and qualify the exact tag-attested archive on fresh Debian 13
   GPT/UEFI and MBR/BIOS fixtures, including the fixed UI/native panel boundary,
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
