# Managed FastCGI cache qualification — 2026-09-06

The native, selectable FastCGI presets passed on Debian 13, Ubuntu 26.04 and
Rocky Linux 10 using Stackfort's production renderer and domain lifecycle.
This is development qualification, not a public release or production-readiness claim.

- Source committed as `8df8fc2ea4cebb13ae8a154d8943c2392c36031b`.
- Shared Linux test binary SHA-256:
  `010c1ed5e0a1db033983dbb09b55d38cffc3d8a0db73a8d9e3184207c03f898f` (Go 1.26.6, linux/amd64, CGO disabled).
- [Machine-readable final-run metrics](2026-09-06-native-fastcgi-cache.json).
- [Policy and limits](../../../docs/adr/0063-opt-in-domain-scoped-fastcgi-cache.md).
- [Harness](../../../tests/integration/host_cache_linux_test.go) and
  [PowerShell wrapper](../Test-StackfortCacheHyperVVm.ps1).

## Environment and method

Windows 11 Hyper-V on Intel Core Ultra 7 270K Plus; each guest has two virtual
CPUs and 4 GiB startup memory. Debian/Ubuntu use ext4 and AppArmor-enabled
kernels; Rocky uses XFS and SELinux Enforcing with the narrow cache label.
The JSON records exact kernels, NGINX native revisions and rebuilt WAF package
digests. All guests use Coraza 3.7.0, libcoraza 1.7.0, connector 0.20.0 with
the repository's sanitized-event patch, CRS 4.25.1 and Vinyl 9.0.1.

Run each guest sequentially, with 32 warm-up requests and 3,000 timed requests
at concurrency 8. HTTP/1.1 uses guest loopback, reused connections, no TLS,
compression or HTTP proxy. The PHP fixture emits host/path/random data and
explicit public cache headers; it is not a WordPress workload. FastCGI uses the
actual public PHP location, not the old temporary port-8008 baseline.
Modes are off, detection-only and blocking PL1. All timed requests succeeded.

## Final-run results

RPS is rounded; p99 is milliseconds. This is one short final matrix, not a
median or confidence interval. Previous successful diagnostic matrices varied,
so differences of a few percent should not be treated as stable rankings.

| Guest | WAF mode | Direct PHP RPS | FastCGI RPS / p99 ms | Vinyl RPS / p99 ms |
| --- | --- | ---: | ---: | ---: |
| debian-13 | off | 18,456 | 66,376 / 0.620 | 24,134 / 1.127 |
| debian-13 | detection_only | 3,385 | 4,327 / 7.402 | 3,515 / 8.163 |
| debian-13 | blocking_pl1 | 3,686 | 4,333 / 6.397 | 3,646 / 8.281 |
| ubuntu-26.04 | off | 22,823 | 78,287 / 0.606 | 28,845 / 1.524 |
| ubuntu-26.04 | detection_only | 3,442 | 4,367 / 6.599 | 3,763 / 6.138 |
| ubuntu-26.04 | blocking_pl1 | 3,691 | 4,405 / 6.303 | 3,696 / 7.631 |
| rocky-10 | off | 18,330 | 64,171 / 0.689 | 20,798 / 1.188 |
| rocky-10 | detection_only | 3,183 | 4,278 / 7.187 | 3,393 / 7.680 |
| rocky-10 | blocking_pl1 | 3,175 | 4,203 / 5.635 | 3,312 / 7.926 |

Native FastCGI has substantially higher warm-hit throughput in this small
WAF-off fixture. With Coraza enabled, inspection dominates and the advantage
shrinks. Results do not establish external/TLS latency, sustained production
capacity, CPU efficiency, tenant fairness or real-application speed. Vinyl
remains available; no engine is silently switched for existing domains.

## Behavior gates

All three guests passed:

- Anonymous MISS/HIT and cached-body equality; security headers survive hits.
- Cookie, authorization, request cache override, query, encoded-path, sensitive
  path, HEAD and request-body bypass; POST never enters native caching.
- Set-Cookie/private responses are not stored; respect-origin rejects missing
  freshness while the WordPress preset permits its bounded fallback.
- Domain and apex/www key separation, generation-based whole-domain purge,
  another domain's warm entries remaining valid, disable/re-enable without
  resurrecting old entries, and log-derived HIT/MISS/BYPASS counters.
- A scanner header against an already warm key succeeds with WAF off and
  detection-only but receives 403 with Blocking PL1, proving inspection before
  hits. Existing Vinyl SQLi, sanitized detection events, narrow exceptions and
  scoped prefix-purge checks still pass.
- Fixture services retire before account cleanup, including on failed runs.

Local verification passed Go tests/vet, gosec (Linux target), 64 browser tests,
actual strict application/template type checks, EN/DE checks, build, npm audit
(no vulnerabilities) and documentation/link checks. Root Linux checks on Debian
also passed cache-directory ownership/symlink rejection, installapply tests and
all 29 historical schema prefixes. Windows verification skips Go race tests;
those remain in Linux CI. This is not a new full fresh-install or attested
upgrade matrix for a releasable archive.

## Failures excluded before the final matrix

The first candidate contained a proxy-only directive not supported by the
FastCGI module; NGINX rejected it before activation and it was removed. Tests
were corrected for native POST's empty cache status and an explicitly
serve-both apex/www fixture. No failed cell is included in the table.

The existing VM WAF packages predated the repository's sanitized-event patch.
Their native package versions alone did not establish the required behavior:
the detection-event gate failed. All three were rebuilt from the locked current
builder (including its worker/event test), installed and verified before the
successful matrices. Existing old local package archives were not rewritten.
A failed early fixture left a PHP service behind; its test-only configuration
was removed, and cleanup now runs on failure as well as success.

The frontend check previously targeted an empty solution configuration and
silently skipped source files. It now explicitly checks application and Vite
projects; surfaced application/test issues were fixed. The documented
`skipLibCheck` exception concerns incompatible upstream declaration internals,
not application-source errors. Final manual EN/DE/narrow-screen review,
independent security review, uninstall and beta support-window decisions remain open.
