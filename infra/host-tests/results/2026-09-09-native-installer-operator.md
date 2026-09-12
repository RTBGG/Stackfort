# Native installer operator qualification — 2026-09-09

Scope: real installer CLI for recorded native status and durable recovery
approvals, with consumption by the opt-in Debian lab supervisor. This is not
production native boot/installer activation. No release, commit, push or
customer-server change is included.

See the [operator guide](../../../docs/native-installer-operator.md) and
[previous admission qualification](2026-09-09-native-service-admission.md).

## Exact inputs

Only `stackfort-native-quota-debian-13`
(`4361f439-15e9-4f9e-a690-9a8e44b6cbd3`) was started. Its offline
`native-quota-before-conversion` checkpoint
(`9e62e0ac-89ad-4851-8ae2-e6b936e40eae`) was explicitly restored after
verifying the prior admission-success checkpoint was preserved.

Debian 13, kernel `6.12.107+deb13-cloud-amd64`, 2 vCPUs, 8 GiB RAM, original
50 GiB system disk plus seed. The authenticated beta.3 fixture from CI run
`34182191221` is unchanged. The new CLI is a separately sealed lab artifact,
not inserted into that candidate's signed archive. The original fixture was
retired after retention; no fallback download or extraction was used.

| Item | SHA-256 |
| --- | --- |
| Sealed integration helper | `2b0b5e3316c5c1c626773844364cf14a79e972e08fdf631a39ce867ab41dd26c` |
| Sealed actual installer CLI | `86410cdac898d844a735a700d3785588b32a5c4daa6ded1800aa49f25cde1272` |
| Linux installapply test executable | `45f61f541c385b9a713b1dd715228bbbde0a1afe9be8b7e4ec7214b739569087` |
| Linux storageprep test executable | `55746423bccba3876d5d3448d7cba7415d50cb58dad61cf9f029aef963064544` |
| Unchanged candidate archive | `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0` |
| Unchanged attestation bundle | `b929c9d6a8e15726f53381bb35e2bc1912ab13908b2fadffe5c73e45178d8344` |

Workspace base HEAD remains `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2`
with preserved local changes.

Native operation: `b3fc5427-db3a-4be6-bcf2-9974d59e7310`.
Manifest digest:
`e6e28b5740ed0d6cf044368c2b986feea81367d7699e60b2305929e91442144f`.

## Automated checks

- Windows `go test ./...` and `go vet ./...`: pass.
- Linux `go vet -tags=integration ./...`: pass.
- Actual Debian binaries: **64 installapply + 15 storageprep top-level tests**
  pass, including real isolated nftables, immutable approval transitions,
  digest comparison before writing, consumed/cancelled replay rejection,
  linked/malformed/unsafe approval records, and existing-lock contention.
- Installer CLI validation tests exercise missing confirmation, invalid
  digests/actions/format, status without mutation, errors, help and failed output.
- On the fresh actual VM, `native status --format=json` succeeds with no
  native state and does not create `/var/lib/stackfort-installer`.
- The updated native-package wrapper passes Debian `sh -n`; the PowerShell
  host wrapper parses. No DEB/RPM package was built or installed for this change.

Status explicitly reports `publicResumeEnabled=false` and
`liveReadinessVerified=false`. Approval does not start a service or reboot.
The existing public installer/updater native-state gates remain in place.

## Actual interruption and CLI recovery

The fresh, authenticated one-shot conversion succeeds, then the real package
transactions complete behind the closed web gate. The sealed fault pauses
before the service stage. SIGKILL is sent only to the coordinator at this safe
point; `ExecStopPost` quarantines the fixed consumers. No package manager or
filesystem conversion process is deliberately interrupted.

The actual installer reports storage `ready`, admission `checking` at attempt
1 and an incomplete package journal. A wrong state digest returns blocked exit
code 2 and creates no approval file.

The supervised recovery test then proves:

1. CLI JSON status matches the locked internal inspection.
2. The matching two-digest approval is pending; repeating it leaves the exact
   approval digest unchanged.
3. A wrong cancellation digest fails without changing the pending record.
4. The matching cancellation persists `cancelled`.
5. Explicit reapproval creates a new request UUID while the web gate stays closed.
6. The supervisor consumes that exact UUID before installation, completes
   admission attempt 2 and leaves the receipt `consumed`.
7. Replaying the old two-digest approval after completion fails and leaves the
   consumed receipt unchanged.

The first eight package stages retain attempt 1; services complete on attempt
2. No package journal or retained release is reset or rebound.

| Recorded identity | Value |
| --- | --- |
| Interrupted admission SHA-256 | `0104c537fd85b25d5589594d7a484d4dbea884c0be6f2f5b24017e19f670e9ea` |
| Interrupted package journal SHA-256 | `3daa488001f3b42d3eb86f4202a1f1697c424a996bc15bbd0856fa3286a6a2a3` |
| Completed package journal SHA-256 | `d887d9e894b075fe31b4fa261b6b9fd49bd8eee041ce08c55a45d810b0828c03` |
| Consumed approval UUID | `397c685b-8eaa-4ddb-aec1-68461b87da29` |
| Consumed approval SHA-256 | `3941daf98e81ddd3236b6d8c39f0f2079ab5f37e18f09f5c81ab9373ec0f6a05` |

## Normal reboot and final state

A subsequent real normal reboot passes all four quota/OCI subtests: account
isolation and quotas, private OCI resources, container deployment lifecycle,
and subordinate-UID quota enforcement. The conversion helper does not rerun.
The installation result is `alreadyInstalled=true`, `resumed=false`,
`changed=false`. The package journal and consumed approval hashes remain
exactly unchanged; admission advances to attempt 3.

Final boot: `5f862e4c-755d-4558-8bda-1f388251c5c0`.
Final admission SHA-256:
`55dfa70e75594ec2d169c51a2ed976b9ba0a285cf741625b2aaa9057e739fb31`.
An external Windows HTTPS health request returns 200 after verification.

The VM is gracefully powered off and saved as offline checkpoint
`native-operator-qualified-success`
(`a0b2a41c-bb0d-4512-b600-bab7ef56d5e7`). Prior checkpoints are retained.
All five Stackfort VMs are off.

## Evidence

Ignored evidence under `infra/host-tests/work/` includes:

- `operator-installapply-linux.log`, `operator-storageprep-linux.log`.
- `native-journal-Prepare-20260909T152618Z`.
- `native-journal-Arm-20260909T152920Z`.
- `native-journal-InterruptInstall-20260909T153003Z`.
- `native-journal-RecoverInstallation-20260909T153254Z`.
- `native-journal-NormalBoot-20260909T153407Z`.

The host wrapper retains both separately built binaries, test/boot logs and its
bounded-scope evidence archive. Raw binaries, candidate archives and VM disks
are not added to Git.

## Remaining production work

The one-time consumption logic is integrated with the real installer-created
approval, but its execution still uses the lab boot supervisor/backend. A pending
approval is not proof of successful recovery or current live readiness.
Production boot dispatch, prerequisite ordering, service/listener/firewall
integration, external package/kernel serialization, capacity reserve, provider
coverage and a new signed native-runtime candidate remain open.

The receipt stores only the most recent pending/consumed/cancelled request, not
an append-only audit log. The process-loss experiment does not qualify power
loss during ext4 metadata or package-manager writes.
