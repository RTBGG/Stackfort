# Performance tests

Performance tests compare static NGINX, uncached PHP-FPM, NGINX FastCGI cache,
and Vinyl Cache with WAF modes isolated. Results include latency percentiles,
throughput, CPU, memory, error rate, and requests reaching the origin.

The Debian-only mod_pagespeed 1.15 evaluation is intentionally separate from
the supported release matrix because the module is proprietary and lacks an
Ubuntu 26.04 package. It compares server throughput and client transfer shape
while proving the requested loopback `MapOriginDomain` and WAF ordering; see
`infra/host-tests/evaluate-pagespeed-nginx.sh`.

Fixtures, host profiles, versions, commands, and raw result data must be retained
so published comparisons are reproducible.
