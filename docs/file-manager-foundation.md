# File-manager foundation

K-001 introduces bounded read-only navigation. K-002 adds authenticated,
descriptor-bound file download without turning the privileged agent's 64-KiB
JSON RPC into a generic content or path channel. K-003 adds resumable staged
upload, atomic no-replace activation, and safe empty-file/directory creation.
K-004 completes ordinary namespace management with atomic rename/move,
staged bounded copy, and recoverable trash. K-005 adds bounded ZIP and tar.gz
creation plus hostile-input extraction with atomic destination activation.

## Trust boundary

- The browser sends only an account ID, a canonical account-relative path, and
  an opaque continuation cursor.
- The application authorizes `account.files.view`, confirms host readiness,
  and derives the immutable Linux identity from control-plane state.
- The privileged agent independently validates that identity and opens
  `/srv/hosting/accounts/<account-id>` one component at a time.
- Every component uses directory descriptors with `O_NOFOLLOW`; parent
  ownership, account ownership, and filesystem device boundaries are checked.
- Entry metadata uses `fstatat(..., AT_SYMLINK_NOFOLLOW)`. Symlinks can be
  displayed but are never traversed.
- A download is opened by a dedicated helper process running with the managed
  account UID/GID and no supplementary groups. The helper independently
  revalidates the identity, descriptor chain, ownership, device, final regular
  file type, and the account's real read permission.
- File writes use another hidden helper mode under the same account UID/GID.
  The privileged parent never accepts an absolute destination, arbitrary
  command, configuration fragment, or caller-selected host identity.
- K-004 and K-005 accept only closed source/destination, archive-format, or
  opaque trash-ID unions.
  Regular files and directories are supported; symbolic links, devices,
  sockets, FIFOs, ownership mismatches, and cross-device targets are rejected.

Absolute paths, dot segments, alternate separators, overlong components,
leading/trailing whitespace, control characters, and invalid UTF-8 are rejected
before host access. Existing entries whose names cannot be safely round-tripped
through JSON are omitted and counted instead of being converted into a
different actionable name.

## Bounded listing

One response contains at most 100 entries and stays within the agent's 64-KiB
JSON limit. Linux directory cookies provide opaque pagination without sorting
or rescanning the whole directory. A request inspects at most 4,096 raw entries,
so a directory containing many non-representable names cannot monopolize the
agent; the returned cursor resumes after the inspected region.

The response exposes only name, kind, byte size, permission mode, modification
time, hidden status, and the next cursor. It does not expose host absolute
paths, UID/GID values, file content, command arguments, or agent internals.

## Streaming download

`GET /api/v1/accounts/{accountID}/files/download?path=...` requires an
authenticated owner/member session and `account.files.view`. The application
derives the immutable host identity and sends an at-most-8-KiB typed control
request over the peer-credential-authenticated Unix socket. The separate
`/stream/v1/files/download` response carries raw bytes; content is never placed
inside JSON or buffered in application memory.

The agent allows four concurrent downloads. One response transfers at most
4 GiB; a larger regular file remains accessible through valid single byte
ranges of at most 4 GiB. Multi-range requests are rejected. Valid ranges return
`206`, `Content-Range`, `Accept-Ranges: bytes`, the exact `Content-Length`, and
`Last-Modified`; an unsatisfiable range returns `416` and `bytes */<size>`.
The public response uses `application/octet-stream`, `nosniff`, `no-store`, and
a disposition filename produced only from the validated final component.

Each stream also has a 30-minute lifetime ceiling at both application and
agent boundaries, preventing an authenticated slow reader from retaining a
download slot indefinitely.

The browser request context owns both HTTP hops and the helper process.
Disconnecting or cancelling closes the Unix response and kills an unfinished
helper. Both server write deadlines are disabled only for the active streaming
responses; all metadata RPCs retain their short global deadlines.

## Current UI

The English and German account navigation now contains **Files/Dateien**. The
view supports root and child-directory navigation, parent navigation, bounded
pagination, file-type metadata, and direct same-origin download links for
regular files. Symlinks and other special entries never become download links.
Account auditors do not receive source-file access. Owners and members can also
upload a file in resumable 8-MiB chunks, cancel an upload, and create an empty
file or directory. The current browser session retains the upload ID and exact
acknowledged offset so selecting the same file after an interrupted request
continues instead of restarting. Every regular file and directory row now also
offers rename, copy, move, and recoverable trash actions. The paginated trash
view supports conflict-safe restore and an explicit permanent purge.
Regular files and directories can be packed as ZIP or tar.gz archives. ZIP and
tar.gz rows can be extracted into a caller-named, previously absent directory.

## Staged resumable upload

The public API and agent use a separate write-stream endpoint. An at-most-8-KiB
strict JSON control prefix is followed by raw bytes only for a chunk request;
file content is never represented as JSON or buffered in full. The application
requires `account.files.manage`, derives the immutable hosting identity, and
creates a durable authorization audit event before contacting the agent.

The account-credential helper stores incomplete sessions below the fixed hidden
`.stackfort-uploads` directory. Each upload has an opaque server-generated ID,
an expected final size, optional expected SHA-256, and an account-relative
destination. Chunks must begin at the exact authoritative part-file offset,
are locked against concurrent writers, are limited to 8 MiB, and are `fsync`ed
before the new offset is acknowledged. Status requests expose only the upload
ID, destination, size, current offset, and creation time. Explicit cancellation
removes both staging records. The internal staging directory is omitted from
normal directory listings.

One account can retain at most eight active sessions. The agent permits four
concurrent write streams, at most 4 GiB per upload, and at most 30 minutes per
request. A lost connection leaves a resumable staging session rather than a
partially visible destination.

## Atomic activation and creation

Completion locks the session, verifies the exact expected size, calculates the
full SHA-256 digest, and compares it when the caller supplied one. It then
`fsync`s the staged file, activates it with Linux `renameat2(RENAME_NOREPLACE)`,
and `fsync`s the destination directory. Existing entries are never replaced.
The resulting uploaded file is mode `0640`.

Empty-file creation uses descriptor-relative `O_EXCL|O_NOFOLLOW` with mode
`0640`; directory creation uses `mkdirat` with mode `0750`. Every ancestor and
target directory is opened without following symlinks and is revalidated for
the managed account owner, group, and filesystem device. All successful and
failed agent outcomes include the durable control-plane audit-event ID plus
actor, session, account, and request correlation in structured logs.

## Atomic relocation and staged copy

Rename is limited to one directory and move requires a different destination
directory. Both open the source and destination parents descriptor-relatively,
revalidate account ownership and the filesystem device, reject symlinks and
special files, then use `renameat2(RENAME_NOREPLACE)`. Existing destinations
survive unchanged. The affected parent directories are `fsync`ed after success.

Copy accepts a regular file or directory and recursively builds a complete
candidate below the fixed hidden `.stackfort-operations/<operation-id>` tree.
Each discovered entry is revalidated without following symlinks. The operation
is bounded to 10,000 entries, 64 levels, 4 GiB, four concurrent agent requests,
eight staging operations, and 30 minutes. Files are copied with bounded memory,
special permission bits are stripped, and every completed file/directory is
`fsync`ed. Only then is the candidate atomically renamed to the requested
destination with no-replace semantics. Kernel project quotas remain
authoritative; `EDQUOT`/`ENOSPC` becomes the typed `file_quota_exceeded` result
and no partial destination becomes visible.

## Recoverable trash

Delete in the normal file manager is an atomic same-filesystem rename into
`.stackfort-trash/<trash-id>/payload`. A strict account-owned metadata record
stores only the original canonical path, entry type, regular-file size, and
UTC deletion time. The internal directory is absent from ordinary listings;
the separate management-only trash API returns at most ten ordered entries per
page and retains at most 256 items.

Restore resolves the metadata inside the helper and uses
`RENAME_NOREPLACE` back to the original parent. A recreated original path
therefore causes a conflict and both versions remain intact. Permanent purge
first performs a bounded descriptor-relative preflight over at most 10,000
entries, 64 levels, and 4 GiB, then removes only regular files/directories
inside that trash item. Trash content deliberately continues counting toward
the account's byte and inode quota until permanently purged.

## Bounded archive creation

Archive creation supports exactly ZIP and tar.gz. The declared format must
match the lowercase destination suffix. One account-owned regular file or
directory is traversed without following links and written through the account
credential into `.stackfort-operations/<operation-id>/payload`. At most 10,000
entries, 64 levels, and 4 GiB of input and output are accepted. Ownership,
device, context, duration, concurrency, project quota, and the eight-operation
staging ceiling are rechecked through the same file-operation boundary.

The completed archive and its parent directories are `fsync`ed before
`RENAME_NOREPLACE` activates it. A conflict preserves the existing destination;
an error or cancellation removes hidden staging and never exposes a partial
archive.

## Hostile-input extraction

Extraction accepts only a regular ZIP or tar.gz file and always creates a new
destination directory. The helper snapshots the source archive into the hidden
operation tree before parsing, so concurrent account writes cannot change the
validated input. Every member name passes the canonical account-relative path
grammar. Absolute paths, dot segments, alternate separators, duplicates,
file/directory collisions, links, devices, FIFOs, sockets, encrypted ZIP
members, unsupported compression, unsafe permission bits, and ownership data
are rejected.

ZIP central-directory metadata is preflighted before the standard decoder.
tar.gz extraction accepts only regular files and directories. Both formats are
bounded to 10,000 entries and 64 levels; extracted data and the fully
decompressed gzip stream are capped by the smaller of 4 GiB or 64 MiB plus 200
times the compressed input size. Extracted directories use mode `0750` and
files use `0640`. Only a completely materialized and `fsync`ed staging tree is
atomically activated with no-replace semantics.

K-007 subsequently completed backup download/upload, manual retention, and
repository quota behavior; see the
[backup foundation](local-file-backup-foundation.md).

## Qualification

The same focused Linux `amd64` integration binary passed on Debian 13, Ubuntu
26.04, and Rocky Linux 10. It exercised 105 paginated entries, an unsafe
control-character name, a symlink to `/etc`, traversal input, and a forged
identity. See the
[K-001 three-guest result](../infra/host-tests/results/2026-08-26-file-manager-navigation-hyper-v.md).
K-002's separately built production helper passed full/range, permission,
symlink, response-limit, and cancellation tests on the same three guests; see
the [K-002 result](../infra/host-tests/results/2026-08-28-file-manager-download-hyper-v.md).
K-003's final production helper passed staging concealment, interrupted-upload
resume, digest verification, atomic activation, safe creation, conflict, and
cancellation checks on the same three guests; see the
[K-003 result](../infra/host-tests/results/2026-08-29-file-manager-write-hyper-v.md).
K-004's final production helper passed rename/move, recursive staged copy,
no-replace conflicts, symlink denial, trash listing/restore/purge, internal-path
concealment, and real project-quota exhaustion on every supported guest; see
the [K-004 result](../infra/host-tests/results/2026-08-29-file-manager-operations-hyper-v.md).
K-005's final production helper passed ZIP and tar.gz create/extract, exact
modes and content, no-replace conflicts, traversal, symlink, hardlink,
duplicate-entry and compressed-bomb rejection, hidden staging concealment, and
cleanup on every supported guest; see the
[K-005 result](../infra/host-tests/results/2026-08-29-file-manager-archives-hyper-v.md).
K-006's production helper passed authenticated manifest and complete-payload
verification, document-root and visible-account replacement, internal-root
preservation, cross-account isolation, modified-payload and symlink rejection,
and staging cleanup on every supported guest; see the
[K-006 result](../infra/host-tests/results/2026-08-29-local-backup-restore-hyper-v.md).
K-007's root parent passed pre-stream full verification, Range download,
resumable import, host-side reauthentication, deletion, and repository quota
enforcement on every supported guest; see the
[K-007 result](../infra/host-tests/results/2026-08-29-backup-transfer-retention-hyper-v.md).
