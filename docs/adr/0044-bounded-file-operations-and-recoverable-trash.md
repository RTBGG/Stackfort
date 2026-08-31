# ADR 0044: Bounded file operations and recoverable trash

- Status: accepted and implemented by K-004
- Date: 2026-08-29

## Context

Rename and move can be atomic on one filesystem, but ordinary recursive copy
can expose a partial tree and unbounded traversal can monopolize the host
agent. Direct deletion is incompatible with deterministic recovery. All four
operations must retain the account's real Unix permissions, project quota, and
descriptor-relative no-symlink boundary while remaining durably attributable.

## Decision

1. Extend the closed file-write action union with explicit rename, move, copy,
   trash, trash-list, restore, and purge variants. Never accept a generic
   command, absolute path, or caller-selected host identity.
2. Require `account.files.manage`, host readiness, immutable identity
   derivation, CSRF, and a durable authorization audit event before every
   namespace mutation. Trash listing remains read-only but requires the same
   management permission.
3. Execute through the existing hidden account-credential helper. Open source
   and destination parents descriptor-relatively, enforce account UID/GID and
   one filesystem device, and support only regular files and directories.
4. Implement rename and move with `renameat2(RENAME_NOREPLACE)` and parent
   directory `fsync`. Rename remains in one directory; move crosses account
   directories but never filesystems.
5. Recursively copy into `.stackfort-operations/<operation-id>/payload` with
   no-follow opens, bounded-memory file transfer, safe permission preservation,
   and file/directory `fsync`. Activate only the complete candidate using
   `RENAME_NOREPLACE`.
6. Limit copy and permanent purge to 10,000 entries, 64 directory levels,
   4 GiB, and 30 minutes. Permit four concurrent write requests and eight
   incomplete internal copy trees. Map kernel `EDQUOT`/`ENOSPC` to a stable
   quota-exceeded response and clean failed copy staging.
7. Implement reversible delete as an atomic rename to
   `.stackfort-trash/<trash-id>/payload` with a strict, `fsync`ed metadata file.
   Hide both internal roots from normal listing.
8. Return trash in ordered pages of at most ten opaque UUIDv7 items and cap the
   account trash at 256 items. Restore only to the recorded original path with
   no-replace semantics.
9. Preflight permanent purge with the same traversal limits, then delete only
   verified regular files/directories descriptor-relatively. Reject links and
   special files even if an account process tampered with internal state.

## Consequences

- Visible destinations are either absent or complete; retries never overwrite
  existing account data.
- Rename, move, trash, and restore are constant-time namespace changes on the
  managed filesystem. Recursive copy and purge remain explicitly bounded.
- Trash consumes the same byte/inode project quota as live content. Users may
  need to purge trash before a copy or upload can succeed.
- A process kill can leave a hidden copy candidate. The eight-item ceiling
  bounds this state; automatic age-based garbage collection remains later
  maintenance policy.
- Copy is a bounded point-in-time best effort rather than an application-level
  snapshot; concurrently modified source content is not promised to be
  transactionally consistent.
- Archive operations are outside this decision. K-005 adds them under the
  separate hostile-input contract in ADR 0045.
