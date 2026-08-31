# WAF runtime and performance qualification on Hyper-V — 2026-08-31

## Outcome

K-010 passed its final runtime gate on fresh Debian 13, Ubuntu 26.04, and
Rocky Linux 10 amd64 checkpoints. Each guest installed the same final-shaped
release archive, completed the journaled installer and unchanged second run,
then executed the destructive host integration test against the installed
NGINX, PHP, ModSecurity, and OWASP CRS stack.

The test covers static, PHP, and redirect domains; off, detection-only, and
PL1 blocking; benign requests; SQL injection, XSS, local-file inclusion,
command injection, and sqlmap-style probes; TLS/SAN; ACME HTTP-01 bypass;
invalid candidate rollback; AppArmor or SELinux; performance; and retirement
of the temporary domains, PHP pool, identity, and cgroup resources.

## Qualified release and packages

| Artifact | SHA-256 | Bytes |
| --- | --- | ---: |
| `stackfort-0.0.0-dev-linux-amd64.tar.gz` | `bd5e6af2352bf3d1c0860657896bcdb8765c530f22c6f508050c63153cb15a33` | 33,374,666 |
| Debian 13 DEB | `f7f7539efba05a2b53a42b1dfe9e79ba94cfdec747ab64d7be4eed21c839d17a` | 786,500 |
| Ubuntu 26.04 DEB | `31dac1ac382d03f9fc5c469aebcb30b5f2deb2b9da5ddd6b1487e9a24bac910c` | 818,760 |
| Rocky Linux 10 RPM | `d9ee890586f1f2505323b5d15c321e9424adf51a22460bba82e6fbca7078d211` | 901,193 |

All installs reported source digest
`0ffcdd62692394a679e0c7983a52938c03b4c5a71cf03bec34699774f787416d`.
The embedded release manifest reported `wafComplete: true` and bound all three
native artifacts. Two independent native-package builds per guest produced
byte-identical DEB/RPM files with `SOURCE_DATE_EPOCH=0`.

## Guest matrix

| Guest | Kernel | Installer/no-op | Corpus | TLS | ACME | Reload rollback | MAC |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Debian GNU/Linux 13 | `6.12.101+deb13-cloud-amd64` | Passed | Passed | Passed | Passed | Passed | AppArmor passed |
| Ubuntu 26.04 LTS | `7.0.0-29-generic` | Passed | Passed | Passed | Passed | Passed | AppArmor passed |
| Rocky Linux 10.2 | `6.12.0-211.16.1.el10_2.0.1.x86_64` | Passed | Passed | Passed | Passed | Passed | SELinux enforcing passed |

For off domains, every malicious probe reached the application and generated
no WAF event. Detection-only domains served the request and recorded the
matches. Blocking PL1 returned HTTP 403 for every malicious probe while benign
traffic remained available. PHP policy edits crossed all three modes, and a
blocking redirect domain inspected the request before returning its redirect.
ACME challenge traffic bypassed WAF deliberately. A syntactically invalid
candidate could not replace the known-good live activation revision.

## Performance

Every measurement sent 3,000 requests at concurrency 8 to local NGINX. The
gate required at least 250 RPS and p99 no more than 100 ms in both enabled
modes, plus at least 1% of the guest's WAF-off throughput.

| Guest | Off RPS / p99 | Detection RPS / p99 | Blocking RPS / p99 | Detection/off | Blocking/off |
| --- | ---: | ---: | ---: | ---: | ---: |
| Debian 13 | 86,911 / 0.483 ms | 1,575 / 10.863 ms | 1,593 / 8.668 ms | 1.81% | 1.83% |
| Ubuntu 26.04 | 90,099 / 0.642 ms | 1,612 / 9.990 ms | 1,621 / 6.935 ms | 1.79% | 1.80% |
| Rocky Linux 10.2 | 82,805 / 0.571 ms | 1,584 / 7.098 ms | 1,547 / 8.597 ms | 1.91% | 1.87% |

The absolute and relative gates passed on all guests. These figures qualify
gross regressions in this fixed local-VM workload; they are not public hosting
capacity claims.

## Defects found and closed during qualification

- A probe header could race across rapid activation reloads. Health checks now
  require the exact activation revision, while the stable content digest
  remains independent from the probe-only header.
- A direct NGINX `return` on redirect domains could execute before the
  ModSecurity access phase. Redirects now pass through a named location after
  inspection.
- Compiling the full CRS separately for every domain produced excessive worker
  state. CRS PL1 is now loaded once in shared HTTP scope; domains include only
  the fixed engine mode. Post-test total NGINX RSS was 209–228 MiB across the
  master and workers on these guests, instead of the approximately 1.4 GiB
  diagnostic state observed with repeated per-domain compilation.
- Ubuntu's module initially used unpatched upstream NGINX 1.28.3 source even
  though the installed binary contained Canonical's package patch series. It
  passed the module signature check but stalled the second request on a reused
  connection. The builder now pins the exact Ubuntu DSC, original tarball, and
  Debian patch tarball, applies the package patch series with `dpkg-source`,
  and builds against that tree. A separate 1,000-request persistent-connection
  diagnostic and the final runtime workload both passed.
- Rocky's ModSecurity cache tree now receives the persistent
  `httpd_cache_t` file context required under enforcing SELinux.

Sanitized account-visible WAF events are intentionally sequenced as K-011;
narrow administrator exceptions are K-012.
