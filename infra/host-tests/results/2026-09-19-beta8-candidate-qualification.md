# Beta.8 candidate qualification — 2026-09-19

Status: **corrected retained candidate selected; native qualification pending;
not publishable**.

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

## Corrected retained candidate

Frozen source: `e217f9fbe6f62491f2142391af8799c5231255a1`.
[Original build 35441790427](https://github.com/RTBGG/Stackfort/actions/runs/35441790427),
[CI 35441754938](https://github.com/RTBGG/Stackfort/actions/runs/35441754938) and
[Security 35441754895](https://github.com/RTBGG/Stackfort/actions/runs/35441754895)
all passed on attempt 1 before the selection commit. The build includes all six
native-package jobs and all three compiler-free Vinyl runtime checks. Slow Rocky
repository downloads delayed its jobs but did not require a retry or bypass.

- Aggregate artifact: `10583803548`, 313,145,063 bytes.
- ZIP SHA-256: `fdf0bc28a3058dbe930d84082c784c0a6d116d35db45829088401c41aa36d903`.
- TAR SHA-256: `b5a7f7a39c3bc6f132d9aafc46d1a024a781aad4e8aa8955b4089aa3326a7da0`.
- Installer SHA-256: `48003e5bc856608ea7bdfe010440d9987f71c4af18130b33515add611e60d14d`.
- Control-plane executable SHA-256: `bff777e41819c2e08fa675db721f973798353a154b40d40963a735af4f55bc89`.
- Agent SHA-256: `905a8c50e1c51231d91b47206757bb7006662175abf955e56169703dc4f93dd5`.
- Bootstrap SHA-256: `6bd01feab35e2306dd404467c4264cbe590ced22060ebc487b45761bd6deecae`.

Mechanical selection validation and strict extraction passed against the
complete seven-artifact API inventory. All ten payload files, checksums, carrier
sidecars, SPDX metadata and source/version bindings were verified. The archived
and standalone installer are identical. A separate read-only inspection checked
the Debian Vinyl revision `9.0.1-2sf1` and the WAF package/module/library/inventory
digests against the authenticated TAR and its component manifest.

The selection is not publication approval. No beta.8 tag-qualified installation,
setup redemption, live product test or release is claimed by these build checks.

## Prepared fresh target

The beta.7 failed state is retained with checkpoint
`bd6eb82c-e64d-4809-bc2b-1460cbfbe4a1`. The exact disposable VM was shut down
gracefully, its complete old disk chain recorded, and both system/seed attachments
replaced with independently prepared vendor-image disks. No disk or checkpoint
was deleted, restored or copied from the installed host. The rescue clone stays
off. This fresh baseline preparation is not post-install removal qualification.

The vendor Debian image remains SHA-256
`85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`,
authenticated using official HTTPS and published SHA512SUMS. The VM-bound public
KVP report authenticated the new SSH key before replacing its strict pin.
Cloud-init completed without errors. Secure Boot is on; the 50 GiB root is plain
GPT/ext4 with 256-byte inodes, approximately 47 GiB free and 3.24 million free
inodes, without project-quota features or Stackfort state/configuration.

Initial boot: `b1c8349b-386a-4ef3-aaac-76c12130d4ef`.
Fresh checkpoint: `63424f12-be04-483c-a58e-01003e09c886`
(`native-beta8-vendor-fresh-20260919`). It remains unchanged while the exact
candidate is built and validated.
