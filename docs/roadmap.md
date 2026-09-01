# Roadmap

The roadmap is organized around complete, testable vertical slices. Dates are
not assigned until the first slice establishes implementation velocity.

## Phase 0: Decisions and development harness

- [x] Select the project name, repository location, and license.
- Record supported distribution images and package sources.
- [x] Create the Go modules and web workspace; complete Linux runtime validation
  in CI and the disposable host harness.
- [x] Establish local/CI formatting, linting, unit tests, dependency locking,
  secret scans, deterministic artifacts, SBOM generation, and attestations;
  confirm them with the first remote run.
- [x] Build disposable Debian 13, Ubuntu 26.04, and Rocky 10 VM test fixtures; the
  verified image catalog, host capability gate, and runner matrix are ready.
- Benchmark clean NGINX and PHP-FPM baselines.

Exit gate: one command creates a disposable test node and CI can build verified
empty control API, host agent, and web artifacts.

## Phase 1: Static-domain vertical slice

- [x] Bootstrap administrator securely.
- [x] Implement identities, sessions, CSRF protection, roles, audit events, and
  English/German localization infrastructure.
- [x] Implement local typed RPC between API and agent.
- [x] Detect host capabilities and service status.
- [x] Implement and VM-validate isolated account identity, filesystem/quota,
  cgroup resource boundaries, and the conflict-safe managed NGINX baseline.
- [x] Implement the deterministic, injection-resistant account/domain NGINX
  renderer and validate its output with vendor NGINX on every supported guest.
- [x] Implement crash-recoverable NGINX site activation with immutable
  operation snapshots, full-revision staging, syntax gating, graceful reload,
  local health checks, rollback, and supported-guest failure recovery.
- [x] Implement and supported-guest validate the replay-safe static-domain API
  lifecycle, worker ACL/SELinux access, shared roots, and non-destructive
  suspend/resume/removal.
- [x] Implement encrypted ACME account registration and fixed HTTP-01 routing
  that bypasses site redirects/cache, including enforcing-SELinux validation.
- [x] Implement and browser-validate the responsive accessible application
  shell, keyboard focus contract, and reduced-motion behavior.
- [x] Centralize locale-aware value formatting and enforce complete catalogs and
  translated critical Vue template text in CI.
- [x] Implement and browser-validate administrator bootstrap/login/MFA,
  inventory, domain-operation, host-service, operation, audit, and update-status
  flows on bounded authorized APIs.
- [x] Implement and browser-validate the membership-derived account-owner
  dashboard, domain/TLS management, package usage, profile, session control,
  and explicit administrator/account workspace switching.
- [x] Qualify one release-shaped archive on Debian 13, Ubuntu 26.04 LTS, and
  Rocky Linux 10 with cross-account, injected-failure, security, and baseline
  performance evidence.
- [x] Persist package assignment and orchestrate the implemented account host
  primitives as one durable control-plane workflow, with crash-gap repair and
  a server-authoritative domain readiness gate.
- [x] Publish the installed UI and loopback API as a dedicated same-origin
  HTTPS management endpoint and expose fixed production-ACME registration in
  administrator settings.
- [x] Create a static domain through the browser workflow and complete
  certificate issuance, activation, renewal, and retirement.
- [x] Implement account-scoped operation progress and transactional rollback.
- [x] Suspend, resume, remove, and reconcile the domain/account.

Exit gate: a browser can safely create and remove an isolated TLS static site on
all supported disposable distributions, including injected-failure tests.

## Phase 2: PHP and databases

- [x] Install and manage the approved native PHP version on each supported
  distribution.
- [x] Create account PHP-FPM pools and connect typed per-domain versions through
  the durable NGINX lifecycle.
- [x] Expose host/package-approved PHP target/version selection, the fixed safe
  pool preset, and tenant-scoped aggregate usage/health views in the account UI.
- [x] Install/manage MariaDB and implement prefixed database/database-user records.
- [x] Build the database wizard and deletion safeguards.
- [x] Implement secure phpMyAdmin signon with session-bound one-time handoffs
  and a dedicated unprivileged runtime.
- [x] Qualify the implemented managed database-user password rotation,
  handoff revocation, and old-password rejection on all three guests.
- Add manual database and account backup/restore foundations.

Exit gate: two hostile test accounts cannot access one another's files, PHP
processes, sockets, schemas, credentials, or backups.

## Phase 3: Files, redirects, and complete local backup

- [x] File-manager foundation with authorization-coupled, descriptor-relative,
  symlink-safe and bounded directory navigation/metadata.
- [x] Add account-credential, descriptor-bound streaming download with bounded
  single-range responses and cancellation.
- [x] Add account-credential staged/chunked upload, resumable exact offsets,
  atomic no-replace activation, and safe empty-file/directory creation.
- [x] Add atomic rename/move, bounded staged recursive copy, typed project-quota
  failures, and paginated reversible trash with conflict-safe restore.
- [x] Complete the file manager with bounded ZIP/tar.gz creation and
  hostile-input-safe atomic extraction.
- [x] Canonical `www` behavior, 301/302 redirects, wildcard validation, and
  server-verified preview.
- [x] Complete versioned file-only account backup manifests and staged restore.
- [x] Backup downloads/uploads, manual retention primitives, and repository quota behavior.
- [x] Add account-scoped access/error log views with data-minimized NGINX
  capture, defense-in-depth redaction, bounded pagination, and fixed retention.
- [x] Scheduled Shell/PHP jobs with account limits, closed UTC schedules,
  systemd sandboxing, and three-distribution qualification.

Exit gate: backup/restore and archive attack corpora pass and destructive actions
have deterministic recovery behavior.

## Phase 4: WAF and Vinyl Cache

- [x] Build Coraza 3/coraza-nginx and OWASP CRS packaging/integration (K-010).
- [x] Detection-only and PL1 blocking modes per domain (K-010).
- [x] Sanitized event views (K-011) and narrow administrator exceptions (K-012).
- [x] Build/package supported Vinyl 9.0.1 artifacts for every supported target
  (K-013).
- [x] Implement opt-in cache presets, metrics, scoped purge, and safe bypass
  behavior (K-014).
- [x] Publish repeatable NGINX FastCGI cache versus Vinyl results (K-014).
- [x] Evaluate mod_pagespeed 1.15/Cyclone with loopback HTTPS origin mapping
  and all WAF modes; reject it as a core cache/dependency after qualification.

K-010 through K-014 complete the closed per-domain WAF and cache slice. The
result includes sanitized event capture, validated rule-scoped administrator
exceptions, reproducible native Vinyl 9.0.1 packages, disabled-by-default cache
presets, private authenticated purge, bounded metrics, and a published
three-distribution performance matrix. Fresh final-release installs pass the
runtime attack corpus, cache-personalization isolation, WAF-before-cache
ordering, TLS/PHP/static/redirect/ACME, mandatory-access-control, native package
drift, purge, metrics, persistent-connection, and performance gates on all
three supported distributions. The final comparison covers WAF off,
DetectionOnly, and Blocking PL1. Vinyl remains opt-in because NGINX FastCGI
cache delivered higher throughput in every guest/mode combination.

Exit gate: cache never serves authenticated/personalized test content across
sessions, and WAF placement/profiles behave consistently on all distributions.

Status: passed on Debian 13, Ubuntu 26.04, and Rocky Linux 10. See the
[cache foundation](cache-foundation.md) and
[qualification matrix](../infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md).
The separate [PageSpeed evaluation](../infra/host-tests/results/2026-09-01-mod-pagespeed-nginx-evaluation.md)
documents why Cyclone is a resource-optimization cache rather than a full-page
replacement and why the proprietary module is not a Stackfort dependency.

## Phase 5: Rootless OCI applications

- [x] Constrained application/project schema.
- [x] Image pull by digest, build limits, private networking, health checks, logs,
  environment secrets, volumes, and domain routing.
- [x] Rootless Podman and systemd Quadlet lifecycle.
- [x] Explicit rejection of privileged/host-level container features.
- [x] Account-wide cgroup aggregation across PHP, jobs, and OCI applications.

Exit gate: container escape-resistance configuration and cross-account tests pass
and no user workload binds a public host port directly.

Status: complete. Tenant-owned, revision-fenced
application drafts accept only constrained sources, ports, and health checks.
Hosts now expose typed rootless-Podman readiness, and each account receives a
deterministic subordinate-ID, storage, runtime, and Quadlet foundation without
an engine API socket. Digest pulls and Containerfile builds are rootless and
bounded, Trivy rejects HIGH/CRITICAL findings, and only immutable scanned image
evidence is persisted. Account-private strictly isolated rootless networks,
envelope-encrypted environment references, and descriptor-verified account
volumes are prepared through durable metadata-only operations. Fixed rootless
Quadlets now deploy approved revisions behind stable loopback ports; transient
secrets, health-gated evidence, bounded logs, replay-safe lifecycle actions,
and atomic NGINX application routing are implemented. The L-006 account-user-
manager placement closes aggregate accounting across PHP, jobs, and OCI. Real
resource exhaustion, private ingress, hostile policy, cross-account isolation,
and reboot recovery pass on Debian 13, Ubuntu 26.04, and Rocky Linux 10. See the
[OCI application foundation](oci-application-foundation.md) and
[rootless OCI runtime](rootless-oci-runtime.md), and
[bounded image preparation](oci-image-preparation.md), and
[private OCI resources](oci-private-resources.md), and
[deployment lifecycle](oci-deployment-lifecycle.md), and
[Phase 5 exit matrix](../infra/host-tests/results/2026-09-01-oci-phase5-exit-matrix-hyper-v.md).

## Phase 6: Installer, updater, and public beta

- [x] Versioned DEB/RPM packages where appropriate.
- [x] Verified one-line and manual installers for clean hosts.
- Stable/beta GitHub Release channels and update checks.
- Staged update, migration, health check, and rollback.
- Upgrade matrices from every supported prior release.
- Documentation, operations guide, security policy, contribution guide, and
  published benchmark methodology/results.
- Complete English and German critical workflows.

The first item is complete: release automation now produces a reproducible,
scriptlet-free `stackfort-release` DEB for Debian/Ubuntu and RPM for Rocky. The
packages carry one immutable release tree and delegate all host mutation to the
existing journaled installer. Build reproducibility plus install, prerelease
upgrade, removal, path ownership, and active-payload non-interference passed on
all three supported guests. See [ADR 0059](adr/0059-passive-native-release-carrier.md)
and the [qualification record](../infra/host-tests/results/2026-09-01-native-core-packages-hyper-v.md).

The second item is complete: the manual archive, GitHub bootstrap, and native
DEB/RPM routes each passed read-only preflight, initial installation,
idempotent rerun, native component drift, security-policy, service, and live
endpoint gates from the same clean checkpoint on Debian 13, Ubuntu 26.04, and
Rocky Linux 10. The bootstrap remains GitHub-HTTPS-only in production; its
explicit root-only local fixture exists solely to qualify unreleased artifacts.
See the [clean-host installer matrix](../infra/host-tests/results/2026-09-01-clean-installer-matrix-hyper-v.md).

Exit gate: the success criteria in `product-spec.md` pass, followed by a limited
public beta with an explicit support window.

## Post-beta candidates

- SFTP/SSH-key management and constrained shell access.
- Scheduled/remote encrypted backups.
- DNS provider integrations and DNS-01.
- FrankenPHP application runtime.
- Team permissions, reseller capabilities, multi-node inventory, and external
  identity providers.
- More languages after translation tooling and review workflows are mature.
