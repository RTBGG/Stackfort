# Custom panel hostname and automatic HTTPS

After installation, Stackfort can serve its panel at `https://panel.example.com/`
on standard port 443. The original `https://<server-address>:8443/` remains a
fallback with its local bootstrap certificate. Configuration is currently
through a root console/SSH command, not a browser settings form.

## Automatic Let's Encrypt certificate

1. Point the hostname's public A record at this server. Any AAAA record must
   also reach this server's IPv6 address. A CNAME is fine if its final addresses
   are correct. Do not add the panel hostname as a tenant website.
2. Allow inbound TCP 80 and 443 through provider/host firewalls and router/NAT.
   HTTP-01 requires public port 80 during issuance **and renewals**. Avoid a
   DNS/CDN proxy that intercepts the validation path during setup.
3. Review [Let's Encrypt's subscriber agreement](https://letsencrypt.org/repository/),
   then run this with your actual hostname and contact email:

   ```sh
   sudo /usr/local/sbin/stackfort-installer panel issue \
     --hostname=panel.example.com \
     --email=admin@example.com \
     --accept-terms --yes
   ```

Stackfort registers a separate root-managed panel ACME account, generates keys,
serves HTTP-01, obtains and verifies the certificate, tests/reloads NGINX and
checks the live HTTPS API. No Certbot, DNS API credentials or external reverse
proxy are needed. Let's Encrypt receives the hostname and registration contact;
public certificates expose their DNS names in Certificate Transparency logs.

During initial issuance only validation tokens are served on HTTP; other
requests receive 503 until HTTPS is ready. Afterwards HTTP redirects to the
fixed HTTPS hostname, except for validation requests. Same-host renewals retain
the working HTTPS listener. Changing the hostname can briefly remove the old
custom origin during validation; 8443 is unchanged. Cookies are host-only:
sign in again and create new phpMyAdmin launch handoffs at the new hostname.

## Renewal and status

The installer enables `stackfort-panel-renew.timer`: twice-daily checks with up
to one hour of randomized delay and persistent catch-up. An unconfigured panel
causes no CA request. Renewal starts in the final third of the certificate's
actual lifetime, at most 30 days before expiry. Every issuance attempt is
durably limited to once per hour, including failures; the CA can impose further
limits. A failed renewal retains the old certificate. Inspect failures before
that certificate expires.

```sh
sudo /usr/local/sbin/stackfort-installer panel status
systemctl status stackfort-panel-renew.timer
sudo journalctl -u stackfort-panel-renew.service
sudo /usr/local/sbin/stackfort-installer panel renew --yes
```

`status --format=json` reports hostname, expiry, renewal policy and recovery
state, not keys, challenge responses or contact email. `renew` does nothing
while the certificate is not due; there is no force/rate-limit bypass. The
first implementation fixes the account contact: a different email is rejected
rather than silently changing an existing ACME account.

## Import an externally managed certificate

Use a canonical lowercase ASCII/punycode hostname, without scheme, port, path,
wildcard or trailing dot. The full chain must be trusted by the server, match
the hostname/key, permit server authentication and remain valid for over 24 hours.

```sh
sudo /usr/local/sbin/stackfort-installer panel configure \
  --hostname=panel.example.com \
  --certificate=/root/panel-fullchain.pem \
  --private-key=/root/panel-private-key.pem --yes
```

Inputs must be root-owned regular files under non-writable root-owned
directories. The key must be mode 0600 or stricter. Symlinks, including Certbot
`live/` links, are rejected: copy resolved files to secure root-owned staging
paths first. Import creates one root-only certificate/key bundle; NGINX never
uses the original paths. Repeat after each external renewal. Import disables
Stackfort's automatic panel renewal; `panel issue` opts back in.

## Disable and recover

```sh
sudo /usr/local/sbin/stackfort-installer panel recover --yes
sudo /usr/local/sbin/stackfort-installer panel disable --yes
```

Syntax, reload or HTTPS health failures restore the prior generated include.
A root-owned journal protects interrupted transitions: the next mutating panel
command, including the renewal timer, restores prior configuration first.
Until recovery completes, new tenant NGINX activations are blocked. A restart
during issuance can leave only the challenge listener temporarily; the original
8443 access remains available. Use `recover` explicitly for immediate recovery.

Disable removes only the custom-origin include and stops future issuance. It
does not revoke certificates, delete ACME identity/consent, or remove retained
root-only certificate bundles. Bundles are kept for recovery; automatic pruning
is not yet implemented. Panel commands, including timer renewals, also take the
installer/updater locks; active or interrupted platform work must finish/recover
before a panel change can proceed. Lock contention fails instead of waiting.

## Boundaries and qualification

- The unknown-host rejecting default on 443 remains intact. UI and API share
  one origin; the actual API still listens only on loopback 8080.
- Active tenant names, aliases and overlapping wildcard vhosts are rejected.
  A tenant operation queued before reservation cannot shadow the panel later;
  it fails at host activation. Changes share the tenant NGINX activation lock.
- Only exact generated configuration is accepted, with bounded root-owned,
  non-symlink files, content-addressed PEMs and atomic include replacement.
- ACME is fixed to production Let's Encrypt HTTPS, without redirects or an
  environment proxy, and with response/time bounds. There is no production
  option for arbitrary CA URLs or skipped certificate verification.
- Private fixture-CA and real-NGINX tests cover challenge delivery, HTTPS UI/API,
  renewal decisions, conflicts and rollback. They do not constitute live public
  CA issuance for your DNS name, full release qualification or an independent audit.

The unpublished `0.1.0-beta.3` candidate passed native installer, panel and
selected hosting regressions on Debian 13, Ubuntu 26.04 and Rocky Linux 10;
see the [artifact-bound qualification](../infra/host-tests/results/2026-09-08-panel-hostname-candidate.md).
No public release or tag was created by that qualification.

See [installed ingress](installed-panel-ingress.md),
[installation](installer-installation.md) and
[ADR 0064](adr/0064-root-managed-panel-hostname-and-acme.md).
