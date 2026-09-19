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

A new original build from the corrected source must pass CI and Security before
selection. The rejected run will not be rerun, substituted into a selection, or
treated as a qualified candidate. No beta.8 installation or publication result
is claimed here.
