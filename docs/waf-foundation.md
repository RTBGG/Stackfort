# Closed per-domain web application firewall

K-010 through K-012 implement and qualify the closed per-domain Coraza/OWASP
CRS policy, sanitized event view, and narrow administrator exceptions without
exposing raw server configuration or matched request values.

## Implemented boundary

- `domain_waf_policies` stores one account/domain-correlated mode with database
  checks and a safe migration default of `off`.
- Domain create/edit API payloads accept only `off`, `detection_only`, or
  `blocking_pl1`; list responses return the effective mode.
- Schema-3 durable operations capture the policy in the immutable desired-state
  document. Schema-2 operations remain replayable and normalize to off.
- The NGINX renderer maps modes only to root-owned fixed paths. It emits no WAF
  directives for off, and it explicitly disables inherited inspection for
  ACME HTTP-01 locations.
- Active servers use `coraza_transaction_id $request_id`, enabling an NGINX
  intervention record with a server-generated correlation value. Complete
  native `Coraza:` lines are replaced at the account-visible error-log boundary
  so matched request values and rule metadata cannot leak through K-008.
- English and German administrator/account forms can select and display the
  policy. Off remains the default to prevent an unexpected availability or
  latency change.

## Selected component line

Stackfort pins Coraza 3.7.0, libcoraza 1.7.0, coraza-nginx 0.20.0, OWASP CRS
4.25.1, and Go 1.25.12. The connector is experimental upstream, so production
installation uses Stackfort-owned per-distribution native packages and never an
unverified system fallback.

The exact NGINX ABI and package revision are locked for Debian 13, Ubuntu
26.04, and Rocky Linux 10. Ubuntu uses its pinned DSC, original source tarball,
and Debian patch tarball so the module is compiled against Canonical's applied
source tree. Each package carries the connector, a private versioned
`libcoraza.so`, the CRS tree, licenses, build inputs, a qualification manifest,
and a complete file inventory.

coraza-nginx loads the Go-backed library and compiles file-backed SecLang only
after the NGINX worker fork. Stackfort therefore treats `nginx -t` as necessary
but insufficient: bundle construction and the privileged installer each start
an isolated worker and prove an inline deny rule returns HTTP 403. The native
package hook performs a syntax/module-load check, while the installer also
validates package metadata, every
payload hash and mode, ELF objects, the connector's fixed private RUNPATH,
NGINX revision, runtime ownership, and transactional rollback.

## Runtime layout

Baseline reconciliation owns root-written profiles below
`/etc/nginx/stackfort/coraza`. The rule directories are `0755` and the
non-secret SecLang files are `0644` because the unprivileged NGINX worker must
read them after fork. The NGINX-only global include stays `0640`, and secret
panel/TLS files retain their separate restrictive modes.

Rules are deliberately not loaded at global HTTP scope. coraza-nginx builds
inherited rules per location, so global loading would allocate CRS WAF objects
even for panel, default, and WAF-off servers. Each active domain instead loads
one fixed profile, which includes the immutable engine/CRS PL1 base and selects
only `DetectionOnly` or `On`. Raw audit logs, debug logs, and response-body
inspection remain disabled. The persistent engine directory lives below
`/var/cache/stackfort/coraza` and is restricted to the distribution NGINX
worker; Rocky additionally enforces `httpd_cache_t`.

## Build and runtime qualification

Two independent native builds from different temporary paths produced
byte-identical bundles, DEBs/RPM, and release records on every guest. Fresh
release installs and unchanged second runs passed on all three distributions,
including package drift checks, service hardening, AppArmor/SELinux, firewall,
and runtime health.

The destructive WAF gate covers static, PHP, and redirect domains; all three
modes; benign traffic; SQL injection, XSS, local-file inclusion, command
injection, and sqlmap-style probes; TLS/SAN; ACME bypass; worker-time invalid
profile rollback; MAC enforcement; domain-scoped logging; and bounded
performance. The final Coraza workload passed everywhere and more than doubled
enabled-mode RPS versus the preserved ModSecurity baseline.

See the [Coraza runtime comparison](../infra/host-tests/results/2026-08-31-coraza-runtime-hyper-v.md),
[historical ModSecurity runtime baseline](../infra/host-tests/results/2026-08-31-waf-runtime-hyper-v.md),
and [ADR 0051](adr/0051-coraza-nginx-waf-engine.md).

K-011's structured event path accepts only connector-produced rule ID,
severity, server correlation ID, method, and normalized queryless URI. The
patched connector deliberately detaches NGINX's automatic request formatter,
and qualification probes verify that matched values and query strings never
enter the event record. The generic domain-error view continues to withhold the
entire native diagnostic.

K-012 permits only administrators with recent platform authorization to
disable one inbound CRS rule ID in the closed `920000`–`944999` range for an
exact path and/or exact parameter name. Exceptions require a package feature,
expire after five minutes to thirty days, are capped at 64 per domain, are
durably audited, and reject regex, wildcard, header, body, and free-form
SecLang input. Their generated local guard rules use a disjoint reserved ID
range and remain subject to candidate validation and rollback.
