# Opt-in Vinyl Cache foundation

K-013 and K-014 provide a closed, disabled-by-default full-page cache for PHP
domains. NGINX remains the public TLS and Coraza edge; Vinyl Cache 9.0.1 is a
private accelerator between that edge and a separate NGINX origin.

## Request path and trust boundary

```text
client -> public NGINX + Coraza -> Vinyl 127.0.0.1:6081
                                  -> NGINX origin 127.0.0.1:9000
                                  -> account PHP-FPM Unix socket
```

Coraza always evaluates the public request before NGINX can forward it to
Vinyl. This is also true for cache hits. Vinyl and its authenticated management
listener (`127.0.0.1:6082`) are loopback-only, and the application API cannot
submit VCL, commands, regular expressions, or management credentials. Domains
which are static, redirects, ACME routes, or have caching disabled never enter
Vinyl.

## Closed presets and safety rules

`disabled` is the migration and new-domain default. PHP domains may explicitly
select one of two fixed presets:

- `respect_origin` stores a response only when the origin supplies `max-age` or
  `s-maxage`.
- `wordpress` uses the same safety rules and supplies a 120-second TTL only
  when the origin did not provide a positive TTL.

The immutable Stackfort VCL bypasses every request which is not GET/HEAD, has a
request body, carries `Authorization` or any `Cookie`, or targets a sensitive
path such as login, account, cart, checkout, API, `wp-admin`, or `wp-login.php`.
It refuses to store responses with `Set-Cookie`, `private`, `no-store`, or
`no-cache`, as well as status codes outside 200–399. Host and complete URL form
part of the cache key, preventing cross-domain reuse. Customer-provided VCL and
public HTTP purge methods are not supported.

## Purge and metrics

Authorized account managers can queue an audited purge for one canonical
domain and a validated literal path prefix. The privileged agent converts that
scope to a renderer-owned anchored ban expression and calls `vinyladm` through
a fixed command profile and the root-managed secret. A purge cannot select
another domain or inject VCL/regular-expression syntax.

The status view counts only `HIT`, `MISS`, and `BYPASS` values from the current
and immediately rotated, data-minimized domain access logs. Input files are
opened without following symlinks and are bounded by the managed 8 MiB active
log limit; the reported window is therefore operational rather than a billing
record. No request headers, cookie values, or response bodies are returned.

## Reproducible native packages

Vinyl 9.0.1 is built from a SHA-256-locked upstream security-release tarball.
The managed VCL has a separate repository lock, and release records bind the
package hash, target, version, and upstream version. Debian 13 and Ubuntu 26.04
receive native DEBs; Rocky Linux 10 receives an RPM and resolves its jemalloc
dependency through EPEL with DNF. Installation rejects package drift and
unsafe management-secret metadata, validates the VCL, and creates the secret
atomically when absent or empty.

The systemd service runs as its own `vinyl` identity in `stackfort-core.slice`
with a read-only system, private temporary storage, no new privileges, and a
restricted address family/capability surface. AppArmor is checked on Debian and
Ubuntu. Rocky qualification runs with SELinux enforcing; an exact local policy
allows NGINX (`httpd_t`) to connect only to Vinyl's `varnishd_port_t` listener,
while the private origin uses stock `http_port_t` port 9000. Broad web-server
network-connect booleans are not enabled.

## Performance result and recommendation

The fixed Hyper-V test uses the same warmed PHP response, 3,000 loopback
requests, concurrency 8, and 2-vCPU/4-GiB guests. It compares uncached PHP via
the normal public NGINX path, a dedicated NGINX FastCGI cache baseline, and the
production Vinyl path in all three WAF modes. Both cache paths load the exact
installed Coraza/CRS profile for active modes. A generation header plus an SQLi
probe proves the newly reloaded FastCGI benchmark worker and Blocking PL1
intervention before measurement. Every path writes one access record using the
same data-minimized Stackfort log format. Results are single-guest
qualification measurements, not an internet-facing capacity forecast.

| Guest | WAF mode | Uncached PHP RPS | NGINX FastCGI cache RPS / p99 | Vinyl RPS / p99 | Vinyl / FastCGI |
|---|---|---:|---:|---:|---:|
| Debian 13 | Off | 22,555.04 | 76,746.16 / 496 µs | 21,931.30 / 1,376 µs | 28.6% |
| Debian 13 | DetectionOnly | 2,495.02 | 3,033.22 / 12,629 µs | 2,657.97 / 12,104 µs | 87.6% |
| Debian 13 | Blocking PL1 | 2,560.17 | 2,992.09 / 13,011 µs | 2,597.64 / 12,362 µs | 86.8% |
| Ubuntu 26.04 | Off | 20,784.89 | 74,431.59 / 563 µs | 20,616.74 / 1,467 µs | 27.7% |
| Ubuntu 26.04 | DetectionOnly | 2,127.00 | 3,008.48 / 9,720 µs | 2,484.46 / 10,943 µs | 82.6% |
| Ubuntu 26.04 | Blocking PL1 | 2,198.29 | 2,921.45 / 14,901 µs | 2,523.34 / 10,290 µs | 86.4% |
| Rocky Linux 10 | Off | 17,596.19 | 86,086.82 / 710 µs | 23,700.82 / 1,046 µs | 27.5% |
| Rocky Linux 10 | DetectionOnly | 2,466.94 | 3,165.10 / 12,946 µs | 2,683.18 / 9,569 µs | 84.8% |
| Rocky Linux 10 | Blocking PL1 | 2,640.92 | 3,048.04 / 10,725 µs | 2,641.94 / 11,258 µs | 86.7% |

The safety and consistency exit gate passes on all three distributions, but
Vinyl does not win the throughput comparison. Without WAF it reaches only
27.5–28.6% of FastCGI cache throughput. With Coraza active, WAF evaluation
dominates both paths and narrows the gap: Vinyl reaches 82.6–87.6% in
DetectionOnly and 86.4–86.9% in Blocking PL1. Stackfort therefore keeps Vinyl
optional and disabled by default. For maximum PHP full-page-cache throughput,
NGINX FastCGI cache remains the preferred future production path. Vinyl should
be enabled only when its shared HTTP-cache/VCL behavior is specifically
valuable, and the choice should be revisited before public beta.

See the [full qualification record](../infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md)
and [ADR 0052](adr/0052-opt-in-vinyl-cache-behind-nginx-and-coraza.md).
