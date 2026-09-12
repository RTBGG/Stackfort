# Retained candidate extraction rehearsal — 2026-09-12

Result: the actual GitHub build artifact was downloaded, digest-checked and
successfully processed by the corrected strict extractor. **This is not native
installation qualification or publication approval.** No tag or release was
created for these rehearsal bytes.

## Actual retained build

- Repository: `RTBGG/Stackfort`.
- Manual release build: `34682742939`, attempt `1`, completed successfully.
- Source commit: `74feaf628e39e8a809bc9a8899fc0af0bb127ff0`.
- Version: `0.1.0-beta.4`.
- Artifact ID: `10294566959`, name `stackfort-0.1.0-beta.4`.
- Downloaded ZIP: 313,036,860 bytes;
  SHA-256 `a648e987ae2846b8896a7d3c97614906098c08129ee078ee9ee92a30987c104a`.
- Release archive: 117,565,787 bytes;
  SHA-256 `0f43ced20622413096852e85ee97f4eef4ee6ecff4777edebacf95683387e91b`.
- Archived/standalone installer:
  SHA-256 `af741779a6e7d510a1739f7d8a8b0ebdfb5bd14347a3bbbb53106c684030bec3`.

GitHub's actual successful-run and complete seven-artifact API inventories were
passed to `promote-release-candidate.mjs --mode verify`. Its output explicitly
has `publicationAuthorized: false`. The resulting receipt and downloaded ZIP
were then passed to `extract-release-candidate.py`.

## Mismatch found and corrected

The real build produces ten flat files, including two native-carrier `.sha256`
sidecars. The original synthetic extractor fixture represented only eight files
and would reject a genuine build. The inventory now requires both sidecars and
checks their exact canonical digest/filename/newline against the actual carriers.
Missing, wrong-target, altered and appended sidecars are rejected by regression
cases. The fixed extractor successfully checked every real payload checksum,
SPDX metadata, archive version/commit and standalone/archived installer equality.

`python scripts/extract-release-candidate.test.py`: four test methods passed,
including their negative-case subtests. The duplicate-ZIP-member test deliberately
produces Python's duplicate-name warning.

The approved experimental removal-policy change and this extraction correction
require a new source commit and build. These rehearsal bytes must not substitute
for that candidate's installer, tag provenance, host tests or release evidence.
The local download and API receipts remain under ignored
`infra/host-tests/work/candidates/34682742939/`.

## Updated gate regression

The new experimental removal contract and corrected extractor were also tested
on Linux using portable, checksum-verified Node 24.19.0 and jq 1.8.1, without
installing packages or activating Stackfort on the fresh VM:

- 160 Node tests passed, zero failures/skips. This includes the real Bash
  promotion wrapper and both reviewed/experimental publication wrappers.
- Four Python extractor test methods passed on Linux as well as Windows.
- `go test ./cmd/stackfort-installer` passed after adding the removal warning
  to the real pre-consent terminal display.
- Pinned actionlint 1.7.12 passed with its external ShellCheck integration
  disabled for this Windows check; no shell program changed in this revision.

Test-source TAR SHA-256:
`d5a85e2fcb550912d7a0dac77299b3e02e5e9f21cef8324ba79fc6b815e21bbd`.
The local log is `infra/host-tests/work/native-beta4-release-gates-final.log`.
These synthetic gate tests do not perform removal or authorize publication.
