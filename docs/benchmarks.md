# Benchmark methodology and results

The published numbers are **local regression and relative-comparison evidence**,
not server-sizing promises. They measure small fixtures on disposable Hyper-V
guests, usually 2 vCPU and 4 GiB RAM, with the load generator inside the guest.
They do not establish internet throughput, browser page-load time, production
multi-tenant capacity, or performance of the latest untested commit.

## Published evidence

| Record | Workload and WAF state | Interpretation |
| --- | --- | --- |
| [Static/API baseline, 2026-08-25](phase1-performance-baseline.md) | Static NGINX and API health, no WAF/cache/TLS, 3 distributions | Broad early regression floor; samples were only tens of milliseconds. |
| [ModSecurity baseline, 2026-08-31](../infra/host-tests/results/2026-08-31-waf-runtime-hyper-v.md) | Static workload, off/detection/blocking, 3 distributions | Historical comparison, not the current WAF engine. |
| [Coraza comparison, 2026-08-31](../infra/host-tests/results/2026-08-31-coraza-runtime-hyper-v.md) | Same WAF workload/profile, off/detection/blocking, 3 distributions | Enabled-mode throughput was more than twice the historical ModSecurity run; off-mode noise is recorded. |
| [Cache matrix, 2026-08-31](../infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md) | Same PHP body: uncached, warm FastCGI cache, warm Vinyl; all 3 WAF modes on all 3 distributions | FastCGI cache won each same-guest/mode comparison. Coraza ran before cache hits as well as misses. |
| [PageSpeed/Cyclone evaluation, 2026-09-01](../infra/host-tests/results/2026-09-01-mod-pagespeed-nginx-evaluation.md) | PHP plus CSS/JS; direct/FastCGI/Vinyl/PageSpeed; all 3 WAF modes; Debian only | Resource rewriting reduced client request/byte counts, but did not beat either full-page cache. |

The cache matrix **was tested both without WAF and with DetectionOnly and
Blocking PL1**. Do not compare a WAF-off row with a WAF-on row as evidence of
cache-engine speed. Detection-only still evaluates rules and has substantial
cost; it is not equivalent to off.

## What is measured

The Go harness in
[`host_nginx_linux_test.go`](../tests/integration/host_nginx_linux_test.go)
uses HTTP/1.1 guest loopback, reused connections, no proxy, no automatic
compression, and no TLS. It warms each target with 32 successful requests,
then measures at concurrency 8: 4,000 static requests and 2,000 API requests
for Phase 1, or 3,000 requests per WAF/cache cell. Timed request failures fail
qualification instead of being omitted. Warm-up may retry transient failures;
it is outside the measured interval.

`STACKFORT_PERFORMANCE` JSON records include request count, concurrency,
duration, RPS, bytes/s, and p50/p95/p99 latency in microseconds. Percentiles use
the sorted sample at index `floor((N - 1) * percentile / 100)`. RPS is total
completed requests divided by the measured wall time. The broad gate of at
least 100 RPS and p99 no greater than one second catches gross regressions; it
is not an SLO. CPU/memory profiles, origin-request totals, external-network
latency, and long-duration statistical confidence are **not** supplied by
these JSON metrics.

The [WAF suite](../tests/integration/host_waf_linux_test.go) proves benign and
attack behavior, isolation, logging, rollback, and MAC enforcement alongside
timing. The [cache suite](../tests/integration/host_cache_linux_test.go) adds
personalization bypass, host isolation, purge, and actual cache-hit checks.
Mode-specific worker responses and SQL-injection probes prevent measurements
from silently using an old graceful-shutdown worker or an inactive WAF.

The separate [PageSpeed harness](../infra/host-tests/evaluate-pagespeed-nginx.sh)
uses ApacheBench with **new connections**, 3,000 requests and concurrency 8.
Its latency units/precision and client differ from the Go harness. Compare
engines within that evaluation; do not combine its absolute RPS with the
separate cache matrix. Its client request/byte counts describe the tiny fixture,
not a measured real-world browser speed-up.

## Reproduce safely

Use the [host-test setup](../infra/host-tests/README.md) to prepare disposable
VMs with the complete qualified Stackfort, NGINX/Coraza, PHP, and Vinyl packages.
Do not run this on a valuable server: the suites mutate host state and create
and retire test accounts. Run one load cell/guest at a time to avoid host
contention. Example from elevated PowerShell in the repository root:

```powershell
.\infra\host-tests\Test-StackfortWAFHyperVVm.ps1 -ImageId debian-13 -VmName stackfort-debian-13
.\infra\host-tests\Test-StackfortCacheHyperVVm.ps1 -ImageId debian-13 -VmName stackfort-debian-13
```

Repeat for `ubuntu-26.04` and `rocky-10` with the corresponding dedicated VM
names. The wrappers build and transfer a Linux integration binary, validate
native/security prerequisites, require the expected markers, and return a VM
they started to Off. Use `-SkipBuild` only when intentionally reusing the exact
recorded test binary across guests. Archive stdout and digests privately before
publishing sanitized results. For PageSpeed, follow its separate dated
evaluation's exact package prerequisites; it is not part of release installation.

## Reporting a new comparison

Retain the following with a dated result:

- commit, test-binary and release-archive SHA-256, native package versions/hashes,
  NGINX ABI, Coraza/CRS versions and mode;
- host CPU, guest CPU/RAM, OS/kernel, filesystem, MAC state, generator/version,
  placement, transport/TLS, connection reuse, payload and response validation;
- cache state (cold/warm), TTL/key/bypass policy, WAF-before-cache proof,
  origin behavior, logging, warm-up, request count, concurrency, and commands;
- raw sanitized per-run metrics, failures and excluded runs with reasons,
  latency units, repeated-run variability, and limitations.

For a new performance decision beyond these regression fixtures, use several
longer repeated runs, vary engine order, report dispersion rather than only the
best RPS, and add an independent external generator, TLS, realistic application
work, cold/warm caches, and resource/origin metrics. These are requirements for
stronger future claims, not measurements retroactively attributed to old runs.

## Current product decision

Vinyl and native NGINX FastCGI are selectable, disabled-by-default PHP caches.
The current harness measures the actual native preset through the public edge,
not the temporary FastCGI baseline used by historical results. See
[ADR 0063](adr/0063-opt-in-domain-scoped-fastcgi-cache.md) for policy differences
and resource limits. PageSpeed/Cyclone remains an evaluation and is not a dependency.
The dated evaluation's performance and distribution/license constraints did
not justify replacing Vinyl. See the [cache design](cache-foundation.md) and
[ADR 0056](adr/0056-do-not-adopt-proprietary-mod-pagespeed-as-core-cache.md).
