# ADR 0051: Coraza engine through coraza-nginx

- Status: accepted and qualified
- Date: 2026-08-31
- Supersedes: the native engine, connector, packaging, and activation-failure
  timing portions of [ADR 0050](0050-closed-per-domain-waf-policy.md)

## Context

ADR 0050 selected libModSecurity for Stackfort's closed per-domain WAF policy.
Its final Hyper-V qualification passed, but enabled modes sustained only
1.79–1.91% of WAF-off throughput in the fixed local workload. The product's
primary performance goal warrants testing a compatible engine before building
event and exception features on top of that dependency.

Coraza implements the relevant ModSecurity SecLang subset in Go and supports
OWASP CRS v4. Its NGINX connector is usable as a dynamic module but is explicitly
labelled experimental upstream. The connector loads `libcoraza.so` and builds
file-backed WAF objects only after NGINX forks unprivileged workers. That design
changes both qualification and rollback timing: `nginx -t` cannot prove that a
worker can read and compile the rules.

## Decision

1. Stackfort pins Coraza 3.7.0, libcoraza 1.7.0, coraza-nginx 0.20.0, OWASP CRS
   4.25.1, and Go 1.25.12. Every downloaded byte is SHA-256 locked.
2. Each distribution receives a native `stackfort-waf` DEB or RPM built against
   its exact installed NGINX source and package revision. The connector module
   has a fixed private RUNPATH and dynamically opens the versioned
   `/usr/lib/stackfort/coraza-1.7.0/lib/libcoraza.so` after the worker fork.
3. Package and installer qualification must start an isolated NGINX worker,
   send a request, and prove an inline deny rule returns HTTP 403. `nginx -t`
   remains necessary but is not accepted as sufficient evidence.
4. Coraza rule directories and non-secret SecLang files are root-owned and
   worker-readable/traversable. Secrets retain their separate restrictive
   modes. The persistent Coraza data directory remains owned only by the NGINX
   worker identity and is MAC-labelled on Rocky Linux.
5. WAF rules are not inherited globally. Because coraza-nginx builds inherited
   rules per location, only WAF-active domains include the fixed PL1 base and
   select `DetectionOnly` or `On`; panel, default, and WAF-off servers allocate
   no CRS WAF.
6. Active domains set `coraza_transaction_id $request_id`. This enables a
   domain-scoped NGINX intervention record without accepting a client-provided
   identifier. The existing log boundary replaces native Coraza lines before
   account display.
7. A file-backed rules error discovered after reload is an activation health
   failure. The candidate revision is rolled back and the known-good revision
   is reloaded. Tests must exercise this worker-time failure explicitly.
8. The user-facing policy remains exactly `off`, `detection_only`, or
   `blocking_pl1`, defaults to off, and exposes no raw SecLang.

## Consequences

- The final three-guest workload sustains 3,400–3,735 RPS in detection mode and
  3,531–3,694 RPS in blocking PL1. This is 2.11–2.39 times the earlier
  ModSecurity throughput on the same guest/workload definitions.
- p99 latency stays below 8.5 ms in every enabled-mode final measurement.
- The connector's experimental upstream status is a release risk. Exact pins,
  fresh-host qualification, WAF-off default, and rollback gates remain
  mandatory; upgrades cannot float automatically.
- Go becomes a build-time dependency, not a host runtime dependency.
- Existing policy records and API contracts do not migrate because only the
  native implementation changed.

## Upstream references

- <https://github.com/corazawaf/coraza/releases/tag/v3.7.0>
- <https://github.com/corazawaf/libcoraza/releases/tag/v1.7.0>
- <https://github.com/corazawaf/coraza-nginx/releases/tag/v0.20.0>
- <https://github.com/coreruleset/coreruleset/releases/tag/v4.25.1>
- <https://go.dev/dl/>

## Qualification evidence

- [Coraza runtime and ModSecurity comparison](../../infra/host-tests/results/2026-08-31-coraza-runtime-hyper-v.md)
- [Historical ModSecurity runtime baseline](../../infra/host-tests/results/2026-08-31-waf-runtime-hyper-v.md)
