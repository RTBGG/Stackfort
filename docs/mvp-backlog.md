# MVP Backlog

This backlog covers Phase 0 and the static-domain vertical slice. Items are
ordered by dependency. `P0` blocks the vertical slice; `P1` is required before
the Phase 1 exit gate; `P2` may follow immediately afterward.

## Definition of done for every item

- Code is formatted, linted, reviewed, and covered by relevant automated tests.
- Errors are structured and do not expose secrets or internal command output.
- Mutations are authorized, idempotent where applicable, and audited.
- English UI/API descriptions exist; German UI copy exists for user-facing
  Phase 1 workflows.
- Supported-distribution behavior is covered or explicitly capability-gated.
- Security-relevant assumptions are documented.

## Epic A: Project and CI foundation

### A-001 — Select identity and license (`P0`, complete)

Stackfort uses `RTBGG/stackfort`, the Go module
`github.com/RTBGG/stackfort`, the `ghcr.io/rtbgg/stackfort-*` image namespace,
the executable and service names recorded in ADR 0006, and the
`AGPL-3.0-or-later` license.

Acceptance:

- Names are recorded in an ADR and used consistently.
- The license file and copyright policy exist.
- No temporary namespace remains in distributable identifiers.

### A-002 — Initialize workspaces (`P0`, implemented; Linux validation pending)

Create the Go workspace/modules and Vue/TypeScript application with locked
toolchain versions.

Implementation note: both Linux binaries cross-build for `amd64` and `arm64`;
the API and bilingual interface have been run locally. Agent startup, Unix
socket permissions, and race tests remain release-gated on Linux in A-003/A-004.

Acceptance:

- Control API, host agent, and web UI build on a clean machine.
- The API and agent start as distinct processes.
- Generated artifacts contain version, commit, and build provenance metadata.

### A-003 — Continuous integration (`P0`, implemented; first GitHub run pending)

Add formatting, lint, tests, dependency review, secret scanning, SBOM generation,
and reproducible release builds.

Implementation note: CI, security, deterministic artifact, SPDX SBOM,
attestation, Dependabot, immutable-action-reference, ShellCheck, and actionlint
configuration is present. Branch protection and a successful remote run require
the future GitHub repository.

Acceptance:

- Pull requests cannot merge while required checks fail.
- Release binaries exist for Linux amd64.
- Artifacts have checksums and provenance attestations.
- CI actions and build dependencies are pinned to immutable revisions where
  practical.

### A-004 — Disposable host harness (`P0`, local Hyper-V harness implemented)

Create automated clean Debian 13, Ubuntu 26.04, and Rocky 10 test nodes using a
documented VM or image workflow.

Implementation note: official image sources, checksum-verified image download,
the VM requirements, an OS/cgroup/quota/security capability gate, and the
ephemeral self-hosted-runner matrix are defined. The Windows Hyper-V harness
now creates, tests, and safely removes all three supported images; clean
validated baselines were also checkpointed locally. Real ext4/XFS byte/inode
enforcement and account/symlink isolation passed on 2026-08-16. One-job GitHub
runner registration remains pending until the remote repository and its
runner-group policy exist.

Acceptance:

- A developer can create, test, and destroy every supported node type.
- The harness exposes systemd, cgroup v2, quotas, networking, and SELinux/AppArmor
  behavior needed by production.
- Test nodes never use production credentials or user data.

## Epic B: Domain model and persistence

### B-001 — SQLite foundation (`P0`, implemented)

Open SQLite with WAL mode, conservative busy handling, foreign keys, backups,
and embedded ordered migrations.

Implementation note: the API now owns a private, CGo-free SQLite 3.53.3 store
with `FULL` synchronization, four bounded connections, serialized immediate
writes, checksum-locked migrations, startup integrity checks, and verified
`VACUUM INTO` snapshots that cannot overwrite an existing backup.

Acceptance:

- Startup refuses an unknown future schema.
- Migration failures leave the prior database recoverable.
- Concurrent reads and bounded writers have integration coverage.

### B-002 — Core records (`P0`, implemented)

Implement identities, password credentials, sessions, roles, hosting accounts,
memberships, packages, desired-state revisions, operations, and audit events.

Implementation note: migration 002 and the typed repository use canonical
UUIDv7 identifiers, restrictive account foreign keys, immutable package and
desired-state revisions, effective-limit assignment snapshots, durable base
operations, and transaction-coupled append-only audit hashing. Administrator
bootstrap, password sessions, and authorization are now implemented by
C-001/C-002/C-003. Worker transition behavior is implemented by B-004.

Acceptance:

- IDs are opaque and globally unique.
- Account-owned records cannot exist without a valid account.
- Package assignments preserve a resolved effective-limit snapshot for audit.

### B-003 — Domain records (`P0`, implemented)

Implement domain, target, document root, canonical host, redirect, TLS, and
applied-revision records.

Implementation note: migration 003 and the typed repository now store stable
Unicode/ASCII domain identities, globally exclusive base/`www` routes,
immutable target and redirect history, retained relative document roots, TLS
intent/state, and account-scoped applied revisions. Package limits, PHP-version
allowlists, redirect permissions, wildcard overlap, redirect-loop checks, and
tenant-bound foreign keys are enforced transactionally. DNS/port/target/config
validation and activation remain in F-002 through F-004; certificate issuance
remains in G-002; OCI targets remain capability-gated until an
account-owned application record can be verified.

Acceptance:

- Unicode input has a stable normalized display form and ASCII routing form.
- Uniqueness is case-insensitive and global where routing requires it.
- Domain removal preserves operation/audit history.

### B-004 — Persisted operations (`P0`, implemented)

Implement durable jobs with stages, idempotency keys, structured progress, retry
classification, and cancellation boundaries.

Implementation note: migration 004, the core repository, and the generic
in-process runner now provide scoped idempotent creation, immutable attempts,
fenced worker claims, bounded leases/heartbeats, monotonic checkpoints,
paginated structured events, safe/manual/none retry behavior, exponential
backoff, cancellation acknowledgement, terminal results, and startup/claim-time
lease recovery. Identical idempotency input returns the existing live or
terminal operation; a key reused with different actor/kind/payload/policy is a
conflict. Concrete domain and host-agent handlers remain in the F/D slices.

Acceptance:

- Process restart does not lose or duplicate an operation.
- Identical idempotency keys return the existing outcome.
- Operations clearly identify whether retry is safe.

## Epic C: Authentication and authorization

### C-001 — Secure administrator bootstrap (`P0`, implemented)

The installer creates a short-lived, single-use bootstrap capability rather than
a default password.

Implementation note: migration 005, the local `stackfort-api bootstrap create`
command, and the bounded bootstrap API now provide a 256-bit capability whose
raw value is displayed once and whose SHA-256 digest alone is stored. Persistent
per-direct-source and global limits run before Argon2id; the final transaction
creates the identity, credential, platform role, capability consumption, and
audit event atomically. Expiry, replacement, reuse, restart, log-redaction, and
concurrent-redemption tests cover the security boundary. C-002 now provides
normal login and browser sessions.

Acceptance:

- The raw token is displayed once and stored only as a hash.
- It expires, is rate-limited, and cannot be reused.
- Bootstrap is disabled after the first administrator is created.

### C-002 — Password login and sessions (`P0`, implemented)

Implement Argon2id passwords, secure cookie sessions, CSRF protection, rotation,
logout, expiry, and rate limiting.

Implementation note: migration 006 and the authentication repository now use
generic dummy-hash failures, persistent global/direct-source/SHA-256-identity
limits before expensive work, bounded Argon2id verification, 256-bit
digest-only session and CSRF values, transactional credential revalidation,
login rotation, explicit logout, and server-side idle/absolute expiry. The HTTP
surface uses host-only `Secure`, `HttpOnly` session cookies, a session-bound
synchronizer CSRF cookie/header, strict SameSite policy, JSON-only login, Fetch
Metadata rejection, and no credentialed CORS. Enumeration, fixation, cross-
session CSRF, logout/expiry replay, restart limits, credential races, cookie
attributes, and secret-redaction paths have automated coverage. C-003 provides
authorization and recent-authentication policy.

Acceptance:

- Authentication responses do not enumerate accounts.
- Session fixation, cross-site mutation, and replay tests pass.
- Passwords and session secrets never enter logs or audit details.

### C-003 — Authorization policy (`P0`, implemented)

Implement administrator and account-owner permissions in the application layer.

Implementation note: the typed deny-by-default policy combines a server-derived
session subject with current platform role, active account membership, account
status, and authentication age. Its table-driven matrix covers administrator,
owner, forward-compatible member/auditor, and unrelated identities. Sensitive
actions carry a non-optional five-minute freshness requirement. The first
protected account endpoint couples its authorization facts and resource lookup
in one statement; unauthorized and missing account IDs return the same response.
Cross-tenant account/domain substitution, role and membership revocation,
suspended-account mutation, stale authentication, and revoked sessions have
automated negative coverage.

Acceptance:

- An authorization matrix is covered by table-driven tests.
- Changing object identifiers never crosses an account boundary.
- Sensitive mutations require recent authentication.

### C-004 — TOTP, recovery, and session management (`P1`, implemented)

Implementation note: migration 007, AES-256-GCM per-record envelope encryption,
and the identity security repository now implement verified TOTP enrollment,
current-factor replacement/removal proof, replay-counter enforcement, ten
hash-only 128-bit recovery codes, persistent factor limits, digest-only
five-minute MFA login challenges, and identity-scoped session review/revocation.
Password login creates no new session while MFA is pending. Successful factor
activation/removal revokes all sessions. The JSON API uses a strict host-only
MFA challenge cookie, bound CSRF for every mutation, recent-authentication
policy, generic verification errors, and one-time secret/code responses. See
[TOTP, recovery codes, and session management](totp-recovery-and-session-management.md).

Acceptance:

- TOTP setup requires a verified challenge before activation.
- Recovery codes are hashed and single-use.
- Users can review and revoke sessions; credential/factor changes can revoke all.

## Epic D: Local privileged agent

### D-001 — Versioned local protocol (`P0`, implemented)

Define a typed protocol over a protected Unix socket with compatibility
negotiation, peer verification, request size/time limits, request IDs, and
idempotency keys.

Implementation note: the agent now authenticates the exact configured control
API UID using Linux `SO_PEERCRED` before HTTP parsing. The strict 64-KiB tagged
JSON contract begins with `protocol.handshake`, negotiates the supported
version range, correlates bounded request IDs, detects semantic idempotency-key
conflicts, and exposes no command/path escape hatch. A bounded client verifies
media type, protocol header, response size, request correlation, and typed
results. Decoder fuzzing, handler negatives, Linux peer/oversize integration,
and the Unix-socket smoke test cover the boundary. See
[Local host-agent protocol](local-agent-protocol.md).

Acceptance:

- There is no arbitrary command or arbitrary path method.
- Only the configured control API service identity can connect.
- Malformed and oversized messages have fuzz and integration coverage.

### D-002 — Capability inspection (`P0`, implemented)

Detect distribution, architecture, systemd, cgroup controllers, filesystem/quota
capability, security module, relevant ports, packages, and managed service state.

Implementation note: version 1 now exposes the empty-payload read-only
`host.capabilities.inspect` operation. Its strictly validated report covers the
three supported distributions, systemd, cgroup v2 controllers, the fixed
`/srv/hosting` quota target, AppArmor/SELinux, ports 80/443, and allowlisted
package/service roles. Each missing or indeterminate feature has a typed status
and stable reason code. Fixed-path package and systemd probes have per-command
timeouts, sanitized environments, output limits, and an eight-second aggregate
deadline. See [Host capability inspection](host-capability-inspection.md).

Acceptance:

- Unsupported capabilities produce typed results, not parsing failures.
- Detection has fixtures for every supported distribution.
- The API can explain why a feature is unavailable.

### D-003 — Safe external process runner (`P0`, implemented)

Build one internal agent utility for allowlisted executables with direct argument
arrays, sanitized environments, timeouts, output bounds, cancellation, and
redaction.

Implementation note: callers now select one compiled-in execution profile, not
an executable or raw option list. Profiles fix absolute paths, argument
templates, exact semantic validation, a minimal environment, `/` as the working
directory, bounded per-profile deadlines, separate 32-KiB output limits, and a bounded
reap delay. Linux starts a new process group and kills the group on cancellation,
timeout, or output exhaustion. Stable errors and stream redaction exclude raw
arguments and OS diagnostics. D-002 package/service probes and E-001's five
fixed account-management profiles now use this shared runner. See
[Safe external process runner](safe-external-process-runner.md).

Acceptance:

- No invocation uses `/bin/sh -c` or interpolated command strings.
- Executable paths are fixed by installation/capability data.
- Timeout and output-exhaustion tests leave no orphaned child processes.

### D-004 — Agent audit correlation (`P1`, implemented)

Implementation note: every protocol operation now has an explicit access
policy. Future privileged mutations require a canonical UUIDv7 durable operation
ID, an identity or system actor, and an optional account ID; read-only/protocol
calls forbid that mutation context. Correlation is part of the semantic
idempotency digest. Fixed structured event builders log only validated IDs,
operation, status, replay state, and stable reason codes. Unexpected local UIDs
and peer-credential failures emit bounded security events before HTTP parsing,
and the `net/http` error hook discards unstructured text. See
[Agent audit correlation](agent-audit-correlation.md).

Acceptance:

- Every privileged mutation is correlated to an API operation and actor.
- Agent logs include no user secrets or full untrusted payloads.
- A local unexpected caller is rejected and generates a security event.

## Epic E: Account isolation

### E-001 — Hosting-account identity (`P0`, implemented and VM-validated)

Create stable internal Unix usernames/UIDs independent from email/display names.

Implementation note: migration 008 now allocates an immutable UUID-derived
username, equal UID/GID from a reserved monotonic range, and a canonical account
root in the account-creation transaction. Correlated typed agent operations
reconcile or delete only that identity through fixed `shadow-utils` profiles.
Bounded local-account snapshots reject name and numeric conflicts before
mutation; descriptor-relative no-symlink traversal creates or repairs only the
exact `0750` account root. Removal requires archive request, archive evidence,
deletion request, an absent live root, and deletion confirmation; retained
tombstones prevent ID reuse. Unit/RPC tests and real mutations pass on all
three disposable supported OS images. See
[Hosting-account Unix identity](hosting-account-unix-identity.md).

Acceptance:

- Reconciliation creates or repairs the expected user, group, and directory
  ownership without adopting conflicting pre-existing identities.
- Account removal follows an explicit staged archive/delete operation.

### E-002 — Filesystem layout and quota (`P0`, implemented and VM-validated)

Implementation note: migration 009 persists immutable project IDs plus
revisioned desired/applied byte and inode limits. The correlated agent applies
one fixed project-quota profile, assigns and verifies project inheritance on an
empty account root, and creates the exact account-owned layout through
descriptor-relative no-symlink operations. A shared canonical path validator
and descriptor walk prevent document-root traversal and symlink escape.
Unavailable quota mounts/tools return a typed capability instead of an
unenforced success. Unit/RPC and Linux descriptor tests plus real ext4/XFS
enforcement pass on all supported OS images.
See [Hosting filesystem layout and project quota](hosting-filesystem-layout-and-quota.md).

Acceptance:

- Account roots are not traversable by other accounts.
- Byte/inode limits are enforced when supported and capability-labelled.
- Document-root validation cannot escape through `..` or symlinks.

### E-003 — Account cgroup/systemd slice (`P0`, implemented and VM-validated)

Implementation note: migration 010 persists revisioned desired/applied CPU,
weight, memory, swap, and task limits without collapsing explicit zero swap
into unlimited. A correlated typed agent operation owns a deterministic
systemd slice below `stackfort-accounts.slice`, writes only marker-owned units,
applies changes live through fixed `systemctl` profiles, and verifies the
resulting cgroup-v2 control files. The aggregate account hierarchy receives at
most 80% of host CPU and memory capacity; `stackfort-core.slice` is the sibling
boundary for panel/platform services. Limit changes, PIDs exhaustion, and a
contained memory OOM pass on Debian 13, Ubuntu 26.04, and Rocky Linux 10. See
[Account systemd slices and cgroup-v2 limits](account-resource-control.md).

Acceptance:

- CPU, memory, swap, and process limits map predictably from package values.
- The host reserves capacity for panel and core services.
- Limit changes and over-limit behavior have integration tests.

### E-004 — Durable account host provisioning (`P0`, implemented)

Account creation now queues one immutable `hosting.account.reconcile` snapshot
covering the E-001 identity, E-002 project quota/layout, and E-003 systemd slice.
A deterministic repair scan closes the persistence-to-queue crash gap. Domain
mutations remain unavailable until all three boundaries are confirmed, while
the APIs and bilingual UI expose only a non-sensitive `hostReady` state. See
[Durable hosting-account provisioning](hosting-account-provisioning.md) and
[ADR 0034](adr/0034-durable-hosting-account-provisioning.md).

Acceptance:

- Account creation automatically queues the exact revisioned host intent.
- Retry, process restart, and duplicate repair scans converge safely.
- No domain operation is queued for a partially provisioned account.

## Epic F: NGINX and static domains

### F-001 — Managed NGINX baseline (`P0`, implemented and VM-validated)

Implementation note: the fixed, correlated agent operation validates the vendor
NGINX binary and service, refuses active or foreign unmanaged state, and owns an
independent root-only configuration selected by a marker-owned systemd drop-in.
It installs rejecting HTTP/HTTPS defaults, trusts forwarded client identity only
from IPv4/IPv6 loopback, validates every candidate with the exact managed main
configuration, and rolls back failed validation or activation. Idempotency,
permissions, unknown-host rejection, TLS-handshake rejection, and core-slice
placement plus boot enablement pass on Debian 13, Ubuntu 26.04, and Rocky Linux
10. Package installation and fresh-host preflight wiring remain part of I-001. See
[Managed NGINX baseline](managed-nginx-baseline.md).

Install or validate NGINX and create stable managed include points, rejecting
conflicting existing installations in the initial fresh-host installer.

Acceptance:

- A rejecting default HTTP/HTTPS host exists.
- Customer configuration cannot affect the panel virtual host.
- Real client information is trusted only from configured local hops.

### F-002 — Deterministic config renderer (`P0`, implemented and VM-validated)

Implementation note: one pure, bounded renderer produces a SHA-256-addressed
account include from revalidated domain records. It canonicalizes domain and
header order, admits only enumerated headers and fixed templates, derives roots
from the account identity, rejects overlapping routes or invalid tagged unions,
and URL-encodes user-originated `$` before appending renderer-owned NGINX
variables. Adversarial unit tests and the vendor `nginx -t` pass on Debian 13,
Ubuntu 26.04, and Rocky Linux 10. F-003 now consumes this output. See
[Deterministic NGINX configuration renderer](deterministic-nginx-config-renderer.md).

Render account/domain config from typed structures with escaping by context.

Acceptance:

- Same desired state produces byte-identical output.
- Hostnames, paths, redirect URLs, and headers have adversarial test cases.
- Users cannot add raw NGINX directives.

### F-003 — Transactional activation (`P0`, implemented and VM-validated)

Implementation note: the typed account snapshot is rendered again inside the
agent and staged as one complete global site revision. A root-owned phase
journal, exclusive lock, atomically replaced relative symlink, fixed candidate
`nginx -t`, graceful reload, service verification, local Host-routed probe, and
old-revision reload make every failure recoverable. The durable operation UUID
also identifies the host revision; exact API replay is idempotent in applied
history, and agent restart recovery does not depend on the in-memory RPC cache.
Failure injection and real interrupted-promotion recovery pass on Debian 13,
Ubuntu 26.04, and Rocky Linux 10. See
[Transactional NGINX site activation](transactional-nginx-activation.md).

Stage a generated revision, validate with NGINX, activate atomically, reload
gracefully, health-check, and roll back on failure.

Acceptance:

- Invalid config never replaces the active revision.
- Injected failures at every stage converge to old or new valid state.
- API restart and agent restart during the operation are recoverable.

### F-004 — Static domain lifecycle (`P0`)

Implemented and VM-validated on Debian 13, Ubuntu 26.04, and Rocky Linux 10.
The account API queues a replay-safe create/edit/suspend/resume/remove saga;
immutable operation-linked desired state feeds F-003 activation. Exact worker
ACLs, default ACL inheritance, Rocky SELinux contexts, and NGINX symlink denial
serve account-owned content without changing its ownership. Logical removal
never invokes filesystem deletion. See
[Static-domain lifecycle](static-domain-lifecycle.md).

Create, edit, suspend, resume, and remove a static domain.

Acceptance:

- Domain conflicts and package limits are enforced transactionally.
- Static files are served as the account identity permits.
- Removing a domain does not remove a shared/non-empty root implicitly.

### F-005 — Canonical host and redirect-only domains (`P1`, implemented and VM-validated)

Migration 015 adds explicit apex-only, `www`-only, and both-host scopes to
immutable redirect revisions. The authorized read-only preview API applies the
same normalization, unsafe-target, wildcard, and loop boundary as persistence.
The renderer isolates unselected hosts while retaining HTTP-01, and constructs
preserved paths before fixed/preserved queries. Real 301/302 and `Location`
responses match the preview on Debian 13, Ubuntu 26.04, and enforcing Rocky
Linux 10.2. See
[Canonical host and redirect-only routing](canonical-and-redirect-routing.md).

Acceptance:

- `www`, apex, 301, and 302 behavior matches the preview.
- Paths and queries follow explicit preservation settings.
- Loop and unsafe-target validation has automated tests.

## Epic G: TLS

### G-001 — ACME account and HTTP-01 routing (`P0`, implemented and VM-validated)

Migration 013 retains one envelope-encrypted P-256 account key per fixed
Let’s Encrypt environment. An administrator-only replay-safe operation performs
RFC 8555 registration, while the typed agent atomically presents and removes
strict tokens below a root-owned fixed directory. Deterministic NGINX locations
bypass canonical/customer redirects and caches. Debian 13, Ubuntu 26.04, and
enforcing Rocky Linux 10.2 served and removed real challenge responses. See
[ACME account and HTTP-01 routing](acme-account-and-http01.md).

Acceptance:

- Challenge routing bypasses customer redirects/cache safely.
- ACME credentials are encrypted and never available to account users.
- Staging CA is used in automated tests.

### G-002 — Certificate lifecycle (`P0`, implemented and VM-validated)

Migration 014 and the `tls.certificate.lifecycle` worker persist a replayable
RFC 8555 order and encrypted P-256 certificate key, validate the exact SAN/key/
chain result, stage only a typed fixed-path agent bundle, and reuse the F-003
NGINX transaction before atomically swapping active state. Initial issuance and
jittered renewal are scheduled automatically with bounded retry rounds; removal
or TLS-intent changes retire the old record. See
[Certificate lifecycle](certificate-lifecycle.md) and
[ADR 0026](adr/0026-transactional-certificate-lifecycle.md).

The fixed-path artifact staging, vendor `nginx -t`, transactional reload, and
real TLS handshake pass on Debian 13, Ubuntu 26.04, and Rocky Linux 10.2. Rocky
remains SELinux enforcing with `httpd_config_t`; chain/key modes are root-owned
`0644`/`0600`.

The qualified lifecycle now also traverses the authenticated account HTTP API,
persisted worker, kernel-credential-verified Unix agent RPC, real NGINX HTTP-01
routes, and a private RFC 8555 CA. Issuance, scheduled renewal, predecessor
retirement, failed-renewal retention, bounded non-secret history, challenge
cleanup, and domain retirement pass from a clean checkpoint on all three
supported distributions.

Acceptance:

- A failed issuance/renewal never removes a valid active certificate.
- Certificate names match the activated domain set.
- Renewal is jittered, observable, and retry-bounded.

## Epic H: UI and localization

### H-001 — Accessible application shell (`P0`, implemented and browser-validated)

The Vue shell now uses native labelled landmarks, a reliable skip link, one
current-page navigation state, live service status, and programmatic H1 focus.
Its narrow drawer makes background content inert, traps Tab/Shift+Tab, closes
with Escape/backdrop, and restores focus. Axe/jsdom and interaction tests pass;
manual Chromium review passed on desktop and at 320×720 without horizontal
overflow, console warnings, or keyboard-flow defects. See
[Accessible application shell](accessible-application-shell.md).

Acceptance:

- Keyboard navigation, focus behavior, labels, landmarks, validation messages,
  contrast, and reduced-motion behavior pass the selected automated checks and
  manual critical-flow review.
- Desktop and narrow mobile layouts remain usable.

### H-002 — Localization foundation (`P0`, implemented and browser-validated)

English is the typed source locale and German has exact key and interpolation
parity. A central `Intl` boundary formats dates, numbers, percentages, binary
bytes, byte rates, and durations while rejecting invalid measurements. CI now
parses Vue templates and rejects untranslated critical text and attributes. The
fixed-timestamp browser flow passed in both locales and the Europe/Berlin time
zone. See [Localization foundation](localization-foundation.md) and
[ADR 0029](adr/0029-centralized-intl-formatting-and-template-literal-gate.md).

Acceptance:

- English source and German catalogs exist.
- Dates, numbers, bytes, rates, and durations use locale-aware formatting.
- CI detects missing keys and literal critical UI strings.

### H-003 — Administrator Phase 1 flows (`P0`, implemented and browser-validated)

Bootstrap, password/MFA login, session restoration, dashboard, package/account
creation, account-selected domain lifecycle actions, host services, operation
progress, audit history, settings/logout, and an honest release-discovery view
now form one responsive administrator console. Bounded admin APIs
reuse server-side authorization, recent-authentication, CSRF, immutable package,
and durable-operation contracts. See
[Administrator Phase 1 flows](administrator-phase1-flows.md) and
[ADR 0030](adr/0030-authenticated-bounded-administrator-console.md).

Build login/bootstrap, dashboard, packages, accounts, domain operations, service
health, operation progress, audit history, and update status. Phase 6 extends
that status with immutable stable/beta release discovery; functional updating
remains separate.

### H-004 — Account-owner Phase 1 flows (`P0`, implemented and browser-validated)

Server-derived self-service context now separates platform capability from
explicit active memberships and returns bounded current package/domain usage
data without host internals. The responsive owner workspace provides account
dashboard/selection, static-domain create/edit/lifecycle actions, TLS status and
retry, honest package limits/usage, own profile editing, and identity-scoped
session revocation in English and German. See
[Account-owner Phase 1 flows](account-owner-phase1-flows.md) and
[ADR 0031](adr/0031-explicit-self-service-account-context.md).

Build login, dashboard, domain list/create/edit/remove, TLS status, package usage,
profile, and session management.

J-002 extends the same domain workspace with host/package-approved PHP targets
and bounded pool health without broadening its membership authorization model.

### H-005 — Installed panel ingress and ACME setup (`P0`, implemented and VM-qualified; browser entrypoint validated)

The installer now publishes the immutable SPA and loopback API as one HTTPS
origin on dedicated TCP port `8443`, using an atomic root-only local bootstrap
certificate without weakening the rejecting customer listener on `443`.
Preflight, firewall, AppArmor/SELinux, exact file checks, NGINX validation, and
static/API health probes cover the endpoint. Administrator settings expose the
existing fixed-environment Let's Encrypt production registration API with
explicit terms acceptance and a pending-operation guard. See
[Installed panel ingress](installed-panel-ingress.md) and
[ADR 0035](adr/0035-dedicated-bootstrap-panel-ingress.md).

The exact release artifact passes installer/no-op, HTTPS static/API health,
SELinux/AppArmor, and the Phase 1 suite on all three supported guests. Rocky
uses a dedicated SELinux API-port type instead of broad HTTPD network-connect
booleans. The real browser has reached the unmocked installed bootstrap route
and API state and switched the installed release between German and English.
The temporary verification adapter pinned the exact self-signed bootstrap
certificate fingerprint and did not modify the host trust store. Submitting
credentials and a production ACME registration remains an explicit operator
action.

The account workspace now retains every returned domain/certificate operation
ID, polls a separately authorized account-scoped status endpoint, announces
bounded progress accessibly, and reloads authoritative account/domain state at
the terminal result. API responses expose no operation payload, result,
request/idempotency key, lease, or worker metadata.

Account users can now expand the existing non-secret certificate-history API
inside a domain card. Active and retired records expose exact names, issuer,
validity, renewal/activation/retirement timestamps, and fingerprint while PEM,
key, and authority URL fields remain absent. Open histories refresh when the
tracked certificate operation succeeds.

Acceptance:

- A fresh supported host serves the real application and proxies the API over
  HTTPS without making the Go process public.
- Strict host-only cookies work on one origin and no user input becomes NGINX
  configuration or an authority URL.
- The administrator can establish the production ACME prerequisite in the
  browser before creating a TLS domain.

## Epic I: Installer and release qualification

### I-001 — Preflight-only installer (`P0`, implemented)

The standalone installer now combines the typed host detector with bounded
CPU, memory, and hosting-storage measurements, produces actionable normalized
checks, and always emits a distribution-specific packages/files/users/services/
ports/security plan. It exposes no mutation command. See
[Read-only installer preflight](installer-preflight.md) and
[ADR 0032](adr/0032-read-only-installer-preflight.md).

Acceptance:

- Unsupported OS, architecture, ports, storage, cgroups, quotas, or conflicting
  services produce actionable failures.
- The plan lists packages, files, users, services, ports, and security-module
  changes.

### I-002 — Idempotent fresh-host installation (`P1`, implemented)

The release-bound installer now persists fixed stages atomically, resumes an
incomplete stage, verifies completed state without rewriting its journal, and
installs exact file metadata, hardened systemd units, dedicated firewall state,
and enforcing AppArmor/SELinux integration. The GitHub bootstrap downloads only
a versioned checksum-verified release. See
[Fresh-host installation](installer-installation.md) and
[ADR 0033](adr/0033-journaled-idempotent-fresh-host-installation.md).

Acceptance:

- A successful rerun changes nothing unexpectedly.
- An interrupted install resumes or rolls back from recorded stages.
- File ownership, modes, systemd sandboxing, firewall integration, and
  SELinux/AppArmor policy are verified.

### I-003 — Phase 1 qualification suite (`P1`, implemented)

The release installer harness now optionally runs the complete destructive
Phase 1 integration suite and machine-checks cross-account, failure-recovery,
filesystem/quota, and performance evidence. The same final archive passed on
Debian 13, Ubuntu 26.04 LTS, and Rocky Linux 10.2. See
[Phase 1 qualification](phase1-qualification.md),
[Phase 1 security review](phase1-security-review.md), and
[Phase 1 performance baseline](phase1-performance-baseline.md).

Acceptance:

- All three amd64 distributions pass install/domain/remove/reconcile scenarios.
- Cross-account and injected-failure suites pass.
- A security review checklist and baseline performance report are published.

## Epic J: PHP and databases

### J-001 — Native account PHP-FPM lifecycle (`P0`, implemented and VM-validated)

The installer converges one approved native PHP runtime per target
distribution and disables its global vendor pool. A closed typed agent request
creates version-specific configuration, unit, PID, and socket paths from the
hosting identity, validates FPM syntax, activates the hardened service in the
account slice, and verifies exact runtime metadata. Domain desired state uses
an additive-before-NGINX/exact-after-NGINX reconcile so sockets are neither
created too late nor retired too early. PHP roots receive typed access and a
narrow writable SELinux label on Rocky. See
[Managed PHP runtime](managed-php-runtime.md),
[ADR 0036](adr/0036-native-account-scoped-php-fpm.md), and the
[three-guest result](../infra/host-tests/results/2026-08-25-php-hyper-v.md).

Acceptance:

- FPM workers run as the hosting account in its cgroup slice.
- Only NGINX can connect to the mode-`0600` account socket.
- PHP can write its own typed root but cannot read a hostile foreign fixture.
- Failed activation restores prior files and unit state.
- Domain removal retires only unreferenced Stackfort-owned pool artifacts.

### J-002 — Account PHP controls and health (`P0`, implemented and VM-qualified)

Expose host-approved versions in package/account responses, add accessible
PHP/static target selection to domain create/edit, and show bounded pool health
and usage without exposing unit paths, process arguments, or other tenants.
The administrator can enable only the exact native runtime reported by the host
when creating an immutable package. Account availability is the intersection
of that package snapshot and current host approval. A separately authorized
read-only endpoint reports pool state and aggregate memory/CPU-time/process
counters; its agent request derives and validates the exact account unit and
cgroup but omits both. English/German and Axe tests pass, and real active and
retired observations pass on all three supported guests. See
[Account PHP controls and health](account-php-controls.md) and
[ADR 0037](adr/0037-tenant-scoped-php-capability-and-observability.md).

Acceptance:

- The UI offers only versions allowed by both host capability and package.
- English and German validation/progress/error strings are complete.
- Pool health and account resource use remain tenant-scoped and bounded.

### J-003 — MariaDB lifecycle and database wizard (`P0`, implemented and VM-qualified)

Install and adopt MariaDB safely, define prefixed account database/user
records, implement typed least-privilege agent operations, and build a
replay-safe account database wizard with explicit deletion safeguards.

Acceptance:

- Database/schema/user names are derived and tenant-owned.
- Passwords never appear in process arguments, logs, or API read models.
- Cross-account grants and destructive ambiguity are rejected.

The installer now adopts the distribution MariaDB service and verifies its
state. Account databases and users use an exact full-UUID prefix, encrypted
credentials, composite account ownership, durable mutations, and fixed
read-only/read-write grants. The bilingual four-step wizard, one-time
credential reveal, and confirmation-bound deletion flows are backed by two
closed agent operations. Real grant isolation, denied cross-account access,
replay, revocation, and removal pass on all three supported guests. See
[Account database lifecycle](account-database-lifecycle.md) and
[ADR 0038](adr/0038-tenant-scoped-mariadb-lifecycle.md).

### J-004 — Secure phpMyAdmin signon (`P0`, implemented and VM-qualified)

Expose an account-scoped automatic-login action for one active managed
database user without returning its password to the browser. Use a short-lived
session-bound one-time handoff, a dedicated authenticated loopback broker, and
an isolated unprivileged phpMyAdmin FPM runtime.

Acceptance:

- A handoff is tenant-, identity-, session-, audience-, and recent-auth-bound,
  stored only as a digest, expires after 30 seconds, and can be redeemed once.
- Tokens and database passwords are absent from URLs, persistent browser
  state, API read models, general logs, and audit details.
- phpMyAdmin receives only the selected localhost principal and never a panel
  or MariaDB administrative credential.
- The release chooses distribution-patched native packages on Debian/Ubuntu
  and a hash-pinned official bundle on Rocky.
- The dedicated capability-free FPM service, loopback broker, NGINX routes,
  AppArmor/SELinux policy, and installer no-op contract pass on all supported
  guests.

The API, Vue action, launcher, signon script, HMAC broker, installer packaging,
and three-guest harness are implemented. See
[Secure phpMyAdmin signon](phpmyadmin-signon.md),
[ADR 0039](adr/0039-session-bound-phpmyadmin-signon.md), and the
[three-guest result](../infra/host-tests/results/2026-08-26-phpmyadmin-hyper-v.md).

### J-005 — Managed database password rotation (`P0`, implemented and VM-qualified)

Rotate an active account principal through a durable, recent-authenticated
workflow without replacing the working control-plane credential before the
host mutation succeeds.

Acceptance:

- The generated candidate remains separately envelope-encrypted until
  MariaDB reports success; overlapping candidates and unsafe deletion are
  rejected.
- Host replay applies the same candidate only to an existing active
  same-account `localhost` principal.
- Promotion resets one-time reveal, advances a generation, revokes outstanding
  phpMyAdmin handoffs, and makes existing non-persistent phpMyAdmin sessions
  fail authentication with the old password.
- The old password fails and the new password retains the existing grant on
  Debian 13, Ubuntu 26.04, and Rocky Linux 10.

Migration 018, core lifecycle, typed agent operation, MariaDB reconciler,
generation fence, worker, HTTP route, and bilingual UI action are implemented.
All local Go/web suites and the real old/new-password and stale-replay checks
pass with the same artifact on all three supported guests. See
[Managed database password rotation](database-password-rotation.md) and
[ADR 0040](adr/0040-failure-safe-database-password-rotation.md), plus the
[three-guest result](../infra/host-tests/results/2026-08-26-database-password-rotation-hyper-v.md).

### K-001 — Safe file-manager navigation (`P0`, implemented and VM-qualified)

Expose bounded account-relative directory navigation before introducing file
content transfer or mutations.

Acceptance:

- Owners and members are authorized independently from auditors and outsiders.
- Absolute paths, traversal, alternate separators, symlink following, ownership
  mismatches, and cross-device descent are rejected.
- The typed agent response contains at most 100 metadata entries and uses an
  opaque resumable cursor without rescanning the whole directory.
- Invalid UTF-8/control-character names are counted but never transformed into
  a different actionable name.
- The English/German account UI supports root, child, parent, and paginated
  navigation without exposing absolute host paths or file contents.

The control-plane service, typed agent/client operation, Linux
descriptor-relative implementation, HTTP route, bilingual UI, and focused
tests are implemented. See [File-manager foundation](file-manager-foundation.md)
and [ADR 0041](adr/0041-descriptor-relative-file-manager-boundary.md), plus the
[three-guest result](../infra/host-tests/results/2026-08-26-file-manager-navigation-hyper-v.md).

### K-002 — Secure streaming file download (`P0`, implemented and VM-qualified)

Add constant-memory download without expanding the metadata RPC into an
arbitrary privileged file channel.

Acceptance:

- Authorization and host readiness are rechecked for each account-relative
  regular-file request; auditors and outsiders receive no file content.
- A per-request helper reads as the account UID/GID and independently enforces
  descriptor-relative, no-symlink, ownership, same-device, and regular-file
  constraints.
- Downloads support one HTTP byte range, correct `200`/`206`/`416` metadata,
  safe attachment names, cancellation, and no application-layer buffering.
- Four concurrent streams and 4 GiB per response bound agent resources;
  larger files can be retrieved through bounded ranges.
- English/German UI actions are available only for regular listed files.

The public API, separate agent streaming endpoint, account-credential helper,
typed client/service, bilingual UI, adversarial tests, and three-guest
qualification are complete. See [File-manager foundation](file-manager-foundation.md),
[ADR 0042](adr/0042-account-credential-file-download-stream.md), and the
[qualification result](../infra/host-tests/results/2026-08-28-file-manager-download-hyper-v.md).

### K-003 — Resumable staged upload and safe creation (`P0`, implemented and VM-qualified)

Add constant-memory file upload without exposing partial destinations or
allowing a retry to replace existing account data.

Acceptance:

- Owners and members require `account.files.manage`; auditors and outsiders
  cannot initiate, inspect, write, complete, cancel, or create.
- Uploads use opaque sessions and sequential exact-offset chunks of at most
  8 MiB. The real locked part-file length remains the authoritative resume
  offset after a process or connection interruption.
- Incomplete content stays in a fixed hidden account staging directory and is
  never visible at the destination. Each acknowledged chunk is `fsync`ed.
- Completion checks the exact final size, computes SHA-256, verifies an optional
  expected digest, and atomically activates with no-replace semantics.
- Empty files and directories are created descriptor-relatively without
  following symlinks, with modes `0640` and `0750`; existing entries survive.
- Four concurrent write requests, 4 GiB per upload, 30 minutes per request, and
  eight active sessions per account bound host resources.
- Every mutation is correlated to a durable authorization audit event before
  host access; the English/German UI supports resume, progress, cancel, and
  empty-file/directory creation.

The public API, separate agent write stream, account-credential helper,
typed client/service, bilingual UI, hostile-path tests, and three-guest
qualification are complete. See [File-manager foundation](file-manager-foundation.md),
[ADR 0043](adr/0043-account-credential-staged-file-write.md), and the
[qualification result](../infra/host-tests/results/2026-08-29-file-manager-write-hyper-v.md).

### K-004 — Bounded file operations and recoverable trash (`P0`, implemented and VM-qualified)

Complete normal file namespace management without partial copies, silent
replacement, unbounded recursion, or direct irreversible deletion.

Acceptance:

- Rename and move support only account-owned regular files/directories on the
  managed filesystem and use atomic no-replace activation.
- Recursive copy remains hidden in fixed account-owned staging until complete;
  it rejects links/special files and is limited to 10,000 entries, 64 levels,
  4 GiB, four concurrent requests, eight staging trees, and 30 minutes.
- Kernel project quota exhaustion is returned as a stable typed error and no
  partial destination becomes visible.
- Normal delete atomically moves an entry into fixed hidden trash. Ordered
  pages contain at most ten entries and the account retains at most 256.
- Restore targets only the recorded original canonical path and never replaces
  a recreated entry. Trash continues counting against byte/inode quota.
- Permanent purge preflights the same entry/depth/byte limits, then deletes only
  verified regular files/directories without following links.
- Owners/members receive English/German rename, copy, move, trash, restore, and
  permanent-delete actions; every mutation has durable audit correlation.

The protocol/helper, service/API, bilingual UI, adversarial tests, project-quota
test, and three-guest qualification are complete. See
[File-manager foundation](file-manager-foundation.md),
[ADR 0044](adr/0044-bounded-file-operations-and-recoverable-trash.md), and the
[qualification result](../infra/host-tests/results/2026-08-29-file-manager-operations-hyper-v.md).

### K-005 — Bounded archive creation and hostile-input extraction (`P0`, implemented and VM-qualified)

Add useful archive workflows without allowing an account-controlled archive to
escape its root, create active links or special files, consume unbounded host
resources, replace existing content, or expose partial output.

Acceptance:

- Creation supports exactly ZIP and tar.gz from one account-owned regular file
  or directory and requires a matching destination suffix.
- Create and extract run under the derived account credential in a fixed hidden
  operation tree, with 10,000-entry, 64-level, 4-GiB, duration, concurrency,
  staging-count, filesystem-device, and project-quota bounds.
- Extraction snapshots the regular archive before parsing and activates only a
  complete, `fsync`ed, newly created destination directory.
- Archive names use the canonical account-relative grammar. Traversal,
  alternate separators, duplicates, namespace collisions, links, special
  files, encrypted/unsupported ZIP entries, and unsafe metadata are rejected.
- Compressed input has a ratio-sensitive expansion bound in addition to the
  absolute output bound; the complete decompressed gzip stream is limited.
- Existing destinations are never replaced. Success, conflict, cancellation,
  quota failure, and hostile input leave no visible partial output or hidden
  operation residue.
- Owners/members receive English/German pack and extract actions, while every
  mutation remains CSRF-protected and durably audit-correlated.

The closed protocol/helper, service/API, bilingual UI, attack corpus, static
analysis, and three-guest qualification are complete. See
[File-manager foundation](file-manager-foundation.md),
[ADR 0045](adr/0045-bounded-archive-creation-and-hostile-extraction.md), and the
[qualification result](../infra/host-tests/results/2026-08-29-file-manager-archives-hyper-v.md).

### K-006 — Authenticated local file backup and staged restore (`P0`, implemented and VM-qualified)

Add a trustworthy privileged replacement source without misrepresenting a file
archive as an application-consistent full-account backup.

Acceptance:

- Expose exactly `account_files` for visible top-level account content and
  `document_root` for one canonical directory; explicitly exclude databases,
  TLS material, email, and server/control-plane configuration.
- Keep artifacts outside the account root in a fixed root-only account/UUID
  repository with schema-1 metadata and an independent root-only HMAC-SHA-256
  key.
- Bind account, backup ID, scope/path, UTC creation time, payload/content
  totals, entry count, and payload SHA-256 into the authenticated manifest.
- Create and extract payloads through the production helper under the derived
  account UID/GID, with descriptor-relative no-symlink traversal and fixed
  10,000-entry, 64-level, 4-GiB, concurrency, and duration bounds.
- Publish only a completely parsed and authenticated backup. Verify the full
  manifest and payload before materializing restore staging or changing visible
  content.
- Replace one document root as a staged unit or the complete preflighted visible
  top-level account set, preserving internal Stackfort roots and rolling back
  ordinary activation failures.
- Permit owner/member list, inspect, verify, and creation. Restrict restore to
  the owner with recent authentication, CSRF, exact UUID confirmation, and a
  persisted authorization audit correlation.
- Reject altered payloads, symlink sources, unsafe archive members, repository
  metadata conflicts, and cross-account lookups without exposing partial
  output or leaked staging.

The closed protocol/helper, service/API, bilingual UI, adversarial integration
test, and Debian 13, Ubuntu 26.04, and Rocky Linux 10 qualification are complete.
See [Local file backup foundation](local-file-backup-foundation.md),
[ADR 0046](adr/0046-authenticated-local-file-backup-and-staged-restore.md), and
the [qualification result](../infra/host-tests/results/2026-08-29-local-backup-restore-hyper-v.md).

### K-007 — Portable backup transfer, manual retention, and repository quota (`P0`, implemented and VM-qualified)

Complete safe local artifact mobility and bounded retention without exporting a
host trust key or implying application-consistent coverage.

Acceptance:

- Export only a fully authenticated and completely verified portable
  `payload.tar.gz`; retain host-bound manifests and HMAC keys locally.
- Support bounded single-range download resume and exact-offset 8-MiB import
  chunks with persisted root-owned status, cancellation, and eight active
  imports per account.
- Reserve declared apparent import size immediately and measure published plus
  staged bytes against the assigned package revision's independent
  `backupStorageBytes` limit (20-GiB default, 1-MiB through 1-TiB explicit).
- Recalculate SHA-256 and parse every archive entry at completion, then create a
  new locally authenticated manifest and publish with no-replace semantics.
- Expose measured bytes/counts and English/German download, resume/import,
  cancel, and delete flows.
- Restrict permanent deletion to an owner with recent authentication, CSRF,
  exact UUID confirmation, and persisted audit correlation; safely permit
  deletion of a corrupted manifest.
- Qualify full/range transfer, import publication, deletion, quota rejection,
  repository isolation, and cleanup on Debian 13, Ubuntu 26.04, and Rocky 10.

The closed protocol, root-agent implementation, service/API, bilingual UI,
adversarial integration coverage, and three-guest qualification are complete.
See [ADR 0047](adr/0047-portable-backup-transfer-retention-and-quota.md) and the
[qualification result](../infra/host-tests/results/2026-08-29-backup-transfer-retention-hyper-v.md).

### K-008 — Privacy-minimized domain log views and bounded retention (`P0`, implemented and VM-qualified)

Expose useful per-domain request/error diagnostics without making the hosting
panel a second credential or behavioral-data store.

Acceptance:

- NGINX access capture contains only timestamp, client, host, method, path
  without query, status, response bytes, and duration; authorization/cookie
  headers, referrer, user agent, complete request line, and query string are
  absent from the format.
- Per-account directories are root-only `0700`; opaque per-domain active/error
  files are root-owned `0640`. Creation and reading reject symlinks, hard links,
  foreign ownership, unsafe modes/types, and replaced parent directories.
- The agent reads only the fixed active and uncompressed `.1` rotation through
  `O_NOFOLLOW`, scans at most 256 KiB per page, returns at most 50 typed records,
  and rejects stale canonical inode/offset cursors.
- Access paths and native NGINX error messages receive a second capped redaction
  pass for query tails, credentials, sensitive path segments, control text, and
  invalid UTF-8 before crossing the agent boundary.
- The control plane derives identity/domain from account state, rechecks host
  readiness and `account.logs.view`, and prevents cross-account substitution.
- The English/German account UI distinguishes access/error fields, supports
  domain/kind selection and older-page loading, and explains redaction and
  retention without claiming that all sensitive application paths are absent.
- `logrotate` enforces daily/seven-day/seven-rotation retention plus an 8 MiB
  active-file threshold using delayed compression and NGINX `USR1`, not
  `copytruncate`; Rocky keeps SELinux enforcing with `httpd_log_t`.

The NGINX baseline/renderer, root log manager, typed agent/client, authorization
service/API, bilingual accessible UI, installer policy, adversarial tests, and
three-guest qualification are complete. See
[Domain log observability](domain-log-observability.md),
[ADR 0048](adr/0048-privacy-minimized-domain-logs.md), and the
[qualification result](../infra/host-tests/results/2026-08-29-domain-log-redaction-retention-hyper-v.md).

### K-009 — Closed systemd scheduled account jobs (`P0`, implemented and VM-qualified)

Add cron-like Shell/PHP automation without creating an arbitrary command or
systemd-configuration interface.

Acceptance:

- Persist only a named account-relative `.sh`/`.php` script, a package-approved
  PHP version where applicable, enabled state, and fixed interval/hourly/daily/
  weekly UTC schedule; reject raw commands, executables, unit names,
  environments, and calendar expressions.
- Enforce the immutable package revision's `maxScheduledJobs` with zero as
  unavailable and a 1,000-job absolute ceiling.
- Derive root-owned service/timer names from account UID plus job UUID and run
  the oneshot service as the exact account identity below its resource slice
  with a five-minute ceiling and reviewed systemd sandbox.
- Validate the script descriptor-relatively on one filesystem and reject
  symlinks, hard links, foreign ownership, unsafe type/mode, and oversized
  content before unit mutation.
- Reconcile create/update/enable/disable/delete transactionally with durable
  operations, idempotency, optimistic revisions, fixed `systemctl` profiles,
  exact agent-response verification, rollback, and persistent-state cleanup.
- Provide owner/member management, auditor viewing, package administration,
  operation polling, and accessible English/German account UI.
- Qualify real Shell/PHP execution, every calendar form, systemd validation and
  sandbox properties, account slice/identity, `PrivateTmp`, hostile links, and
  lifecycle cleanup on Debian 13, Ubuntu 26.04, and Rocky Linux 10.

The closed protocol, durable repository/worker, privileged Linux reconciler,
package/admin integration, account API, bilingual UI, adversarial tests, and
three-guest qualification are complete. See
[Scheduled account jobs](scheduled-jobs.md),
[ADR 0049](adr/0049-closed-systemd-scheduled-account-jobs.md), and the
[qualification result](../infra/host-tests/results/2026-08-30-scheduled-jobs-hyper-v.md).

### K-010 — Closed per-domain Coraza/OWASP CRS policy (`P0`, complete)

Add WAF policy without exposing SecLang or native request diagnostics.

Acceptance:

- Persist and audit only off, detection-only, and OWASP CRS PL1 blocking modes,
  with off as the migration/new-domain default.
- Carry policy through replay-compatible durable domain operations and immutable
  desired-state revisions.
- Render only root-owned fixed profile paths, with an explicit ACME HTTP-01
  bypass and candidate-tested atomic NGINX activation.
- Disable raw audit/debug output and withhold complete native Coraza
  diagnostic lines from the generic account error-log view.
- Provide accessible English/German account and administrator controls.
- Build and verify the pinned Coraza 3.7.0/libcoraza 1.7.0/coraza-nginx
  0.20.0/CRS 4.25.1 tuple for each exact NGINX ABI, then qualify
  benign/malicious corpora and rollback on all three guests.

The complete boundary passes unit, adversarial, installer, and three-guest
runtime qualification. Source/target locks and the native DEB/RPM builder pass
exact-ABI loads and byte-identical repeat builds; Ubuntu uses the exact patched
distribution source package. The manifest-bound journaled installer verifies
hashes, native metadata, qualification inventory, ELF linkage, NGINX version,
ownership, and isolated module loading with rollback. Fresh installs pass the
benign/malicious corpus in all three modes, persistent connections, static,
PHP, redirects, TLS, ACME bypass, reload rollback, AppArmor/SELinux, package
retirement, and bounded performance gates on Debian 13, Ubuntu 26.04, and Rocky
Linux 10. GitHub release attestations cover the archive and embedded manifest.
See the
[build qualification](../infra/host-tests/results/2026-08-30-waf-build-qualification-hyper-v.md),
[native package qualification](../infra/host-tests/results/2026-08-30-waf-native-packages-hyper-v.md),
[release installer qualification](../infra/host-tests/results/2026-08-31-waf-release-installer-hyper-v.md),
[historical ModSecurity runtime qualification](../infra/host-tests/results/2026-08-31-waf-runtime-hyper-v.md),
[Coraza runtime comparison](../infra/host-tests/results/2026-08-31-coraza-runtime-hyper-v.md),
[WAF foundation](waf-foundation.md) and
[ADR 0051](adr/0051-coraza-nginx-waf-engine.md).

### K-011 — Sanitized WAF event view (`P0`, complete)

Expose actionable domain-scoped WAF events without returning native Coraza
diagnostics or attacker-controlled matched data.

Acceptance:

- Patch the pinned connector reproducibly and emit only rule ID, severity,
  server correlation ID, method, and normalized queryless URI.
- Keep native Coraza diagnostics wholly withheld from generic account logs and
  enforce bounded, symlink-safe event pagination through the agent protocol.
- Provide an English/German account event view and qualify that sensitive
  query/match values never cross the API boundary.

The connector patch has its own SHA-256 lock and is included in native package
qualification. Unit, API, UI, and three-guest hostile-query tests pass.

### K-012 — Narrow administrator WAF exceptions (`P0`, complete)

Allow a time-bounded false-positive exception without exposing raw SecLang.

Acceptance:

- Require an administrator with recent platform authorization and an enabled
  package feature; account users cannot create exceptions.
- Accept one inbound CRS rule ID (`920000`–`944999`) and an exact path and/or
  exact parameter, with no regex/wildcard/header/body input.
- Limit each domain to 64 active exceptions and each lifetime to five minutes
  through thirty days; persist, audit, replay, expire, and remove safely.
- Render only Stackfort-owned guard rules and retain candidate validation,
  atomic activation, and rollback.

The repository, durable schema-4 lifecycle, renderer, API, bilingual admin UI,
and WAF-before-cache runtime corpus are complete.

### K-013 — Supported Vinyl 9.0.1 native packages (`P0`, complete)

Provide a reproducible cache runtime instead of relying on unavailable or
incompatible distribution packages.

Acceptance:

- Build a source- and VCL-hash-locked amd64 DEB/RPM for Debian 13, Ubuntu
  26.04, and Rocky Linux 10 and bind all three to the release manifest.
- Run data and authenticated management listeners on loopback only, reject
  unsafe secret metadata, verify installed package drift/VCL, and sandbox the
  dedicated service.
- Install dependencies transactionally, including EPEL/jemalloc through DNF
  on Rocky, and pass AppArmor/SELinux-enforcing runtime qualification.

All native packages and the journaled install/verify/rollback path pass the
three-guest matrix. See the [packaging contract](../packaging/vinyl/README.md).

### K-014 — Safe cache policy, purge, metrics, and comparison (`P0`, complete)

Add an opt-in full-page cache without serving authenticated or personalized
content across requests, accounts, or domains.

Acceptance:

- Persist only `disabled`, `respect_origin`, and `wordpress`; disabled remains
  the default and customer input never becomes VCL.
- Execute Coraza before every cache lookup/hit; bypass credentials, cookies,
  bodies, unsafe methods, sensitive paths, and personalized/private responses.
- Provide bounded data-minimized HIT/MISS/BYPASS metrics and an audited,
  canonical-domain/literal-path scoped purge through authenticated `vinyladm`.
- Publish one repeatable NGINX FastCGI cache versus Vinyl matrix on every
  supported distribution and make the product recommendation follow evidence.

The Phase 4 exit gate passes on all guests. Vinyl remains optional because it
measured 27.5–28.6% of NGINX FastCGI cache throughput with WAF off. Coraza
narrows the gap, but Vinyl still reaches only 82.6–87.6% in DetectionOnly and
86.4–86.9% in Blocking PL1.
See the [cache foundation](cache-foundation.md),
[ADR 0052](adr/0052-opt-in-vinyl-cache-behind-nginx-and-coraza.md), and
[qualification result](../infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md).

## Phase 5 backlog: rootless OCI applications

### L-001 — Constrained tenant-owned application drafts (`P0`, complete)

Establish the application parent and reject unsafe container intent before any
Podman execution exists.

Acceptance:

- Persist tenant-owned, revision-fenced drafts behind the package feature and
  count limit, with account-unique slugs, logical removal, and audit events.
- Accept only an explicit-registry digest-pinned image or normalized
  account-relative Containerfile source, one internal port, and a bounded HTTP
  or TCP health check.
- Omit privileged, namespace, device, host-mount, engine-socket, capability,
  command-override, and public-host-port fields entirely.
- Require an active, fully applied, same-account application before an OCI
  domain target can be persisted, with a matching SQLite trigger.
- Cover malicious references/paths, stale revisions, limits, retention, audit
  integrity, inactive applications, and cross-account access in tests.

See the [foundation](oci-application-foundation.md) and
[ADR 0053](adr/0053-constrained-oci-application-drafts.md).

### L-002 — Rootless Podman host capability and account runtime (`P0`, complete)

Detect and install the supported Podman/netavark/slirp runtime, provision only
account-owned rootless storage and runtime directories, and prove that the API
and workloads cannot access an engine socket or rootful daemon.

Acceptance:

- Install and report Podman, netavark, aardvark-dns, passt/pasta, slirp4netns,
  fuse-overlayfs, and distribution-specific subordinate-ID helpers.
- Require typed rootless, Quadlet, network, storage, and rootful-socket
  isolation capabilities before account mutation.
- Derive collision-free 65,536-ID subordinate UID/GID ranges and fixed storage,
  runtime, and root-owned Quadlet paths from the immutable account identity.
- Mask rootful and global-user Podman API units, reject both system and
  per-account socket artifacts, and expose no generic engine API operation.
- Make successful runtime preparation an immutable host-readiness condition and
  remove only empty/exact runtime state during archive-gated identity deletion.
- Cover mapping ambiguity/overlap, caller-selected values, typed RPC failures,
  capability detection, package selection, persistence, and replay convergence.

See [Rootless OCI account runtime](rootless-oci-runtime.md) and
[ADR 0054](adr/0054-rootless-podman-account-runtime.md).

### L-003 — Digest pull and bounded Containerfile build (`P0`, complete)

Resolve/pull by digest, build inside fixed CPU/memory/time/output limits, scan
the resulting image, and persist the immutable deployed digest without exposing
free-form Podman or build arguments.

Acceptance:

- Queue only a server-reconstructed, revision-fenced source and account
  identity through the correlated `oci.image.prepare` operation.
- Pull only explicit digest references with TLS verification, or snapshot and
  validate a bounded, symlink-resistant Containerfile context.
- Execute rootless Podman through fixed CPU, memory, process, file, network,
  output, and duration profiles with no engine API socket.
- Bundle checksum-pinned Trivy, scan a bounded OCI archive, and fail closed on
  scanner failure or any HIGH/CRITICAL result.
- Persist append-only deployed/source digests and policy evidence; require
  exact host/database replay convergence and retain no transaction archive.

See [Bounded OCI image preparation](oci-image-preparation.md) and
[ADR 0055](adr/0055-digest-pinned-bounded-oci-image-preparation.md).

### L-004 — Private network, secrets, and bounded volumes (`P0`, complete)

Create account-private networking, encrypted environment-secret references,
and descriptor-verified account-owned volumes. Reject public host ports,
arbitrary mounts, devices, namespaces, and capabilities.

Acceptance:

- Envelope-encrypt tenant values and keep plaintext out of public records,
  jobs, audit events, command arguments, and persistent agent/replay artifacts.
- Reconcile a deterministic per-account rootless bridge with DNS, strict
  Netavark isolation, exact labels, and no public host-port field.
- Derive all account-owned volume paths below a hidden reserved root, verify
  them descriptor-relatively, and inherit the account project quota.
- Fence durable work to image-approved revisions and secret generations while
  retaining append-only evidence across rotations.
- Provision project inheritance before rootless Podman storage, reject foreign
  network state and links, and prove replay and cross-account denial on a
  disposable Debian 13 host.

See [Private OCI resources](oci-private-resources.md) and
[ADR 0057](adr/0057-account-private-oci-resources.md).

### L-005 — Quadlet lifecycle, health, logs, and domain routing (`P0`, complete)

Generate fixed rootless Quadlets, reconcile through the typed agent boundary,
health-check before atomic domain activation, expose bounded sanitized logs,
and provide replay-safe deploy, suspend, resume, rollback, and remove flows.

Acceptance:

- Allocate one immutable high loopback port and render only fixed, root-owned,
  digest-pinned, private-network Quadlets with hardening and managed resources.
- Decrypt generation-fenced values only immediately before the authenticated
  local agent call, inject them through fixed Podman stdin, clear transient
  buffers, and retain no plaintext replay state.
- Probe the real loopback HTTP/TCP endpoint before persisting active/applied
  evidence; restore the previous workload on candidate failure.
- Render active OCI domain targets through fixed NGINX upstreams and the
  existing atomic activation/rollback pipeline; refuse disruptive lifecycle
  actions while an active route remains.
- Bound and sanitize journald output and make deploy, suspend, resume,
  rollback, and remove converge safely, including derived secret retirement.
- Pass unit/repository/protocol tests and the focused real Podman/systemd
  lifecycle qualification on the disposable Debian 13 guest.

See [Rootless OCI deployment lifecycle](oci-deployment-lifecycle.md),
[ADR 0058](adr/0058-health-gated-rootless-quadlet-lifecycle.md), and the
[qualification result](../infra/host-tests/results/2026-09-01-oci-deployment-lifecycle-hyper-v.md).

### L-006 — Aggregate accounting and three-guest exit matrix (`P0`, complete)

Place PHP, scheduled jobs, and OCI applications below the same account slice;
then qualify resource exhaustion, reboot recovery, private ingress, malicious
images/builds, and cross-account filesystem/network/process isolation on all
supported distributions.

The resource reconciler now installs one marker-owned, UID-derived
`user@<uid>.service.d` drop-in and verifies the live user-manager cgroup. A
wrongly placed existing manager is restarted only through a fixed profile and
must reappear below the account slice. Account provisioning applies the slice
before enabling the rootless runtime, closing the first-start and crash gaps.

The disposable Hyper-V matrix passes on Debian 13, Ubuntu 26.04, and Rocky
Linux 10. It covers OCI and generic PID/memory exhaustion, loopback-only
ingress, hostile source/Containerfile/schema/mount inputs, separate rootless
networks, filesystem traversal and process-signal denial, plus a real reboot
and healthy Quadlet replay. See the
[qualification result](../infra/host-tests/results/2026-09-01-oci-phase5-exit-matrix-hyper-v.md).

## Deferred from the current Phase 2 slice

Database/application-consistent backup,
SFTP, functional-updater upgrade-matrix qualification, and arm64 support remain
deliberately sequenced as described in `roadmap.md`.
