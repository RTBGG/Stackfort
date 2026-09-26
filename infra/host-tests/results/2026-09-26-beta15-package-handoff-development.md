# Beta.15 package-handoff development checks

Date: 2026-09-26. **Development regression evidence, not release qualification.**
The new candidate has not yet been built, tagged, installed or published. The
public bootstrap remains pinned to Beta.13. The previous Beta.14 GPT failure and
MBR success remain preserved in their separate checkpoints and report.

## Change and boundary review

`WithNativeOnboardingSetup` uses a synchronous continuation under the original
preparation OFD package guard. The coordinator validates the sealed manifest,
closes the shared source lock successfully, and only then invokes the existing
CLI validation, sealed child arm and acknowledged reboot request. The child still
reopens/verifies canonical source/runtime state and acquires compatible read
locks. Preparation's deferred closure runs on success, error and panic; no lock
descriptor, setup secret, override flag or injected executor crosses into the
child. Legacy laboratory prepare-only APIs still close their locks before return.

The implementation does not disable update timers, remove lock files, bypass
APT locking, skip package/boot/tool hashes, retry consumed conversions or create
a persistent reboot fence. Own prerequisite APT transactions retain their existing
guard release/reacquire and exact-delta checks. Process death or the interval
after reboot request until shutdown remains covered by the existing fail-closed
boot/admission checks, not by a claim of durable package exclusion.

## Completed development tests

- Windows: `go test ./...`, `go vet ./...`, formatting and `git diff --check`.
- Cross-compiled Linux tests on the already qualified disposable MBR Debian 13
  fixture, without modifying its installed candidate binaries or journals:
  - root mount-namespace package guard suite: all 24 scenarios passed;
  - real APT and dpkg clients rejected while the read guard was held;
  - four new sealed-handoff cases: success, error, cancellation and panic;
  - compatible child guard acquisition/exit left the parent locks held; POSIX
    writers were rejected on both files during the continuation and admitted
    after cleanup;
  - seven coordinator handoff cases checked source-lock closure before callback,
    manifest rejection, no continuation after preparation/close failure and
    cleanup on continuation error/cancellation/panic;
  - existing Linux onboarding preparation/setup and CLI `TestOnboard*` checks
    passed, including refusal, secret redaction and no reboot after arm failure.
- Local PowerShell driver contract passed. Failure reporting now returns only
  closed-vocabulary codes such as `arm-package-drift`, never raw diagnostic or
  transcript fragments. Oversized input and unknown/ambiguous causes are bounded.
- Documentation/link checks passed. Exact-source remote CI, security and release
  archive/host qualification remain required separately.

## Retained local receipts

Ignored directory: `infra/host-tests/work/beta15-candidate-20260926/`.
No passwords, raw setup codes, cookies or interactive terminal transcript retained.

| Artifact | SHA-256 |
| --- | --- |
| Linux installapply development test binary | `42d6d68d059d040f60912f466c4ca3d85ad09ed459f0c84478619683bdbe91bb` |
| `package-guard-development.log` | `ebba2f3cfc2f1957acdb0362d0fb010b3c9e80fd3cdfc3944fee355a94ebc750` |
| `onboard-development.log` | `9971f4ed5565181cf486fcee6c8ce0bb8cf70f9485ab5eb20b0951064e2391f1` |

The root test bind-mounts synthetic package-manager files in its private mount
namespace. It is evidence for kernel lock behavior, not an actual unattended
upgrade racing a newly built public installer. The latter needs exact-candidate
host qualification. See the [candidate plan](../../../docs/beta15-candidate.md).
