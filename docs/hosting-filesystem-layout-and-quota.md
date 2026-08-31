# Hosting filesystem layout and project quota

E-002 gives every hosting account a fixed descendant layout and a directory-tree
quota without turning the privileged agent into a general file manager.

## Durable intent

Migration `009_hosting_account_filesystems.sql` creates exactly one retained
filesystem row per account. Its immutable project ID equals the account's
immutable UID. The row separately records:

- desired byte and inode limits copied from the current package assignment;
- the last byte and inode limits confirmed on the host;
- an optimistic revision;
- `pending`, `applied`, or `blocked` reconciliation state;
- the observed quota capability status and stable reason code; and
- the correlated durable operation and timestamps.

Assigning a new package increments the filesystem revision and returns it to
`pending` without claiming that the previously applied limits changed. An
agent result can confirm only the current revision and an operation belonging
to that account. A successful confirmation copies desired limits to applied
limits; an unavailable capability records `blocked` while preserving the last
known applied truth.

Package byte limits must be at least 1 KiB and a whole number of 1-KiB quota
blocks. A nil limit becomes zero at the host boundary, which `setquota` treats
as unlimited for that dimension.

## Fixed account tree

The account root remains `/srv/hosting/accounts/<account UUID>` with the
account UID:GID and mode `0750`. Reconciliation creates only these immediate
children:

| Directory | Mode | Purpose |
| --- | --- | --- |
| `applications` | `0750` | Account-owned OCI application data |
| `backups` | `0700` | Private account-visible backup workspace |
| `domains` | `0750` | Per-domain content roots |
| `logs` | `0750` | Account-visible service logs |
| `public_html` | `0750` | Default web root |
| `tmp` | `0700` | Private temporary workspace |

Every directory is owned by the immutable account UID:GID. Other hosting
accounts are neither members of that account's group nor able to traverse its
`0750` root. Existing descendants with a different type, owner, mode, or
filesystem device are rejected rather than adopted or repaired recursively.

The Linux implementation opens `/`, `srv`, `hosting`, `accounts`, the account,
and every descendant one component at a time with directory descriptors,
`O_NOFOLLOW`, and close-on-exec. Managed ancestors must remain root-owned and
not group/world writable. The account and all descendants must stay on the
same filesystem as `/srv/hosting`; a nested mount cannot silently bypass the
quota boundary.

## Project quota mechanics

The capability gate permits enforcement only for an ext4 mount with
`prjquota`, or XFS with `prjquota`/`pquota`. Before any project-owned content is
created, the agent invokes one fixed `/usr/sbin/setquota` profile:

```text
setquota -P <immutable project ID> <KiB soft> <KiB hard> \
  <inode soft> <inode hard> /srv/hosting
```

Soft and hard values are equal so enforcement is immediate rather than
grace-period based. The RPC caller supplies no executable, option, mount path,
or raw argument list; the shared typed storage specification is revalidated by
the protocol, reconciler, and process profile.

The account root then receives its numeric project ID and project-inherit flag
through Linux `FS_IOC_FSGETXATTR`/`FS_IOC_FSSETXATTR`, followed by a read-back
verification. A new empty root can be assigned in place. A non-empty root that
does not already have the exact project ID and inherit flag receives
`filesystem_migration_required`; Stackfort will not attempt a risky recursive
online rewrite. A different nonzero project ID is a conflict.

If the mount probe is unavailable or unsupported, the agent returns
`quota_unavailable` with the complete typed capability and reason code before
running filesystem mutations. Failure to start the fixed quota utility becomes
the stable `quota-tool-unavailable` capability. Other tool or ioctl failures do
not masquerade as successfully applied limits.

## Document roots

The control plane and agent share one canonical relative-path validator. It
rejects absolute paths, backslashes, padding, controls, empty and dot segments,
noncanonical separators, unsupported characters, and values over 1,024 bytes.

The agent independently validates again, requires the account root's exact
project ID and inherit flag, and opens or creates every component relative to
the prior descriptor with `O_NOFOLLOW`. Every component must have the account
owner, mode `0750`, and the managed filesystem device. Thus `..`, symlinks, and
nested foreign filesystems cannot redirect document-root creation outside the
account tree. Generated web-server activation must use the same reconciled
relative path; database validation alone is not a filesystem authorization.

## Verification boundary

Cross-platform tests cover storage-spec validation, exact `setquota` argument
construction, capability-first failure, typed RPC unions/errors, durable
desired/applied revisions, and lexical traversal rejection. Linux tests open
real directory descriptors and prove a symlink target is not modified.

Runtime validation of project assignment, hard byte/inode enforcement, and
the ext4/XFS tool behavior remains part of the disposable supported-VM gate for
Debian 13, Ubuntu 26.04, and Rocky Linux 10. Do not run it on a shared host.

References:

- [Linux quota subsystem](https://docs.kernel.org/filesystems/quota.html)
- [`quotactl(2)` project quota semantics](https://man7.org/linux/man-pages/man2/quotactl.2.html)
- [`setquota(8)` project mode](https://man7.org/linux/man-pages/man8/setquota.8.html)
- [`FS_IOC_FSGETXATTR(2)` and project inheritance](https://man7.org/linux/man-pages/man2/ioctl_xfs_fsgetxattr.2.html)

