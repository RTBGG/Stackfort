# ADR 0043: Account-credential staged file writes

- Status: accepted and implemented by K-003
- Date: 2026-08-29

## Context

Uploading directly to a visible destination can expose truncated content after
a disconnect and makes retries capable of silently replacing an existing file.
Putting file bytes into the existing 64-KiB JSON RPC would also require full
buffering and turn a bounded control channel into a generic privileged write
surface. Mutations need durable attribution before the host changes, while the
kernel must still enforce the managed account's actual filesystem identity.

## Decision

1. Keep the metadata RPC and download stream unchanged. Add a distinct closed
   file-write endpoint on the peer-credential-authenticated Unix socket.
2. Prefix each request with at most 8 KiB of strict typed JSON. Only a chunk
   request may append raw bytes, with an exact declared length of at most
   8 MiB. Unknown fields, trailing bytes, and non-canonical paths are rejected.
3. Authorize `account.files.manage`, confirm host readiness, derive the
   immutable Linux identity, and persist an authorization audit event before
   sending a mutation to the agent. Carry that event ID and actor/session/
   account/request correlation into every agent outcome log.
4. Run the same root-owned agent binary in a hidden helper mode with the
   account UID/GID, no supplementary groups, and a parent-death signal. The
   helper independently verifies its effective identity and descriptor chain.
5. Stage incomplete uploads below the fixed account-owned
   `.stackfort-uploads` directory. Hide that reserved internal directory from
   file listings. Permit at most eight active sessions per account.
6. Treat the locked part-file size as the authoritative resumable offset.
   Accept only sequential exact-offset chunks and `fsync` each chunk before
   acknowledging its new offset.
7. At completion, require the exact expected size, calculate the full SHA-256
   digest, compare an optional expected digest, `fsync` the part file, and use
   `renameat2(RENAME_NOREPLACE)` followed by destination-directory `fsync`.
8. Create empty files descriptor-relatively with `O_EXCL|O_NOFOLLOW` and mode
   `0640`; create directories with `mkdirat` and mode `0750`. Never replace an
   existing namespace entry.
9. Bound the write surface to four concurrent requests, 4 GiB per upload, and
   30 minutes per request. Explicit cancellation removes both staging records.

## Consequences

- A disconnect cannot expose a partial destination; the acknowledged offset is
  sufficient to resume the same session.
- Upload activation and empty-node creation are conflict-safe and do not
  overwrite account data, including under concurrent requests.
- File bytes use constant application memory and never cross a JSON encoding
  boundary.
- The managed account's kernel identity remains authoritative for traversal and
  writes even though the parent agent is privileged.
- Interrupted sessions intentionally remain resumable and consume one of the
  eight account slots until completed or cancelled. Automated expiration and
  quota accounting are follow-up policy work rather than implicit deletion.
- Rename, copy, move, trash, and archive operations remain outside this
  contract and require their own bounded semantics.
