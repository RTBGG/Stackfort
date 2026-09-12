# Security policy

## Report a vulnerability privately

Use [GitHub's private vulnerability report form](https://github.com/RTBGG/Stackfort/security/advisories/new).
Private vulnerability reporting is enabled for this repository. Reports may
be written in English or German. Do not disclose an unpatched vulnerability
in a public issue, pull request, discussion, or benchmark report.

Include, where available:

- the affected Stackfort version or commit, operating system, and architecture;
- the affected component, required role/access, and security impact;
- minimal reproduction steps on your own disposable test host;
- expected versus observed behavior and a suggested mitigation, if known.

Use synthetic data. Never attach passwords, bootstrap or recovery codes,
session cookies, private keys, full databases, or customer backups. Sanitize
logs before sharing them, including in a private report. Test only systems you
own or have explicit permission to test; stop after establishing the issue
without accessing another person's data.

If the private form is unavailable, open a public issue **only to request a
private contact channel**, without vulnerability details or proof-of-concept
code. Ordinary non-security bugs belong in
[GitHub Issues](https://github.com/RTBGG/Stackfort/issues).

## Supported versions

Stackfort is currently pre-beta. No public release has been published and no
version is supported for production use. Findings against `main` are welcome;
development artifacts and local rehearsal versions are not supported releases.

The selected **`0.1.0-beta.6` candidate** is intended as the first experimental
public version, subject to its complete technical qualification and RTBGG's
separate publication approval. Naming it here does not mean that it is published,
approved or independently audited. If published, its deployment/support scope is:

- Fresh disposable **Debian 13 amd64** test servers with plain GPT/ext4 root
  storage and GRUB; no production workloads or important data.
- The native one-line installation path only. Ubuntu/Rocky native conversion,
  retained-data conversion, LVM/RAID and separate persistent boot/state
  filesystems are not supported by this experimental scope.
- Community-only GitHub support under the terms below. No response/fix SLA,
  maintenance duration or future upgrade compatibility is promised.
- No published predecessor exists at candidate preparation. Unpublished lab
  installations must not be treated as supported upgrade sources; use a fresh
  OS installation. Any later supported upgrade requires its own qualified path.
- No in-place uninstaller: removal is complete OS reinstallation, destroying
  all server data, configuration and services. The shared-root capacity
  limitation described below remains applicable.

Release notes and the digest-bound candidate support/publication records must
confirm this exact scope before public availability. Check the
[release list](https://github.com/RTBGG/Stackfort/releases) for actual publication;
neither a Git tag nor a successful artifact build is a published release.

Support is **community-only through GitHub**, provided voluntarily by
[RTBGG](https://github.com/RTBGG) and any future community contributors. There is
no commercial support, service-level agreement, guaranteed response or fix, or
promised support period/end date. Contributions and voluntary reviews are welcome;
their availability must not be assumed.

Before public beta, maintainers must identify the released versions, upgrade
expectations and deployment limits here and in release notes under these
community-only terms. Do not invent support end dates or maintenance guarantees. The
[upgrade catalog](packaging/upgrades/supported-releases.json) records the exact
published predecessors that require upgrade qualification; retirement requires
an explicit reason. It does not by itself create a production support promise.
See the [release checklist](docs/release-checklist.md).

## Handling and disclosure

Reports are handled on a best-effort basis; there is no guaranteed response
time or fix, paid incident-response service, or promised bounty. Maintainers use the
private report to assess impact, coordinate a fix and regression test, and
agree on disclosure and attribution with the reporter where possible. A fix
must retain the normal integrity, provenance, health, and rollback gates.

If a report concerns a dependency, include its exact version and the Stackfort
integration affected. Do not assume an upstream advisory proves exploitability
in Stackfort, or that a passing scanner proves the integration safe.

## Scope and limits

Authentication, account isolation, privileged agent operations, files/archives,
database access, WAF/cache boundaries, OCI workloads, and installation/updates
are all security-sensitive. The [security model](docs/security.md) documents
threats and residual risks; the [operations guide](docs/operations.md) describes
the current operating and recovery limits. Neither is an independent security
audit or a guarantee that hosted applications are safe. No independent security
review has been completed. A professional paid audit is not currently funded;
an actual independent voluntary/community review is welcome, but automated or
agent-generated checks cannot be presented as such a review.

On 2026-09-12, RTBGG explicitly authorized an experimental beta without an
independent review, only for fresh disposable test servers with no important
data and never for production. This general policy does not mean a candidate
has passed technical tests or received publication approval. Any such release
must prominently disclose the missing review, meet the technical gates and
record candidate-specific approval. The first native fresh-root scope is Debian
13 amd64; prepared-storage qualification on other systems is a separate path.

RTBGG also approved complete operating-system reinstallation as the experimental
beta's only removal method on 2026-09-12. No in-place uninstaller is available.
Removal irreversibly destroys all server data, configuration and services;
removing a passive DEB/RPM is not removal of the active platform. An actual
same-target full-OS reprovision test of each exact experimental candidate remains
mandatory. This authorization is not a passed removal test, production support
or an exception to reviewed releases' active-uninstall requirement. See the
[removal policy](docs/experimental-beta-removal.md).

The native experimental profile does not yet guarantee a durable OS disk/inode
reserve. Aggregate account usage or platform data can exhaust the shared root
filesystem and make the entire test server unavailable, even when individual
account quotas are enforced. Initial installation headroom is not ongoing
capacity protection. This is a production blocker and must remain an explicit
experimental release limitation; see the [capacity boundary](docs/one-line-installation-readiness.md#experimental-capacity-limitation).
