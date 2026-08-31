# Core records

Migrations `002_core_records.sql`, `003_domain_records.sql`,
`008_hosting_account_unix_identities.sql`, and
`009_hosting_account_filesystems.sql`, and
`010_hosting_account_resources.sql`, and `016_managed_databases.sql`, together with the `internal/core`
repository, implement Stackfort's initial control-plane domain model.

## Record groups

| Group | Records and invariants |
| --- | --- |
| Identity | Identity, Argon2id credential parameters and hash, sessions, and platform-role assignments. Email uniqueness uses a separately normalized value. Raw passwords and session tokens have no storage column. |
| Account | Hosting account, immutable local Unix/project identity, revisioned filesystem quota and cgroup resource intent/application state, staged removal state, and account memberships. Human identity and hosting account remain separate so one identity can own multiple accounts. |
| Package | Mutable package identity, immutable numbered revisions, and immutable account assignment snapshots. Every account references exactly one current assignment. |
| Desired state | Immutable JSON object revisions with a transactionally allocated sequence per account. |
| Operation | Durable logical job with account/actor context, scoped idempotency, retry policy, current state, bounded payload/result, immutable attempts, fenced leases, and append-only progress events. |
| Audit | Append-only events containing actor/session/source, action, target, account, request/operation context, result, sanitized details, and a SHA-256 predecessor hash. |
| Domain | Stable Unicode/ASCII identity, canonical-host policy, immutable target/redirect revisions, retained relative document roots, and TLS intent/state. |
| Managed database | Tenant-owned databases, localhost principals, fixed grant presets, envelope-encrypted one-time credentials, and retained durable provisioning/deletion mutations. |
| Applied state | Immutable links from a desired-state revision and optional operation to the SHA-256 digest of the configuration actually activated for an account. |

All account-owned relationships use SQLite foreign keys with restrictive delete
behavior. Accounts, identities, sessions, revisions, and assignments are
retained or explicitly archived/revoked rather than cascaded away.

## Identifiers

`core.NewID` generates canonical lowercase UUIDv7 identifiers with the
cryptographically secure random source used by `github.com/google/uuid`.
`core.ParseID` rejects alternate encodings and UUID versions. The schema repeats
the UUIDv7 shape check so malformed IDs cannot be introduced through direct SQL.

UUIDs identify records; they grant no authority and must never be treated as
unguessable capabilities.

References:

- [RFC 9562 UUID version 7](https://www.rfc-editor.org/rfc/rfc9562.html#name-uuid-version-7)
- [Go UUID package](https://pkg.go.dev/github.com/google/uuid)

## Package revisions and assignments

Package limits cover resource counts, CPU, memory, swap, processes, storage,
inodes, block I/O, monthly transfer, PHP versions, and feature permissions.
Optional numeric fields distinguish an unavailable/unlimited limit from an
enforced value. PHP versions are sorted and deduplicated before serialization.

Updating a package creates a new immutable revision under optimistic revision
checking. Existing accounts do not change automatically. Assigning the current
package revision creates a new account assignment whose `effective_limits_json`
is a complete copy of the resolved limits. Later package edits cannot rewrite
that snapshot.

Account creation is one transaction containing:

1. the account;
2. its stable Unix username, UID/GID, and canonical root allocation;
3. its owner membership;
4. its pending project-quota intent copied from that package;
5. its initial package snapshot; and
6. its audit event.

Any missing identity/package relation or audit failure rolls back the complete
creation.

Unix identity allocation, host reconciliation, and forward-only archive/delete
stages are documented in
[Hosting-account Unix identity](hosting-account-unix-identity.md) and
[ADR 0018](adr/0018-stable-hosting-account-unix-identities.md).
Filesystem layout, desired/applied quota truth, and document-root resolution are
documented in [Hosting filesystem layout and project quota](hosting-filesystem-layout-and-quota.md)
and [ADR 0019](adr/0019-account-project-quota-and-descriptor-layout.md).

## Authentication boundary

B-002 stores only already-derived Argon2id hashes, salts, and explicit
parameters, plus SHA-256-sized session and CSRF hashes. C-001 accepts one
password during [secure administrator bootstrap](administrator-bootstrap.md).
C-002 now compares credentials, creates and rotates strict browser cookies,
binds CSRF proof to a session, and enforces logout/expiry as described in
[Password authentication and browser sessions](password-authentication-and-sessions.md).
C-003 now authorizes current session subjects against platform roles, active
memberships, account state, and authentication age as described in
[Authorization policy](authorization-policy.md).
C-004 adds encrypted TOTP factors/setup challenges, digest-only recovery/login
challenges, MFA replay/rate state, session authentication levels, and
identity-scoped session control as described in
[TOTP, recovery codes, and session management](totp-recovery-and-session-management.md).

## Audit boundary

Every repository mutation appends its event in the same immediate transaction.
Audit details are bounded to 16 KiB and reject nested key names associated with
passwords, credentials, cookies, authorization data, CSRF values, private keys,
secrets, or tokens. Database triggers reject update and deletion.

`Repository.VerifyAuditChain` recomputes the complete visible chain using
constant-time hash comparison. A later exporter must checkpoint the latest hash
outside the database to detect privileged removal of the newest events.

Domain-specific normalization, lifecycle, history, and current activation
boundaries are documented in [Domain records](domain-records.md) and
[ADR 0008](adr/0008-domain-identity-and-history.md).

Operation claiming, retries, cancellation, recovery, and handler boundaries are
documented in [Persisted operations](persisted-operations.md) and
[ADR 0009](adr/0009-operation-leases-and-retries.md).
