# Managed PHP runtime

Status: implemented and VM-qualified on 2026-08-25

Stackfort runs PHP through one version-specific PHP-FPM service per hosting
account. The control plane submits only a typed account identity, an approved
version, and bounded pool limits; callers cannot select an executable,
configuration path, socket path, unit name, or arbitrary FPM directive.

## Supported native runtimes

| Distribution | Package | Binary | Approved version | NGINX socket owner |
| --- | --- | --- | --- | --- |
| Debian 13 | `php8.4-fpm` | `/usr/sbin/php-fpm8.4` | 8.4 | `www-data` |
| Ubuntu 26.04 LTS | `php8.5-fpm` | `/usr/sbin/php-fpm8.5` | 8.5 | `www-data` |
| Rocky Linux 10 | `php-fpm` | `/usr/sbin/php-fpm` | 8.3 | `nginx` |

The installer disables the package's distribution-wide pool and verifies that
it is inactive and disabled. On Rocky it also removes only the exact
RPM-provided NGINX `Wants=php-fpm.service` drop-in; altered or foreign content
is a conflict rather than an implicit deletion.

This initial matrix deliberately follows each target distribution's native
runtime. Additional repositories and side-by-side runtime packages are not yet
trusted installation inputs.

## Account isolation

For account UID `200123` and PHP 8.4, Stackfort derives these fixed resources:

- configuration: `/etc/stackfort/php/account-200123-php8.4.conf`;
- service: `stackfort-php-8-4-200123.service`;
- socket: `/run/stackfort-php/account-200123-php8.4.sock`;
- PID file: `/run/stackfort-php/account-200123-php8.4.pid`.

The FPM worker runs as the account UID/GID. Only the distribution's NGINX user
owns the mode-`0600` Unix socket. The service belongs to the account's existing
systemd slice, so its CPU, memory, I/O, and task use is included in the same
account budget as future jobs and OCI applications.

The generated systemd unit uses a closed executable path and a hardened
sandbox. Its FPM master retains only the capabilities needed to change to the
account worker identity, own the socket, and terminate workers. The pool uses
`pm=ondemand`, a bounded child count, a request timeout, worker recycling,
cleared environment variables, `.php` script restriction, and a forced PHP
memory limit. The current domain workflow uses four children and 128 MiB per
child; the typed boundary accepts only 1–128 children and 16–2048 MiB.

Document-root intent distinguishes `static` from `php`. On Rocky, static roots
remain read-only `httpd_sys_content_t`, while PHP roots receive the persistent
`httpd_sys_rw_content_t` label required for account-owned writes. A root shared
by a static and a PHP domain is promoted to PHP access.

## Reconciliation order

A domain desired-state document contains the canonical document roots and all
referenced PHP versions. Activation follows a two-phase pool contract:

1. Ensure document roots and their access mode.
2. Add or repair every PHP pool referenced by the new NGINX revision without
   removing any old pool.
3. Transactionally validate and activate the complete NGINX revision.
4. Reconcile the exact PHP set and retire versions no longer referenced.
5. Confirm the domain state in the control database.

This order prevents NGINX from pointing at a missing socket and prevents an old
pool from disappearing while the old NGINX revision may still be active. Pool
configuration/unit writes are atomic and conflict-safe. Syntax, active/enabled
state, account-slice placement, configuration metadata, unit metadata, and
socket ownership/mode are verified. If activation fails after it began, the
reconciler stops newly introduced units, restores the prior files and service
state, and reloads systemd.

Domain removal is non-destructive for account content. Once NGINX no longer
references the pool, the exact reconcile stops/disables its unit and removes
only Stackfort-owned configuration, unit, socket, and PID artifacts.

## Qualification evidence

The same release archive passed fresh installation, no-op rerun, mandatory
access-control checks, real PHP execution, cross-account file denial,
account-owned writes, socket/worker ownership, cgroup placement, bounded
aggregate pool accounting, and exact missing-state reporting after retirement
on Debian 13, Ubuntu 26.04 LTS, and Rocky Linux 10. See the
[PHP Hyper-V qualification result](../infra/host-tests/results/2026-08-25-php-hyper-v.md)
and [ADR 0036](adr/0036-native-account-scoped-php-fpm.md).

Administrator packages and domain creation now expose only the native versions
approved by the current host. The account workspace intersects that list with
its immutable package assignment for static/PHP create and edit controls and
shows tenant-scoped state plus aggregate memory, CPU time, and process count.
Unit, socket, PID, configuration, cgroup, and process details remain inside the
agent. See [Account PHP controls and health](account-php-controls.md).
