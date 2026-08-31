# ADR 0002: Use NGINX as the web server and Vinyl as an optional cache

Status: Accepted

## Context

The panel must serve static sites, arbitrary PHP applications, and account-owned
OCI projects. It also needs TLS, safe reloads, redirects, WAF integration, and
high performance. Vinyl Cache is a specialized HTTP accelerator, does not replace
an origin web server, and currently requires separate TLS termination.

## Decision

Use NGINX for public TLS termination, WAF integration, static delivery, and
routing, plus a local NGINX origin for PHP-FPM behavior where appropriate. Place
Vinyl between edge and origin only for domains with an explicitly selected safe
cache profile. Keep direct NGINX routing available.

## Consequences

- Static sites avoid an unnecessary cache hop.
- Unknown PHP/OCI applications remain correct by default with caching off.
- Coraza can inspect requests before cached content is served.
- The system carries additional complexity when Vinyl is enabled.
- Vinyl version compatibility and distribution packages must be actively
  maintained.
- NGINX FastCGI cache remains a benchmark comparator and possible fallback.
