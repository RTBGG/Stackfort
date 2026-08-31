# mod_pagespeed 1.15 / Cyclone Cache evaluation — 2026-09-01

## Scope

This is an evaluation, not a Stackfort production dependency. The test ran the
vendor's signed, unlicensed `nginx-module-pagespeed` package
`1.15.0-r22~trixie` on the existing Debian 13 Hyper-V guest. The guest had two
virtual CPUs, 4 GiB RAM, stock NGINX `1.26.3-3+deb13u7`, PHP-FPM 8.4, Vinyl
Cache 9.0.1, and the installed Stackfort Coraza/OWASP CRS package.

The repository signing key was checked before repository setup against the
vendor-published RSA-4096 fingerprint
`DF00 E296 BE91 41CE A541 1346 F50D 6054 F107 12A0`. Production use was not
claimed: the responses correctly carried the vendor's unlicensed evaluation
warning.

## Evaluated NGINX design

The repeatable harness uses a fixed loopback-only origin and implements the
requested HTTPS approach 2:

```nginx
pagespeed off;
pagespeed UseNativeFetcher on;
pagespeed NumRewriteThreads 1;
pagespeed NumExpensiveRewriteThreads 1;
pagespeed FetchWithGzip off;
pagespeed FileCachePath /var/lib/stackfort-pagespeed-evaluation/cyclone;
pagespeed FileCacheSizeKb 524288;
pagespeed CycloneRamCacheKb 0;
pagespeed CycloneZeroCopy on;
pagespeed FetcherTimeoutMs 500;
pagespeed RewriteDeadlinePerFlushMs 10;
pagespeed ImplicitCacheTtlMs 300000;
pagespeed HttpCacheCompressionLevel 6;

server {
    # Evaluation listener; only this server overrides the safe global default.
    pagespeed on;
    pagespeed RewriteLevel CoreFilters;
    pagespeed FetchHttps disable;
    pagespeed MapOriginDomain "http://127.0.0.1:9000"
                              "https://pagespeed-evaluation.stackfort.test";
}
```

Global `pagespeed off` is essential. Without it, the module touched unrelated
servers, replaced the origin's public cache lifetime with `max-age=0,
no-cache`, and consequently prevented Vinyl from storing the response. The
global thread counts avoid multiplying an automatic CPU choice across NGINX
workers. The 512 MiB fixed Cyclone file is large enough to receive the
metadata tier while remaining proportionate to this 4 GiB host; the separate
RAM tier stays off because the memory-mapped file already uses the operating
system page cache. Zero-copy is enabled for this performance evaluation, but
is still an upstream opt-in feature.

HTTPS fetching cannot fall back to the public network: `FetchHttps` is
disabled and the one literal public origin maps to the fixed loopback HTTP
listener. PageSpeed admin/statistics locations are not configured. Coraza is
attached to the public server and therefore evaluates the request before the
PageSpeed response filter.

## Method

One dynamic PHP page linked two CSS and two JavaScript files. Every route
returned the same application content and was warmed before measurement. Each
path then received 3,000 new-connection HTTP requests at concurrency 8 using
ApacheBench:

- direct NGINX to PHP-FPM;
- public NGINX to a warm Vinyl full-page cache;
- a warm native NGINX FastCGI full-page cache; and
- NGINX plus mod_pagespeed CoreFilters and a warm Cyclone resource cache.

The complete matrix ran with WAF off, DetectionOnly PL1, and Blocking PL1. A
mode header had to be observed on eight consecutive new connections after
each graceful reload. An SQL-injection probe had to return 403 only in the
blocking mode on every public route. Separate assertions required `HIT` from
both full-page caches, an `X-Page-Speed: 1.15` marker, at least one rewritten
`.pagespeed.` URL, and successful local-origin fetching. The figures are one
same-guest relative run, not a public capacity claim.

## Results

| WAF mode | Direct PHP RPS / p99 | Vinyl RPS / p99 | NGINX FastCGI RPS / p99 | PageSpeed/Cyclone RPS / p99 | PageSpeed / Vinyl |
| --- | ---: | ---: | ---: | ---: | ---: |
| Off | 24,297.99 / 1 ms | 22,178.19 / 1 ms | 56,726.86 / <1 ms | 3,880.49 / 4 ms | 17.5% |
| DetectionOnly | 2,614.66 / 13 ms | 2,338.51 / 16 ms | 2,692.28 / 8 ms | 1,437.84 / 19 ms | 61.5% |
| Blocking PL1 | 2,579.50 / 10 ms | 2,384.16 / 14 ms | 2,660.72 / 9 ms | 1,393.35 / 20 ms | 58.4% |

For the small client fixture, CoreFilters inlined both CSS files and combined
the two JavaScript files. The warm page shape fell from five requests and
1,057 body bytes to two requests and 900 body bytes: 60% fewer requests and
14.9% fewer body bytes. This is a real browser-delivery benefit, but it is not
full-page caching.

## Decision

mod_pagespeed/Cyclone does not replace Vinyl or NGINX FastCGI cache. Cyclone
caches optimization inputs, metadata, and generated resources; the dynamic
HTML still reaches PHP-FPM and passes through PageSpeed's HTML rewrite filter
on every request. That distinction explains why PageSpeed improves the client
resource shape while delivering only 17.5% of Vinyl's WAF-off request
throughput and 58.4–61.5% with Coraza active. NGINX FastCGI cache remains the
throughput winner in every mode.

The condition for removing Vinyl—PageSpeed outperforming it—was not met, so
Vinyl is not removed by this change. The module is also not added to the
Stackfort installer: its source is not public, production use requires a
commercial per-site/hoster license, its NGINX binary is coupled to the exact
distribution NGINX build, and the vendor currently publishes no Ubuntu 26.04
package. Making it a core dependency would break Stackfort's free, uniform
three-distribution installation contract.

The evaluation did expose and fix two native NGINX issues: the Stackfort
service now has a file-descriptor limit that actually supports its 4,096
worker connections, and reloads validate the active managed configuration
instead of the unused distribution configuration.

## Reproduction

The guest must be disposable and must already contain the exact PageSpeed,
WAF, Vinyl, PHP-FPM, and ApacheBench packages described above. Then run:

```sh
sudo bash infra/host-tests/evaluate-pagespeed-nginx.sh
```

The script owns only its fixed evaluation directory and temporary NGINX
fragment, removes the fragment on exit, reloads the original configuration,
and stops PHP-FPM if it started that service.
