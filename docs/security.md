# Security Model

Status: Draft 0.1

## 1. Assets

The system protects:

- platform administrator and hosting-user identities;
- hosted files, databases, backups, and application secrets;
- TLS and backup private keys;
- host integrity and service availability;
- account resource allocations;
- configuration history and audit evidence;
- software update integrity.

## 2. Trust boundaries

1. Internet to edge NGINX.
2. Browser to control API.
3. Control API to privileged host agent.
4. Edge/cache/origin to account PHP or OCI workload.
5. Account workload to shared MariaDB.
6. Host to GitHub release/update infrastructure.
7. Account user to file/archive/backup content.

Every boundary assumes the less-trusted side may be hostile.

## 3. Principal threats

- authentication bypass, session theft, CSRF, or token leakage;
- horizontal access from one hosting account to another;
- vertical escalation from account user to administrator or root;
- command, path, configuration, SQL, header, or template injection;
- symlink races and traversal through file, archive, or restore operations;
- malicious PHP or container workloads exhausting or escaping isolation;
- unsafe proxy/cache behavior exposing personalized data;
- WAF bypass or overly broad WAF exclusions;
- malicious or compromised updates;
- database credentials leaking through phpMyAdmin signon;
- denial of service through uploads, decompression, logs, backups, jobs, or API
  fan-out;
- audit-log deletion or misleading partial operations.

## 4. Mandatory controls

### 4.1 Authentication

- Argon2id password hashing with parameters stored per credential.
- Minimum password length and breached-password rejection when a privacy-safe
  check is available; no arbitrary composition rules.
- TOTP and single-use recovery codes.
- Generic login/recovery responses that avoid identity enumeration.
- Rate limits per source, identity, and global pressure state.
- Short-lived bootstrap and recovery tokens stored only as hashes.
- Session rotation on login and privilege changes.
- Secure, HTTP-only, SameSite cookies; no session tokens in local storage.
- Recent-authentication checks for passwords, email, factors, tokens, restore,
  update, and destructive administration.

C-001 implements the first-administrator subset with a locally generated
256-bit, short-lived, single-use capability stored only as a SHA-256 digest.
Persistent direct-source and global limits run before Argon2id. The identity,
credential, role, capability consumption, and audit event commit atomically;
see [Secure administrator bootstrap](administrator-bootstrap.md). C-002
implements normal login, sessions, and CSRF.

C-002 now implements password login with a dummy Argon2id path and generic
failure, persistent global/direct-source/hashed-identity limits, two concurrent
derivation slots, digest-only 256-bit session/CSRF values, strict host-only
cookies, a session-bound synchronizer header, Fetch Metadata defense, login
rotation, logout, and server-side 30-minute idle/12-hour absolute expiry. See
[Password authentication and browser sessions](password-authentication-and-sessions.md).

C-004 implements verified TOTP setup, replay-counter enforcement, hash-only
single-use recovery codes, persistent MFA limits, and a digest-only two-phase
login challenge that creates no session before factor verification. TOTP
secrets use per-record AES-256-GCM envelope encryption under an external private
host key. Factor changes require recent authentication and current-factor proof
where applicable, then revoke every identity session. Self-service session
lookup and revocation are identity-scoped. See
[TOTP, recovery codes, and session management](totp-recovery-and-session-management.md).

### 4.2 Authorization

- Deny by default.
- Every object lookup is scoped by identity membership and account ID.
- Role checks occur in the domain/service layer, not only in HTTP middleware.
- API tokens use explicit scopes, expiry, last-used information, and revocation.
- Agent requests contain the already authorized actor and account context, but
  the agent independently validates system ownership and resource identity.

C-003 implements the first application-layer authorization boundary. Opaque
subjects can be created only from authenticated sessions, permission state is
read on every protected request, unknown actions are denied, and sensitive
actions require authentication within five minutes. The administrator/owner/
member/auditor matrix, account lifecycle behavior, role revocation, and
cross-tenant identifier substitution have automated negative coverage. See
[Authorization policy](authorization-policy.md).

### 4.3 Privilege separation

- The control API is unprivileged and not a member of container-engine or
  unrestricted administration groups.
- The host agent listens only on a protected Unix socket.
- Agent RPC is typed and allowlisted; arbitrary shell text is impossible.
- When external tools are unavoidable, arguments are passed directly without a
  shell and are built from canonical server-derived values.
- The agent uses restrictive systemd sandboxing compatible with its explicit
  duties and delegates narrow child operations when possible.

D-001 implements the first local privilege boundary: the filesystem Unix socket
is owned for the control API service group, while Linux `SO_PEERCRED` requires
the exact configured API UID before any HTTP is parsed. RPC JSON is limited to
64 KiB, rejects unknown/trailing fields, negotiates protocol compatibility, and
dispatches only typed allowlisted operation unions. Version 1 contains a
handshake, an empty-payload read-only capability inspection, two E-001
account-identity mutations, and E-002's typed filesystem/quota and canonical
account-relative document-root mutations. E-003 adds typed account-resource
reconciliation and F-001 adds an empty-payload global NGINX-baseline mutation.
F-003 adds typed site activation and G-001 adds strict fixed-root HTTP-01
present/cleanup; none exposes a caller-selected command, argument, environment,
package, unit, port, or arbitrary host path.
Capability source files, subprocess time/output, and the aggregate probe are
bounded, and raw command output never crosses the RPC boundary. See
[Local host-agent protocol](local-agent-protocol.md) and
[Host capability inspection](host-capability-inspection.md).

D-003 centralizes unavoidable native execution behind compiled-in profiles.
Each profile fixes an absolute executable, direct argument template, exact
semantic allowlist, minimal environment, deadline, and independent stream
limits. Linux process groups are killed and reaped on cancellation, timeout, or
output exhaustion; stable failures and profile-directed redaction keep raw
arguments, output, and operating-system diagnostics out of error text. See
[Safe external process runner](safe-external-process-runner.md).

D-004 requires privileged mutations to carry the durable API operation,
identity or system actor, and optional account as canonical UUIDv7 correlation.
That context is bound into idempotency and emitted only through fixed structured
event fields. Payloads, idempotency keys, raw errors, and HTTP server diagnostics
are excluded. A failed peer-credential lookup or unexpected kernel UID emits a
stable `agent.peer.rejected` security event before HTTP parsing. See
[Agent audit correlation](agent-audit-correlation.md).

### 4.4 Filesystem safety

- Account identifiers are opaque canonical IDs, not usernames inserted into
  paths.
- Server code opens paths relative to account root and resists symlink races
  using file-descriptor-relative operations where available.
- Ownership and device/filesystem identity are rechecked at sensitive steps.
- Upload and extraction occur in account-owned staging with byte, file-count,
  nesting, time, and quota limits.
- Archives cannot create absolute paths, parent traversal, devices, FIFOs,
  sockets, unsafe links, setuid bits, or unexpected ownership.
- Deletes use a bounded account trash/recovery flow before permanent removal.
- Local file backups live in a separate root-only repository. A versioned
  HMAC-authenticated manifest binds their account/scope and complete payload
  digest; restore verifies and parses the entire payload into account-owned
  staging before replacing visible content.

E-001 now derives the Unix name and account root exclusively from the canonical
account UUID, reserves equal UID/GID values, rejects name or numeric collisions,
and walks the account root with no-symlink directory descriptors. Identity
deletion requires archive evidence plus an absent live root and uses neither
recursive nor forced `userdel`; see
[Hosting-account Unix identity](hosting-account-unix-identity.md).

### 4.5 Web and proxy safety

- Strict host allowlists and a rejecting default virtual host.
- Domain ownership is keyed by normalized lowercase IDNA ASCII names. A base
  domain and its `www` alias are reserved together and globally exclusive while
  live; Unicode and ASCII forms are both shown where homographs may matter.
- Redirect-only domains accept absolute HTTPS targets without credentials or
  fragments. Obvious self-loops, control characters, and exact/wildcard route
  overlaps are rejected before persistence.
- Consistent request parsing across edge, Vinyl, and origin to reduce request
  smuggling/desynchronization risk.
- Bounded headers, request bodies, timeouts, upstream responses, and buffers.
- Trusted proxy information accepted only over the private local hop.
- Cache keys include the canonical host and required variants.
- Authenticated/session-bearing traffic bypasses shared caching unless a vetted
  profile proves it safe.
- Cache and WAF behavior receive integration tests for every preset.

F-001 makes the default-host and proxy rules concrete. The active service reads
only Stackfort's marker-owned main configuration; panel and site include trees
are separate root-owned mode-`0750` directories, so account users cannot inject
directives into either. Unknown HTTP hosts are closed with status 444 and
unknown HTTPS SNI is rejected during the handshake. `set_real_ip_from` contains
only IPv4 and IPv6 loopback. An active unmanaged NGINX service, foreign systemd
drop-in, unsafe ownership/mode, symlink, or hard-linked managed file fails as a
conflict before adoption. See [Managed NGINX baseline](managed-nginx-baseline.md).

ADR 0035 adds an explicit, separate TLS management listener on port `8443`.
Its root-owned configuration serves only immutable panel assets and proxies the
fixed `/api/` prefix to loopback. The initial local certificate/key is one
atomic root-only file and is never used by the rejecting customer-port default.
This enables `Secure` browser cookies on a clean host while keeping public API
binding, arbitrary proxying, and account-writable configuration impossible.
Rocky retains enforcing SELinux and grants `httpd_t` loopback-proxy capability
only through the Stackfort-specific API port type assigned to TCP `8080`; the
broad HTTPD network-connect and relay booleans stay disabled.

F-003 accepts only typed domain intent and binds one durable operation UUID to
one complete root-owned site revision. A strict synced journal and atomic
relative-symlink switch bracket syntax validation, graceful reload, and a local
Host-routed probe. Cleanup never recursively follows discovered paths. A crash
or post-promotion failure restores, validates, and reloads the known previous
revision before removing the failed candidate; exact API replay cannot create a
second applied-history row. See
[Transactional NGINX site activation](transactional-nginx-activation.md).

F-004 gives the NGINX distribution worker only traverse access to an account
root and validated intermediate directories, and read/traverse access to an
enabled document root, through exact POSIX ACLs; a default ACL covers future
account-owned files without overriding their effective mode mask. Rocky keeps
SELinux enforcing and labels only the exact account root, intermediate
directories, and selected content subtree. Static locations reject symlink
components below the document root. The operation-linked desired snapshot and
target UUID confirmation prevent stale replay from marking a later edit active,
and no domain lifecycle path exposes a filesystem delete primitive. See
[Static-domain lifecycle](static-domain-lifecycle.md).

G-001 keeps ACME account private keys inside the per-record AES-256-GCM envelope
boundary and exposes registration/metadata only to recently authenticated
platform administrators. Directory URLs are selected from a compiled-in enum.
HTTP-01 agent input is a closed present/cleanup union with a strict token and
matching key authorization, not a path. Responses live in one root-owned fixed
directory, bypass customer redirects/cache, reject symlinks, and receive a
narrow persistent SELinux context on Rocky. See
[ACME account and HTTP-01 routing](acme-account-and-http01.md).

### 4.6 PHP and account processes

- Unique UID/GID and PHP-FPM pool per account.
- No cross-account writable directories.
- `clear_env`, bounded execution/input/upload values, disabled dangerous
  features where compatible, and a controlled extension set.
- cgroup limits, process limits, and filesystem quotas are mandatory before an
  account becomes active.
- Scheduled jobs run as the account identity with time and resource bounds.

K-009 accepts only an account-relative `.sh` or package-approved `.php` script
plus one fixed UTC schedule. Executables, commands, environment variables,
unit names, and raw systemd calendars are derived or rejected. Script traversal
uses directory descriptors with `O_NOFOLLOW`, exact UID/GID and same-device
checks, and single-link regular-file enforcement. Root-owned unit promotion is
transactional; the service runs below the account slice with a five-minute
ceiling, no capabilities/privilege gain, private devices/tmp, strict system
protection, socket-bind denial, and only the account home writable. Standard
input/output/error are null to avoid turning unattended output into a secret or
disk-exhaustion channel. See [Scheduled account jobs](scheduled-jobs.md).

E-003 places every account workload below its immutable UID-derived
`stackfort-accounts-<UID>.slice`. The aggregate accounts slice is capped at 80%
of online CPU capacity and 80% of physical memory; the remaining capacity stays
outside the customer hierarchy for the host and platform. Package limits form
an additional child boundary. An explicit zero swap limit is retained and
verified as `memory.swap.max=0`; absent limits remain unlimited. Stackfort
refuses symlinked, writable, oversized, or non-marker-owned unit files rather
than adopting them. See [Account systemd slices and cgroup-v2 limits](account-resource-control.md).

### 4.7 OCI workloads

- Rootless Podman only for customer workloads.
- No privileged mode, engine socket, host namespaces, arbitrary devices, or
  arbitrary host paths.
- Drop all capabilities and add only a reviewed minimum.
- `no-new-privileges`, read-only image filesystem where compatible, bounded
  writable volumes, seccomp, and SELinux/AppArmor confinement.
- Application intent begins as the closed draft schema in
  [Constrained OCI application foundation](oci-application-foundation.md):
  digest or normalized Containerfile source, one internal port, and one bounded
  health check. Dangerous host/runtime features are absent from the type and
  database schema rather than represented as caller-controlled switches.
- Registry references resolve to recorded immutable digests for deployments.
- Builds have CPU, memory, storage, duration, and log-output limits.

### 4.8 Databases and phpMyAdmin

- Administrative MariaDB credentials are local to the agent and excluded from
  the web API process.
- Each database principal is scoped to account-owned schemas and least
  privilege.
- Physical schema and principal names are derived from the complete account
  UUID plus a restricted alias; browsers cannot submit a physical name or host.
- Only `localhost` principals and fixed read-only/read-write privilege sets are
  accepted. The agent records an ownership marker and refuses unmarked or
  differently owned objects.
- Creation and deletion are durable, replay-safe operations. Schema removal
  revokes all schema-wide privileges first, and principal removal is rejected
  while any managed grant exists.
- Generated passwords are envelope-encrypted at rest, revealed once after
  recent authentication, omitted from list/read models, and erased when the
  principal is retired.
- phpMyAdmin signon handoffs are short-lived, single-use, audience-bound, and
  protected against cross-account substitution.
- Database passwords and signon material never enter URLs, general logs, audit
  details, or analytics.

### 4.9 Domain log privacy

- Managed NGINX access logs omit query strings, complete request lines,
  authorization/cookie headers, referrers, and user agents at capture time.
- Per-domain files and their account directories are root-owned, path-derived,
  non-symlink regular files; the privileged reader rechecks every boundary.
- Account-facing access paths and native NGINX error messages receive a second
  bounded redaction pass for query tails, credential-like fields, sensitive
  path segments, invalid text, and control characters.
- The account API returns at most 50 newest-first entries and rejects stale
  inode/offset cursors rather than silently crossing a rotation boundary.
- Seven-day/rotation retention and an 8 MiB active-file ceiling use rename plus
  NGINX reopen, never `copytruncate`.

### 4.10 Backups and restores

- Versioned manifest and authenticated checksums.
- Consistent database dump state.
- Optional encryption before data leaves the node; remote credentials are
  separately encrypted.
- Restore is treated as hostile input, even for locally created archives.
- Restores cannot change account ownership, escape paths, install privileged
  units, or silently overwrite unrelated resources.
- Restore tests are part of release qualification.

### 4.11 Updates and dependencies

- Immutable releases, checksums, provenance attestations, and SBOMs.
- Update artifacts are verified before any privileged process handles content.
- Installer and updater pin a version; mutable branch content is never executed
  as an update payload.
- Dependencies are locked and continuously scanned.
- Security updates can be prioritized, but emergency behavior must still retain
  verification and rollback safeguards.

### 4.12 Durable operations

- Every worker claim has a unique attempt ID and worker ID. Heartbeats,
  checkpoints, completion, failure, and cancellation acknowledgement must match
  both, and an expired lease is permanently fenced from further writes.
- Automatic replay is allowed only for operations classified `safe` and within
  their attempt bound. `manual` requires an audited user action; `none` cannot
  be replayed.
- Cancellation is cooperative at declared safe boundaries. A running handler
  must stop privileged work and acknowledge cleanup before the operation becomes
  cancelled.
- Payloads, results, progress details, and audit summaries are size-bounded and
  reject secret-bearing field names. Persisted failures use stable codes rather
  than raw command, panic, or exception text.
- Operation events are append-only and API reads are account-scoped and
  paginated. Heartbeats are not copied into the global audit chain, while
  creation, manual retry, cancellation, and terminal outcomes are audited.

## 5. Audit log

Audit events include UTC time, event ID, actor, authentication/session context,
source information, action, target, account, request/operation ID, result, and a
sanitized change summary.

Events never include passwords, tokens, secret values, complete request bodies,
or database contents. Entries form an append-only hash chain and are exported or
checkpointed so a privileged local attacker cannot silently rewrite history
without detection. Audit retention is distinct from application log retention.

## 6. Security release gates

Before public beta:

- automated authorization matrix tests;
- cross-account filesystem, PHP, backup, database, and OCI tests;
- fuzz/property testing for identifiers, paths, redirects, domain names, archive
  entries, agent messages, and configuration rendering;
- dependency and container-image scanning;
- secret scanning and generated SBOM;
- installer/update provenance verification tests;
- WAF/cache bypass and false-positive test corpus;
- documented vulnerability reporting and supported-version policy;
- independent review of authentication, agent RPC, file/archive operations,
  phpMyAdmin handoff, and updater.

## 7. Known residual risks

- Containers and Unix accounts share one kernel; this is not virtual-machine
  isolation.
- Shared MariaDB makes exact per-account I/O and memory enforcement impractical.
- A privileged host compromise can defeat local secrets and audit guarantees.
- WAF rules reduce common application attacks but cannot make vulnerable hosted
  code safe.
- Full-page caching remains application-sensitive and can leak data if unsafe
  policies are introduced.

These limitations must be documented rather than obscured by the interface.
