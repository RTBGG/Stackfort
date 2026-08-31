# Architecture

Status: Draft 0.1

## 1. System shape

The initial release manages one Linux node and separates browser-facing logic
from privileged host mutation.

```text
Browser
  |
  | HTTPS
  v
NGINX panel virtual host
  |
  v
control-api (unprivileged)
  |        \
  |         \ SQLite + encrypted secrets + audit chain
  |
  | authenticated Unix socket RPC
  v
host-agent (minimal privileged service)
  |
  +-- Linux users, directories, quotas, and cgroups
  +-- generated NGINX, PHP-FPM, Vinyl, and WAF configuration
  +-- MariaDB administrative connection through a local credential
  +-- rootless Podman/systemd account units
  +-- service validation, reload, health check, and rollback
```

## 2. Components

### 2.1 Web interface

Vue 3 and TypeScript produce static assets served by NGINX. The interface uses
English translation keys as its source and ships complete English and German
catalogs. It contains no server credentials and communicates only with the
control API.

### 2.2 Control API

The Go control API runs as a dedicated unprivileged system user. It owns:

- authentication, sessions, TOTP, recovery codes, and API tokens;
- authorization and account membership;
- product state and resource definitions;
- validation that does not need host privilege;
- operation scheduling, progress, cancellation boundaries, and user messages;
- audit-event creation;
- status aggregation and metrics presentation;
- the local RPC client for the host agent.

The API cannot execute arbitrary commands, write service configuration, access
account files directly, or connect as MariaDB root.

### 2.3 Host agent

The Go host agent is the only panel component allowed to perform privileged host
changes. Its local RPC protocol exposes typed operations such as:

- `EnsureAccount`
- `ApplyAccountLimits`
- `EnsureDomain`
- `ValidateWebConfiguration`
- `ActivateWebConfiguration`
- `EnsurePhpPool`
- `EnsureDatabase`
- `CreateBackup`
- `RestoreBackup`
- `InspectServices`

There is deliberately no `RunCommand(string)` endpoint. Inputs use strict
schemas, bounded sizes, canonical identifiers, and server-side path derivation.
The agent authenticates the peer through Unix-socket permissions and peer
credentials, and it rejects unexpected callers.

D-001 now implements that boundary with an exact Linux `SO_PEERCRED` UID check
before HTTP parsing, a strict 64-KiB versioned tagged-JSON contract, explicit
compatibility negotiation, bounded request/idempotency identifiers, and a
timeout-bounded control API client. Version 1 exposes `protocol.handshake`, the
read-only `host.capabilities.inspect`, and E-001's correlated
`hosting.identity.reconcile`/`hosting.identity.delete` mutations. E-002 adds
correlated `hosting.filesystem.reconcile` and `hosting.document-root.ensure`
with complete identity/quota or canonical relative-path schemas; every further
privileged operation requires its own typed schema and dispatch case. Capability inspection returns
independently typed distribution, systemd, cgroup, filesystem/quota, LSM, port,
package, and service states. See
[Local host-agent protocol](local-agent-protocol.md) and
[Host capability inspection](host-capability-inspection.md).

All unavoidable native utilities run through compiled-in agent execution
profiles. The profile—not the RPC caller—owns the absolute executable, argument
template, semantic allowlist, environment, timeout, and output bounds. Linux
process-group termination prevents ordinary descendants from surviving a
timeout or exhausted output budget. See
[Safe external process runner](safe-external-process-runner.md).

Each protocol operation also declares an audit policy. Privileged mutations
must carry the UUIDv7 durable operation and identity/system actor from the
control plane, plus the account when scoped. The agent binds this context into
idempotency and emits only fixed, payload-free event fields; unexpected socket
peers generate security events before HTTP parsing. See
[Agent audit correlation](agent-audit-correlation.md).

Every mutating request has an operation ID, actor, and idempotency key. The agent
records safe correlated outcomes; operation checkpoints record the stage so
retries do not duplicate destructive work.

L-003 adds `oci.image.prepare` to that closed boundary. The control plane queues
only a persisted source revision and derived account identity. The agent
performs a digest pull or bounded rootless build, exports a size-limited OCI
archive, scans it with the bundled Trivy executable, and returns only immutable
digest/policy evidence. Host replay manifests and database artifacts are
create-only; no engine socket or general container/build API exists. See
[Bounded OCI image preparation](oci-image-preparation.md).

### 2.4 Job runner

Long operations are persisted jobs, not synchronous HTTP handlers. A job has:

- immutable request and actor metadata;
- pending/running/succeeded/failed/cancelling status;
- current stage and percentage when meaningful;
- structured events safe for display;
- an idempotency key;
- retry classification;
- resulting resource revisions and audit references.

The first implementation can run workers in the control API process while
retaining a boundary that allows a separate worker service later. The generic
runner now uses UUIDv7 worker/attempt fencing, bounded renewable leases, and
append-only progress events. A stopped process leaves its attempt leased until
recovery applies the stored retry classification; see
[Persisted operations](persisted-operations.md).

### 2.5 State database

SQLite in WAL mode stores panel control state. It is not exposed to hosting
users and is independent from the MariaDB service managed for customer sites.
The self-contained Go driver embeds SQLite 3.53.3; Stackfort rejects versions
older than 3.51.3 because they lack the upstream WAL-reset race fix. State must
reside on a local filesystem.

Important record groups:

- identities, credentials, factors, sessions, API tokens;
- hosting accounts, immutable Unix identities, staged-removal tombstones,
  memberships, packages, and effective limits;
- domains, targets, TLS state, redirects, caching, and WAF policy;
- PHP pools, databases, database principals, applications, and jobs;
- backups, manifests, update state, service observations, and usage rollups;
- desired-state revisions, applied revisions, operations, and audit events.

All timestamps are UTC. User-facing localization occurs at presentation time.
Schema migrations are embedded, ordered, and covered by upgrade/rollback tests.
Applied migration names and normalized SQL checksums are immutable. Startup
refuses future, drifted, discontinuous, or unmanaged schemas.

Core records use canonical UUIDv7 identifiers. Package definitions have
immutable numbered revisions, and every account assignment keeps a complete
effective-limit snapshot. Desired-state revisions and audit events are
append-only; repository mutations and their SHA-256-chained audit event commit
atomically. Domain names retain both a stable Unicode display form and a
lowercase ASCII routing form; target and applied-state history is retained
rather than overwritten. See [Core records](core-records.md) and
[Domain records](domain-records.md).

Hosting accounts receive UUID-derived local names, reserved UID/GID allocations,
and canonical account roots in the creation transaction. Reconciliation rejects
foreign name/numeric collisions and deletion requires a separately confirmed
archive. See [Hosting-account Unix identity](hosting-account-unix-identity.md).

Account creation queues one immutable, retry-safe host snapshot covering that
identity, the current project-quota revision, and the current cgroup revision.
A deterministic repair scan closes the transaction-to-queue crash gap, and
domain mutations require all three boundaries to be confirmed available. See
[Durable hosting-account provisioning](hosting-account-provisioning.md) and
[ADR 0034](adr/0034-durable-hosting-account-provisioning.md).

### 2.6 Secret storage

Passwords are never reversibly stored. Secrets that must be retrieved—database
credentials, imported private keys, DNS provider tokens, backup keys—are stored
with envelope encryption:

- one host master key with restrictive filesystem permissions;
- independent data-encryption keys per secret or resource class;
- authenticated encryption with associated record identity and schema version;
- explicit key versioning and rotation support;
- secret values excluded from logs, job events, crash reports, and audit details.

Future TPM or external KMS support can replace master-key storage without
changing the record model.

Migration 007 applies this model to TOTP setup and active-factor secrets using
AES-256-GCM with independent per-record data keys. The API creates the external
`master.key` with mode `0600`, rejects symlinks/non-regular files, and can use an
absolute `STACKFORT_MASTER_KEY_PATH`. See
[TOTP, recovery codes, and session management](totp-recovery-and-session-management.md).

Migration 013 applies the same boundary to one P-256 ACME account key per fixed
CA environment. Registration is a replay-safe global operation and no browser
response contains credential material. See
[ACME account and HTTP-01 routing](acme-account-and-http01.md).

## 3. Reconciliation and configuration activation

The database is authoritative for panel-managed resources. Generated files are
artifacts, not an alternate editable database.

For a configuration change:

1. validate and authorize the requested desired state;
2. commit a new desired-state revision and enqueue an operation;
3. render all affected files into a private staging directory;
4. check ownership, modes, paths, references, and resource conflicts;
5. run native syntax/configuration validation;
6. snapshot the previously active generated artifacts;
7. atomically replace or switch the generated revision;
8. gracefully reload affected services;
9. run local and external-facing health checks as appropriate;
10. record the applied revision, or restore the prior revision on failure.

Manual customizations belong in explicitly documented include locations. The
agent never rewrites unmanaged main configuration files after initial setup.

## 4. Filesystem layout

Proposed managed paths:

```text
/etc/stackfort/                     root-owned configuration
/etc/stackfort/generated/           versioned generated service config
/var/lib/stackfort/                 SQLite, operation state, manifests
/var/lib/stackfort/staging/         private transactional staging
/var/lib/stackfort-agent/acme-http01/ root-owned public challenge responses
/var/log/stackfort/                 panel and root-owned per-domain web logs
/srv/hosting/accounts/<account>/    hosting-account root
/srv/hosting/backups/<account>/     local backup repository
/run/stackfort/                     Unix sockets and runtime state
```

Each account root contains, as features require:

```text
public_html/
domains/
applications/
backups/
tmp/
logs/
```

The account's public document roots never contain panel secrets or generated
server configuration.

## 5. HTTP data plane

### 5.1 Edge NGINX

The public NGINX layer owns:

- ports 80 and 443;
- TLS and ACME challenge routing;
- canonical-host and user redirect rules;
- request-size and connection/rate limits;
- Coraza and OWASP CRS;
- access logging and transfer accounting;
- direct static delivery;
- routing either through Vinyl or directly to an origin/application.

The panel itself uses a dedicated virtual host and never passes through customer
cache configuration.

For first setup, that boundary is the dedicated HTTPS management listener on
TCP `8443`. NGINX serves the immutable SPA and proxies only `/api/` to the
loopback control process. A locally generated certificate avoids plain-HTTP
sessions without weakening the rejecting default on customer port `443`; a
public panel hostname/certificate remains follow-up work. See
[Installed panel ingress](installed-panel-ingress.md) and
[ADR 0035](adr/0035-dedicated-bootstrap-panel-ingress.md).

F-001 runs the vendor binary through a Stackfort-owned main configuration at
`/etc/nginx/stackfort/nginx.conf`, selected by a root-owned systemd drop-in.
Rejecting defaults load before separate `panel-enabled` and `sites-enabled`
include points. Both directories are root-owned and unavailable to hosting
accounts; domain operations submit typed intent rather than NGINX source. Only
loopback peers may replace the apparent client address. Candidate baseline
changes must pass the exact managed `nginx -t` before restart/reload and restore
their filesystem snapshots on failure. See
[Managed NGINX baseline](managed-nginx-baseline.md) and
[ADR 0021](adr/0021-stackfort-owned-nginx-main-configuration.md).

F-002 adds a pure, bounded account renderer. It repeats persisted identity,
IDNA, path, target-union, redirect, and route-conflict validation; sorts all
semantically unordered input; and emits only fixed templates plus closed header
enums. User-controlled NGINX source does not exist in the schema. Redirect URL
literals and renderer-owned variables use separate construction paths, including
percent encoding a user `$` before NGINX compiles rewrite variables. The result
contains exact bytes and a SHA-256 digest but performs no filesystem or service
mutation. See [Deterministic NGINX configuration renderer](deterministic-nginx-config-renderer.md)
and [ADR 0022](adr/0022-typed-context-specific-nginx-rendering.md).

F-003 consumes that renderer through the account-scoped
`web.nginx-sites.activate` agent operation. The durable control operation stores
the typed domain snapshot; its UUID is also the host revision UUID. The agent
stages a complete global account-config set below `site-revisions`, validates it
through a private main configuration, and atomically moves the `sites-current`
symlink only after syntax success. A root-only phase journal brackets promotion,
graceful systemd reload, service inspection, and a local Host-routed probe.
Failure restores, validates, and reloads the former pointer. Agent restart
recovers the journal first; API replay finds either the same active manifest or
repeats the convergent transaction and records one operation-unique applied
revision. See [Transactional NGINX site activation](transactional-nginx-activation.md)
and [ADR 0023](adr/0023-crash-recoverable-nginx-site-revisions.md).

F-004 wraps domain persistence and F-003 activation in one replay-safe saga.
The account API requires authorization, CSRF proof, and idempotency; the worker
captures one operation-linked complete desired revision before touching the
host. It ensures document roots without following symlinks, grants only the
distribution NGINX worker exact POSIX access/default ACLs, and preserves
enforcing Rocky SELinux with narrow persistent web-content contexts. Pending
rows become active only after matching applied state, while suspend and logical
removal update routing without deleting files or root records. See
[Static-domain lifecycle](static-domain-lifecycle.md) and
[ADR 0024](adr/0024-replay-safe-static-domain-lifecycle.md).

G-001 adds a fixed HTTP-01 location to each eligible typed domain server. It
serves a root-owned token directory before canonical/customer redirect
locations and never routes through Vinyl. A separate typed agent operation can
only present or clean one validated RFC 8555 token; Rocky applies a persistent
narrow SELinux web-content context. ACME account keys are encrypted before the
first authority request so retry reuses the same identity. See
[ACME account and HTTP-01 routing](acme-account-and-http01.md) and
[ADR 0025](adr/0025-encrypted-acme-accounts-and-fixed-http01-routing.md).

### 5.2 Vinyl Cache

Vinyl binds only to a local TCP address or Unix socket and is unreachable from
the public network. It receives WAF-inspected traffic from the edge and forwards
misses to the origin.

VCL is assembled from versioned, tested templates. Account-selected presets map
to template parameters; they are not concatenated as source code. Cache purge
uses a local authenticated control path and scoped host/URL selectors.

Vinyl's Linux work directory and memory configuration follow upstream platform
guidance. Cache memory is reserved at the platform level so account limits do not
starve the host.

### 5.3 Origin NGINX and PHP-FPM

Origin listeners bind locally. Each hosting account has a PHP-FPM pool running
as its UID/GID and listening on its own Unix socket. Resource limits apply to the
account's encompassing systemd slice so PHP, jobs, and account-owned application
processes share the account budget.

The initial runtime matrix uses the approved native package for each supported
distribution. A typed two-phase reconcile adds required pools before NGINX
activation and retires obsolete pools only after the new revision is live. See
[Managed PHP runtime](managed-php-runtime.md) and
[ADR 0036](adr/0036-native-account-scoped-php-fpm.md). Browser selection is
the intersection of current host approval and the account's immutable package
snapshot. The tenant status view reports only bounded pool state and aggregate
systemd accounting; see [Account PHP controls and health](account-php-controls.md).

### 5.4 OCI applications

Podman applications run rootless as the hosting-account identity. Quadlets make
their lifecycle visible to systemd. The panel creates private networks and maps
an application only to a loopback/high internal port or Unix socket. Edge NGINX
is the sole public ingress path.

The control plane first persists only the closed, tenant-owned draft described
in [Constrained OCI application foundation](oci-application-foundation.md) and
[ADR 0053](adr/0053-constrained-oci-application-drafts.md). A domain cannot
reference the draft until a later privileged lifecycle has applied and health-
checked the exact current revision.

Before workload execution exists, L-002 establishes a typed host readiness
contract and prepares deterministic per-account subordinate UID/GID mappings,
rootless storage, systemd user runtime, and a root-owned future Quadlet
directory. Podman system and global-user API units remain masked; neither the
control API nor the host agent uses an engine socket. Account host-readiness now
requires an immutable successful runtime marker. See
[Rootless OCI account runtime](rootless-oci-runtime.md) and
[ADR 0054](adr/0054-rootless-podman-account-runtime.md).

L-003 prepares—but does not run—the exact application image. Registry sources
are pulled by SHA-256 digest; Containerfile contexts and syntax are bounded and
built as the account identity without instruction network access. A
size-limited OCI archive is scanned by the checksum-pinned Trivy bundle, and
HIGH/CRITICAL findings fail closed. Only append-only deployed/source digest and
policy evidence can move the draft to `pending`; see
[Bounded OCI image preparation](oci-image-preparation.md) and
[ADR 0055](adr/0055-digest-pinned-bounded-oci-image-preparation.md).

## 6. Resource accounting

### 6.1 Hard limits

- CPU, memory, swap, and process count: cgroup v2/systemd.
- account file bytes/inodes: filesystem quotas.
- block-device read/write BPS and IOPS: cgroup v2 where the actual device and
  storage driver support meaningful enforcement.
- resource counts: transactional checks in the control database.

Account processes run below `stackfort-accounts-<UID>.slice`. The aggregate
`stackfort-accounts.slice` is capped at 80% of online CPU capacity and physical
memory, leaving the remaining capacity outside the customer hierarchy.
Platform services join the sibling `stackfort-core.slice`; see
[ADR 0020](adr/0020-systemd-account-slices-and-host-reserve.md).

### 6.2 Measured limits

- network transfer: edge access-log/metrics pipeline aggregated by domain and
  account, with defined treatment for headers, retries, and cache hits.
- MariaDB storage: measured per schema/table and treated as advisory initially
  because database files and I/O belong to a shared server process.
- backup storage: separately measured and quota-controlled.

The UI labels each value as hard, soft, measured, unavailable, or unsupported.

## 7. Observability

The product exposes bounded, authenticated views of:

- component health and version compatibility;
- request rates, latency, status classes, and transferred bytes;
- cache HIT/MISS/BYPASS and backend health;
- PHP pool activity plus aggregate memory, cumulative CPU time, and process
  count (queue pressure remains future work);
- account CPU, memory, process, and I/O usage as each measured source becomes
  available;
- storage and inode usage;
- backup and restore results;
- WAF event summaries;
- update checks and applied revisions.

High-cardinality labels such as arbitrary paths and raw client identifiers are
not retained in general metrics. Per-domain access/error logs use a separate
seven-day, 8-MiB-active-file policy, data-minimized capture, and a second
redaction pass before account display. Audit retention remains independent.

## 8. Updates

GitHub Releases are the initial distribution channel. Release assets include:

- control API and agent binaries for supported architectures;
- web assets;
- distribution packages when ready;
- installer;
- checksum manifest, SBOM, signatures/attestations, and release metadata.

The updater downloads a selected immutable semantic version, verifies provenance
and hashes, stages it, checks compatibility and free space, snapshots state,
applies migrations, swaps versioned binaries/assets, restarts services in order,
and runs health checks. It rolls back binaries/configuration automatically when
possible and clearly reports when a migration crossed the rollback boundary.

## 9. API approach

The browser API is versioned under `/api/v1`. Mutations use request IDs and
idempotency keys. Validation failures return stable machine-readable codes and
localized text is resolved by the browser.

The administrator console resolves bootstrap and session state before mounting
protected navigation. Its package, hosting-account, operation, and audit list
endpoints are server-authorized, deterministically ordered, and bounded to at
most 200 records. Sensitive mutations retain recent-authentication and CSRF
requirements; list responses omit privileged host identity internals, audit
hashes, and operation payload/result objects. See
[Administrator Phase 1 flows](administrator-phase1-flows.md) and
[ADR 0030](adr/0030-authenticated-bounded-administrator-console.md).

The initial implementation may use REST plus Server-Sent Events for operation
progress. WebSockets are unnecessary unless a later terminal or bidirectional
streaming feature justifies them.

## 10. Performance methodology

The project must publish reproducible results for at least:

- static files through edge NGINX;
- uncached PHP through NGINX/PHP-FPM;
- cacheable PHP through NGINX FastCGI cache as a comparison;
- cacheable PHP through Vinyl;
- WAF off, detection-only, and blocking PL1;
- cold cache, warm cache, small objects, representative pages, and large files;
- latency percentiles, throughput, CPU, memory, error rate, and origin requests.

Benchmarks run on fixed hardware/VM profiles and never replace end-to-end
correctness and isolation tests.
