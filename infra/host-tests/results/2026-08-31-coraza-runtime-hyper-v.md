# Coraza runtime and ModSecurity comparison on Hyper-V — 2026-08-31

## Outcome

The Coraza replacement passed the complete Stackfort WAF runtime gate on fresh
Debian 13, Ubuntu 26.04, and Rocky Linux 10 amd64 installations. Every guest
passed native-package qualification, first installation, unchanged second run,
service/security checks, and the destructive WAF integration suite.

The test covers static, PHP, and redirect domains; off, detection-only, and PL1
blocking; benign requests; SQL injection, XSS, local-file inclusion, command
injection, and sqlmap-style probes; domain-scoped intervention logging; TLS/SAN;
ACME HTTP-01 bypass; worker-time invalid-profile rollback; AppArmor or SELinux;
performance; and retirement of temporary identities and resources.

## Qualified native components

| Component | Version |
| --- | ---: |
| Coraza | 3.7.0 |
| libcoraza | 1.7.0 |
| coraza-nginx | 0.20.0 |
| OWASP Core Rule Set | 4.25.1 |
| Go build toolchain | 1.25.12 |

| Native package | SHA-256 | Bytes |
| --- | --- | ---: |
| Debian 13 DEB | `41ca68ef5f029c4a1822ded29c05e59dbb53b619e1f5632b9936a9920ffdb2e1` | 4,045,380 |
| Ubuntu 26.04 DEB | `8616bdf807c6eef0c48860c949f27f5b28ab385bdafed0d330800b5560067903` | 4,047,324 |
| Rocky Linux 10 RPM | `6c5d0b6c286ee4a39231cfe190a6d59b04f34e5281f942f5080193757eadac69` | 4,477,396 |

Two independent builds from different temporary paths produced byte-identical
qualification bundles, native packages, and release records for every guest.
The connector was compiled against each target's exact NGINX package/source
revision. Bundle and installer checks started a real unprivileged worker and
proved an inline deny rule returned HTTP 403; the native package's syntax check
alone was not accepted as sufficient installer evidence.

After the final renderer/profile changes, two independent release assemblies
also produced the identical 43,447,953-byte amd64 archive with SHA-256
`e2ed91733459b5923ff69b6e4b996eab65a9a9f6c76f3dc5f01a4f17d60dd6ae`.
Its manifest reported `wafComplete: true` and bound the three package hashes
above. A fresh Debian install of that exact final archive, followed by its
unchanged second run, passed with source digest
`5682642e884c6cec65a6efb65541c3aa2fa76a02be11f08475f72c4cd716f20c`.

## Guest matrix

| Guest | Installer/no-op | Corpus | Logging | TLS | ACME | Rollback | MAC |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Debian GNU/Linux 13 | Passed | Passed | Passed | Passed | Passed | Passed | AppArmor passed |
| Ubuntu 26.04 LTS | Passed | Passed | Passed | Passed | Passed | Passed | AppArmor passed |
| Rocky Linux 10.2 | Passed | Passed | Passed | Passed | Passed | Passed | SELinux enforcing passed |

WAF-off traffic reached the application and emitted no Coraza event.
Detection-only served each malicious request, while blocking PL1 returned HTTP
403 for every malicious probe. Benign traffic remained available. Active
domains used a server-generated NGINX request ID for Coraza intervention
correlation. Native diagnostic text stayed behind the account-log redaction
boundary.

An invalid file-backed profile is accepted by `nginx -t` because coraza-nginx
builds WAFs after the worker fork. The activation health checkpoint detected the
fatal worker initialization, restored the previous revision, and proved the
known-good domain still responded.

## Performance

Every measurement sent 3,000 requests at concurrency 8 to local NGINX. The
same request generator, payload, domain types, CRS PL1 configuration, VM shapes,
and gates were used by the preserved
[ModSecurity baseline](2026-08-31-waf-runtime-hyper-v.md). Results are gross
local-VM regression signals, not public hosting-capacity claims.

| Guest | Off RPS / p99 | Detection RPS / p99 | Blocking RPS / p99 | Detection/off | Blocking/off |
| --- | ---: | ---: | ---: | ---: | ---: |
| Debian 13 | 89,660 / 0.474 ms | 3,735 / 7.088 ms | 3,531 / 8.463 ms | 4.17% | 3.94% |
| Ubuntu 26.04 | 77,683 / 0.545 ms | 3,400 / 8.038 ms | 3,535 / 7.510 ms | 4.38% | 4.55% |
| Rocky Linux 10.2 | 75,234 / 0.680 ms | 3,475 / 7.541 ms | 3,694 / 8.431 ms | 4.62% | 4.91% |

### Direct enabled-mode comparison

| Guest | ModSecurity detection RPS | Coraza detection RPS | Speed-up | ModSecurity blocking RPS | Coraza blocking RPS | Speed-up |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Debian 13 | 1,575 | 3,735 | 2.37× | 1,593 | 3,531 | 2.22× |
| Ubuntu 26.04 | 1,612 | 3,400 | 2.11× | 1,621 | 3,535 | 2.18× |
| Rocky Linux 10.2 | 1,584 | 3,475 | 2.19× | 1,547 | 3,694 | 2.39× |

Across the three guests, mean detection throughput increased from 1,590 to
3,537 RPS (2.22×) and mean blocking throughput from 1,587 to 3,587 RPS (2.26×).
All Coraza p99 values remained below 8.5 ms. WAF-off results varied by +3.2%,
-13.8%, and -9.1% between the separate runs, which is why the engine conclusion
uses the large, consistent enabled-mode improvement rather than treating the
off baseline as a stable capacity measurement.

## Coraza-specific defects found and closed

- `libcoraza.so` is an executable shared object and now has an explicit `0755`
  installed-inventory contract.
- File-backed rules are read after privilege drop. The non-secret SecLang chain
  and its parent directories are now root-owned but worker-readable/traversable;
  private data and secrets remain restricted.
- Invalid file-backed rules fail during worker initialization rather than
  `nginx -t`. The activation rollback test now requires the health-check failure
  and verifies restoration of the known-good revision.
- coraza-nginx emits its NGINX intervention line only when a transaction ID is
  configured. Active domains now use `$request_id`, enabling domain-scoped logs
  without trusting a client-provided identifier.
- Global rule inheritance caused coraza-nginx to allocate WAF objects for panel,
  default, and WAF-off contexts. The fixed PL1 base now loads only from active
  detection/blocking profiles.

## Risk decision

coraza-nginx 0.20.0 is explicitly experimental upstream. The measured
performance and simpler dependency line justify the switch, but WAF remains
off by default. Exact version/ABI locks, real worker checks, three-distribution
attack-corpus qualification, and rollback tests are mandatory for each future
upgrade. See [ADR 0051](../../../docs/adr/0051-coraza-nginx-waf-engine.md).
