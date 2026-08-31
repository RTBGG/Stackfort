# ADR 0056: Do not adopt proprietary mod_pagespeed as Stackfort's core cache

- Status: accepted after evaluation
- Date: 2026-09-01
- Relates to: [ADR 0052](0052-opt-in-vinyl-cache-behind-nginx-and-coraza.md)

## Context

mod_pagespeed 1.15 adds a maintained NGINX module and Cyclone Cache. It can
rewrite HTML, CSS, JavaScript, and images at response time, and HTTPS approach
2 maps public HTTPS resource URLs to a same-host HTTP origin. This appeared to
offer a simpler, faster replacement for Vinyl.

The products do not implement the same cache layer. Vinyl and NGINX FastCGI
cache can retain complete application responses. Cyclone retains PageSpeed's
resource fetches, metadata, and optimized outputs; it does not prevent PHP
from generating dynamic HTML on each request.

There are also distribution and licensing constraints. The 1.15 module source
is not public, production use requires a commercial license per site or a
hoster agreement, and each NGINX module build is pinned to the exact stock
NGINX binary. The vendor supports Debian 13 and Rocky Linux 10 but does not
currently publish an Ubuntu 26.04 NGINX package.

## Decision

1. Stackfort does not install, redistribute, load, or require mod_pagespeed
   1.15 in the product or release artifact.
2. A Debian 13 evaluation harness is retained so the decision can be repeated
   against future versions without presenting the proprietary module as a
   supported feature.
3. The harness uses global `pagespeed off`, enables CoreFilters only on the
   test vhost, maps one literal HTTPS origin to `127.0.0.1`, disables public
   HTTPS fetching, leaves admin/statistics handlers unset, and sizes Cyclone
   and rewrite workers explicitly for the 2-vCPU/4-GiB guest.
4. Vinyl is not removed under the requested condition because PageSpeed did
   not outperform it. Vinyl remains optional and disabled by default.
5. Native NGINX FastCGI cache remains the production performance direction.
   A future product preset must retain the existing personalization bypass,
   WAF-before-cache ordering, scoped purge, and cross-domain isolation gates.
6. The NGINX baseline raises both the service and worker file-descriptor limits
   to 65,536 and replaces `ExecReload` so the active managed configuration is
   syntax-checked before reload.

## Consequences

- The free AGPL project keeps a uniform dependency and installer contract
  across Debian 13, Ubuntu 26.04, and Rocky Linux 10.
- Stackfort does not expose a setting that silently incurs a per-site license
  or activates an unlicensed warning on customer responses.
- Automatic resource rewriting, request combining, and image recompression are
  not product features. Operators can still optimize assets in their build
  pipeline or place a separately licensed optimizer/CDN outside Stackfort's
  support boundary.
- The test fixture did reduce five client requests/1,057 body bytes to two
  requests/900 bytes, but PageSpeed reached only 17.5% of Vinyl's throughput
  without WAF and 58.4–61.5% with Coraza active. Native NGINX FastCGI cache won
  every server-throughput comparison.
- The file-limit and reload fixes benefit every Stackfort deployment without a
  proprietary runtime or an additional request hop.

## Reconsideration gate

Reconsider only if distribution/redistribution terms fit an AGPL hosting panel,
all supported Stackfort targets have compatible signed packages, and a fresh
three-guest test demonstrates a worthwhile client-performance improvement
without losing the native NGINX throughput, security, or operability baseline.

## Evidence and upstream references

- [Evaluation method and results](../../infra/host-tests/results/2026-09-01-mod-pagespeed-nginx-evaluation.md)
- <https://modpagespeed.com/1.1/docs/getting-started/>
- <https://modpagespeed.com/1.1/docs/downloads/>
- <https://modpagespeed.com/1.1/docs/caching/>
- <https://modpagespeed.com/1.1/docs/https-configuration/#approach-2-maporigindomain-with-http-backend>
