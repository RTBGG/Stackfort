# Phase 1 security review

Status: Passed for the static-domain and installed-panel slice on 2026-08-25

This checklist reviews the implemented Phase 1 scope as of its stated date.
PHP, databases, file and archive management, backups, WAF, Vinyl Cache, OCI
applications, and updates were outside this particular review. Later slices
carry their own security gates; the Coraza WAF boundary is documented in
[ADR 0051](adr/0051-coraza-nginx-waf-engine.md).

## Checklist

| Area | Result | Evidence |
| --- | --- | --- |
| Authentication and session handling | Pass | Argon2id, generic failures, bounded rate limits, digest-only session/CSRF material, rotation, idle/absolute expiry, recent-auth checks, TOTP replay prevention, and session revocation have unit/API coverage. |
| Authorization and tenant scope | Pass | Deny-by-default permissions and account-scoped repository/API lookups have identifier-substitution tests; the VM suite proves a foreign account cannot remove or change the owning domain. |
| Privileged boundary | Pass | The unprivileged API reaches a root agent only through a local Unix socket with peer validation and typed allowlisted requests; arbitrary command/path execution is not exposed. |
| Filesystem isolation | Pass | Deterministic Unix identities, non-traversable account roots, descriptor-relative/fail-closed path handling, symlink escape denial, ACL checks, and byte/inode project quotas pass on all guests. |
| Resource isolation | Pass | systemd/cgroup v2 CPU, memory, process, and I/O controls are capability-gated; process and memory enforcement are exercised on every guest. |
| NGINX and domain configuration | Pass | Typed rendering, normalized hosts, escaped literals, vendor `nginx -t`, full-revision promotion, health checks, rollback, rejecting defaults, and symlink denial pass on every guest. |
| TLS and ACME secrets | Pass | ACME credentials and private keys use envelope encryption; fixed-path root-owned HTTP-01 and certificate staging reject unsafe types, modes, ownership, names, and key/certificate mismatch. |
| Failure safety and auditability | Pass | Durable operation snapshots, idempotency keys, structured error codes, audit correlation, interrupted NGINX-promotion recovery, and non-destructive domain removal are covered. |
| Installer and supply chain | Pass | Absolute non-symlink source root, `os.Root`-bounded file access, bounded files/tree, ELF architecture/static-link checks, payload digest, release SHA-256, atomic stage journal, exact metadata, and no-op rerun pass. GitHub Actions are pinned to full commits. |
| Host hardening | Pass | Root-only state, locked service identity, hardened systemd properties, dedicated firewall state, AppArmor enforcement on Debian/Ubuntu, and SELinux enforcing plus persistent contexts and a dedicated NGINX-to-API port policy on Rocky are checked externally by the harness. |
| Dependency and static analysis | Pass | `govulncheck v1.7.0`: no called vulnerability; `gosec v2.28.0`: no finding; `npm audit`: 0 vulnerabilities; `go vet`, Actionlint, Go tests, and 29 web tests pass. |
| Secret leakage | Pass | Repository ignore policy covers local keys/state; API and operation errors are structured and tests reject raw credentials/internal command output. GitHub gitleaks and CodeQL jobs are configured for remote history/code scanning. |

## Review findings

No blocking finding remains in the Phase 1 scope. The review closed:

- unsafe negative-to-unsigned representation in preflight resource reporting;
- a generic variable-file warning by moving bounded reads to Go's rooted
  filesystem API;
- retryable classification of a denied cross-account domain removal; and
- the NGINX traversal mismatch above the public ACME HTTP-01 directory; and
- the invalid `CPUQuota=infinity` live reset for packages without a CPU limit;
- asynchronous panel-listener readiness after NGINX reload; and
- Rocky SELinux denial of the NGINX-to-loopback-API proxy connection, resolved
  with a dedicated port type while broad HTTPD network booleans remain off.

The final artifact and all corrections were requalified on all supported
distributions. Results are tied to the hashes in
[Phase 1 qualification](phase1-qualification.md).

## Commands executed

```sh
bash scripts/verify.sh
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 -quiet ./...
```

The local Windows verification skips Linux race/socket execution. Linux host
behavior is instead exercised by the compiled integration suite on all three
VMs; the CI workflow still runs `go test -race ./...`, the Unix-socket smoke
test, gitleaks, dependency review, and CodeQL on Linux when the repository is
connected to GitHub.

## Residual constraints

- The loopback control API is intentionally HTTP behind the same-host boundary;
  production external access must terminate TLS at the managed edge.
- The current SELinux integration uses persistent file contexts, a narrow
  NGINX-to-API port rule, and systemd sandboxing; dedicated Stackfort process
  domains remain a hardening option.
- GitHub-hosted history scans, CodeQL, provenance, and attestations cannot be
  evidenced locally and remain mandatory release-workflow gates.
- The Phase 1 benchmark is a regression signal, not a denial-of-service or
  Internet-facing load test.
