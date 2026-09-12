# Maintainer release checklist

This checklist does not publish a release. Stackfort remains pre-beta, and the
first public release still needs its exact release scope under the agreed
community-only support policy and the
remaining [Phase 6](roadmap.md#phase-6-installer-updater-and-public-beta) exit
review. Do not interpret development/rehearsal version numbers as published
releases or claim independent review from automated tests alone.

## Before the first beta

- [ ] Complete the product-spec success-criteria and remaining roadmap review;
  record accepted gaps and exclusions in the beta notes.
- [ ] Review critical English **and** German workflows end-to-end: setup,
  authentication/recovery, account/domain/database/file/backup/application
  actions, destructive confirmations, errors, and updates. Retain accessibility
  and narrow-screen evidence as well as catalog/type checks.
  MFA setup/replacement/removal and recovery-code display now have EN/DE browser
  flows and automated coverage; include real authenticator and narrow-screen
  checks in the final review.
- [ ] Qualify removal for the selected release class on every supported profile:
  `reviewed-release` requires active-installation uninstall; the expressly
  authorized disposable `experimental-beta` instead requires actual complete OS
  reinstallation of the same target after testing the active exact candidate.
  Follow the [experimental removal contract](experimental-beta-removal.md),
  retain media authentication and clean post-install observations, and prominently
  disclose destruction of all server data/configuration/services with no in-place
  uninstaller. Passive carrier removal and snapshot rollback cannot satisfy this
  requirement. No candidate-specific removal pass is recorded by this checklist.
- [ ] Complete the independent security review required by the
  [security model](security.md#6-security-release-gates), or use the explicitly
  authorized experimental-beta contract with accurate missing-review disclosure
  and candidate-specific RTBGG approval; CI is not an independent audit. A voluntary/community
  review may satisfy this requirement if it is genuinely independent and its
  scope/findings are recorded. No paid audit is currently funded and no
  independent review is complete. The 2026-09-12 experimental policy authorization
  permits only fresh disposable test servers, not production or important data;
  it does not satisfy any technical test or approve a particular candidate.
- [ ] Publish exact beta versions, upgrade expectations and deployment limits
  in [SECURITY.md](../SECURITY.md) and release notes under community-only GitHub
  support by RTBGG and possible future contributors. There is no guaranteed
  response, fix, support period/end date, LTS or backport promise.
- [ ] Verify private vulnerability reporting and the report link still work;
  confirm who will receive and triage reports. It was enabled with owner
  approval on 2026-09-06.
- [ ] Review the [operations guide](operations.md), recovery exclusions,
  bootstrap-certificate boundary, and [benchmark caveats](benchmarks.md).
  No database/full-host backup or general production-readiness claim is implied.
  Include FastCGI's shared soft cache bounds and whole-domain purge limitation.
  Explicitly disclose the native shared-root disk/inode exhaustion risk and
  possible whole-server unavailability: initial free-space/inode checks are not
  durable OS reserves. Aggregate/unlimited account admission and platform usage
  bounds remain production blockers, not completed experimental protections.

## For every candidate

1. Record one immutable candidate commit and canonical `X.Y.Z` or
   `X.Y.Z-beta.N` version. Review changes and release notes, including downtime,
   package/ABI changes, migrations, known limitations, and recovery guidance.
2. Run CI and security gates, build the native WAF/Vinyl matrices and passive
   DEB/RPM carriers, and require reproducible archives, an SBOM, and provenance.
   Manual release-workflow dispatch produces a candidate, not a publication.
3. Qualify the **exact candidate archive** on each advertised installation
   profile. The first native fresh-default beta is Debian 13 amd64 only;
   prepared-quota installation on Debian/Ubuntu/Rocky is a separate route.
   Retain first-install/no-op, host-security,
   service, tenant-isolation, WAF/cache, and OCI evidence appropriate to the
   release. Historical result links alone do not qualify a changed archive.
   Include the class-specific removal evidence and explicit candidate-specific
   support/publication acknowledgement of destructive reprovisioning when used.
   The [2026-09-12 private-image/kernel cycle](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md)
   passed internally on beta.3 platform files with a separately pinned installer;
   it is not final beta.4 archive/public-onboarding evidence.
   Retain the real positive fresh-host firewall eligibility check as well as
   listener/reload/failure/reboot tests; passing isolated rule tests alone does
   not qualify the complete installer path.
4. Follow the complete [upgrade-matrix procedure](upgrade-matrix.md). Keep all
   published predecessors in the catalog with explicit support/retirement;
   verify all pages of the release inventory and every required scenario.
   Commit reviewed artifact-bound evidence to `main` separately from the fixed
   tested source commit. Rehearsal evidence cannot satisfy publication.
5. Run the offline documentation check and manually review public entry points,
   security reporting, version examples, installation and recovery commands.
   Local link validation does not test remote availability or execute commands.

## Publish only after explicit approval

- Verify repository release immutability is enabled. Follow the
  [channel/publication contract](update-channels-and-checks.md).
- Satisfy the [machine-validated readiness contract](../packaging/releases/README.md)
  using real artifact-bound installation evidence, live CI/security results and
  recorded human decisions. Its default is closed, not an audit waiver.
  The [Linux validator/promotion suites](../infra/host-tests/results/2026-09-12-release-readiness-validation.md)
  passed with 110 Node and four Python tests; synthetic machinery tests are not
  the candidate evidence or approval required to open that gate.
- Follow [exact artifact promotion](../packaging/releases/PROMOTION.md): tag the
  fixed candidate source, obtain unpublished genuine tag provenance, qualify it,
  then commit evidence and rerun the blocked promotion job. Never rebuild or
  relabel old evidence to substitute different payload bytes at publication.
- Confirm the workflow passed the full inventory/evidence gate and published
  the expected immutable release with all digested assets, SBOM and provenance.
  Beta releases must remain prereleases and must not replace latest stable.
- Verify public download/checksum and discovery behavior on a disposable host.
  First-beta installation must select the exact beta version if no stable
  release exists. Do not weaken verification to get a release advertised.
- Record the result and maintain the support catalog for the next candidate.

No release/tag is created by marking a roadmap implementation item complete.
