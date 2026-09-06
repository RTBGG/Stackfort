# Performance tests

Performance tests compare static NGINX, uncached PHP-FPM, NGINX FastCGI cache,
and Vinyl Cache with WAF modes isolated. The current Go records contain latency
percentiles, throughput, bytes/s, request count, concurrency, and duration;
timed request errors fail the test. CPU/memory profiles and origin-request
totals require additional instrumentation and are not claimed by these records.

Start with the [methodology, published results, and reproduction guide](../../docs/benchmarks.md).

The Debian-only mod_pagespeed 1.15 evaluation is intentionally separate from
the supported release matrix because the module is proprietary and lacks an
Ubuntu 26.04 package. It compares server throughput and client transfer shape
while proving the requested loopback `MapOriginDomain` and WAF ordering; see
[evaluation harness](../../infra/host-tests/evaluate-pagespeed-nginx.sh).

Fixtures, host profiles, versions, commands, and raw result data must be retained
so published comparisons are reproducible.
