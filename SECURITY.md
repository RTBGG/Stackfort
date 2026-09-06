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

Before public beta, maintainers must publish the supported versions, support
end dates, and upgrade expectations here and in release notes. The
[upgrade catalog](packaging/upgrades/supported-releases.json) records the exact
published predecessors that require upgrade qualification; retirement requires
an explicit reason. It does not by itself create a production support promise.
See the [release checklist](docs/release-checklist.md).

## Handling and disclosure

Reports are handled on a best-effort basis; there is no guaranteed response
time, paid incident-response service, or promised bounty. Maintainers use the
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
audit or a guarantee that hosted applications are safe.
