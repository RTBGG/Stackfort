# Local file backup and portable transfer foundation

K-006 adds authenticated, root-owned local file backups and staged restore to
Stackfort. It deliberately does not label the result a complete account backup:
the implemented scopes contain files only and exclude MariaDB data and users,
TLS keys and certificates, email, and generated server/control-plane state.

## User-visible scopes

`account_files` captures all visible top-level content beneath the derived
hosting-account root. The internal `.stackfort-uploads`,
`.stackfort-operations`, and `.stackfort-trash` trees are omitted and preserved
during restore. `document_root` captures and restores exactly one normalized
account-relative directory such as `public_html` or `domains/example.test`.

Account owners and members can list, inspect, verify, and create these backups.
Only the account owner can restore, and the request requires recent
authentication plus exact confirmation of the backup UUID. The English and
German UI explains both the exclusions and replacement semantics before the
action.

## Privilege and integrity boundary

The API derives the immutable account UID/GID and passes a closed action union
to the root agent. Repository selection is fixed at
`/srv/hosting/backups/<account-id>` and is never caller controlled. The root
parent owns the repository, locks one account at a time, and invokes the same
production binary in a hidden helper mode under the account credential. The
helper has no supplementary groups and performs descriptor-relative,
same-device, no-symlink traversal.

Each backup contains a mode-`0600` `payload.tar.gz` and `manifest.json`. Schema
1 binds the backup UUID, account UUID, scope, canonical source path, UTC
creation time, payload/content totals, entry count, and payload SHA-256. The
manifest is authenticated with HMAC-SHA-256 and a lazily created independent
32-byte mode-`0600` host key. The signature is not exposed through the public
API; callers receive bounded authenticated metadata and digest fingerprints.

## Creation and restore

Creation streams a deterministic sorted tar.gz payload into a root-only hidden
directory with a 4-GiB ceiling. The parent then parses the entire payload using
the same hostile-archive rules, signs and durably writes the manifest, and
publishes the UUID directory with `RENAME_NOREPLACE`.

Restore verifies the manifest HMAC, payload file metadata, complete SHA-256,
entry count, content size, and complete tar grammar before extraction. The
account helper extracts only safe directories and regular files into a fixed
hidden operation tree. A document root is activated as one same-filesystem
replacement. An account-files restore preflights and stages the complete
visible top-level set, preserves internal roots, and rolls back normal
activation failures.

Both directions cap content and payload at 4 GiB, entries at 10,000, nesting at
64 levels, and execution at 30 minutes through the existing bounded agent
transport. Links, special files, unsafe modes, invalid paths, duplicates,
cross-device trees, wrong ownership, and altered manifests or payloads fail
closed.

## K-007 transfer, retention, and capacity

K-007 exports only the portable `payload.tar.gz`; the host-bound HMAC manifest
and secret key never leave the root boundary. Downloads authenticate and fully
verify the payload before streaming and support a bounded single byte range.
Imports use exact-offset 8-MiB chunks, persist root-owned resume state, reserve
their declared apparent size immediately, recalculate SHA-256 on the host, parse
the complete archive, and publish only after signing a new local manifest.

Every package may set an independent `backupStorageBytes` ceiling from 1 MiB to
1 TiB. A missing field uses 20 GiB. The UI shows measured bytes, backup count,
the fixed 256-backup ceiling, and active imports. Owners can permanently delete
a backup after recent authentication and exact UUID confirmation; deletion is
the initial manual retention primitive.

## Deferred beyond K-007

- scheduled age/count retention and remote encrypted storage;
- scheduled backups;
- database dumps and restore; and
- a durable power-loss recovery journal for multi-tree account activation.

See [ADR 0046](adr/0046-authenticated-local-file-backup-and-staged-restore.md),
[ADR 0047](adr/0047-portable-backup-transfer-retention-and-quota.md), and the
[K-007 three-guest qualification](../infra/host-tests/results/2026-08-29-backup-transfer-retention-hyper-v.md).
