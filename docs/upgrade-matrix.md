# Release upgrade qualification

Every supported prior release must upgrade directly to a candidate on Debian 13,
Ubuntu 26.04, and Rocky Linux 10 (`amd64`). Each pair requires success, rollback
after a final health failure, and recovery after the updater process disappears
following database migration. A missing, duplicate, failed, or stale result
blocks publication.

## Support catalog

[`packaging/upgrades/supported-releases.json`](../packaging/upgrades/supported-releases.json)
lists every published predecessor, its immutable Linux amd64 archive SHA-256,
and `supported` or `retired` status. Retirement requires an explicit documented
reason. The default is to retain published predecessors as supported; no support
window is silently inferred from age, release count, or semantic version.

Before building the next candidate, add the last published release to this
catalog. Include beta predecessors when promoting to stable, and keep earlier
supported releases so skipped-version upgrades are tested. The publication
gate compares the catalog with **all pages** of the GitHub release inventory;
omitting a release does not reduce the required matrix.

There are currently no published Stackfort releases. The checked-in catalog is
therefore empty. Local `0.1.0-beta.1` / `0.1.0-beta.2` rehearsal builds are not
public releases and do not establish a support commitment.

## Candidate procedure

1. Finalize and commit the candidate source, including the support catalog.
   Run the release workflow manually at that commit with the candidate version.
   This builds and attests the candidate without publishing a GitHub Release.
2. Download its exact amd64 archive and each catalog predecessor's immutable
   release archive. Retain their checksums. Fetch the corresponding prior tags.
3. Build one qualification driver per predecessor:

   ```console
   PRIOR_VERSION=0.1.0-beta.1 bash scripts/build-upgrade-driver.sh
   ```

   The driver uses the prior tag's production Go source and version, with the
   current integration test added. It compares its commit with the predecessor
   archive's `COMMIT` before installation. It is not included in release payloads.
4. Run the complete matrix on the three dedicated Hyper-V fixtures:

   ```powershell
   .\infra\host-tests\Test-StackfortUpgradeMatrixHyperV.ps1 `
     -TargetVersion 0.1.0-beta.2 -TargetArchive <candidate.tar.gz> `
     -PriorArchiveDirectory <downloaded-archives> `
     -PriorDriverDirectory infra/host-tests/work/upgrade-drivers `
     -ResetDisposableCheckpoint
   ```

   **This explicitly restores each dedicated VM's clean installer checkpoint.**
   The wrapper requires the VM to be off, verifies its distribution, checks
   transferred archive/driver hashes, and returns it to off after each pair.
   Logs and reports remain under the ignored `infra/host-tests/work/` directory.
   Release-candidate runs also fetch and verify the predecessor using the real
   GitHub stager and installed GitHub CLI before accepting test results.
5. Review the generated evidence and commit it to `main` as
   `packaging/upgrades/evidence/<candidate-version>.json`. Its `kind` must be
   `release-candidate`. Keep the candidate source commit from step 1 unchanged.
6. Tag **that tested candidate commit**, not the later evidence commit. The tag
   workflow [promotes the exact retained artifact](../packaging/releases/PROMOTION.md)
   without rebuilding, uploads genuine tag-provenance qualification material
   before publication gates, and checks every source/target digest and matrix
   cell before publication. For native qualification, obtain that unpublished
   tag material before the host tests, then commit evidence and rerun the blocked
   promotion job. A changed build requires fresh qualification.

The first release has no predecessors; the gate permits its empty matrix only
after confirming the empty published inventory. Its clean-host installation
qualification remains necessary. Manual candidate builds deliberately do not
require upgrade evidence: they supply the artifacts that qualification needs.
The [2026-09-12 gate review](../infra/host-tests/results/2026-09-12-release-readiness-validation.md)
confirmed this first-release path and its missing/null-inventory negative tests.
Unpublished tag artifacts do not create predecessors. The current upgrade
matrix still hardcodes all three OS profiles per supported predecessor; review
release-specific support scope before the next candidate rather than silently
claiming the Debian-only first native beta was qualified on Ubuntu/Rocky.

## What the host matrix checks

The real installer prepares the prior release, followed by the prior release's
real updater runner. There are no mocked packages, systemd services,
configuration writes, SQLite backups, migrations, or health checks. Failure
injection is confined to the integration driver. The interruption case runs a
separate updater subprocess that exits after a durable migration journal, then
starts a fresh runner and verifies rollback before an explicit retry.

Each pair checks installed payload/package/configuration health, live panel and
service health, durable terminal journals, preserved panel metadata, SQLite
integrity, and unchanged encryption/TLS/phpMyAdmin secrets. Recovery cases run
before the final successful upgrade. The suite is additive to tenant-isolation,
WAF/cache, OCI, and clean-installer qualification; it is not their replacement.

## Continuous migration coverage and rehearsals

`TestUpgradeFromEveryHistoricalSchema` upgrades every embedded schema prefix to
the current schema, including already-current reopen. It preserves Unicode
metadata, identity/credential/role relationships, and historical migration
checksums; verifies foreign keys and integrity; and verifies that the backup
is still readable under the prior migration list. This runs in ordinary CI,
including Linux race tests, and automatically grows with new migrations.

For an unpublished local rehearsal, first build a development archive, then:

```console
BASELINE_COMMIT=<exact-40-character-prior-commit> bash scripts/build-upgrade-rehearsal.sh
```

This produces two clearly unpublished archives with complete existing native
component packages, plus a prior-source driver. Run the per-VM wrapper with
`-Kind rehearsal`, explicit archive/version/driver inputs, and
`-ResetDisposableCheckpoint`. Rehearsal evidence is always rejected by the
publication gate. It validates the machinery before public predecessors exist;
it cannot prove compatibility between releases that have not yet been published.
