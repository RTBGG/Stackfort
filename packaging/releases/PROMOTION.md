# Exact candidate promotion and tag qualification

The release workflow builds native components and the release archive only on
manual `workflow_dispatch`. Tag runs **do not rebuild the candidate**. Floating
native build images/toolchain packages and native DEB/RPM metadata make a later
full rebuild an inappropriate source of the previously tested bytes, even though
the top-level TAR/GZIP timestamps are normalized. A different rebuild is a new
candidate and requires its own evidence.

## Two phases, no public-release prerequisite

1. Finalize the candidate source/version and run the manual release workflow.
   Its successful run uploads `stackfort-<version>` containing the complete
   archive, standalone installer, passive DEB/RPM carriers and sidecars,
   `SHA256SUMS`, and SPDX SBOM. Keep that exact artifact available.
2. Record its actual GitHub metadata in canonical
   `packaging/releases/promotion/<version>.json` on `main`, after the fixed
   candidate commit. This **mechanical selection is not publication approval**:
   it has exactly `schemaVersion: 1`, `kind: "candidate-promotion"`, and
   `candidate` using the [readiness candidate fields](README.md#evidence-object-fields).
   Do not invent run IDs, attempts, hashes or source commit values.
3. Tag the fixed candidate source. The promotion job pins one descendant `main`
   commit, validates the selection and live GitHub source run/artifact metadata,
   downloads that artifact's ZIP and verifies its SHA-256 before extraction.
   It requires the same repository/source repository, release workflow, exact
   commit and successful latest `workflow_dispatch` attempt, plus the exact
   unique, nonexpired artifact ID/name/digest. Missing or deleted artifacts fail;
   there is no fallback rebuild or adoption of similarly named artifacts.
4. The exact tag job issues genuine `refs/tags/v<version>` provenance for the
   retained payloads and uploads the **unpublished** artifact
   `stackfort-tag-candidate-<version>-attempt-<N>`. It includes
   `build-attestation.jsonl`. This occurs **before publication gates**, so the
   first run can intentionally stop for missing readiness evidence without
   needing a public GitHub Release first.
5. Download that exact archive and tag attestation for disposable-host/local
   fixture qualification. Use the real archived installer with the ordinary
   strict `tag-release` origin policy. No public branch/lab origin exception
   is introduced. Complete current technical tests and candidate-specific review
   or experimental disclosures/approvals, then commit real readiness and upgrade
   evidence to `main` as required by the [readiness contract](README.md).
6. Rerun the blocked tag promotion job. It downloads the same pinned original
   artifact, generates a separately named tag-qualification artifact for this
   attempt, and evaluates all current publication gates. The original archive,
   installer, carriers and SBOM remain byte-identical. A changed promotion
   identity cannot pass old candidate evidence.
7. Only after readiness and upgrade gates succeed does the job issue final
   provenance for all publication files, create the immutable release with clear
   test-only/community-support warnings, and verify its immutable channel policy.
   The earlier tag bundle remains valid provenance for the unchanged archive;
   generating another attestation does not modify its bytes.

No production release, tested-host claim, independent approval or review waiver
is created merely by recording a promotion selection or creating a tag artifact.
An experimental-beta policy authorization does not waive technical gates or
candidate-specific RTBGG publication approval. A changed source commit requires
a new candidate; do not move an existing tag to substitute different code.

## Extraction and retention boundaries

The extractor uses Python 3.11+ standard-library ZIP/TAR readers, checks the
complete fixed flat regular-file inventory, ZIP/member size bounds and CRCs,
every required release checksum, the candidate archive SHA, archive VERSION and
COMMIT, and equality of the standalone versus archived installer. It never calls
archive extraction with archive-selected paths. Symlinks, special files,
directories, traversal, extra/missing/duplicate members, changed checksums and
an existing output directory are rejected. Partial temporary output is never
copied into `dist` after an extraction failure.

Current bounds are 2 GiB per downloaded artifact/aggregate extracted data, 2 GiB
of expanded TAR member data, at most 100,000 TAR members, and
512 MiB per member. JSON metadata is bounded to 1 MiB and the SPDX file to 16 MiB.
Tag promotion requires a fresh checkout without a preexisting `dist` directory.

Build and tag-qualification artifacts currently request **30-day retention**.
Qualification and publication must finish while the pinned original build
artifact still exists and is downloadable; the source run must not be rerun in
the meantime, because its attempt is pinned. Retain downloaded copies and test
evidence as appropriate, but a local copy cannot substitute for a missing
verified GitHub artifact in this workflow. If retention expires, perform a new
manual build and qualify the newly selected exact artifact. To allow a longer
qualification window, review/configure retention before creating the candidate;
do not assume an expired artifact can be restored or silently relabeled.

The workflow's pre-gate tag artifacts resolve the provenance/qualification
dependency without publishing unqualified assets or weakening native origin
verification. No promotion/readiness records are supplied by the repository
until actual artifact metadata and qualifying evidence exist.
