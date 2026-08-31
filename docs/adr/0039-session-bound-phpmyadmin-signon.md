# ADR 0039: Exchange session-bound one-time phpMyAdmin handoffs

- Status: Accepted
- Date: 2026-08-26

## Context

An account owner should open phpMyAdmin as one selected managed database
principal without copying its password again. Supplying that password to
JavaScript, a query string, a general-purpose session store, or a phpMyAdmin
process shared with customer PHP would expand the credential boundary. Giving
phpMyAdmin a MariaDB administrative account would also bypass Stackfort's
tenant model.

## Decision

1. Allow signon only for an active managed database user owned by the selected
   account and only from an active, recently authenticated panel session with
   the account resource-management capability.
2. Issue 32 random bytes with a 30-second lifetime. Persist only their SHA-256
   digest and bind the row to the fixed audience, account, database user,
   identity, and panel session. Atomically consume it once.
3. Return the opaque handoff to the browser and submit it in a transient
   same-origin POST form. Store it only in a secure, HTTP-only, strict-same-site
   cookie scoped to the phpMyAdmin path, and clear the cookie before exchange.
4. Keep database credential decryption in the Go control process. Expose one
   bounded redeem operation on fixed loopback port 8081, authenticated with an
   installer-created 32-byte HMAC key shared only with the dedicated
   phpMyAdmin runtime.
5. Run phpMyAdmin as the unprivileged `stackfort-pma` identity in a dedicated,
   capability-free, `NoNewPrivileges` FPM service. Share only its mode-0660
   Unix socket with NGINX.
6. Use distribution-patched native phpMyAdmin on Debian and Ubuntu. Bundle the
   official, hash-pinned upstream release on Rocky to avoid an unrelated
   Apache/PHP dependency stack.

## Consequences

- The browser never receives the database password and a leaked handoff has a
  small, session-bound, one-use window.
- phpMyAdmin has one tenant principal's MariaDB authority, never root or a
  panel service credential.
- The loopback HMAC is defense in depth in addition to firewall, systemd IP
  policy, AppArmor/SELinux, file modes, and process identity.
- Signon depends on the control API being available. Failure returns no
  credential and cannot fall back to a privileged account.
- Password rotation revokes outstanding handoffs and invalidates existing
  non-persistent phpMyAdmin database credentials; J-005 implements and
  qualifies that lifecycle separately.
