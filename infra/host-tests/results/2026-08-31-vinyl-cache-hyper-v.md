# Vinyl Cache Phase 4 Hyper-V qualification — 2026-08-31

## Scope

The final K-013/K-014 wrapper built one Linux integration binary, validated the
exact native WAF and Vinyl artifacts, and ran the same bounded qualification on
fresh Debian 13, Ubuntu 26.04, and Rocky Linux 10 guests. Each guest had 2
virtual CPUs and 4 GiB RAM. Rocky ran with SELinux enforcing.

The test exercised the production domain lifecycle, NGINX renderer, Coraza
module, Vinyl service/VCL, agent RPC, fixed privileged commands, cache metrics,
and durable purge rather than a mock cache endpoint.

## Required markers

Every guest produced all of the following markers:

```text
STACKFORT_QUALIFICATION vinyl-runtime-sandbox-and-loopback=passed
STACKFORT_QUALIFICATION cache-personalization-isolation=passed
STACKFORT_QUALIFICATION cache-waf-order-and-exceptions=passed
STACKFORT_QUALIFICATION cache-scoped-purge-and-metrics=passed
```

This proves that Vinyl's data and management sockets are loopback-only, its
systemd sandbox is active, cookie/authorization/body/sensitive-path requests
are never cached, hosts do not share objects, Coraza runs before both cache
misses and hits, narrow WAF exceptions retain their exact scope, and a purge
invalidates only its canonical domain/path target. Native package verification
reported no file drift on all three systems.

## Performance method

After warm-up, every path/mode combination received 3,000 HTTP requests with
concurrency 8 over guest loopback and returned the same small PHP-generated
body:

- uncached PHP through the normal public NGINX/FastCGI route;
- a dedicated NGINX FastCGI cache listener on a stock SELinux HTTP port;
- the production public NGINX -> Coraza -> Vinyl -> NGINX origin route.

Each path was measured with WAF off, the exact installed DetectionOnly PL1
profile, and the exact installed Blocking PL1 profile. All three paths wrote
one access record in Stackfort's fixed data-minimized format. The FastCGI
benchmark used an otherwise production-equivalent Coraza server boundary. A
mode-specific response header had to be returned by eight new connections
after every reload, preventing an old graceful-shutdown worker from
contaminating the next measurement. An SQLi probe additionally had to return
403 in Blocking PL1 and 200 in the other modes before timing started.

The figures are one bounded qualification run per guest. They establish a
same-guest relative comparison and regression record; they are not a public
network capacity claim.

| Guest | WAF mode | Uncached RPS | NGINX FastCGI cache RPS | FastCGI p99 | Vinyl RPS | Vinyl p99 | Vinyl / FastCGI |
|---|---|---:|---:|---:|---:|---:|---:|
| Debian 13 | Off | 22,555.0365 | 76,746.1593 | 496 µs | 21,931.2982 | 1,376 µs | 0.2858 |
| Debian 13 | DetectionOnly | 2,495.0188 | 3,033.2234 | 12,629 µs | 2,657.9709 | 12,104 µs | 0.8763 |
| Debian 13 | Blocking PL1 | 2,560.1682 | 2,992.0865 | 13,011 µs | 2,597.6357 | 12,362 µs | 0.8682 |
| Ubuntu 26.04 | Off | 20,784.8882 | 74,431.5920 | 563 µs | 20,616.7395 | 1,467 µs | 0.2770 |
| Ubuntu 26.04 | DetectionOnly | 2,126.9952 | 3,008.4807 | 9,720 µs | 2,484.4595 | 10,943 µs | 0.8258 |
| Ubuntu 26.04 | Blocking PL1 | 2,198.2909 | 2,921.4510 | 14,901 µs | 2,523.3358 | 10,290 µs | 0.8637 |
| Rocky Linux 10 | Off | 17,596.1874 | 86,086.8153 | 710 µs | 23,700.8236 | 1,046 µs | 0.2753 |
| Rocky Linux 10 | DetectionOnly | 2,466.9429 | 3,165.0966 | 12,946 µs | 2,683.1799 | 9,569 µs | 0.8477 |
| Rocky Linux 10 | Blocking PL1 | 2,640.9239 | 3,048.0396 | 10,725 µs | 2,641.9370 | 11,258 µs | 0.8668 |

## Result

The Phase 4 safety and consistency exit gate passes on all supported
distributions. Vinyl 9.0.1 is a supportable opt-in cache, including under
enforcing SELinux, but NGINX FastCGI cache won throughput in every guest and
WAF mode. Vinyl sustained 27.5–28.6% of FastCGI cache throughput with WAF off,
82.6–87.6% in DetectionOnly, and 86.4–86.9% in Blocking PL1. The active WAF
dominates request cost and makes the two cache implementations much closer,
but does not reverse their ordering. Vinyl remains disabled by default. NGINX
FastCGI cache is the evidence-based performance recommendation for a later
production preset; Vinyl remains useful when HTTP-cache/VCL behavior is
specifically required.
