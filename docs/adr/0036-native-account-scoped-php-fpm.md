# ADR 0036: Native account-scoped PHP-FPM

Status: accepted

## Context

PHP must share the static-domain lifecycle without giving panel users raw FPM,
systemd, executable, or filesystem-path input. A distribution-wide `www` pool
would mix tenants, sit outside each account's cgroup budget, and expose one
shared socket. Installing several third-party PHP repositories immediately
would also expand the package trust and upgrade surface before the basic
runtime contract is qualified.

NGINX activation and pool retirement form one ordering problem: a new NGINX
revision must not reference a socket that does not exist, while an old socket
must remain available until no active NGINX revision references it.

## Decision

1. Use the target distribution's approved native PHP-FPM package: PHP 8.4 on
   Debian 13, PHP 8.5 on Ubuntu 26.04, and PHP 8.3 on Rocky Linux 10.
2. Disable the package's distribution-wide pool and create one Stackfort-owned,
   version-specific systemd service and Unix socket per hosting account.
3. Derive all executable, configuration, unit, PID, and socket paths from a
   closed distribution/version matrix and the validated hosting identity.
4. Run FPM workers as the hosting UID/GID, expose the mode-`0600` socket only to
   NGINX, and place the service in the account's existing systemd slice.
5. Generate a fixed hardened FPM preset and systemd sandbox from bounded child
   and memory limits; do not accept raw PHP/FPM directives.
6. Reconcile additively before NGINX activation and authoritatively after it,
   retiring absent pools only after the new NGINX revision is live.
7. Treat existing non-Stackfort artifacts or altered package-integration files
   as conflicts. Roll back files and prior unit state when activation fails.
8. Mark PHP document roots separately from static roots so Rocky receives a
   narrowly writable persistent SELinux type only where PHP requires it.

## Consequences

- PHP worker identity, Unix-socket access, and cgroup accounting are isolated
  per hosting account and directly verifiable.
- A failed or replayed domain operation converges without an NGINX-to-socket
  race and does not retire the predecessor pool too early.
- The initial release offers one runtime per distribution, not identical PHP
  versions across all supported hosts. Package limits must advertise only the
  host's approved native version.
- Adding third-party repositories or parallel versions later requires a new
  trust, update, compatibility, and qualification decision.
- Pool health/usage UI and user-selectable PHP target controls remain separate
  product work even though the typed API and host lifecycle are available.

## Rejected alternatives

- A single distribution-wide FPM pool does not provide the required tenant
  identity, socket, or account-budget boundary.
- Running FPM inside a container for every account would add image lifecycle,
  storage, networking, and patching complexity to the first PHP slice.
- Removing old pools before NGINX activation can break the still-active
  predecessor configuration; removing them only on a later cleanup job leaves
  unbounded stale privileged services.
- Accepting arbitrary `php.ini`, pool, unit, binary, or socket values would turn
  the privileged local agent into a configuration and command-injection path.
