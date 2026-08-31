# ADR 0045: Bounded archive creation and hostile-input extraction

- Status: accepted and implemented by K-005
- Date: 2026-08-29

## Context

Archives combine recursive filesystem access, attacker-controlled path names,
compression, metadata, and potentially extreme expansion ratios. Parsing or
extracting them directly at a visible destination would let a malformed input
escape the account root, create active links or special files, replace existing
data, expose partial output, or monopolize the privileged agent.

## Decision

1. Extend the closed file-write union with only `archive.create` and
   `archive.extract`. Support exactly ZIP and tar.gz and require the selected
   format to match the lowercase source or destination suffix.
2. Require `account.files.manage`, host readiness, immutable identity
   derivation, CSRF, and a durable authorization audit event before mutation.
   The helper independently validates that identity and runs under the account
   UID/GID with no supplementary groups.
3. Permit creation from one account-owned regular file or directory. Traverse
   descriptor-relatively without following links and reject ownership, device,
   link, or special-file violations.
4. Snapshot an extraction source into a fixed hidden operation directory before
   parsing. Materialize output only under that same hidden tree and activate it
   as a new destination directory with `RENAME_NOREPLACE` after all files and
   directories have been `fsync`ed.
5. Apply the canonical account-relative path grammar to every archive member.
   Reject absolute paths, dot segments, backslashes, invalid names, duplicates,
   implicit-parent collisions, links, devices, FIFOs, sockets, unsafe modes,
   encrypted ZIP members, unsupported ZIP methods, and tar entry types other
   than regular file or directory.
6. Preflight the ZIP end record and central directory before decoder use. Reject
   multidisk and ZIP64 inputs, excessive names/extras/comments, and more than
   10,000 entries.
7. Bound both directions to 10,000 entries, 64 directory levels, 4 GiB, four
   concurrent agent requests, eight hidden operations, and 30 minutes. For
   extraction, cap expanded data and the complete decompressed gzip stream at
   the smaller of 4 GiB or 64 MiB plus 200 times the compressed input size.
8. Use safe output modes (`0750` directories and `0640` files), preserve the
   account project quota, map `EDQUOT`/`ENOSPC` to a stable error, and remove all
   hidden staging after success or failure. Never replace a visible path.

## Consequences

- Archive creation and extraction never reveal partial output; the visible
  namespace changes once, atomically, after complete validation and durability.
- Archives created by Stackfort deliberately omit ownership, links, devices,
  special permission bits, and richer metadata. This favors a portable hosting
  content format over a full filesystem backup format.
- ZIP64, encrypted ZIP, non-deflate/non-store ZIP methods, links, sparse files,
  hardlinks, and special tar entries are not supported.
- Extraction has an intentionally conservative expansion-ratio policy. A valid
  but unusually compressible archive may need to be repacked before upload.
- The operation is a bounded point-in-time best effort, not an application-level
  snapshot. K-006 backup manifests and staged restore remain a separate design.
