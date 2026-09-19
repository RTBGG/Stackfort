# Beta.8 candidate qualification — 2026-09-19

Status: **pre-selection checks; not publishable**.

The [beta.7 installation failure](2026-09-19-beta7-candidate-qualification.md)
identified missing Vinyl runtime compiler/header dependencies. The next source
declares those dependencies and adds three compiler-free container checks.

## Rejected pre-selection source

Source `a27492d25d04028e7d2a0898e4d3e9b822d3df3c` was submitted to
[build run 35440648878](https://github.com/RTBGG/Stackfort/actions/runs/35440648878).
All three new Vinyl minimal-runtime jobs passed: Debian 13, Ubuntu 26.04 and
Rocky Linux 10. These checks install the exact package into a fresh container
without a compiler, then compile its managed VCL. They are not complete host
installation or service qualification.

This source was **not selected or tagged**. Its
[CI run](https://github.com/RTBGG/Stackfort/actions/runs/35440589715) failed because
the Linux-only Rocky transaction test still expected the former dependency
command. Windows tests do not exercise Linux-tagged files. The test expectation
now includes the required compiler and C headers; the fixed three Vinyl tests
were cross-compiled and executed successfully on Linux using only mocked
package commands and temporary test fixtures, without repairing beta.7.

The [Security run](https://github.com/RTBGG/Stackfort/actions/runs/35440589733)
also flagged the report's public executable SHA-256 as a generic API key.
The value was independently verified against the retained beta.7 archive, not
obtained from a credential store. The historical exception is limited to that
exact commit/file/rule/line fingerprint. The current label avoids the ambiguous
wording; no rule or report directory is excluded from scanning.

## Second pre-selection source: checks passed, documentation stale

Source `8035d8044029355dc3a8b6f1799c021f11b0f815` passed
[CI](https://github.com/RTBGG/Stackfort/actions/runs/35441008430),
[Security](https://github.com/RTBGG/Stackfort/actions/runs/35441008422) and
[original build 35441057414](https://github.com/RTBGG/Stackfort/actions/runs/35441057414),
all on attempt 1. The full seven-artifact inventory and aggregate artifact
`10583299321` (313,140,612 bytes) passed download/integrity inspection.
ZIP SHA-256: `7bc78871d86e7a3f537a1a3b0d897fe006f6a05d3aeae6beff73322a99be2886`;
TAR SHA-256: `8174012320606e9319699113236d1c702ff4de83680087f0a4cf761e8d974988`.
The strict extractor verified all ten files, checksums, source/version, SBOM
and archived/standalone installer equality. The corrected Vinyl revision and
independent WAF-member digests were also inspected.

Before committing a selection or creating a tag, review found stale beta.6
version labels in `SECURITY.md` and the installer command README. This would
contradict exact-candidate support disclosure. The uncommitted proposed
selection was retained with this run's ignored diagnostic files, not committed
as a promotion. No tag or native installation was started from this source.
The labels are corrected and a documentation regression now requires current
support/quick-start versions to agree with the canonical bootstrap default;
synthetic stale, mixed, missing and ambiguous versions are rejected.

A new original build from the corrected source must pass CI and Security before
selection. Neither superseded run will be rerun, substituted into a selection,
or treated as a qualified candidate. No beta.8 installation or publication
result is claimed here.
