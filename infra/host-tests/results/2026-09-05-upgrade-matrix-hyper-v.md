# Upgrade matrix rehearsal — Hyper-V — 2026-09-05

The complete nine-cell local rehearsal passed with the same two archives and
prior-source driver on all three supported amd64 distributions. Each guest was
restored from `stackfort-installer-ready-20260824` and returned to `Off` afterward.

**This is an unpublished rehearsal, not a published-release upgrade claim.**
GitHub's release inventory was empty at qualification time. Both binary sets use
the corrected production source at `7ccb008efc4906b306ca49c8dbd9287c4220b643`,
with distinct injected versions and deliberately changed rehearsal web assets.
Native WAF/Vinyl package versions are the same in both archives. Historical
database schema transitions are covered separately by the 28-prefix CI suite.

## Matrix

| Guest | Successful upgrade | Final-health rollback | Process-loss recovery |
| --- | --- | --- | --- |
| Debian 13 | passed | passed | passed |
| Ubuntu 26.04 | passed | passed | passed |
| Rocky Linux 10 | passed | passed | passed |

The source is local `0.1.0-beta.1`; the target is local `0.1.0-beta.2`. Neither
version is a Git tag or published release. Archive SHA-256 values:

```text
from: 5d2616b59676b32fd461991b70d89cc411087b9eb3f4bb8798298081d41d6f4d
to:   f0dd5cec7117bd701d0066050aa7fc2667e6630ca1c4ff414400b267f8e4c72f
```

[Machine-readable evidence](2026-09-05-upgrade-matrix-rehearsal.json) retains
`kind: rehearsal`, which the publication gate rejects.

## Exercised behavior

- Real clean installation followed by the prior source's production updater
  runner, full native-package verification, binary replacement, configuration,
  database command, service restart, and installation health checks.
- Exact replacement of the changed web marker and removal/addition of the
  version-specific web assets in both update and rollback directions.
- An injected failure **at the final health stage**, after all earlier stages
  completed, followed by exact prior payload and database recovery.
- A separate updater subprocess that exits after writing the completed
  migration journal, followed by a fresh runner that recovers before retry.
- Durable final journals, preserved Unicode panel metadata, SQLite integrity,
  and unchanged master-key, panel TLS, and phpMyAdmin secret bytes.
- Distribution security configuration, including enforcing SELinux on Rocky,
  verified through the production installation-health gates.

The standalone root-owned payload corpus also passed on Debian: fresh installer
conflict refusal, allowed forward/reverse replacement, mixed-state recovery,
obsolete asset removal, and refusal of unrecognized or symlinked content. The
Rocky dependency/rollback command tests passed with the same Linux test binary.
All 28 historical schema-prefix migrations passed on Windows and native Linux;
ordinary CI additionally runs them with the Go race detector.

## Issues found and fixed

1. `systemctl show` cannot inspect the raw `stackfort-update@.service` template
   as a loaded unit. The installer now inspects a fixed inactive instance.
2. The previous updater reused the fresh-install conflict policy for binaries
   and web files. It could not replace a changed executable. A separately
   authorized, exact-source-pair payload transition now handles both directions
   while preserving unknown-content and symlink refusal.
3. Rocky's exact RPM activation did not install Vinyl's EPEL `jemalloc` runtime
   prerequisite. It now resolves this prerequisite before the exact RPM step.

The negative health test also asserts that the intended failure stage was
reached; an earlier failure followed by rollback cannot count as a pass.

## Reproduction and scope

Use [the upgrade-matrix operator guide](../../../docs/upgrade-matrix.md) and
`scripts/build-upgrade-rehearsal.sh` with the recorded source commit. The actual
run used `Test-StackfortUpgradeMatrixHyperV.ps1` with an explicit one-predecessor
local rehearsal catalog and `-Kind rehearsal -ResetDisposableCheckpoint`.

This qualifies the matrix machinery and current real host transitions. Once a
public predecessor exists, the release-candidate procedure requires its exact
published archive and verified provenance, plus the exact candidate archive.
The first public release remains subject to the separate clean-installer,
security, operations-documentation, and critical-language workflow gates.
