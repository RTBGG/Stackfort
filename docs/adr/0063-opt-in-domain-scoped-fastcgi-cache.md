# ADR 0063: Opt-in, domain-scoped NGINX FastCGI cache

- Status: accepted
- Date: 2026-09-06

## Decision

Keep caching disabled by default. PHP domains may select Vinyl's existing
`respect_origin`/`wordpress` presets or native NGINX
`fastcgi_respect_origin`/`fastcgi_wordpress`. Engines are mutually exclusive:
native caching uses the public NGINX FastCGI location and account PHP socket,
without a Vinyl hop or extra origin listener. Coraza remains before cache hits.

The respect-origin preset requires explicit positive max-age/s-maxage. The
WordPress preset allows a 120-second fallback for anonymous pages. Both require
bodyless GETs without cookies, authorization, query strings, cache overrides,
ranges, encoded paths or known sensitive endpoints; only 200 responses can be
stored. Preserve NGINX's Cache-Control, Expires, Set-Cookie and Vary handling.
Ignore X-Accel-Expires so an application cannot override this policy with it.
Use cache locking; do not serve stale content or update cached pages in the
background. Operators must validate their application's personalization and
Vary headers before opting in; a generic preset cannot identify every private
route or application-specific request header.

Keys bind account, domain, server-generated UUIDv7 generation, scheme, actual
host and original URI. Persist the generation in migration 029 and desired-state
snapshots (lifecycle schema 6). Every domain edit rotates it; retries reuse the
same mutation. Disabling/re-enabling cannot resurrect the previous namespace.
Account clients cannot submit arbitrary keys, directories, NGINX text or VCL.

OSS NGINX does not provide the commercial cache-purge directive. Full-domain
purge therefore queues a typed domain-lifecycle operation: recheck the active
engine, rotate the namespace, validate the complete candidate, reload, health
check, and commit through the existing rollback-capable activation. Another
domain's generation is unchanged. Prefix purges remain Vinyl-only. Old cache
files expire asynchronously; logical invalidation is not immediate disk erasure.

## Resource boundary

The fixed `/var/cache/stackfort-fastcgi` directory is worker-owned, mode 0700,
under root-owned `/var/cache`; reject unexpected ownership, permissions and
symlinks. Rocky uses a narrow `httpd_cache_t` label, without disabling SELinux.
The shared cache uses 16 MiB of keys, 512 MiB `max_size`, 1 GiB `min_free`,
10-minute inactivity and `use_temp_path=off`. NGINX's cache-manager limits are
asynchronous, **not** hard per-account quotas or an absolute disk-write ceiling.
Monitor free space. Fair per-tenant cache allocation and physical purge remain
future work; account CPU/RAM limits do not meter shared NGINX cache work.

## Verification and limits

Use production renderers and lifecycle operations in the existing three-OS
cache harness, replacing its temporary FastCGI-only benchmark listener. Check
bypass/storage rules, host separation, toggle, whole-domain purge, metrics and
a scanner request against an already warm key in every WAF mode. Preserve
Vinyl's exception/prefix-purge regression tests. Keep compiler/unit checks for
closed intent, old schemas, generation replay and confined directory ownership.

These short loopback tests establish behavior and a regression baseline, not
production readiness, TLS performance or a statistically robust engine ranking.
Stackfort remains pre-beta. PageSpeed/Cyclone is still evaluation-only. Existing
Vinyl sites continue to work; this change does not remove that optional engine.

Sources: [NGINX FastCGI module](https://nginx.org/en/docs/http/ngx_http_fastcgi_module.html)
and [cache foundation](../cache-foundation.md).
