# Control API

This directory will contain the unprivileged Go API. Its module path is
`github.com/RTBGG/stackfort` and its installed executable is `stackfort-api`.

The current implementation opens and migrates the private SQLite panel state,
then exposes health, build provenance, and a tightly bounded first-administrator
bootstrap surface on `127.0.0.1:8080`. Health returns `503` if storage is
unavailable. Bootstrap is not normal authentication and creates no session, so
the API must not be exposed directly to an untrusted network.

Linux state defaults to `/var/lib/stackfort/stackfort.db`. For local development,
`STACKFORT_STATE_PATH` may specify another absolute local path. See
[`docs/persistence.md`](../../docs/persistence.md) for database invariants and
backup behavior.

Create the one-time administrator capability locally:

```sh
stackfort-api bootstrap create
```

The default lifetime is 15 minutes. `--ttl=10m` selects another lifetime from
one minute through one hour; `--replace` explicitly invalidates an outstanding
capability. See
[`docs/administrator-bootstrap.md`](../../docs/administrator-bootstrap.md).

After bootstrap, the API provides JSON password login, current-session, and
CSRF-protected logout endpoints. Session bearers are accepted only from strict,
host-only, secure cookies and are stored only as digests. The first protected
account endpoint applies current platform/account roles, lifecycle state, and
recent-authentication policy while hiding cross-tenant resources. See
[`docs/password-authentication-and-sessions.md`](../../docs/password-authentication-and-sessions.md)
and [`docs/authorization-policy.md`](../../docs/authorization-policy.md).

The API owns authentication, authorization, desired state, jobs, audit records,
and status presentation. It must not contain a generic shell executor or require
root privileges.
