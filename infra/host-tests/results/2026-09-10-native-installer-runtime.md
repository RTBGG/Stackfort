# Native installer runtime qualification — 2026-09-10

Scope: the real installer's **post-ready** gate, live storage verifier, admission
and process-loss quarantine. The first offline conversion/finalization remains
the previously qualified lab helper. No public activation, release, repository
publication or customer-server operation is part of this result.

## Target and artifact identity

- Dedicated Hyper-V VM: `stackfort-native-quota-debian-13`,
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`.
- Debian 13, kernel `6.12.107+deb13-cloud-amd64`, 2 vCPU, 8 GiB RAM, nominal
  50 GiB plain ext4 root and separate NoCloud seed only.
- Fresh checkpoint: `native-quota-before-conversion`,
  `9e62e0ac-89ad-4851-8ae2-e6b936e40eae`.
- Source checkout: dirty local work based on
  `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2`; no commit or tag created.
- Final integration helper SHA-256:
  `78befa96630b453de3c730d71376f157472a2f45720d5d13c3ca8d1151b24fcb`.
- Final real installer dispatcher SHA-256:
  `50b8ed27be46c38b2c96508c5da20bc3ca5a47752ec1207c91fc1f5ab623534c`.
- Operation: `6fe58644-7fcc-4295-9ec5-a9b6078db256`.
- Native release manifest SHA-256:
  `a2e4271678d8b41505a6840e5c6449c758471ce5e5e13bd70c423828cf345710`.
- Runtime intent SHA-256:
  `a9570adca8c8a6ad381cd2f33b4eb549a07b1bbae0cee17dc5a79cefba98dab8`.

The retained authenticated fixture is unchanged:

- Candidate: `0.1.0-beta.3`, commit
  `5282946bec1f865de7222128a6a5d0d8a656f34c`.
- Archive SHA-256:
  `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0`.
- Attestation bundle SHA-256:
  `b929c9d6a8e15726f53381bb35e2bc1912ab13908b2fadffe5c73e45178d8344`.
- Retained source digest:
  `48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8`.

The dispatcher is a separately pinned current-code lab artifact, not a
replacement smuggled into the authenticated candidate. The original fixture
directory was retired before arming; the services use retained evidence.

## Local and Linux checks

- Windows `go test ./...` and `go vet ./...`: pass.
- Linux cross-build and integration-aware vet for installer, storage protocol
  and host integration package: pass.
- Linux root tests with explicit disposable opt-in: **100 top-level tests pass,
  none skipped** (75 installapply, 16 storageprep, 9 installer CLI).
- Includes linked/unsafe/changed dispatcher files, current-executable path,
  binary-specific bounds and exclusive publication, bound runtime fields,
  no offline actions, feature/geometry validation, exact service templates,
  pre-journal orphan runtime rejection and existing approval/admission tests.
- Final Linux test log SHA-256:
  `e83f978592b727bc4481146ff7127db69881bb704e8b91daa76d67a3751d9e4a`.
- `systemd-analyze verify` accepts the final four generated service/mount units.

## Implementation findings

An initial attempt correctly stopped before the storage journal was created:
the JSON-record writer's 64 KiB limit rejected the executable. A separate
64 MiB bounded exclusive binary writer fixes this without weakening JSON limits;
a regression test covers a binary above 64 KiB. That failed run is retained as
`work/native-journal-Prepare-20260910T172229Z`.

An intermediate build then passed fresh installation, normal reboot, external
web quarantine and actual dispatcher SIGKILL. Review additionally identified
the pre-journal fragment boundary: runtime files must block ordinary installation
even if preparation stopped before the journal was created. This was added to
the public gate and operator inspection, with regression tests. All final
qualification runs start again from the clean checkpoint with the final pins
above; no changed executable is substituted into an already-sealed operation.

## Final boot and failure checks

The final artifact pair passes fresh installation and normal boot. All nine
package stages complete once; the normal boot returns `alreadyInstalled=true`,
`changed=false`, retaining their original attempts and timestamps. Quota/account
isolation, OCI private resources, OCI lifecycle and rootless sub-UID quota
enforcement pass on both boots. Normal boot shows the lab resumer skipped
(`ConditionResult=no`, no process), no preparation command-line token and no
current-boot offline proof. Runtime service logs contain no test invocation.

External Windows-host checks first receive a healthy panel response, then call
the real dispatcher's `close`: TCP 80/443/8443 time out externally while NGINX
remains active and loopback HTTPS health succeeds. The exact owned inet table
contains the TCP and UDP web-port drops; IPv4/IPv6 rule behavior is also covered
by the Linux namespace suite. This is not an external IPv6 routing qualification.

The real dispatcher is then stopped with SIGKILL during `checking` on a
revalidation of a completed installation. Its independent real-CLI
`ExecStopPost` closes the gate and stops managed consumers. The package journal
is byte-for-byte unchanged; admission attempt 3 remains `checking`, requiring
an exact operator review instead of an automatic retry. No apt/dpkg transaction
is interrupted.

- Interrupted admission-state SHA-256:
  `03e5c092cee1121f3543eee9902d2855bf04f5f25ef08ff3a4cd2d2eef5386b1`.
- Completed package-journal SHA-256:
  `cfcfe8143b47937e54faa3ca46273b179a93e0359bc668fbbc2dcc279d4ebfb2`.

A subsequent normal boot with no approval remains quarantined and preserves
both review digests. The real operator flow then passes status, approval,
idempotent repeat, wrong cancellation rejection, correct cancellation,
reapproval, single consumption by the real service, and stale replay rejection.
Admission attempt 4 succeeds with the original complete package journal intact.
One more full normal-boot quota/OCI validation passes as admission attempt 5,
boot `0b63d997-d825-46ec-b52d-ee787de3ef3a`. The consumed approval UUID remains
`336fe391-a868-4e13-8cef-e287ab959ce5`, receipt SHA-256
`d2630dcc09ac16614724ad021708721b78b425b2b7246bdce605ef9dca43c4b3`.

After an offline successful checkpoint, a separate negative boot changes only
the lab `/etc/fstab` permissions from root:root `0644` to `0664`. The real storage
verifier rejects the unsafe metadata, persists `recovery-required` /
`readiness-lost`, and refuses the hosting mount. NGINX, the admission service
and the synthetic consumer remain inactive; the web gate stays closed. The
previously admitted record remains historical, correctly accompanied by
`liveReadinessVerified=false`; it does not authorize this failed boot.
The failed state and logs are retained, then the whole disposable VM is restored
offline to the previously successful checkpoint, not repaired by resetting its
journals. No runtime auto-repair or storage-recovery bypass was introduced.

Final local evidence directories (under ignored `infra/host-tests/work/`):

- `native-journal-Prepare-20260910T173418Z`
- `native-journal-Arm-20260910T173532Z`
- `native-journal-Validate-20260910T173622Z`
- `native-journal-NormalBoot-20260910T173904Z`
- `native-journal-RuntimeInterrupt-20260910T173952Z`
- `native-journal-BlockedInstallBoot-20260910T174038Z`
- `native-journal-RecoverInstallation-20260910T174147Z`
- `native-journal-NormalBoot-20260910T174315Z`
- `native-journal-BlockedInstallBoot-20260910T174504Z` (fstab metadata drift)

Each driver directory retains exact executables, test/boot logs and an evidence
archive. The external web-probe log is `native-runtime-external-web-final.log`,
SHA-256 `8633b64d2548423111cabee0db88d2c245e9c80f253ad7be2f0a1350d883b056`.
Supplemental logs:

- `native-runtime-final-state.log`, SHA-256
  `db6ca16ab2ee677f16937c34e02037f8a50fcc21b0eff4b85af0068be5206855`.
- `native-runtime-readiness-drift-final.log`, SHA-256
  `ac81ac9226d9dde680d4e29f1611ff756795fab7fd9080ff5e3f4558f8edafe6`.

Final offline checkpoint: `native-runtime-qualified-success`,
`aad343f1-d701-4f93-aa67-4260f0067771`. All five Stackfort VMs are **Off**;
the four unrelated VMs were never started. Previous checkpoints are retained.

## Remaining boundaries

This is not a ready public one-line native installer. Still required:
packaged initial preparation/offline backend and handoff, provider eligibility
and prerequisites, external package/kernel serialization, capacity reserve,
full listener/firewall interactions, Ubuntu/Rocky coverage and a fresh signed
native-runtime candidate. The fixed bind source is still the disposable fixture.
The web gate covers TCP/UDP 80/443/8443 in inet, not arbitrary tenant-published
ports or externally initiated global firewall flushes/reloads.

See [runtime design and qualification interface](../../../docs/native-installer-runtime.md).
