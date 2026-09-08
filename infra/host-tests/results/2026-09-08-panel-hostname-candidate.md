# Panel hostname and automatic HTTPS candidate — 2026-09-08

`0.1.0-beta.3` passed native installation, panel/private-CA tests and six selected
hosting regressions on Debian 13, Ubuntu 26.04 and Rocky Linux 10. It is an
**unpublished test candidate**, not a public beta or a production-readiness
claim. [Machine-readable evidence](2026-09-08-panel-hostname-candidate.json)
records artifact identities, exact environments, test names and local log
digests. All three test VMs were shut down gracefully afterwards.

## Artifact identity

- Fixed source: `5282946bec1f865de7222128a6a5d0d8a656f34c`.
- [Candidate build and original download](https://github.com/RTBGG/Stackfort/actions/runs/34182191221):
  manual `workflow_dispatch`, with no tag or release publication.
- Actions artifact `10039391940`, `stackfort-0.1.0-beta.3`, 311,727,204 bytes.
  Original ZIP SHA-256:
  `69eaef3e3013e4d66684f0f1f822ca4718407f140101b4e17dd34e8ae4f80b4c`.
  Artifact retention ends on 2026-10-08; this is not a permanent release URL.
- Archive SHA-256:
  `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0`.
- DEB SHA-256:
  `abf4c26237f77fc9de5492d5e50b46dc9b0e306c2f812c02f1e43f9fca226e20`.
- RPM SHA-256:
  `03bddbc0a131defd50f0bfc50f4500332038efca539540bca9108c1dec83480d`.

The original ZIP digest was checked before extraction. All six `SHA256SUMS`
entries and all ten attested subject digests matched the downloaded files,
including the SBOM and native-package sidecars. Cryptographic verification used
the bundled `stackfort-gh attestation verify`, with repository `RTBGG/Stackfort`,
source ref `refs/heads/main`, the exact source digest above, signer workflow
`.github/workflows/release.yml`, and `--deny-self-hosted-runners`. This manual
candidate's main-branch provenance does **not** relax the updater's tag policy.

The matching [CI](https://github.com/RTBGG/Stackfort/actions/runs/34182174724)
and [Security](https://github.com/RTBGG/Stackfort/actions/runs/34182174787)
runs passed, including Linux Go/race tests and reproducible artifacts. Local
verification also passed Go tests/vet, Linux-target gosec, 64 frontend tests,
type/i18n checks, frontend build, npm audit and source/packaging checks.

## Fresh-host matrix

Only the three dedicated Hyper-V guests were restored to
`stackfort-installer-ready-20260824`. Each has two virtual CPUs, 4 GiB startup
memory and a separate project-quota-enabled hosting filesystem. Tests used the
downloaded passive DEB/RPM carriers, not locally rebuilt or repaired binaries.

| System | Native first install / no-op | Panel tests | Selected hosting regressions |
| --- | --- | --- | --- |
| Debian 13, ext4, AppArmor enabled | passed | 9 passed | 6 passed |
| Ubuntu 26.04, ext4, AppArmor enabled | passed | 9 passed | 6 passed |
| Rocky Linux 10, XFS, SELinux Enforcing | passed | 9 passed | 6 passed |

The [installer harness](../Test-StackfortInstallerHyperVVm.ps1) used `-SkipBuild`,
`-ArchiveDirectory` pointing at the verified original candidate directory,
`-InstallMethod native` and the matching `-NativePackagePath`. It checked
preflight, complete installation journal/no-op replay, file modes, service
sandbox settings, firewall/MAC, WAF/Vinyl package integrity, phpMyAdmin ingress,
API/UI health and the installed panel renewal timer. Starting its unconfigured
oneshot succeeded without creating an ACME account or contacting a CA.

Two test executables were compiled from the same fixed source using Go 1.26.6,
`GOOS=linux GOARCH=amd64 CGO_ENABLED=0`:

- `go test -c ./internal/hostnginx`, binary SHA-256
  `c60b3b23d2b2c69d5a021d441a5f195a3c1df1813138db0a6ec1ae3028256b97`.
  Run as root with `STACKFORT_TEST_PANEL_HOST=1`, `-test.run '^TestPanel'`
  and `-test.timeout=2m`.
- `go test -tags=integration -c ./tests/integration`, binary SHA-256
  `67c1aff9f244ac0802b94254764a88520150c12262cd69dbbeb2aaab61a13cab`.
  Run as root with `STACKFORT_DISPOSABLE_HOST_TEST=1` and `-test.timeout=10m`,
  selecting exactly these `TestDisposableHost` suffixes:
  `StaticDomainLifecycleAndWorkerAccess`, `ProjectQuotaAndAccountIsolation`,
  `VinylCacheSafetyWAFAndPerformance`, `WAFRuntimeAndPerformance`,
  `OCIDeploymentLifecycle`, `PrivateACMETLSLifecycleOverAgentRPC`.

Panel tests exercise real NGINX HTTP-01 delivery, trusted HTTPS UI/API on 443,
wrong-Host denial, certificate import/rotation, renewal/no-op decisions,
hostname conflicts, unsafe input rejection, interrupted transitions and
rollback. The fixtures do not modify the OS trust store or contact public
Let's Encrypt. The [panel guide](../../../docs/panel-hostname.md) describes the
actual operator command and DNS/port requirements.

Hosting regressions cover static/PHP tenant isolation, quotas, private-CA TLS
through agent RPC, rootless OCI deploy/suspend/resume/rollback/remove and account
resource limits. WAF attack/TLS/rollback tests and Vinyl/FastCGI safety tests
include WAF off, detection-only and blocking PL1, plus inspection before warm
FastCGI hits, personalization bypass, per-domain toggle and purge/isolation.
Incidental throughput collected while guests ran concurrently is **not** a new
controlled performance comparison; existing benchmark caveats still apply.

## Excluded candidates and failure handling

The beta.1 build succeeded but installation exposed an overly strict WAF
manifest reader that omitted the pinned connector patch field. The corrected
reader now requires that exact patch digest. Installer verification also
exposed omitted renewal units in a duplicated service inventory; install and
verification now derive their list from the same templates.

Beta.2 passed native installation and panel tests on all three distributions,
but failed supplemental OCI tests. Minimal Debian/Ubuntu lacked explicitly
installed `catatonit` and/or `dbus-user-session`; a runtime directory alone did
not prove a ready systemd user session. Rocky image files on the separate XFS
disk inherited `unlabeled_t`. Beta.3 installs the missing runtime dependencies,
checks the account-owned user-bus socket, and seeds only an empty, verified
private container store with a fixed SELinux label. Negative tests cover
populated stores, wrong ownership/mode, attribute failures and noncanonical
paths. No recursive tenant relabeling, socket exposure or MAC weakening was
introduced; see [runtime policy](../../../docs/rootless-oci-runtime.md).

The first beta.3 Ubuntu attempt encountered `unattended-upgrades` holding the
dpkg frontend lock beyond the installer's 120-second bound. The installer
failed closed and preserved its stage journal. That log/journal was retained,
then only the Ubuntu VM was restored again. Standard OS maintenance was allowed
to complete, the package database was audited, and the VM rebooted before the
unchanged candidate retest. Maintenance restarted D-Bus and disconnected two
status clients; final service success and an empty `dpkg --audit` were checked
independently afterwards. Security updates were not disabled and no lock was
deleted or bypassed.

## Release boundary

This matrix does not prove public Let's Encrypt issuance for an operator's DNS
name, a real renewal after elapsed certificate lifetime, full reboot/upgrade
qualification of an installed candidate, or all manual product workflows.
It does not close the independent audit, EN/DE manual review, active uninstall,
support-window or public release/upgrade gates in the
[release checklist](../../../docs/release-checklist.md).

No release/tag was published. The public one-line installer cannot retrieve
this unpublished candidate. Use the matching passive carrier only on a fresh,
disposable amd64 host after reviewing the
[installation requirements](../../../docs/installer-preflight.md).
