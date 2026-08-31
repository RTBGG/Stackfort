# Managed NGINX baseline

F-001 provides the first public data-plane host mutation. The privileged agent
validates an installed NGINX package, owns one isolated configuration root, and
activates it through the distribution's `nginx.service`. Package installation
remains an installer responsibility; the disposable-host harness installs the
vendor package before exercising the same production reconciler.

## Owned layout

```text
/etc/nginx/stackfort/
├── .stackfort-managed
├── nginx.conf
├── global/
│   └── trusted-proxies.conf
├── default/
│   ├── 00-default-http.conf
│   └── 10-default-https.conf
├── panel-enabled/
├── sites-enabled/
│   └── 00-current.conf
├── sites-current -> site-revisions/<active UUIDv7>
├── site-revisions/
├── site-transactions/
├── site-activation.json
└── .site-activation.lock

/etc/systemd/system/nginx.service.d/
└── 20-stackfort.conf
```

The managed root is root-owned mode `0755` so the unprivileged Coraza workers
can traverse its fixed SecLang include chain. The virtual-host include
directories remain root-owned mode `0750`. Configuration is root-owned mode
`0640`; the systemd drop-in is `0644`.
Hosting-account users cannot write or traverse either include directory.
Panel and customer virtual hosts have separate include points, and future
renderers may write only fixed, root-owned files into the appropriate one.

The installer now owns `panel-enabled/00-panel.conf` as a fixed HTTPS
management listener on port `8443`; see
[Installed panel ingress](installed-panel-ingress.md). It does not alter the
customer-port defaults described below.

Stackfort does not edit or include the distribution's main configuration.
Instead, the systemd drop-in replaces `ExecStartPre` and `ExecStart` with fixed
commands that select `/etc/nginx/stackfort/nginx.conf`. The service joins
`stackfort-core.slice`. It also replaces `ExecReload`, so a reload validates
the same managed configuration before signalling the active master instead of
validating the unused distribution configuration. The service and NGINX
workers receive a 65,536-file limit; this safely covers the configured 4,096
worker connections, including proxied requests that can consume two
descriptors, without inheriting Debian's 1,024-file ceiling. On Rocky, the
preflight first removes only the fixed
`/run/nginx.pid`; this preserves the vendor unit's SELinux-safe stale-PID
behavior after command-line configuration tests.

## Conflict policy

Initial adoption is allowed only when `nginx.service` is loaded but inactive,
the managed root is absent, the Stackfort drop-in name is unused, and no other
NGINX service drop-in exists. An active unmanaged service, a managed path
without the exact ownership marker, a symlink, non-root ownership, unsafe
directory permissions, a hard-linked managed file, or a foreign service
drop-in produces the stable `nginx_conflict` result. Stackfort never silently
adopts such state.

Once owned, reconciliation snapshots every file and directory it may change,
writes files atomically, runs the fixed equivalent of:

```text
/usr/sbin/nginx -t -q -c /etc/nginx/stackfort/nginx.conf
```

and only then enables the service at boot and restarts or gracefully reloads
it. Validation or
activation failure restores the previous snapshots and systemd view. This
transaction protects the baseline itself. F-003 now extends it with revisioned
staging and health-checked activation for generated domain configurations; see
[Transactional NGINX site activation](transactional-nginx-activation.md).

## Default-host and proxy policy

The port-80 default server closes unknown-host requests with NGINX status 444.
The port-443 default uses `ssl_reject_handshake on`, so it needs no fallback
certificate and rejects unknown SNI during the TLS handshake. These servers are
not catch-all application sites.

Only `127.0.0.1` and `::1` are trusted as address-replacing proxy hops. The
baseline uses `X-Forwarded-For` with recursive processing; a public peer cannot
spoof its client address because it is not in `set_real_ip_from`. Future Vinyl
or local-proxy topology changes must update this fixed allowlist deliberately
and add integration coverage.

The global `stackfort_redacted` JSON access format stores only timestamp,
client address, normalized host, method, `$uri` path, status, response bytes,
and request duration. It never references `$args`, `$request`, authorization or
cookie headers, referrer, or user agent. Generated customer virtual hosts write
to root-owned per-account, per-domain files whose domain component is a SHA-256
digest. See [Domain log observability](domain-log-observability.md).

## Typed agent boundary

`web.nginx-baseline.reconcile` is a global privileged mutation. Its request has
no path, unit, executable, argument, proxy range, or configuration-text field.
It requires durable audit correlation with no account ID and returns exact
managed paths, the two trusted hops, validation/service state, and a typed
capability. Stable failures distinguish unavailable NGINX/platform support,
host-state conflict, configuration validation, and an internal activation
failure.

## Verification

Unit tests cover byte-stable rendering, distribution worker users, unknown-host
defaults, trusted-hop restrictions, fixed process profiles, strict protocol
unions, inactive-only adoption, rollback, and idempotency. The opt-in Hyper-V
test runs the real reconciler twice on Debian 13, Ubuntu 26.04, and Rocky Linux
10, and verifies:

- both configuration tests succeed, the service is enabled at boot, and the
  second reconcile changes nothing;
- managed paths have exact root ownership and modes;
- trusted-proxy content is byte-identical to the loopback-only baseline;
- an unknown HTTP host receives no response body and unknown HTTPS SNI cannot
  complete a handshake; and
- `nginx.service` runs below
  `/stackfort.slice/stackfort-core.slice/nginx.service`.

Upstream references:

- [NGINX request processing and default servers](https://nginx.org/en/docs/http/request_processing.html)
- [NGINX server names](https://nginx.org/en/docs/http/server_names.html)
- [`ngx_http_ssl_module` and `ssl_reject_handshake`](https://nginx.org/en/docs/http/ngx_http_ssl_module.html)
- [`ngx_http_realip_module`](https://nginx.org/en/docs/http/ngx_http_realip_module.html)
- [NGINX command-line parameters](https://nginx.org/en/docs/switches.html)
- [NGINX configuration reload behavior](https://nginx.org/en/docs/beginners_guide.html)
