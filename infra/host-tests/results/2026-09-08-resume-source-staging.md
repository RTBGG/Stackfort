# Durable resume-source staging — 2026-09-08

Outcome: **durable release-copy and independent-process verification pass on
Debian Linux**, including the existing complete beta.3 candidate. Native
installer activation and actual post-boot package installation remain disabled.
See the [implementation and trust boundaries](../../../docs/native-quota-release-staging.md).

## Tested changes and environment

Base commit `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2` plus working-tree changes:

- `internal/installapply/source_pin.go`: canonical operation/version/content/
  metadata/executable/manifest binding and plan compatibility.
- `internal/installapply/source_stage_linux.go`: shared installer lock, private
  fixed-path copy, durable receipt, full re-verification and incomplete-state
  rejection. No release execution or production CLI registration.
- `tests/integration/host_resume_source_linux_test.go`: real release source and
  fixed-path APIs in a private mount namespace, followed by a new verifier process.

Only `stackfort-native-quota-debian-13` was started: Debian 13,
`6.12.107+deb13-cloud-amd64`, Hyper-V ID
`4361f439-15e9-4f9e-a690-9a8e44b6cbd3`. No checkpoint restore, filesystem
conversion, package install or boot-configuration edit was performed. Existing
native resume/consumer services remained active. The VM was shut down normally
after testing; all five test VMs are off.

## Results

| Check | Result |
| --- | --- |
| Full ordinary Windows `go test ./...` | Pass |
| Linux installer and integration build/vet | Pass |
| Complete installer unit suite on Linux root | 42 top-level tests pass, including 8 new pin/staging tests; no skips |
| Canonical pin, source/operation/version binding | Pass |
| Original bootstrap directory removed, stage closed and reopened | Pass |
| Tampering and unsafe metadata | 18 negative subcases pass; no automatic repair |
| Unsafe input/cancellation rejection before creating a stage | 11 subcases pass |
| Simulated incomplete stage layouts | 4 subcases pass: empty directory, partial tree, complete tree without receipt, partial receipt |
| Existing package journal and excessive directory depth | Rejected |
| Real beta.3 candidate staged and verified by a new process after bootstrap cleanup | Pass |
| Actual shared installer/storage lock | Both competing lock acquisitions blocked |
| New staging after native journal creation | Blocked |
| Existing native journal production guard regression | All six phases plus corrupt state rejected; absent-state compatibility passes |

Unit fixtures have small synthetic ELF headers and synthetic package bytes to
exercise the real inspector without executing anything. The integration instead
uses the retained complete archive from candidate workflow run `34182191221`:
`stackfort-0.1.0-beta.3-linux-amd64.tar.gz`, 117,210,432 bytes, SHA-256
`3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0`.
The local file and the transferred archive both matched the retained checksum.
This turn did not fetch or newly attest a GitHub release.

The copied source produced:

| Identity | Value |
| --- | --- |
| Operation | `20000000-0000-4000-8000-000000000001` (qualification fixture) |
| Source digest | `48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8` |
| Metadata/content tree SHA-256 | `33794b4ff6afb444ede52d40e2a79bd6f53b1483ef2847840960db1d40335965` |
| Retained installer SHA-256 | `11bf099d1c37848f7eb3685873c68b992a1225ed03f7bb55e88d930fbbd63a77` |
| Release manifest SHA-256 | `2e2e6075ab003dd581c153d6fb2cfac9ec36c476ffe21ccd15d552fc051c4cc3` |

The actual VM's storage journal SHA-256 remained
`5973bf8338036ec6ef557fc4d446a60d4af451b8e6a06ebaa755fcd0e8e0be4b`, matching
the previously retained successful boot archive. The staging directory was
absent from the actual host after the isolated test. Temporary extraction and
namespace fixtures were cleaned up by the tests; the retained candidate and
local evidence were not removed.

## Retained local evidence

Relative to ignored `infra/host-tests/work/`; SHA-256 of the final executed
binaries and captured logs:

| File | SHA-256 |
| --- | --- |
| `resume-source-installapply.test` | `e11afac1a5309ebe652ec6d43c1ca63fcf9870d391c47c442387dcdfb0193a7f` |
| `resume-source-integration.test` | `5d3615ede849447fda153386511115988f993a0ef4d8d5a499aa3de76eb03059` |
| `resume-source-final-linux.log` | `080c92f499fe8b3152bd73af8a7185b430f03dfc0c7bf859626005e3d77d6fb1` |
| `resume-source-final-integration-linux.log` | `50d2f49b5ed49ba8acbb54ea3e36b8e6e69dd2a1e52afdf5b8eae6767f3caefb` |
| `resume-source-host-state.log` | `5f9b8fe9d4c6b13ef0494aa649331f89cd7f6944ffc7a8da0f528b2fb2c0b296` |

## Not qualified by this result

This does not authenticate a new release, bind the pin into the existing lab's
boot manifest, run a new quota-conversion/reboot cycle, execute the retained
installer or establish physical power-loss recovery. The API must still be
connected to a reviewed coordinator and the actual installer/service graph.
Origin verification, source/manifest/boot locking, fresh-server eligibility,
operator recovery, OS reserve and multi-OS onboarding remain open.

No commit, push, public release, automatic reboot activation or customer-VPS
change was made. The public installer retains its prepared-storage requirement.
