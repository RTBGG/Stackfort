# Beta.15 candidate preparation

Status: **unpublished; qualification in progress**. The public installer remains
on Beta.13 until immutable, qualified Beta.15 assets exist and have been verified.
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
