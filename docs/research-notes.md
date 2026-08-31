# Web Stack Research Notes

Last reviewed: 2026-08-31

This document records upstream facts that materially affect the initial design.
It is not a substitute for pinning and rechecking exact versions during release
qualification.

## Vinyl Cache

- Vinyl Cache is a caching HTTP reverse proxy placed in front of an HTTP origin.
- Vinyl 9.0.1 was released on 2026-05-18 with an upstream EOL date of 2027-03-16.
- The project documents a roughly six-month release cadence.
- Vinyl 9 does not currently terminate TLS directly. Its upstream documentation
  recommends HAProxy for TLS offload/onload and supports a Unix-domain socket
  plus PROXY protocol arrangement.
- As of the review date, upstream does not provide new Vinyl 9 Debian/Ubuntu or
  Red Hat family binary packages, so source or project-owned packaging is needed
  for the requested distributions unless that changes.
- Upstream recommends a Linux `tmpfs` work directory and documents constraints
  around `noexec`, locked memory, and Transparent Huge Pages.
- Built-in caching behavior normally passes requests with cookies or
  `Authorization` and avoids caching responses with `Set-Cookie`; application
  presets still require explicit correctness testing.

Primary sources:

- <https://vinyl-cache.org/releases/>
- <https://vinyl-cache.org/docs/9.0/>
- <https://vinyl-cache.org/docs/9.0/installation/install_debian.html>
- <https://vinyl-cache.org/docs/9.0/installation/install_redhat.html>
- <https://vinyl-cache.org/docs/9.0/installation/platformnotes.html>
- <https://vinyl-cache.org/tutorials/tls_haproxy.html>
- <https://vinyl-cache.org/docs/9.0/users-guide/increasing-your-hitrate.html>

## NGINX

- NGINX provides static serving, TLS, reverse proxying, FastCGI, graceful
  configuration reload, and FastCGI response caching.
- FastCGI cache supports locks, background updates, stale responses, bypass
  conditions, and configurable keys/validity.
- The official HTTP/3 module is documented as experimental, so HTTP/3 should be
  capability-gated until the project's test matrix proves it suitable.
- The coraza-nginx connector integrates NGINX with libcoraza. Version 0.20.0
  defers loading the Go-backed shared library and building WAF objects until
  after the worker fork.

Primary sources:

- <https://nginx.org/en/>
- <https://nginx.org/en/docs/http/ngx_http_fastcgi_module.html>
- <https://nginx.org/en/docs/http/ngx_http_v3_module.html>
- <https://nginx.org/en/docs/control.html>
- <https://github.com/corazawaf/coraza-nginx>

## WAF alternatives

- OWASP CRS paranoia level 1 is the recommended general-purpose starting point;
  higher levels increase false-positive tuning needs.
- Coraza 3.7.0 supports OWASP CRS v4 and the relevant ModSecurity SecLang
  subset. libcoraza 1.7.0 exposes it to native connectors and requires Go
  1.25 or newer; Stackfort pins Go 1.25.12 for the native build.
- coraza-nginx 0.20.0 is upstream's current NGINX connector. Upstream labels
  the connector experimental, so Stackfort keeps WAF opt-in and requires
  exact-ABI builds, real worker requests, attack-corpus tests, and rollback
  tests on every supported distribution.
- A plain `nginx -t` is insufficient for qualification because connector
  configuration files are parsed into WAF objects only in the unprivileged
  worker. Stackfort therefore starts an isolated worker and proves a fixed
  deny rule returns HTTP 403 during package installation.
- The earlier libModSecurity 3.0.16/connector 1.0.4 implementation remains a
  recorded baseline. On the same 3,000-request, concurrency-8 Hyper-V workload,
  Coraza more than doubled enabled-mode throughput on Debian 13, Ubuntu 26.04,
  and Rocky Linux 10. This result, plus the simpler dependency set, caused ADR
  0051 to supersede the engine portion of ADR 0050.

Primary sources:

- <https://coreruleset.org/faq/>
- <https://github.com/corazawaf/coraza/releases/tag/v3.7.0>
- <https://github.com/corazawaf/libcoraza/releases/tag/v1.7.0>
- <https://github.com/corazawaf/coraza-nginx/releases/tag/v0.20.0>
- <https://github.com/coreruleset/coreruleset/releases/tag/v4.25.1>
- <https://go.dev/dl/>
- <https://github.com/corazawaf/coraza-nginx>

## Caddy and FrankenPHP

- Caddy provides automatic HTTPS, reverse proxying, static serving, and PHP-FPM
  integration.
- FrankenPHP combines Caddy and PHP and can improve supported frameworks using
  long-lived worker mode.
- Worker mode keeps application state in memory between requests and requires
  compatible application lifecycle behavior, making it unsuitable as the
  universal default for arbitrary shared PHP sites.

Primary sources:

- <https://caddyserver.com/docs/automatic-https>
- <https://caddyserver.com/docs/caddyfile/directives/php_fastcgi>
- <https://frankenphp.dev/docs/>
- <https://frankenphp.dev/docs/worker/>
- <https://frankenphp.dev/docs/performance/>

## Isolation and limits

- cgroup v2 supports hierarchical CPU, memory, process, BPS, and IOPS control;
  block I/O limits are device-specific and delay I/O after configured limits.
- Docker documentation notes the root daemon attack surface and explains that
  containers otherwise have no resource constraints by default.
- Podman Quadlet integrates rootless containers with systemd and requires cgroup
  v2. Quadlets accept normal systemd resource controls.
- PHP-FPM supports multiple pools with distinct users/groups and Unix sockets.
- XFS supports project quotas suitable for directory trees; exact filesystem and
  mount capabilities must be detected instead of assumed.

Primary sources:

- <https://docs.kernel.org/admin-guide/cgroup-v2.html>
- <https://docs.kernel.org/admin-guide/xfs.html>
- <https://docs.docker.com/engine/security/>
- <https://docs.docker.com/engine/containers/resource_constraints/>
- <https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html>
- <https://www.php.net/manual/en/install.fpm.configuration.php>

## phpMyAdmin and releases

- phpMyAdmin's `signon` authentication mode supports session or script-based
  single sign-on. This enables a short-lived controlled handoff without a MariaDB
  administrative login.
- GitHub supports immutable releases and artifact attestations. Release assets
  should be immutable, attested, checksummed, and verified before installation.

Primary sources:

- <https://docs.phpmyadmin.net/en/latest/config.html>
- <https://docs.phpmyadmin.net/en/latest/setup.html>
- <https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes>
- <https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations>
