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
- [x] Stable/beta GitHub Release channels and update checks.
- [x] Staged update, migration, health check, and rollback.
- [x] Upgrade matrix generation, host qualification, and publication gates for
  every supported prior release; published-release executions begin after the
  first release exists.
- [x] Documentation, operations guide, security policy, contribution guide, and
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

The third item is complete: strict stable and beta tags now publish through an
immutability-gated workflow, and the API performs default-on, ETag-aware release
discovery without downloading or applying content. Only immutable releases with
the complete digested amd64 artifact inventory become candidates. Policy
changes are recent-authenticated and audited; the administrator UI is complete
in English and German. See [ADR 0060](adr/0060-immutable-stable-beta-release-discovery.md)
and the [update-check operations guide](update-channels-and-checks.md).

The fourth item is complete: a recent-authenticated administrator can start
only the exact immutable release accepted by discovery. A separate root-owned
updater verifies repository-scoped current and target GitHub provenance bundles
locally without a server token, stages both trees, snapshots SQLite, applies
ordered native-package, payload, configuration, and migration stages, then
commits only after full installation health. Its
private journal recovers interrupted work by restoring the exact prior release.
Success, injected health failure, migration rollback, and interrupted-journal
recovery passed with the same test binary on Debian 13, Ubuntu 26.04, and Rocky
Linux 10. See [ADR 0061](adr/0061-attested-health-gated-platform-updates.md),
the [operator guide](staged-platform-updates.md), and the
[qualification record](../infra/host-tests/results/2026-09-02-staged-update-transaction-hyper-v.md).

The fifth item is complete as release infrastructure: an explicit support catalog
generates every predecessor/distribution/scenario cell, and publication compares
the catalog with the full GitHub release inventory and requires reviewed evidence
for the exact rebuilt archive. All nine real-host rehearsal cells passed on the
three supported distributions, alongside all 28 historical SQL schema prefixes.
The rehearsal exposed and fixed systemd-template inspection, exact binary/web
payload replacement, and Rocky's missing Vinyl runtime dependency. No releases
have been published yet, so the support catalog remains empty and the recorded
evidence is explicitly non-publishing rehearsal evidence. See
[ADR 0062](adr/0062-exhaustive-artifact-bound-upgrade-matrices.md), the
[operator guide](upgrade-matrix.md), and the
[qualification record](../infra/host-tests/results/2026-09-05-upgrade-matrix-hyper-v.md).

The sixth item is complete: the [documentation index](README.md) now routes
operators and contributors to installation, [operations](operations.md),
[troubleshooting](troubleshooting.md), [security reporting](../SECURITY.md),
[contribution guidance](../CONTRIBUTING.md), and the
[benchmark methodology/results](benchmarks.md). The review corrected stale
update/bootstrap instructions and distinguishes file-only backups, updater
rollback, and full-host recovery. At that point it labelled FastCGI as benchmark-only,
records current support limits, and adds offline path/anchor checks to CI.
Private vulnerability reporting was enabled with owner approval on 2026-09-06.
The [release checklist](release-checklist.md) keeps final EN/DE workflow review,
independent security review, product-spec gaps (including full uninstall), and
the support-window decision explicit; this documentation work publishes no beta.

Exit gate: the success criteria in `product-spec.md` pass, followed by a limited
public beta with an explicit support window.

Workflow follow-up (2026-09-06): administrator Settings and account Profile now
include EN/DE TOTP setup, replacement, confirmed removal and a recovery-only
one-time code display after session revocation. Automated browser/API tests cover
secret cleanup, freshness errors, expiry, proof requirements and accessibility.
PHP domains also gain opt-in NGINX FastCGI presets, toggle and whole-domain
invalidation alongside Vinyl; see [ADR 0063](adr/0063-opt-in-domain-scoped-fastcgi-cache.md).
The [three-OS qualification](../infra/host-tests/results/2026-09-06-native-fastcgi-cache.md)
passed every WAF mode with the same test binary. Strict browser project checks
now run against actual source files; 64 browser tests and all 29 schema prefixes pass.
The README now exposes the HTTPS-only one-line installer with the explicit
no-public-release warning. These changes do not complete the final manual
workflow review, independent security audit, uninstall or support-window gates.

Panel-hostname follow-up: root-console configuration now adds a named management
origin on 443 while retaining 8443. Production Let's Encrypt HTTP-01 issuance,
automatic renewal, trusted certificate import, tenant hostname reservation and
durable rollback are implemented; see [panel hostname](panel-hostname.md).
The exact unpublished `0.1.0-beta.3` candidate passed native installation,
panel/private-CA and selected hosting regressions on all three supported
distributions; see the [artifact-bound evidence](../infra/host-tests/results/2026-09-08-panel-hostname-candidate.md).
This does not close the remaining browser/manual-review or public-release gates.

Single-disk onboarding follow-up (2026-09-08): a real Debian VPS exposed that the
installer still assumes a prepared quota filesystem. The new
[storage image prototype](storage-image-prototype.md) provisions a quota-capable
hosting image without changing root filesystem features. Its Debian hard quota,
OCI, I/O-control, full-image and reboot/fail-closed tests pass, but random-write
and fsync benchmarks show material overhead; see the
[experimental evidence](../infra/host-tests/results/2026-09-08-storage-image-prototype.md).
The [three-way XFS follow-up](../infra/host-tests/results/2026-09-08-storage-xfs-comparison.md)
also passes functional/reboot checks but shows no useful XFS-over-ext4 image
performance improvement; neither image configuration is adopted as the default.
This is not a production installer implementation or a release qualification.
Automatic safe storage provisioning on ordinary single-disk VPS images remains
an explicit open release gate; prepared-disk installer results do not close it.

The [native ext4 quota follow-up](native-quota-prototype.md) now passes an
unattended fresh-checkpoint Debian replay: offline preparation, reboot/resume,
hard quotas, OCI and fail-closed tests. The verified native-root quota execution
path distinguishes accounting from actual enforcement. Its before/after fio
run does not show the image experiments' fsync collapse; see the
[native evidence](../infra/host-tests/results/2026-09-08-native-quota-prototype.md).
Production installer/reboot-state integration, recovery and ordinary three-OS
qualification are still open; no automatic root conversion ships yet.

The [native installation-state foundation](native-quota-installation-state.md)
now persists source/host-bound phases with a shared installer lock, one-shot
staging/resume and terminal recovery. Installer/update entry points reject any
unqualified storage journal. Protocol failure injection and actual Linux
journal/gate tests pass; see the [state-protocol evidence](../infra/host-tests/results/2026-09-08-native-storage-journal.md).
The offline backend, one-shot boot dispatch, real service continuation, OS
reserve, power-loss recovery and three-OS qualification remain open. The public
installer still requires prepared hosting storage.

The [journal-bound one-shot boot experiment](native-quota-boot-handoff.md) connects
the durable protocol to the Debian offline helper. Main GRUB configuration and
normal initrd remain unchanged; fresh conversion/resume and normal-reboot quota/
OCI regressions pass. This is a lab backend, not an enabled installer feature;
production boot recovery, release staging, real installer continuation and the
remaining multi-OS/capacity gates stay open.

The [durable release staging API](native-quota-release-staging.md) now retains
an operation-bound copy outside bootstrap temporary paths and verifies its full
content/metadata in a new process after the original download is removed. The
real beta.3 source, shared-lock conflicts, tampering and incomplete-stage tests
pass on Debian; see the [staging evidence](../infra/host-tests/results/2026-09-08-resume-source-staging.md).
Staging alone is a local integrity/persistence component, not publisher
authentication or an enabled resume path.

The [release-origin/manifest follow-up](native-quota-release-origin.md)
(2026-09-09) now verifies the retained candidate's actual attestation with an
independently pinned verifier, compares its archive to the staged source, and
seals source, origin, host and boot intent under the shared lock. The same final
binary passes eight origin/tampering scenarios and the real Debian conversion
and normal-reboot quota/OCI checks with the original source path absent; see
the [origin/boot evidence](../infra/host-tests/results/2026-09-09-native-release-origin.md).
The `main`-signed candidate is explicitly lab-only, not a qualified tag release.
That follow-up did not yet execute real package stages. Production recovery,
external package/kernel coordination, capacity reserve and multi-OS onboarding
remain open. No public native-storage activation or new release is included.

The [post-boot installation continuation](native-quota-install-continuation.md)
(2026-09-09) now holds the shared installer/storage lock while the current
coordinator executes all nine real stages from the retained authenticated
candidate. The package journal is separately bound to the exact native plan.
Fresh Debian installation and a normal reboot pass with no package-stage replay;
quotas, OCI and health checks pass on both boots. The tests also exposed and
corrected a missing boot-time PHP runtime directory and divergent shared-slice
templates. Separate actual boots with an incomplete package journal or changed
retained-source metadata leave the mount and all checked consumers inactive.
See the [installation/reboot evidence](../infra/host-tests/results/2026-09-09-native-install-continuation.md).
This remains an internal API and opt-in lab: old candidate binaries do not
become a qualified native release.

The [service-admission and recovery follow-up](native-quota-service-admission.md)
(2026-09-09) adds durable admission state, a dedicated closed web-port gate,
supervisor quarantine and recovery bound to exact admission/package digests.
The Debian lab exercises real process loss after package transactions, explicit
continuation, installed-file metadata rejection and repeated boot admission.
See the [admission evidence](../infra/host-tests/results/2026-09-09-native-service-admission.md).
This closes the tested fixed-web-listener failure boundary, not general
production onboarding. Next are packaged boot/coordinator/recovery entry points,
prerequisite ordering and full listener/firewall integration, then a fresh signed
native-runtime candidate and the remaining coordination/capacity/multi-OS gates.

The [native installer operator interface](native-installer-operator.md)
(2026-09-09) adds `native status`, `approve-recovery` and
`cancel-recovery` to the real installer and native-package wrapper.
Inspection never creates state; explicit approvals are durable, source/plan
bound, cancellable by digest and consumed once under the existing shared lock.
The Debian supervisor now uses this interface instead of its ad-hoc request
file. See the [CLI/continuation evidence](../infra/host-tests/results/2026-09-09-native-installer-operator.md).
Production preparation/boot dispatch and automatic continuation are still
disabled. The next integration step is the packaged boot backend and supervisor,
with prerequisite ordering and listener/firewall qualification.

The [real installer boot-service integration](native-installer-runtime.md)
(2026-09-10) moves post-ready storage verification, service admission and
independent process-loss quarantine out of the test executable. A runtime plan
and executable are sealed before the storage journal; normal boots use the real
installer dispatcher and cannot invoke conversion. The initial proof-bearing
offline transition still uses the qualified Debian lab helper. See the
[runtime evidence](../infra/host-tests/results/2026-09-10-native-installer-runtime.md).
The final Debian artifact pair passes fresh installation, normal boots, real
dispatcher SIGKILL/quarantine, blocked boot without approval, single-use reviewed
recovery, and live fstab-metadata rejection before mounting hosting storage.
All 100 Linux package/CLI tests pass; the qualified checkpoint is retained offline.
The [initial installer preparation and boot handoff](native-installer-preparation.md)
(2026-09-11) now also run from regular installer components: sealed preparation,
separate one-shot initrd with the real installer, offline authorization/conversion,
current-boot finalization, and the existing verified installation/admission.
No test executable remains in this profile's boot path. Fresh Debian installation,
a normal non-converting/non-reinstalling boot, real quota/OCI checks, and all
108 Linux package/CLI tests pass. The successful checkpoint is retained offline.
Post-arm fstab drift is rejected before offline mutation, and a further normal
reboot stays quarantined without repeating preparation.
See the [preparation evidence](../infra/host-tests/results/2026-09-11-native-installer-preparation.md).

The [host eligibility and prerequisite follow-up](native-installer-host-eligibility.md)
adds conservative Debian 13 fresh-host checks and installs only missing
quota/nftables prerequisites before sealing boot artifacts. Exact new-package
plans are checked again against APT's actual transaction, and a durable receipt
is verified in initramfs and runtime. Public activation is still disabled;
provider-wide image coverage remains unqualified.
The real missing-prerequisite install, offline conversion, normal reboot and
quota/OCI checks pass with the final dispatcher, along with all 116 Linux
package/CLI tests. See the [host/prerequisite evidence](../infra/host-tests/results/2026-09-11-native-host-eligibility.md).
The successful checkpoint is retained offline.

The [power-loss containment follow-up](native-installer-power-loss.md) adds a sealed
recovery-first temporary GRUB default, raw-device flush barriers and an initramfs
stop on every uncertain guarded boot. Real VM hard-power-off and recovery-console
checks exercise the safety stop without test executables or pause switches in
the boot path. This is containment, not automatic filesystem repair.
Four hard-off cases reach recovery without mounting root or repeating conversion.
The successful install/normal-boot quota and OCI tests and all 119 Linux package/CLI
tests pass; see the [dated containment evidence](../infra/host-tests/results/2026-09-11-native-power-loss.md).
The successful and interrupted lab checkpoints are retained offline.

The [deterministic crash-image follow-up](native-installer-crash-replay.md) records
the same pinned conversion tools through dm-log-writes on scratch ext4 images.
It checks write and sector boundaries plus synthetic half-sector tears, proves
full replay matches the real converted bytes, preserves inconsistent evidence
and verifies a complete file-image backup/restore. The final run passes 167 cases
(26 distinct byte states), including 112 intra-write sector boundaries and 38
half-sector variants; 110 cases produce expected nonzero read-only fsck diagnoses.
See the [replay evidence](../infra/host-tests/results/2026-09-11-native-crash-replay.md).
This is not a physical torn-write guarantee or a bootable whole-disk restore.

The [whole-disk rescue follow-up](native-installer-whole-disk-recovery.md) now
passes complete 50 GiB readback, offline root/EFI checks, eight boot-file pins
and two normal Secure Boot starts from a new replacement disk. An independent
rescue OS reads a backup retained outside the affected guest disk; the original
VM/checkpoints are preserved. WWN-based lookup also survives a changed Linux
disk name on the second boot. See the [whole-system evidence](../infra/host-tests/results/2026-09-11-native-whole-disk-restore.md).
This qualifies the Debian lab recovery route, not off-host disaster recovery,
automatic repair or a portable end-user restore feature.

The [recovery policy and operator handoff](native-installer-recovery-policy.md)
now define backup/reinstallation boundaries and implement the read-only
`native recovery-plan` command. It distinguishes prerequisite, storage and
admission states, suppresses unsafe partial evidence, and cannot grant backup,
restore, repair or reinstallation authority. Windows checks and Linux isolated
inspection plus the real recorded Debian state pass; see the
[dated evidence](../infra/host-tests/results/2026-09-11-native-recovery-policy.md).

The [explicit preparation decision](native-installer-recovery-policy.md) now
binds fresh-disposable acceptance to the operation, authenticated release,
dispatcher, host/root and reviewed package/boot snapshot before prerequisites.
The exclusive durable receipt blocks reuse and public installation even before
a storage journal exists; APT, sealing and offline/runtime checks enforce its
binding. The [dated qualification](../infra/host-tests/results/2026-09-11-native-recovery-choice.md)
records boundary tests and a new real Debian preparation/conversion/install plus
normal reboot with quota/OCI checks.

Point 3's scoped whole-system restore experiment, diagnostic handoff and internal
fresh-disposable decision binding are complete. External-backup mode remains
rejected until independent backup verification is implemented; no generic restore
or provider reinstallation exists. Public consent transport and activation remain
open.

The [package-coordination follow-up](native-installer-package-coordination.md)
adds held OFD locks compatible with APT/dpkg during host inspection, preparation
outside its own APT calls, and boot-image arming. Checked APT handoffs reject
unrelated full-inventory changes, and arming rejects a changed sealed package
baseline. These are process-lifetime guards, not a persistent reboot fence;
direct kernel/initramfs/GRUB tool calls remain outside their exclusion boundary.
The [dated qualification](../infra/host-tests/results/2026-09-12-native-package-guard.md)
passes 134 Linux package/CLI tests, real APT/dpkg conflict cases, and complete
Debian preparation/conversion/install plus normal reboot with quota/OCI checks.
The next safety step is durable operation-bound package/boot coordination through
process exit, shutdown and reboot, with failure handling and safe release.
Public activation, external
package/kernel serialization, capacity reserve, full listener/firewall
qualification, additional OS boot variants and a signed native-runtime candidate
remain open.

## Post-beta candidates

- SFTP/SSH-key management and constrained shell access.
- Scheduled/remote encrypted backups.
- DNS provider integrations and DNS-01.
- FrankenPHP application runtime.
- Team permissions, reseller capabilities, multi-node inventory, and external
  identity providers.
- More languages after translation tooling and review workflows are mature.
