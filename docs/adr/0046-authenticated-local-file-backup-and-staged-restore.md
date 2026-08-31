# ADR 0046: Authenticated local file backup and staged restore

- Status: accepted and implemented by K-006
- Date: 2026-08-29

## Context

A backup becomes a privileged replacement source. Account-controlled archives,
paths, metadata, or manifests must not be able to cross account boundaries,
restore links or special files, overwrite internal Stackfort state, or alter
visible content before the complete backup has been authenticated and parsed.
Calling a file archive an “account backup” would also be misleading if it did
not include databases and control-plane configuration.

## Decision

1. K-006 exposes exactly two file scopes: `account_files` means every visible
   top-level file tree in one hosting account; `document_root` means one exact
   canonical account-relative directory. Neither scope includes databases,
   database users, TLS material, email, or server/control-plane configuration.
2. Store local backups outside the account root at
   `/srv/hosting/backups/<account-id>/<backup-id>`. The account directory and
   backup directory are root-owned mode `0700`; `manifest.json` and
   `payload.tar.gz` are root-owned mode `0600`.
3. Use a versioned schema-1 manifest containing the immutable account, backup,
   scope, source, creation time, payload size, content size, entry count, and
   payload SHA-256. Authenticate its canonical unsigned JSON with HMAC-SHA-256
   using a separate 256-bit root-only key at
   `/var/lib/stackfort-agent/backup-manifest.key`.
4. The root parent owns repository selection, locking, manifest authentication,
   payload digest verification, and publication. A separate production-helper
   process runs with the derived account UID/GID and no supplementary groups to
   read or materialize account data.
5. Creation writes a hidden root-owned staging directory, streams a sorted
   tar.gz payload from descriptor-relative account traversal, independently
   parses the completed payload, signs the manifest, `fsync`s it, and publishes
   the backup with no-replace rename.
6. Reject symlinks, hardlinks, special files, traversal, alternate separators,
   duplicate/colliding entries, unsafe modes, ownership/device conflicts, more
   than 10,000 entries, more than 64 levels, and content or payload beyond
   4 GiB. Exclude `.stackfort-uploads`, `.stackfort-operations`, and
   `.stackfort-trash` from `account_files`.
7. Restore authenticates the manifest, verifies the complete payload digest,
   parses the complete tar stream, and materializes safe `0750` directories and
   `0640` files in account-owned hidden staging before any visible change.
8. A document-root restore swaps that exact directory through same-filesystem
   staged rename. An account-file restore moves the preflighted visible
   top-level set aside, activates the backed-up set, rolls back ordinary
   activation failures, and preserves Stackfort's three internal roots.
9. Owners and members may list, inspect, verify, and create. Restore is
   owner-only, requires recent authentication, CSRF, a persisted authorization
   audit event, and exact re-entry of the backup UUID.

## Consequences

- A corrupted or unauthenticated backup is rejected before visible account
  content changes.
- Normal activation failures have deterministic rollback. This milestone does
  not yet claim power-loss recovery across the short multi-rename account
  activation window; a future journal may strengthen that guarantee.
- The repository is local to one server. Download, upload, deletion, retention,
  encryption for remote transport, scheduled backups, database dumps, and
  application-consistent snapshots remain separate work.
- Local backup payloads count against host capacity, not the account's project
  quota. Restore materialization and the final visible data remain subject to
  that account quota.

