# ADR 0052: Keep Vinyl Cache opt-in behind NGINX and Coraza

- Status: accepted and qualified
- Date: 2026-08-31
- Extends: [ADR 0002](0002-nginx-vinyl-data-plane.md)

## Context

Stackfort needs a safe optional full-page cache without weakening the public
TLS, routing, or WAF boundary. Vinyl Cache 9.0.1 is an HTTP accelerator with a
powerful VCL policy and authenticated management interface, but supported
distribution packages are not available for the complete Stackfort target
matrix. Its relative performance also has to be measured against the simpler
NGINX FastCGI cache available in the existing edge.

## Decision

1. NGINX remains the only public listener and executes Coraza before cache
   routing. Vinyl listens on `127.0.0.1:6081`; its authenticated management
   endpoint listens on `127.0.0.1:6082`; a private NGINX origin listens on
   `127.0.0.1:9000`.
2. Stackfort pins and builds Vinyl 9.0.1 as a native amd64 DEB/RPM on Debian
   13, Ubuntu 26.04, and Rocky Linux 10. Source, managed VCL, package payload,
   target, and release manifest are hash-bound. No unpinned/system fallback is
   permitted.
3. Cache policy is a closed enum: `disabled`, `respect_origin`, or `wordpress`.
   Disabled is the default. User input never becomes VCL.
4. The fixed VCL independently bypasses authenticated, cookie-bearing,
   request-body, unsafe-method, and sensitive-path traffic, and refuses to
   store personalized or explicitly private responses.
5. Purge is an audited, durable, account/domain-scoped operation. Only a
   validated literal path prefix reaches a fixed privileged `vinyladm` command
   profile; no public PURGE method is exposed.
6. Metrics are bounded operational counters derived from data-minimized managed
   logs, not a billing or long-term analytics source.
7. Vinyl stays optional. The production recommendation is not changed merely
   because its qualification succeeds: a repeatable FastCGI-cache comparison
   is part of the release evidence.

## Consequences

- Cache hits still traverse the WAF, and personalization/cross-host isolation
  has a single enforced path on every supported distribution.
- Native package and SELinux/AppArmor maintenance become Stackfort release
  responsibilities. Rocky needs EPEL for the jemalloc runtime dependency and
  an exact NGINX-to-Vinyl SELinux connect rule.
- The final 2-vCPU/4-GiB Hyper-V matrix measured Vinyl at 27.5–28.6% of NGINX
  FastCGI cache throughput without WAF. Coraza narrows the relative gap because
  its request evaluation dominates both cache paths: Vinyl reaches 82.6–87.6%
  in DetectionOnly and 86.4–86.9% in Blocking PL1. Vinyl is therefore disabled
  by default and is not the current maximum-throughput recommendation.
- A later NGINX FastCGI cache product preset can reuse the closed policy,
  metrics, and purge principles, but requires a separate design and gate.

## Upstream references

- <https://vinyl-cache.org/releases/rel9.0.1.html>
- <https://vinyl-cache.org/docs/9.0/installation/install.html>
- <https://vinyl-cache.org/docs/9.0/reference/vinyladm.html>
- <https://vinyl-cache.org/docs/9.0/users-guide/increasing-your-hitrate.html>

## Qualification evidence

- [Cache foundation](../cache-foundation.md)
- [Three-distribution runtime and performance matrix](../../infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md)
