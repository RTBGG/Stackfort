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

Stackfort is currently an experimental beta. No version is supported for
production use. Findings against `main` are welcome;
development artifacts and local rehearsal versions are not supported releases.

## Unpublished Beta.14 candidate

`0.1.0-beta.14` is being prepared for qualification; it is **not a published
release** and has no candidate-specific publication approval. Beta.13 remains
the public installer's selected release. No in-place upgrade is offered.

The candidate addresses ACME registration, service-state presentation and the
database wizard, and adds administrator-only panel-domain setup with automatic
certificates. Its intended test scope remains fresh disposable Debian 13 amd64
servers with qualified ext4/GRUB storage, not production or important data.
Community-only support, no independent security review, and destructive complete
OS reinstallation as the only removal method remain explicit limitations.
These proposed terms do not replace exact-candidate host tests or RTBGG's
publication decision. See the [candidate plan](docs/beta14-candidate.md).

## Beta.13: fresh installations only

[`0.1.0-beta.13`](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.13)
was published on 2026-09-26 as an immutable experimental prerelease under
[RTBGG's candidate-specific approval](docs/release-evidence/2026-09-26-beta13-publication-approval.md).
The public bootstrap selects Beta.13 for fresh disposable Debian 13 amd64 hosts,
using qualified ext4/GRUB GPT/UEFI or primary-MBR/BIOS storage. It fixes rejection
of larger valid initramfs inventories and retains bounded, redacted arming diagnostics.
Exact-candidate onboarding on both profiles, host security, resource/isolation,
WAF/cache, real rootless OCI, process-loss quarantine and same-target OS removal
passed, as did the original live CI/security, checksum and provenance gates.
All 14 public downloads were verified; the 12 original tag-qualified files are unchanged.

**No upgrades from any installed release are supported.** Use a fresh OS;
interrupted conversions cannot be resumed or reset. No independent security
review, production use or important data. Community-only support and complete
OS reinstallation as the only removal method remain unchanged.
See [candidate evidence](infra/host-tests/results/2026-09-26-beta13-candidate-qualification.md)
and [public installation status](infra/host-tests/results/2026-09-26-beta13-public-installation.md).
The frozen candidate's earlier unpublished-status text remains historical;
this later publication decision does not alter the release's bound terms.

## Earlier published release (Beta.12)

[`0.1.0-beta.12`](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.12)
was published on 2026-09-24 as an immutable experimental prerelease under
[RTBGG's candidate-specific approval](docs/release-evidence/2026-09-24-beta12-publication-approval.md).
The public bootstrap selected Beta.12 at publication; it now selects the current
release above. Beta.12 was qualified for fresh disposable Debian 13 amd64 hosts,
using qualified ext4/GRUB GPT/UEFI or primary-MBR/BIOS storage.
It fixes rejection of [inactive optional optical-media entries](docs/native-installer-optical-media.md).
Exact-candidate host qualification, failure containment, same-target OS removal,
live CI/security and provenance gates passed; public assets match the original bytes.

**No upgrades from Beta.10, Beta.11 or any other installed release are supported.**
Do not use the updater or install over an existing system. No predecessor is
retired and no failed upgrade test is counted as passed. No independent security
review, production use or important data. Community-only support and the
full-OS-reinstallation removal requirement below apply unchanged.
See [candidate evidence](infra/host-tests/results/2026-09-24-beta12-candidate-qualification.md)
and [publication evidence](infra/host-tests/results/2026-09-24-beta12-public-installation.md).

Known installation limitation reported on 2026-09-26: the initramfs inventory's
64 KiB output limit rejects larger valid listings during boot preparation. The
[fix and stopped-host guidance](docs/native-installer-initrd-inventory.md)
do not authorize journal resets, sealed-runtime replacement or resuming a failed
conversion. Historical qualification remains scoped to its tested hosts.

## Earlier published release (Beta.11)

[`0.1.0-beta.11`](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.11)
was published on 2026-09-24 as an immutable experimental prerelease under
[RTBGG's candidate-specific approval](docs/release-evidence/2026-09-24-beta11-publication-approval.md).
It adds qualified primary active MBR/ext4 roots with BIOS/GRUB to Debian 13 amd64
GPT/UEFI fresh installation. The same community-only terms and limitations below
apply: no independent review, production use or important data; removal requires
complete OS reinstallation. The public bootstrap selected Beta.11 at publication; it now selects the current release above.

**Upgrades from Beta.10 or any other existing release are unsupported and
unqualified.** Do not use the updater or install Beta.11 over an existing system.
Use a fresh OS installation. The explicit
[fresh-only exception](docs/release-evidence/2026-09-24-beta11-fresh-install-scope.md)
does not turn the failed predecessor test into a pass or globally retire Beta.10.
All remaining exact-candidate technical gates passed, including resource/isolation,
WAF/cache, OCI, failure quarantine and same-target OS-removal qualification.
See the [candidate evidence](infra/host-tests/results/2026-09-24-beta11-candidate-qualification.md)
and [MBR boundary](docs/native-installer-mbr.md). The frozen candidate's earlier
unpublished-status text remains historical; this later publication decision is authoritative.

## Earlier published release (Beta.10)

**[`0.1.0-beta.10`](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.10)**
was published on 2026-09-24 after exact-candidate technical qualification and
[RTBGG's publication approval](docs/release-evidence/2026-09-24-beta10-publication-approval.md).
No independent security review has been performed. Its deployment/support scope is:

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

Release notes and the digest-bound candidate support/publication records confirm
this scope. The release receipt binds the candidate's earlier `SECURITY.md`;
this publication-status update changes none of its support or deployment terms.
Neither an unrelated Git tag nor a successful artifact build is a published release.

Support is **community-only through GitHub**, provided voluntarily by
[RTBGG](https://github.com/RTBGG) and any future community contributors. There is
no commercial support, service-level agreement, guaranteed response or fix, or
promised support period/end date. Contributions and voluntary reviews are welcome;
their availability must not be assumed.

For each public beta, maintainers must identify the released versions, upgrade
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
