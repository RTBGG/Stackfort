# Local host-agent protocol

D-001 establishes the narrow transport between the unprivileged Stackfort
control API and the privileged Linux host agent. D-002 adds read-only host
inspection; E-001 adds account-identity mutations; E-002 adds bounded account
filesystem/quota and document-root mutations; E-003 adds account resource-slice
reconciliation; F-001 adds global NGINX-baseline reconciliation.
F-003 adds transactional site activation, and G-001 adds fixed HTTP-01 token
presentation/cleanup. G-002 adds fixed-path certificate staging; J-001 adds
account-scoped PHP-FPM pool reconciliation, J-002 adds its bounded read-only
inspection, J-003 adds tenant-scoped MariaDB provisioning and deletion, and
J-005 adds managed-principal password rotation.
K-001 adds bounded file listing, K-008 adds privacy-minimized domain-log
reading, and K-009 adds closed scheduled-job reconciliation.

## Transport and peer identity

The agent listens only on the filesystem Unix stream socket
`/run/stackfort/agent.sock`. Its real, non-symlink parent directory is mode
`0750`; the socket is mode `0660`. Both receive the configured control API
service group. The agent resolves the `stackfort-api` service identity at
startup and refuses to start if it cannot do so.

Filesystem permissions are necessary but not the authentication decision. For
every accepted connection, the listener reads Linux `SO_PEERCRED` before HTTP
parsing and compares the kernel-reported UID with the configured control API
UID. Credential lookup failure and every other UID—including UID 0 when it is
not the configured identity—cause immediate connection closure and a bounded
security log entry. Credentials are fixed by Linux when the peer connects.

## Versioned typed contract

HTTP/1.1 is only the bounded local framing layer. The RPC endpoint is
`POST /rpc/v1` with `application/json`; it is never bound to TCP. Version 1 has
nineteen allowlisted operations: `protocol.handshake`, the empty-payload
`host.capabilities.inspect`, `hosting.identity.reconcile`,
`hosting.identity.delete`, `hosting.filesystem.reconcile`,
`hosting.files.list`, `hosting.logs.read`, `hosting.resources.reconcile`,
`hosting.document-root.ensure`, the empty-payload global mutation
`web.nginx-baseline.reconcile`, `web.nginx-sites.activate`,
`tls.acme-http01.reconcile`, `tls.certificate.stage`, `php.fpm-pools.inspect`,
`php.fpm-pools.reconcile`, `database.provision`, `database.password.rotate`,
`database.drop`, and `hosting.jobs.reconcile`, represented by a tagged Go
request/response union.
There is no generic method name, command string, argument array, environment
object, or caller-selected filesystem path.

Every request contains:

- wire version `1`;
- a bounded request ID;
- a required bounded idempotency key;
- one allowlisted operation discriminator; and
- exactly the typed payload belonging to that operation.

Every registry entry also declares whether it is protocol, read-only, or a
privileged mutation. Mutations require a separate `correlation` object with the
durable API operation, actor, and account IDs; other operations reject it. The
account ID must equal the identity payload account, and the username/path are
recomputed from it. These canonical UUIDv7 values bind agent execution and safe
logs to the control database's audit chain. See
[Agent audit correlation](agent-audit-correlation.md).

The global NGINX mutation forbids an account ID and accepts no configuration,
path, unit, executable, argument, or proxy input. Its response is bound to the
fixed managed paths and loopback-only trusted hops. See
[Managed NGINX baseline](managed-nginx-baseline.md).

The handshake carries the client's minimum and maximum supported semantic
versions. The agent returns the highest overlapping version, its supported
range and build provenance, and the explicit operation list. A range without
overlap receives `426 incompatible_protocol`.

JSON decoding rejects unknown fields, trailing values, malformed tagged
payloads, unsupported operations, and invalid identifiers. Responses contain
exactly one typed result or a stable error code and repeat both the request ID
and wire version. The client verifies the response media type, protocol header,
request ID, tagged result, advertised operation set, and size before use.

## Bounds and idempotency

| Boundary | Limit |
| --- | --- |
| Request body | 64 KiB |
| Response body | 64 KiB |
| Request and idempotency IDs | 1–128 restricted ASCII characters |
| Header bytes | 64 KiB |
| Agent read/write timeout | 10 seconds |
| Client request timeout | 10 seconds |
| Client connect timeout | 2 seconds |
| Client response-header timeout | 5 seconds |
| Idle connection timeout | 30 seconds |

The protocol layer keeps at most 2,048 successful or deterministic RPC
outcomes for 15 minutes. Replaying a key with the same semantic request returns
the same outcome under the new request ID; using that key with different
input receives `409 idempotency_conflict`. Request IDs and the key itself are
excluded from the semantic SHA-256 digest. Mutation audit correlation remains
inside the digest, so the key cannot be rebound to another operation or actor.

This cache is deliberately bounded and process-local. It prevents immediate
duplicate dispatch but is not the durable side-effect guarantee for privileged
mutations. Mutation handlers must reconcile desired state and use the
persistent operation/fencing context so a restart is safe.

## Verification

Cross-platform unit tests cover strict decoding, tagged-union validation,
version incompatibility, response correlation, idempotent replay/conflict,
timeouts and response bounds. The protocol decoder has a fuzz target seeded
with valid, malformed, and command-shaped input. Linux-only integration tests
exercise the real Unix socket, `SO_PEERCRED`, denied UIDs before handler entry,
an oversized request through the socket, the typed client handshake, and a
capability inspection. Supported-distribution fixtures cover typed capability
classification and parser failures. The repository smoke test performs the same
build-time service-identity wiring used by its disposable process.

E-001 additionally covers account-correlation matching, create/delete response
unions, mutation replay, stable conflict/archive errors, and a typed client.
Real user/group mutation remains an explicit disposable-host gate because it
must never run on a shared CI host.

E-002 adds a complete immutable identity plus project/byte/inode intent for
filesystem reconciliation and an independently revalidated account-relative
document root. Quota failures carry a typed capability status and reason. See
[Hosting filesystem layout and project quota](hosting-filesystem-layout-and-quota.md).

F-001 adds strict global-correlation and request/response-union tests, stable
capability/conflict/validation errors, and a typed client. Real NGINX activation
and rejecting HTTP/TLS defaults are exercised only on disposable hosts.

F-003 adds the account-scoped `web.nginx-sites.activate` union. It carries an
immutable identity, desired-revision ID, domain tagged unions, and closed
renderer options—never configuration text or a path. The correlated operation
UUID selects the fixed revision transaction, so replay remains durable after
the process-local idempotency cache is lost. See
[Transactional NGINX site activation](transactional-nginx-activation.md).

K-010 extends that domain union by one closed WAF enum: off, detection-only, or
blocking PL1. The renderer, not the caller, maps it to one root-owned profile;
module paths, SecLang, rule IDs, thresholds, and exception text never cross the
protocol. Schema-2 desired-state documents decode a missing field as off. See
[WAF foundation](waf-foundation.md).

G-001 adds account-correlated `tls.acme-http01.reconcile`. Its tagged intent
contains only present/cleanup, an RFC 8555-shaped token, and—only for
presentation—the matching key authorization. The fixed root-owned directory is
not caller-selectable and the response never echoes challenge content. See
[ACME account and HTTP-01 routing](acme-account-and-http01.md).

J-001 adds account-correlated `php.fpm-pools.reconcile`. Its typed intent
contains a complete hosting identity, a sorted bounded approved-version set,
bounded child/memory limits, and an explicit additive-or-exact convergence
flag. All runtime paths and process profiles are agent-owned. See
[Managed PHP runtime](managed-php-runtime.md).

J-002 adds read-only `php.fpm-pools.inspect`; correlation is therefore
forbidden. Its request contains a validated account identity and sorted bounded
approved-version set. The agent derives each exact unit and verifies active
placement in the account slice. Its response exposes only version, bounded
state, and optional aggregate memory bytes, cumulative CPU nanoseconds, and
task count—never a unit/socket/configuration/cgroup path, PID, argument, or
environment. See [Account PHP controls and health](account-php-controls.md).

J-003 adds account-correlated `database.provision` and `database.drop`. Both
carry only canonical derived object names, `localhost`, a closed read-only or
read-write grant preset, and the exact account correlation. Provisioning sends
the new password only as an in-memory JSON byte value over the peer-authenticated
Unix socket; it never appears in an argument, SQL text, response, or log. The
agent connects to MariaDB through a verified root Unix socket, records ownership
in a fixed private control schema, and rejects marker collisions. Deletion
requires that marker, revokes every schema-wide grant before dropping a schema,
and rejects users that still have grants. See
[Account database lifecycle](account-database-lifecycle.md).

J-005 adds account-correlated `database.password.rotate`. Its closed payload
contains one account-derived active local principal and a bounded in-memory
candidate password. The agent requires the existing same-account ownership
marker and applies no arbitrary SQL, grant, database name, path, or command.
Repeating the same candidate is safe after agent restart. See
[Managed database password rotation](database-password-rotation.md).

K-001 adds read-only `hosting.files.list` for bounded metadata pages. K-002
does not add file content to that union: `POST /stream/v1/files/download` is a
separate peer-authenticated endpoint with an at-most-8-KiB typed request and a
raw bounded response. Its only selectors are the validated managed identity,
canonical account-relative regular-file path, and optional single byte-range
union. A helper running as that account revalidates the descriptor chain and
emits one bounded metadata frame before the exact content bytes. See
[File-manager foundation](file-manager-foundation.md) and
[ADR 0042](adr/0042-account-credential-file-download-stream.md).

K-003 adds a separate `POST /stream/v1/files/write` endpoint; it does not add
file bytes or generic paths to the JSON RPC. An at-most-8-KiB strict control
prefix selects initiate, status, exact-offset chunk, complete, cancel, or
empty-node creation. Only a chunk carries trailing raw bytes, bounded to 8 MiB
and an exact content length. The account-credential helper stores resumable
parts under a fixed hidden account directory, `fsync`s acknowledged chunks,
verifies the final size and SHA-256, and atomically activates with
`RENAME_NOREPLACE`. All mutations carry a previously persisted audit-event
correlation. See [ADR 0043](adr/0043-account-credential-staged-file-write.md).

K-004 extends that same closed write union with explicit `node.rename`,
`node.move`, `node.copy`, `trash.put`, `trash.list`, `trash.restore`, and
`trash.purge` actions. Rename/move use descriptor-relative
`RENAME_NOREPLACE`. Copy uses a fixed hidden per-operation staging tree and is
bounded by bytes, entries, nesting, duration, concurrency, and the account's
kernel project quota before atomic activation. Trash is a fixed hidden
same-filesystem namespace with opaque IDs, bounded ordered listing, recorded
original paths, no-replace restore, and preflighted permanent purge. See
[ADR 0044](adr/0044-bounded-file-operations-and-recoverable-trash.md).

K-005 adds `archive.create` and `archive.extract` to the closed union. The only
formats are ZIP and tar.gz, and the declared format must match the archive
suffix. Creation accepts one account-owned regular file or directory, writes a
bounded candidate under `.stackfort-operations/<operation-id>/payload`, then
activates it with `RENAME_NOREPLACE`. Extraction first snapshots a regular
archive into that hidden operation tree, validates every member, materializes
only regular files and directories, and atomically activates a new destination
directory. Both directions retain the account credential, filesystem, entry,
depth, byte, duration, concurrency, quota, and audit-correlation boundaries.
See [ADR 0045](adr/0045-bounded-archive-creation-and-hostile-extraction.md).

K-006 adds `backup.create`, `backup.list`, `backup.inspect`, `backup.verify`,
and `backup.restore` to the same closed write transport. The caller selects
only a derived identity, an opaque UUIDv7 backup ID, one of the fixed
`account_files`/`document_root` scopes, and for the latter one canonical source
path. Root selects `/srv/hosting/backups/<account-id>`, authenticates schema-1
manifests with a separate host HMAC key, and verifies the complete payload
before invoking restore. Payload creation and extraction run through the
production helper as the account UID/GID; backup bytes never enter JSON.
Restore carries a unique operation ID and durable authorization correlation.
See [ADR 0046](adr/0046-authenticated-local-file-backup-and-staged-restore.md).

K-007 adds the closed `backup.upload.initiate/status/chunk/complete/cancel` and
`backup.delete` actions. Upload control and at most 8 MiB of raw bytes share the
existing framed write endpoint; exact offsets and root-owned metadata make the
transfer resumable. Every backup response includes privileged measured
repository bytes/counts and the effective package limit. The separate
`POST /stream/v1/backups/download` endpoint accepts an identity, UUIDv7, and an
optional single range. Root authenticates and fully verifies the backup before
returning `application/gzip`. Completion calculates SHA-256, parses the hostile
archive, signs a new local manifest, and publishes atomically. See
[ADR 0047](adr/0047-portable-backup-transfer-retention-and-quota.md).

K-008 adds read-only `hosting.logs.read`. The browser supplies a persisted
domain ID, but the control plane resolves both the normalized domain name and
derived hosting identity from the account before RPC. The closed request then
contains only that identity, domain, `access`/`error`, a maximum-50 limit, and
an opaque inode/offset cursor. Root opens only the SHA-256-derived active log
and delay-compressed `.1` file below the fixed account directory using
`O_NOFOLLOW`; ownership, modes, type, and link count are rechecked. Parsed
records are capped and redacted again before the JSON response. See
[ADR 0048](adr/0048-privacy-minimized-domain-logs.md).

K-009 adds account-correlated `hosting.jobs.reconcile`. The request contains a
complete derived hosting identity, one UUIDv7 job, an account-relative Shell or
PHP script definition, one fixed schedule union, enabled state, and
present/absent intent. It cannot carry a command, executable, environment,
working directory, unit name, or calendar expression. The agent derives the
distribution runtime and systemd service/timer pair, validates the script
descriptor-relatively, and reconciles root-owned units with fixed `systemctl`
profiles. See [Scheduled account jobs](scheduled-jobs.md) and
[ADR 0049](adr/0049-closed-systemd-scheduled-account-jobs.md).

See [Host capability inspection](host-capability-inspection.md) for D-002's
allowlists, result semantics, and probe bounds. See
[Safe external process runner](safe-external-process-runner.md) for the internal
execution profiles and Linux cleanup contract used by those probes. See
[Agent audit correlation](agent-audit-correlation.md) for mutation attribution,
safe agent events, and rejected-peer security events.
See [Hosting-account Unix identity](hosting-account-unix-identity.md) for the
first mutation's conflict, filesystem, and archive/delete invariants.

References:

- [Linux `unix(7)` and `SO_PEERCRED`](https://man7.org/linux/man-pages/man7/unix.7.html)
- [Go `net/http` request bounds and server timeouts](https://pkg.go.dev/net/http)
- [`golang.org/x/sys/unix`](https://pkg.go.dev/golang.org/x/sys/unix)
