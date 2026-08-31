# ADR 0050: Closed per-domain WAF policy

- Status: superseded by [ADR 0051](0051-coraza-nginx-waf-engine.md); the closed
  policy and privacy boundary remain accepted
- Date: 2026-08-30

## Context

This record preserves the original ModSecurity engine decision and its
reasoning. ADR 0051 changes the native engine and packaging line without
expanding the account-controlled policy surface described here.

Stackfort needs ModSecurity and OWASP CRS ahead of static, PHP, and later OCI
targets without turning the panel into a remote NGINX/SecLang editor. WAF
diagnostics may contain the exact matched request value, so reusing native
NGINX error text as an account event feed would cross the existing privacy
boundary. The three supported distributions also do not provide one suitably
current dependency set: Debian 13 and Ubuntu 26.04 ship CRS 3.3.x, and the
enabled Rocky Linux 10 repositories in the qualification guest expose none of
the expected ModSecurity/CRS package names.

## Decision

1. Account-managed policy is exactly `off`, `detection_only`, or
   `blocking_pl1`. Empty policy exists only as a backwards-compatible decoder
   input and normalizes to `off`. Account users cannot submit a module path,
   SecLang, rule ID, paranoia level, threshold, or audit-log directive.
2. Stackfort pins libModSecurity 3.0.16, ModSecurity-nginx 1.0.4, and OWASP CRS
   4.25.1 LTS. The CRS path contains its version and every NGINX server points
   only at a renderer-owned profile path.
3. New and migrated domains default to `off`. Changing a mode is an ordinary
   audited domain mutation, produces an immutable desired-state revision, and
   passes through the existing candidate-test/atomic-activation transaction.
4. The detection profile uses `SecRuleEngine DetectionOnly`; the blocking PL1
   profile uses `SecRuleEngine On`. Both share fixed request limits, disable
   response-body inspection, raw audit logs, the status engine, and debug
   logging, then include the pinned CRS setup and rules. The bundled
   `crs-setup.conf` keeps paranoia level 1 and CRS's standard anomaly threshold.
5. WAF policy applies at the public domain server before future cache/upstream
   handling. Root-owned ACME HTTP-01 locations explicitly set
   `modsecurity off` so customer policy cannot break certificate validation.
6. Native ModSecurity diagnostic lines are entirely replaced at the
   account-visible error-log boundary. K-011 subsequently added structured,
   allowlisted, sanitized WAF events; native matched values are never returned
   there.
7. Distribution-native ModSecurity/CRS packages are not silently mixed. A
   following packaging slice builds signed/hash-pinned Stackfort artifacts for
   the exact supported NGINX ABI and verifies the full version tuple before a
   non-off policy is made production-ready.

## Consequences

- Existing domains and old schema-2 desired-state operations remain valid and
  decode as WAF-off.
- A missing module, profile dependency, or incompatible NGINX module fails
  `nginx -t` before activation; the previous live revision stays active.
- Detection-only still consumes CPU and should be benchmarked before becoming
  a default. It is opt-in in K-010.
- Raw administrator exceptions and higher paranoia levels are unavailable.
  Narrow expiring exception records require their own authorization and audit
  design.

## Upstream references

- <https://github.com/owasp-modsecurity/ModSecurity/releases/tag/v3.0.16>
- <https://github.com/owasp-modsecurity/ModSecurity-nginx/releases/tag/v1.0.4>
- <https://github.com/coreruleset/coreruleset/releases/tag/v4.25.1>
- <https://packages.debian.org/trixie/libnginx-mod-http-modsecurity>
- <https://packages.debian.org/trixie/modsecurity-crs>
- <https://packages.ubuntu.com/resolute/libnginx-mod-http-modsecurity>
- <https://packages.ubuntu.com/resolute/modsecurity-crs>
