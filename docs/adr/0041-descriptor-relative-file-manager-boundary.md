# ADR 0041: Descriptor-relative file-manager boundary

- Status: accepted and partially implemented by K-001
- Date: 2026-08-26

## Context

A file manager necessarily accepts user-selected names inside a tenant tree.
String-prefix checks, path cleaning alone, and ordinary absolute-path opens do
not prevent symlink swaps, mount escapes, or mismatched host identity state.
Unbounded alphabetical pagination would also rescan large directories for each
page and undermine the project's performance and availability goals.

## Decision

1. Accept only canonical account-relative paths and derive the account root
   from the persisted immutable UUID/Linux identity.
2. Resolve the root and every descendant component with directory-relative
   Linux operations, `O_NOFOLLOW`, ownership checks, and a same-device fence.
3. Inspect entries without following symlinks and never make a displayed
   non-round-trippable name actionable under a transformed value.
4. Keep metadata listing in the bounded typed agent RPC, with at most 100
   returned entries and 4,096 inspected raw entries per call.
5. Use opaque Linux directory cookies for continuation rather than rescanning
   and sorting the directory by name.
6. Authorize file visibility separately from general resource telemetry;
   owners, members, and platform administrators may browse, while auditors and
   outsiders may not.
7. Introduce separate streaming and mutation protocols for downloads/uploads
   instead of expanding the 64-KiB JSON union into a generic file channel.

## Consequences

- Traversal and symlink attacks cannot select a path outside the derived
  account tree through the supported listing operation.
- Listing work is bounded per request and can resume through very large or
  adversarial directories without retaining server-side cursor state.
- Kernel directory order is shown; optional UI sorting can only be applied to
  the currently loaded page unless a future indexed metadata layer is added.
- File contents and all mutation features remain unavailable until their
  distinct streaming, staging, quota, audit, and recovery contracts exist.
