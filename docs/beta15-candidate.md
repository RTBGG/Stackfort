# Beta.15 candidate preparation

Status: **published; required host qualification and public asset checks passed**.
The public installer selects Beta.15. The literal README installation, original
setup/login, hosting/API smoke, rerun and normal reboot all passed, as recorded
in the [public installation report](../infra/host-tests/results/2026-09-26-beta15-public-installation.md).
Beta.14's frozen tag and its failed GPT qualification are retained unchanged.

Beta.15 retains the [Beta.14 product fixes](beta14-candidate.md) for ACME account
registration, database-wizard labels, service status and administrator-managed
panel domains with automatic certificates. It also closes the package-lock gap
found during Beta.14 qualification: preparation now holds its original Linux OFD
read locks through the closed-source handoff, sealed child arming and consented
reboot request. Package, boot-artifact and offline-tool drift checks are unchanged.
APT keeps its own checked prerequisite handoff; automatic update timers are not
disabled. This is not a persistent reboot fence or protection against arbitrary
root writers. See [package coordination](native-installer-package-coordination.md).

The qualification driver exposes only a bounded closed-vocabulary failure code,
never the raw terminal transcript or setup credentials. Kernel-level package
guard regressions are included in CI as well as local disposable-host checks.

RTBGG requested on 2026-09-26: continue the fix and then publish for the one-line
installer. This conditionally authorizes a newly built and fully qualified
candidate under the existing fresh-disposable Debian 13 amd64 experimental scope:
no upgrades, production or important data, no independent security review,
community-only GitHub support, and full OS reinstallation as the only removal
method. It does not waive technical gates or turn development tests into release
qualification. Actual source, artifact, provenance and evidence identities must
be recorded before publication.

Required sequence: freeze source; pass exact-source CI/security; build once;
retain original artifact metadata; obtain exact-tag provenance; qualify the
unchanged archive on GPT/UEFI and primary-MBR/BIOS, including installed API,
resources/isolation, rootless OCI, WAF/cache, rerun/reboot, failure quarantine and
same-target OS removal; validate the explicit fresh-only publication exception;
publish immutable assets; verify all anonymous public downloads; advance and test
the public one-line selector. Never mix binaries into existing installations,
reset failed conversion journals or replace/move existing release identities.

## Frozen candidate and retained build

- Source: `d93c4bfb9fb527e80a648eeb8482b159d0076b29` (PR #19).
- Exact-source CI: [36260359106](https://github.com/RTBGG/Stackfort/actions/runs/36260359106), successful attempt 1.
- Exact-source security: [36260378988](https://github.com/RTBGG/Stackfort/actions/runs/36260378988), successful attempt 1.
- Build: [36260323807](https://github.com/RTBGG/Stackfort/actions/runs/36260323807), successful attempt 1.
- Retained original artifact: `10911968743`, `stackfort-0.1.0-beta.15`.
- Original ZIP SHA-256: `309ae1aa1cb4711eaa0f39c4a4730df871e0d3d6a6a280427fceb80144ea620b`.
- Runtime archive SHA-256: `b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`.
- Standalone/archived installer SHA-256: `0050f649a384e3daedcab2d27a1cfdb123167f63f44236e1abbf7575e8eb3fff`.
- `SHA256SUMS` SHA-256: `698dfb44e4f022d55c074515dbcfd33455b2ebd06d3f4d1688ce882b47539a4c`.

The original ten-file artifact passed bounded extraction, checksum, source and
installer-equality checks. The [mechanical selection](../packaging/releases/promotion/0.1.0-beta.15.json)
only permits exact-tag provenance for these same bytes. The
[host qualification report](../infra/host-tests/results/2026-09-26-beta15-candidate-qualification.md)
and [same-target removal report](../infra/host-tests/results/2026-09-26-beta15-os-removal.md)
record completed checks. The [publication workflow](https://github.com/RTBGG/Stackfort/actions/runs/36262976996)
passed all remaining gates and published the unchanged assets. All 14 anonymous
downloads passed size/hash checks; the 12 original tag-qualified files are identical.
