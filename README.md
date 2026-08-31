# Stackfort

**Secure hosting. Simple operations.**

Stackfort is a free and open-source server control plane for shared web
hosting and containerized applications. It aims to combine the approachable
account management of cPanel/WHM, the focused server administration of
CloudPanel, and the application deployment model of Coolify without making the
web application itself a privileged system process.

The project is in its implementation-foundation phase. The first API, host
agent, and bilingual web interface build independently, but Stackfort is not
ready to install on a production server.

## Goals

- Fast static and PHP hosting with NGINX and isolated PHP-FPM pools.
- Optional per-domain full-page caching with Vinyl Cache.
- Rootless OCI container deployments through Podman.
- Separate administrator and hosting-account experiences.
- Enforceable CPU, memory, process, storage, and I/O limits.
- Secure configuration changes with validation, atomic activation, and rollback.
- A simple English and German interface, with English as the source language.
- Reproducible installation on Debian 13, Ubuntu 26.04, and Rocky Linux 10.

## Proposed platform

| Area | Proposed technology |
| --- | --- |
| Control API and host agent | Go |
| Web interface | Vue 3, TypeScript, Vite |
| Panel state | SQLite in WAL mode |
| Edge and origin server | NGINX |
| PHP runtime | One PHP-FPM pool per hosting account |
| Optional page cache | Vinyl Cache 9.x |
| Web application firewall | Coraza 3, coraza-nginx, and OWASP Core Rule Set |
| Managed SQL service | MariaDB |
| Container runtime | Rootless Podman with systemd Quadlets |
| Resource control | systemd and cgroup v2 |
| Storage quotas | Filesystem project quotas |
| Service management | systemd |

## Request path

```text
Client
  -> NGINX edge (TLS, redirects, rate limits, Coraza WAF)
      -> static file
      -> NGINX origin -> PHP-FPM account pool
      -> Vinyl Cache -> NGINX origin -> PHP-FPM account pool
      -> rootless OCI application
```

Vinyl Cache is an optional accelerator, not a mandatory dependency. Unknown or
personalized applications default to no full-page caching. Static files are
served directly by NGINX.

## Repository map

- `cmd/stackfort-api`: unprivileged HTTP API and background orchestration.
- `cmd/stackfort-agent`: minimal privileged service with an allowlisted local API.
- `web`: browser interface and translations.
- `docs/product-spec.md`: product scope and acceptance requirements.
- `docs/architecture.md`: component boundaries and system flows.
- `docs/security.md`: threat model and mandatory safeguards.
- `docs/roadmap.md`: implementation sequence and release gates.
- `docs/mvp-backlog.md`: ordered Phase 0/Phase 1 implementation tickets.
- `docs/localization-foundation.md`: locale, value-formatting, and CI contracts.
- `docs/administrator-phase1-flows.md`: authenticated administrator UI and API contracts.
- `docs/account-owner-phase1-flows.md`: role-aware account-owner UI and self-service API contracts.
- `docs/account-php-controls.md`: host/package PHP selection and tenant-scoped pool observability.
- `docs/account-database-lifecycle.md`: tenant-scoped MariaDB wizard, credentials, grants, and deletion.
- `docs/phpmyadmin-signon.md`: session-bound one-time phpMyAdmin automatic login and runtime isolation.
- `docs/installer-preflight.md`: read-only fresh-host gate and explicit installation plan.
- `docs/installed-panel-ingress.md`: initial HTTPS panel access and fixed NGINX boundary.
- `docs/phase1-qualification.md`: supported-OS exit matrix and qualified artifact identity.
- `docs/phase1-security-review.md`: static-domain security checklist and residual constraints.
- `docs/phase1-performance-baseline.md`: reproducible installed-system loopback baseline.
- `docs/research-notes.md`: upstream facts behind the selected web stack.
- `docs/adr`: architecture decision records.
- `packaging`: installer and future DEB/RPM packaging.
- `tests`: integration, end-to-end, and performance suites.

## Current status

The repository currently contains:

- a versioned Go API with health/build provenance and a single-use,
  rate-limited first-administrator bootstrap boundary;
- a separate Linux host-agent process exposing health and strictly typed local
  RPC only on a Unix socket;
- a responsive, browser-validated Vue application shell with native landmarks,
  deterministic page focus, keyboard-contained mobile navigation, reduced
  motion, and complete English/German shell copy;
- locked Go, Node.js, npm, and frontend dependency versions;
- unit tests and successful Linux `amd64` and `arm64` cross-builds;
- private SQLite state with versioned identities, accounts, packages, domains,
  TLS intent, applied revisions, and a hash-chained audit log;
- a durable operation state machine with scoped idempotency, fenced worker
  leases, structured progress, retry policy, cancellation, and restart recovery;
- Argon2id administrator creation with a local 256-bit one-time capability,
  digest-only persistence, atomic role/credential/audit creation, and no default
  password;
- non-enumerating Argon2id login with persistent pressure limits, strict
  host-only browser cookies, session-bound CSRF proof, rotation, logout, and
  server-side idle/absolute expiry;
- deny-by-default platform/account authorization with current role and
  membership checks, five-minute freshness for sensitive actions, and a
  protected cross-tenant-safe account endpoint;
- bounded self-service context that separates platform capability from explicit
  memberships, plus a browser-validated account-owner workspace for domain/TLS
  management, package usage, own profile, and identity-scoped sessions;
- verified TOTP enrollment with encrypted per-record secrets, replay
  prevention, hash-only single-use recovery codes, two-phase MFA login, and
  identity-scoped session review/revocation;
- a versioned, strictly typed local agent protocol with Linux kernel peer-UID
  authentication, compatibility negotiation, bounded messages/timeouts,
  request correlation, and semantic idempotency conflict detection;
- read-only host capability inspection with typed reason codes and fixtures for
  Debian 13, Ubuntu 26.04, and Rocky Linux 10, covering systemd, cgroup v2,
  quota mounts, AppArmor/SELinux, ports, packages, and service state;
- a profiled external-process boundary with fixed executables and arguments,
  sanitized environments, bounded/redacted output, deadlines, and Linux
  process-group cleanup;
- enforced audit correlation for privileged agent mutations, structured
  payload-free outcome logs, and pre-HTTP security events for rejected peers;
- stable UUID-derived hosting-account Unix identities with reserved UID/GID,
  conflict-safe user/group/root reconciliation, and staged archive/deletion
  tombstones;
- revisioned desired/applied project-quota state, a fixed isolated account
  layout, capability-labelled byte/inode enforcement, and descriptor-relative
  no-symlink document-root creation;
- revisioned CPU, scheduling-weight, memory, swap, and task intent mapped to
  verified per-account systemd slices, with a fixed aggregate customer-workload
  ceiling that reserves host capacity for Stackfort platform services;
- a conflict-safe, root-owned NGINX baseline with separate panel/site include
  points, rejecting HTTP/HTTPS defaults, loopback-only real-client trust,
  syntax-gated activation, rollback, and successful tests on all three target
  distributions;
- a deterministic, bounded account/domain NGINX renderer with repeated persisted
  invariant checks, context-specific URL/source handling, enumerated headers,
  no raw-directive input, and vendor syntax tests on all target distributions;
- crash-recoverable NGINX site revisions with typed immutable operation input,
  full-revision staging, fixed vendor syntax tests, atomic pointer promotion,
  graceful reload, local route health checks, rollback, API/agent replay, and
  interrupted-promotion tests on all target distributions;
- an authenticated, CSRF-protected static-domain API and durable lifecycle
  worker for create/edit/suspend/resume/remove, with immutable replay state,
  account-owned shared roots, exact NGINX worker ACLs, enforcing Rocky SELinux
  contexts, symlink denial, and non-destructive removal validated on all three
  target distributions;
- replay-safe administrator ACME account registration with fixed Let’s Encrypt
  environments, envelope-encrypted P-256 credentials, and a root-owned typed
  HTTP-01 presenter whose redirect/cache bypass passed on all target guests;
- replayable RFC 8555 certificate orders with envelope-encrypted P-256 keys,
  exact SAN/key/chain validation, fixed-path root artifact staging,
  transactional HTTPS activation, jittered bounded renewal, predecessor-safe
  failure handling, and logical retirement, with fixed-path staging and real
  TLS handshakes validated on all three target guests;
- authoritative domain-routing previews with explicit apex/`www` redirect
  scopes, safe wildcard/loop validation, and exact canonical/customer 301/302
  path/query behavior validated through real NGINX responses on all three
  target guests;
- immutable-reference CI, security analysis, deterministic release archives,
  SBOM generation, and build-attestation workflows;
- a capability gate and verified base-image catalog for disposable supported-OS
  virtual machines;
- a standalone, read-only installer preflight with stable actionable blockers,
  bounded CPU/memory/storage checks, a complete distribution-specific change
  plan, text/JSON output, and distinct ready/blocked/error exit codes;
- a release-bound, root-only fresh-host installer with atomic stage journaling,
  convergent interruption recovery, immutable payload checks, exact ownership
  and modes, hardened systemd services, AppArmor/SELinux and firewall
  integration, plus verified no-op reruns on all three target distributions;
- the complete Phase 1 qualification matrix for one identical release archive,
  including domain lifecycle/removal/reconcile, cross-account denial, injected
  recovery, security review, and static/control-API performance baselines.
- a dedicated installed HTTPS management endpoint on port 8443 with an atomic
  root-only local bootstrap certificate, immutable SPA delivery, loopback API
  proxying, firewall/MAC integration, installer health checks, and a bilingual
  administrator flow for fixed-environment Let's Encrypt account registration.
- a typed account-scoped PHP-FPM lifecycle using each distribution's approved
  native runtime, hardened version-specific systemd services, mode-0600 Unix
  sockets, two-phase NGINX-safe reconciliation, static/PHP document-root access
  intent, and real cross-account qualification on every supported guest;
- host/package-intersected PHP controls in administrator and account domain
  forms, immutable package version selection, and a bounded tenant-authorized
  pool-health view with aggregate memory, CPU-time, and process counters;
- native MariaDB installation and adoption, tenant-prefixed database/users,
  encrypted one-time credentials, fixed least-privilege grants, a bilingual
  replay-safe wizard, and confirmation-bound database/user deletion.
- secure account-scoped phpMyAdmin automatic login using digest-only 30-second
  handoffs, an HMAC-authenticated loopback broker, and a dedicated
  capability-free FPM runtime, qualified on all supported guests.
- a failure-safe managed database-password rotation path with a separately
  encrypted candidate, typed MariaDB mutation, one-time reveal reset, and
  phpMyAdmin handoff invalidation, qualified on all supported guests.
- a bilingual, authorization-coupled file browser with bounded metadata pages,
  descriptor-relative traversal, same-device ownership fences, and no symlink
  following;
- constant-memory regular-file download through a separate bounded stream,
  account-UID permission enforcement, safe attachment names, cancellation, and
  single-range support, qualified on all supported guests;
- resumable staged upload in bounded chunks, atomic no-replace activation,
  empty-file/directory creation, and durable mutation correlation, qualified on
  all supported guests;
- atomic rename/move, bounded staged recursive copy, quota-failure reporting,
  and paginated reversible trash with no-replace restore, qualified on all
  supported guests;
- bounded ZIP and tar.gz creation plus hostile-input extraction through hidden
  staging and atomic no-replace activation, qualified on all supported guests.
- root-owned local file backups with HMAC-authenticated versioned manifests,
  complete payload verification, and staged document-root or visible-account
  restore, qualified on all supported guests.

Browser account creation now enters the durable host-orchestration path and
domains are gated on confirmed account readiness. Static TLS and the managed
PHP backend, account controls, MariaDB tenant lifecycle, and secure phpMyAdmin
signon and database-password rotation are qualified. The K-001 file-manager
navigation and secure streaming download are qualified on all three supported
guests; staged upload and safe creation are qualified as K-003, while rename,
copy, move, and reversible trash are qualified as K-004. Safe ZIP and tar.gz
creation/extraction are qualified as K-005. K-006 adds versioned local
file-backup manifests and staged restore, with explicit exclusions for
databases, TLS material, email, and server configuration. K-007 adds fully
verified Range-capable download, resumable hostile-archive
import, owner-confirmed deletion, and separately measured package backup quota.
K-008 adds privacy-minimized per-domain access/error logs, bounded account
views, defense-in-depth redaction, and seven-day size-bounded rotation,
qualified on all three supported guests.
K-009 adds package-bounded Shell/PHP scheduled jobs with closed UTC schedules,
durable revision-fenced lifecycle operations, derived systemd timers, account
identity/cgroup isolation, and a bilingual account workspace, qualified on all
three supported guests.
K-010 completes the first closed WAF slice with a persisted, audited
off/detection/OWASP CRS-PL1 domain policy, fixed NGINX profiles, explicit ACME
bypass, native diagnostic withholding, and bilingual controls. Its hash-pinned
native builder produces byte-identical Coraza 3.7.0/libcoraza 1.7.0/
coraza-nginx 0.20.0 DEB/RPM packages for the exact supported NGINX revisions;
Ubuntu's connector is built from the exact patched distribution source package.
Only WAF-active domains load and compile a fixed closed CRS profile. The release embeds a closed,
SHA-256-bound three-distribution package manifest, and the privileged installer
revalidates the package, qualification inventory, ELF linkage, NGINX revision,
runtime ownership, and isolated module load with transactional rollback. Fresh
installs on Debian 13, Ubuntu 26.04, and Rocky Linux 10 pass the benign and
malicious HTTP corpus in all modes, TLS, PHP, static, redirect, ACME, mandatory
access-control, reload rollback, and bounded performance gates.
The final Coraza matrix more than doubled enabled-mode throughput versus the
same earlier ModSecurity workload on every supported guest; see the
[Coraza runtime comparison](infra/host-tests/results/2026-08-31-coraza-runtime-hyper-v.md).

Phase 4 is complete. K-011 adds a connector-backed sanitized WAF event view
whose allowlisted records exclude matched values and query strings. K-012 adds
administrator-only, expiring, exact-path/parameter CRS exceptions without raw
SecLang. K-013 supplies hash-locked native Vinyl Cache 9.0.1 packages and a
journaled installer for all three supported distributions. K-014 completes the
disabled-by-default `respect_origin` and `wordpress` presets, WAF-before-cache
ordering, personalization-safe bypasses, bounded metrics, and audited scoped
purge. The Hyper-V exit matrix passes everywhere. Vinyl delivered 27.5–28.6%
of NGINX FastCGI cache throughput with WAF off, 82.6–87.6% in DetectionOnly,
and 86.4–86.9% in Blocking PL1. It therefore remains opt-in; NGINX FastCGI
cache is the current performance recommendation for a future production
preset. See the [cache foundation](docs/cache-foundation.md) and
[qualification result](infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md).

See the [roadmap](docs/roadmap.md) for the planned order of work.

## Development

Toolchain requirements, verification, and local commands are documented in
[DEVELOPMENT.md](DEVELOPMENT.md). The shortest local start is:

```sh
go run ./cmd/stackfort-api
cd web
npm ci
npm run dev
```

The API listens on `127.0.0.1:8080` by default and the web development server
on `127.0.0.1:5173`. The host agent is Linux-only and should be run separately
with a development socket outside production paths.

## Safety notice

Do not run development installers or unreleased builds on a server containing
valuable data. Early versions will support fresh disposable systems only.

## License

Stackfort is licensed under the GNU Affero General Public License v3.0 or later
(`AGPL-3.0-or-later`). See [LICENSE](LICENSE) and [COPYRIGHT.md](COPYRIGHT.md).
