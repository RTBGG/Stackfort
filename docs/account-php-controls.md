# Account PHP controls and health

Status: implemented, accessibility-tested, and VM-qualified on 2026-08-25

J-002 connects the managed PHP runtime to package policy, domain forms, and a
tenant-scoped health view. The browser never decides which PHP installation is
trusted: it can select only the intersection reported by the host and stored in
the account's immutable package assignment.

## Version policy

`GET /api/v1/admin/host/capabilities` adds `managedPhpVersions`. A version is
present only when the distribution is supported, systemd is available, and the
exact approved native FPM package is reported as installed. The administrator
package form exposes only this closed list. Selecting no version creates a
static-only package.

For an account, the server computes:

```text
available versions = host-approved versions ∩ assigned-package versions
```

The administrator and account domain forms use only that result for PHP target
selection. Domain create/edit still submits the existing typed `static` or
`php` target with an account-relative document-root mode; a PHP target also
contains exactly one approved version. Pool sizing is not free-form browser
input. J-002 retains J-001's reviewed fixed preset of four on-demand children
and a 128 MiB PHP memory limit per child.

## Tenant-scoped status API

`GET /api/v1/accounts/{accountID}/php` requires `account.resources.view` for
that exact account. Authorization occurs before any host-agent request, and a
foreign or absent account returns the same opaque 404 response. The endpoint
joins the live host report, current immutable package assignment, and current
domain state, then returns:

- a typed runtime capability;
- host-approved, package-allowed, and effective version arrays;
- one bounded pool state per approved native version: `missing`, `inactive`,
  `active`, or `failed`;
- the configured PHP-domain count; and
- optional aggregate systemd memory bytes, cumulative CPU nanoseconds, and
  process count.

The response omits Unix identities, unit names, configuration/socket/PID paths,
cgroup paths, process IDs, arguments, environment, logs, and other accounts.
Agent or accounting failure produces `503 php_status_unavailable`; unavailable
or unsupported host capability remains a successful typed status so the UI can
explain why PHP cannot be selected.

## Read-only agent operation

`php.fpm-pools.inspect` is the thirteenth version-1 agent operation and is
classified read-only, so audit correlation is forbidden. Its request contains
one validated hosting identity and a sorted, unique, bounded approved-version
list. The agent derives each unit, uses the fixed `systemctl show` profile, and
requires an active unit to be enabled inside the exact account slice. Only the
bounded state and aggregate counters cross the RPC boundary.

## Interface and verification

The account domain page shows runtime availability, effective versions, pool
state, configured-domain count, memory, CPU time, and processes in English and
German. Create and edit forms support static/PHP targets and disable PHP when
the version intersection is empty. Administrator package creation and domain
creation use the same host-approved controls.

Protocol, agent, host parser, authorization/service, HTTP, API-client, Vue,
locale-parity, type, and Axe tests cover the contract. The exact release
archive was then installed from the clean checkpoint on Debian 13/PHP 8.4,
Ubuntu 26.04/PHP 8.5, and Rocky Linux 10/PHP 8.3. Every guest reported an active
pool with aggregate accounting after a real PHP request and `missing` with no
metrics after exact retirement. Existing cgroup, AppArmor/SELinux,
cross-account, TLS, and performance suites also passed. See
[ADR 0037](adr/0037-tenant-scoped-php-capability-and-observability.md) and the
[PHP Hyper-V result](../infra/host-tests/results/2026-08-25-php-hyper-v.md).
