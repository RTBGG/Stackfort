# Installed panel ingress

The fresh-host installer now publishes the immutable web application and the
loopback-only control API as one same-origin HTTPS management endpoint on TCP
port `8443`.

## Initial access

Open the following URL after installation, substituting an address of the
server:

```text
https://<server-address>:8443/
```

The installer generates a local P-256 certificate because a new server does
not yet have a configured panel hostname or an ACME account. It contains the
installation-time hostname, loopback names/addresses, and the host's current IP
addresses. The browser will not trust this certificate automatically. Verify
that the address belongs to the intended server before accepting the warning.
The certificate is transport protection for first setup, not proof of the
server's public identity. A future panel-hostname workflow will replace it with
a publicly trusted certificate.

Create the short-lived, single-use administrator capability over the already
authenticated server console or SSH session:

```sh
sudo -u stackfort -- /usr/local/bin/stackfort-api bootstrap create
```

Enter the displayed token in the panel. The token never appears in a URL,
NGINX log, installed file, or API response.

## NGINX boundary

The root-owned fixed configuration
`/etc/nginx/stackfort/panel-enabled/00-panel.conf`:

- listens only on the dedicated TLS port `8443` and does not weaken the
  rejecting unknown-host server on customer port `443`;
- serves immutable assets from `/usr/share/stackfort/web` with an SPA fallback;
- proxies only `/api/` to `127.0.0.1:8080`;
- applies TLS 1.2/1.3, disables session tickets, and uses a strict static-content
  CSP; and
- accepts no user-selected path, upstream, directive, header, or port.

The combined certificate/key PEM is an atomic root-owned mode-`0600` file at
`/etc/stackfort/panel-tls/bootstrap.pem`. NGINX's root master reads it before
workers drop privileges. The installer rejects symlinks, hard links, unsafe
ownership, malformed keys, key/certificate mismatches, invalid signatures, and
unexpected certificate identities. A valid bundle is retained on a no-op run;
an expiring managed bundle is rotated before the 30-day window.

On Rocky Linux, Stackfort installs a local SELinux module that permits the
confined NGINX worker to connect only to the dedicated
`stackfort_api_port_t`; the installer assigns that type only to TCP port
`8080`. It does not enable the broad `httpd_can_network_connect` or
`httpd_can_network_relay` booleans.

## Browser and API behavior

The panel and API share the same origin, so strict host-only session and CSRF
cookies work without CORS or browser token storage. The control API remains
bound to loopback and still derives the request source from its direct NGINX
peer. Per-client trusted-proxy attribution is intentionally a later hardening
item; the persistent global authentication pressure limit remains effective.

The administrator settings page exposes the existing bounded ACME API. It can
register only the compiled-in Let's Encrypt production environment, requires
explicit terms acceptance, sends a CSRF proof and idempotency key, and displays
non-secret registration state. Authority URLs and account keys remain
server-controlled.

## Validation

The installer verifies the generated bundle, exact panel configuration,
vendor `nginx -t`, HTTPS static response, and HTTPS-proxied API health. The
The same release archive passed the disposable installer/no-op/Phase 1 suite
on Debian 13, Ubuntu 26.04 LTS, and Rocky Linux 10.2 with the installed
management endpoint. Frontend tests cover the fixed ACME request, pending-state
guard, localization, and accessibility; the real in-app browser reached the
unmocked bootstrap page through a local SSH-forwarded origin.
