# ADR 0019: Account project quota and descriptor-relative layout

Status: Accepted

## Context

An account tree can contain static files, PHP output, application data, and
files created by more than one service identity. A Unix user quota therefore
does not reliably describe the complete account tree. String-only path checks
also cannot defend privileged creation from symlinks, mount boundaries, or
filesystem races.

Quota support differs by filesystem and mount configuration. Reporting a
package limit as active when the kernel cannot enforce it would be a control
plane integrity failure.

## Decision

1. Use one filesystem project per hosting account. Bind its immutable numeric
   project ID to the account's immutable UID.
2. Persist desired and last-confirmed byte/inode limits separately, with an
   optimistic revision, capability state, stable reason, and correlated
   operation.
3. Require byte limits to be representable as 1-KiB quota blocks. Apply equal
   soft and hard project limits through one fixed no-shell `setquota` profile.
4. Permit automatic first assignment only on an empty account root. Set and
   verify the project ID plus inherit flag before creating the fixed layout.
   Require an offline migration for a non-empty unassigned tree.
5. Keep the account root at `0750`; use `0750` for public/application/log
   descendants and `0700` for backup/temporary descendants. Never recursively
   adopt conflicting existing content.
6. Walk trusted ancestors, layout children, and document roots with directory
   descriptors and no-symlink opens. Require exact ownership/modes and the
   `/srv/hosting` filesystem device.
7. Validate document roots through one shared canonical relative-path type in
   both the control plane and agent. Do not expose arbitrary privileged paths.
8. Refuse quota reconciliation before filesystem mutation when the fresh mount
   capability is not available. Return the capability and stable reason rather
   than claiming an unenforced limit.

## Consequences

- The complete directory tree is charged to the account even when individual
  files have different Unix owners.
- Supported production storage must use ext4 `prjquota` or XFS
  `prjquota`/`pquota` and provide `/usr/sbin/setquota`.
- Existing non-empty pre-E-002 roots require an explicit offline migration;
  the initial implementation intentionally has no recursive online adoption.
- A package can express unlimited bytes, unlimited inodes, or both, but any
  nonzero byte value has 1-KiB granularity.
- A nested mount, symlink, wrong owner, or wrong mode blocks reconciliation and
  requires operator remediation.
- The same relative-path invariant must be used later by deterministic NGINX
  rendering and activation; E-002 does not by itself activate a virtual host.
- Kernel quota enforcement still requires supported-VM integration tests; unit
  and cross-build tests cannot emulate the ext4/XFS quota ioctls.

