# Maintainer release checklist

This checklist does not publish a release. Stackfort remains pre-beta, and the
first public release still needs an explicit support-window decision and the
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
  In particular, MFA login exists but browser MFA enrollment/removal is not
  implemented yet; the authenticated API alone does not close that workflow.
- [ ] Implement and qualify active-installation uninstall on every supported
  distribution, as required by the product specification. Passive carrier
  removal tests do not cover removal of the active platform.
- [ ] Complete the independent security review required by the
  [security model](security.md#6-security-release-gates), or explicitly keep
  publication blocked; CI is not an independent audit.
- [ ] Decide and publish exact supported beta versions, support end dates,
  upgrade expectations, and deployment limits in [SECURITY.md](../SECURITY.md)
  and release notes. Do not invent an LTS or backport promise.
- [ ] Verify private vulnerability reporting and the report link still work;
  confirm who will receive and triage reports. It was enabled with owner
  approval on 2026-09-06.
- [ ] Review the [operations guide](operations.md), recovery exclusions,
  bootstrap-certificate boundary, and [benchmark caveats](benchmarks.md).
  No database/full-host backup or FastCGI production preset is implied.

## For every candidate

1. Record one immutable candidate commit and canonical `X.Y.Z` or
   `X.Y.Z-beta.N` version. Review changes and release notes, including downtime,
   package/ABI changes, migrations, known limitations, and recovery guidance.
2. Run CI and security gates, build the native WAF/Vinyl matrices and passive
   DEB/RPM carriers, and require reproducible archives, an SBOM, and provenance.
   Manual release-workflow dispatch produces a candidate, not a publication.
3. Qualify the **exact candidate archive** on clean Debian 13, Ubuntu 26.04,
   and Rocky Linux 10 amd64 hosts. Retain first-install/no-op, host-security,
   service, tenant-isolation, WAF/cache, and OCI evidence appropriate to the
   release. Historical result links alone do not qualify a changed archive.
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
- Tag the **tested candidate source commit**, not the later evidence commit.
  If any input changes, rebuild and requalify; do not relabel previous evidence.
- Confirm the workflow passed the full inventory/evidence gate and published
  the expected immutable release with all digested assets, SBOM and provenance.
  Beta releases must remain prereleases and must not replace latest stable.
- Verify public download/checksum and discovery behavior on a disposable host.
  First-beta installation must select the exact beta version if no stable
  release exists. Do not weaken verification to get a release advertised.
- Record the result and maintain the support catalog for the next candidate.

No release/tag is created by marking a roadmap implementation item complete.
