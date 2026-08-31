# Secure phpMyAdmin signon

Status: implemented, accessibility-tested, and VM-qualified on 2026-08-26

J-004 adds account-scoped automatic phpMyAdmin login without sending a
database password to the browser. An account owner selects one active managed
database user. Stackfort then exchanges a short-lived one-time handoff through
a dedicated local broker and phpMyAdmin's supported `signon` authentication
mode.

## Browser and control-plane flow

`POST /api/v1/accounts/{accountID}/database-users/{userID}/phpmyadmin-handoffs`
requires an active account grant, `account.resources.manage`, CSRF proof, and
recent authentication. The database user must be active and belong to the
same account. The response contains only a 43-character random handoff token,
its expiry, and the fixed launcher path.

The web application submits the token in a transient same-origin POST form to
`/phpmyadmin/stackfort-launch.php`. It is never placed in a URL, browser
storage, application state, analytics, or an audit detail. The launcher puts
it in a `Secure`, `HttpOnly`, `SameSite=Strict` cookie scoped to
`/phpmyadmin/`, then redirects to phpMyAdmin.

The signon script clears that cookie before redemption. It sends the token to
the fixed `127.0.0.1:8081` broker using an HMAC-authenticated request. The
broker atomically consumes the handoff and returns the managed `localhost`
principal and password only to the isolated PHP process. phpMyAdmin uses the
credential for its MariaDB connection; Stackfort administrative credentials
are never available to it.

## Handoff properties

- 32 cryptographically random bytes encoded as unpadded base64url;
- a 30-second lifetime and fixed `phpmyadmin-signon-v1` audience;
- bound to account, database user, identity, and current panel session;
- only SHA-256 stored in SQLite;
- issuing a replacement revokes the prior unconsumed handoff;
- redemption, expiry, replay, revoked session, stale authentication, retired
  user, or account-grant removal fails closed;
- successful issue and redemption are audited without token or password.

## Runtime isolation

The API alone can decrypt the database credential. A separate HTTP server
binds exactly to loopback port 8081 and accepts only the fixed redeem path,
bounded JSON, and a valid HMAC made with the 32-byte installer-owned broker
key. It emits no credential-bearing logs.

phpMyAdmin runs in `stackfort-phpmyadmin.service` as the unprivileged
`stackfort-pma` identity, not in an account PHP pool or the distribution's
global pool. The service retains `NoNewPrivileges=yes`, an empty capability
bounding set, a strict read-only system view, private devices and temporary
storage, a restricted address-family set, and an IP policy that permits only
localhost. Its FPM socket is mode `0660` for `stackfort-pma` and the NGINX
worker group; NGINX receives no access to the broker key.

Sessions, upload temporary files, and the blowfish secret live under the
dedicated state root. PHP `open_basedir`, disabled process functions, secure
session flags, request timeouts, and bounded upload/memory settings are fixed
by the installer. NGINX exposes only the launcher, reviewed PHP entry points,
and static assets; setup, libraries, templates, vendor internals, and examples
are denied directly.

## Distribution sources

Debian 13 and Ubuntu 26.04 use their patched native phpMyAdmin packages so the
application matches their PHP 8.4 and PHP 8.5 runtimes. Rocky Linux 10 uses
the official phpMyAdmin 5.2.3 all-languages archive bundled into the Stackfort
release because the EPEL package would introduce an Apache and alternate PHP
stack. The build downloads that archive from `files.phpmyadmin.net`, verifies
the pinned SHA-256 before extraction, rejects traversal and symlink entries,
and removes setup/examples.

## Verification

Unit and HTTP tests cover digest-only persistence, expiry, replay,
replacement, audience and tenant boundaries, revoked sessions, HMAC
authentication, loopback enforcement, response redaction, CSRF behavior,
one-origin form submission, and bilingual accessible UI states. Production
build, type checking, i18n validation, Go tests, and vet pass.

The same release archive was installed from a clean checkpoint on Debian 13,
Ubuntu 26.04, and Rocky Linux 10. The harness verifies native versus bundled
package selection, exact secrets and file modes, the unprivileged FPM user and
socket, vendor-pool retirement, loopback broker binding, PHP syntax, launcher
redirect headers, AppArmor/SELinux policy, firewall state, installer no-op
replay, and the complete existing destructive integration suite.

See [ADR 0039](adr/0039-session-bound-phpmyadmin-signon.md), the
[database lifecycle](account-database-lifecycle.md), and the
[Hyper-V result](../infra/host-tests/results/2026-08-26-phpmyadmin-hyper-v.md).

