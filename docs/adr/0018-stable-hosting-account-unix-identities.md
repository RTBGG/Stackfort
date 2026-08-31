# ADR 0018: Stable hosting-account Unix identities

Status: Accepted

## Context

Static files, PHP pools, quotas, cgroups, and rootless container processes need
a durable OS ownership boundary. Email addresses, display names, and account
slugs are mutable user-facing data and may contain characters or relationships
that are unsuitable for Linux identities. Automatically adopting an existing
name or numeric ID would turn an installation conflict into cross-tenant access.

Account removal also cannot make `userdel -r` an implicit data-deletion API.
Archive evidence and identity removal must be separate, reviewable stages.

## Decision

1. Allocate one immutable username, UID, GID, and account-root path in the same
   transaction that creates a hosting account.
2. Derive the username and path only from the canonical UUIDv7 account ID. Keep
   UID equal to GID and allocate monotonically in the Stackfort-reserved
   `200000–599999` range.
3. Retain allocation rows permanently, including deleted tombstones, so a
   numeric identity is never reused by Stackfort.
4. Reconcile by checking both local names and numeric IDs before mutation.
   Create missing exact identities and repair metadata only when the immutable
   name/UID/GID tuple already matches. Never adopt, rename, or renumber a
   conflicting identity automatically.
5. Restrict `shadow-utils` calls to fixed profiled argument templates. Use no
   shell and put no password on a command line.
6. Walk the managed directory hierarchy with directory descriptors and
   no-symlink opens. Require trusted root-owned ancestors; change ownership only
   on the exact account root in this slice.
7. Model removal as archive request, archive confirmation with a durable
   reference, deletion request, and deletion confirmation. Keep account and
   identity tombstones plus hash-chained audit events.
8. Require the account root to be absent before invoking plain `userdel` and
   `groupdel`. Never use recursive or forced user deletion.

## Consequences

- User-facing changes never rename a Linux account or change file ownership.
- A pre-existing host collision blocks reconciliation and requires explicit
  operator remediation; availability is sacrificed to avoid identity adoption.
- The installer must reserve/check the numeric range and coordinate future
  rootless-container subordinate-ID ranges so they do not overlap.
- Reconciliation is convergent across retries, while the durable operation ID
  and agent idempotency key correlate each attempt.
- E-002 owns recursive layout, document-root traversal, and quotas; E-001 only
  creates or repairs the exact account-root ownership boundary.
- Backup/archive implementation remains a separate worker, but identity
  deletion cannot run until that worker has removed the live root and the
  control plane has recorded archive evidence.
