# ADR 0021: Stackfort-owned NGINX main configuration

Status: accepted

## Context

Debian, Ubuntu, and Rocky ship different NGINX main configurations, include
trees, worker identities, package modules, and service preflight commands.
Editing those files in place would make ownership ambiguous and upgrades prone
to merge conflicts. Including customer configuration in the panel path or
trusting forwarded addresses from public peers would also cross security
boundaries.

Stackfort initially supports fresh hosts. It must reject an existing live web
server rather than guessing whether taking over ports 80 and 443 is safe.

## Decision

1. Validate the vendor `/usr/sbin/nginx` binary and `nginx.service`, but run it
   with Stackfort's independent `/etc/nginx/stackfort/nginx.conf` selected by a
   marker-owned systemd drop-in.
2. Permit initial adoption only while the vendor service is inactive and no
   foreign service drop-in or managed-name collision exists.
3. Separate root-owned `panel-enabled` and `sites-enabled` include directories;
   never expose arbitrary NGINX source or a caller-selected path through RPC.
4. Install rejecting default HTTP and HTTPS servers. Trust forwarded client
   addresses only from the two local loopback hops.
5. Snapshot candidate changes, atomically write fixed content, execute the
   managed `nginx -t`, enable/activate only after validation, and restore prior
   filesystem and enablement state on any failure.
6. Place `nginx.service` in `stackfort-core.slice` and preserve Rocky's fixed
   stale-PID cleanup before the service-context configuration test.

## Consequences

- Distribution package upgrades may replace vendor files without overwriting
  Stackfort's main configuration.
- Administrators cannot mix arbitrary NGINX service drop-ins with the managed
  baseline; a conflict must be resolved explicitly.
- The package must keep `/usr/sbin/nginx`, `/usr/bin/systemctl`, and
  `/usr/bin/rm` at the supported fixed paths.
- Baseline rollback is independent from the revisioned activation workflow.
  F-003 now adds generated-domain staging, health checks, and atomic revision
  promotion without changing baseline ownership.
- Changing the local proxy topology is a reviewed baseline change, not a user
  setting.

## Rejected alternatives

- Editing `/etc/nginx/nginx.conf` or distribution `conf.d`/`sites-enabled`
  trees would couple Stackfort to vendor layout and ownership.
- Adopting an active server based only on a successful syntax check would risk
  taking over unrelated sites and ports.
- A fallback self-signed certificate for unknown HTTPS hosts would complete a
  TLS session unnecessarily; supported NGINX versions can reject the handshake
  without one.
- Trusting an RFC1918 range or all peers would allow direct clients on reachable
  networks to forge forwarded client addresses.
