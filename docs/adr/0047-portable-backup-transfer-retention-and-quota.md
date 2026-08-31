# ADR 0047: Portable backup transfer, retention, and repository quota

- Status: accepted and implemented by K-007
- Date: 2026-08-29

## Context

K-006 deliberately made `manifest.json` host-bound: its HMAC is meaningful only
to the root agent holding that host's independent key. Exporting that manifest
would either create a false cross-host trust signal or require exporting a
secret. Backup imports are hostile compressed input, while interrupted uploads
must not bypass package capacity by remaining hidden or sparse.

## Decision

1. The portable artifact is only the fully verified `payload.tar.gz`. The local
   HMAC manifest and key never cross the privileged boundary.
2. A download authenticates the manifest and verifies the complete payload
   digest and tar grammar before streaming its first byte. It supports one
   bounded HTTP byte range for reliable resume.
3. An import receives the payload in exact-offset chunks of at most 8 MiB. Its
   root-owned staging directory reserves the declared apparent size at
   initiation, persists bounded resume metadata, and permits at most eight
   concurrent imports per account.
4. Completion calculates SHA-256 on the host, parses the complete hostile
   archive, and creates a new account/host-bound schema-1 manifest. Only then is
   the upload UUID published as a backup with `RENAME_NOREPLACE`.
5. `backupStorageBytes` is a separate immutable package-revision limit. If it is
   absent, the conservative default is 20 GiB; accepted explicit values range
   from 1 MiB through 1 TiB. Apparent bytes of published backups and active
   imports are measured under the account repository lock.
6. Manual owner-only deletion with recent authentication, CSRF protection, a
   durable authorization audit event, and exact UUID confirmation is the first
   retention primitive. The root agent renames the exact UUID directory to a
   hidden deleting name before descriptor-relative removal. It can therefore
   safely remove a corrupted backup without trusting its manifest.

## Consequences

Backups can move between Stackfort hosts without sharing trust keys. Interrupted
imports resume after a page reload and can be canceled to release their
reservation. Lowering a package below current measured use does not destroy
data: creation/import fails closed until an owner deletes enough backups.

Automated age/count policies, schedules, remote object storage, encryption for
remote artifacts, databases, and application-consistent snapshots remain later
features. Manual deletion is intentionally not presented as a scheduled
retention policy.
