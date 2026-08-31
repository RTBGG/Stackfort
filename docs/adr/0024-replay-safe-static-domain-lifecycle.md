# ADR 0024: Replay-safe static-domain lifecycle and web-root access

Status: accepted

## Context

Creating a usable static domain spans SQLite, account-owned storage, privileged
host policy, generated NGINX state, a service reload, and a final database
status. Those steps cannot share one atomic transaction. A retry must neither
create a second domain nor render later mutable rows under the identity of an
older operation. NGINX also runs as a distribution-owned worker identity rather
than as the hosting-account owner, while account roots must remain closed to
other hosting accounts.

## Decision

1. Queue create, edit, suspend, resume, and remove as `safe` durable operations.
   Require account-resource authorization, CSRF proof, and a caller
   idempotency key at the browser API.
2. Use the create-operation UUID as the new domain UUID. Persist one immutable
   `(operation, account, domain, action)` marker in the same transaction as each
   domain mutation.
3. After the mutation, capture the complete current account routing state in
   one immutable desired-state revision linked uniquely to the operation.
   Replay uses that document and never rereads mutable domain rows.
4. Before host activation, require that the captured revision is still the
   account's latest revision. A superseded operation stops without activating
   stale configuration.
5. Create every referenced document root through descriptor-relative,
   no-symlink filesystem code. Grant only the NGINX worker `--x` on the account
   root and each validated intermediate directory, and `r-x` on the selected
   document root, through exact POSIX ACLs. Add a matching default ACL for
   future account-owned files.
6. On Rocky Linux, add persistent `httpd_sys_content_t` rules for the exact
   account root, each validated intermediate directory, and the selected
   document-root subtree, then apply them with a bounded recursive `restorecon`.
   Do not disable SELinux.
7. Render static locations with `disable_symlinks on from=$document_root`.
   Files remain owned and writable by the account; NGINX can read only what the
   resulting ACL and mandatory-access policy permit.
8. Reuse the F-003 staged activation transaction. Mark a pending domain active
   only after an applied revision with the same operation and desired revision
   exists, and only while its current target still matches the captured target
   UUID.
9. Removing a domain removes only its route from desired NGINX state. It never
   deletes a document-root record or any account file, including a shared or
   non-empty root.

## Consequences

- An API/worker restart can replay every committed boundary without duplicating
  domain intent, desired revisions, or applied revisions.
- Package counts, global host conflicts, wildcard conflicts, target history,
  TLS intent, and the domain mutation remain one serialized SQLite transaction.
- A newer edit cannot be marked active by an older activation result.
- Default ACLs cover future files. A hosting account can deliberately remove
  the worker's effective read permission with its file mode; Stackfort does not
  override that choice recursively on every request.
- SELinux policy entries outlive domain removal because the corresponding
  content root also survives. Account deletion will need a separate, exact
  policy cleanup step after archival and root removal.
- F-004 serves HTTP static content. G-001 now supplies the HTTP-01 exception;
  ACME issuance and TLS activation remain in G-002, and the browser user
  interface remains a later vertical slice.

## Rejected alternatives

- Running NGINX workers as each account would require a different serving
  architecture and substantially expand process/configuration complexity.
- Making account roots world-traversable or files world-readable weakens
  isolation and ignores the account owner's effective permission choice.
- Recursively changing ownership or file modes during domain activation would
  mutate customer content and make shared-root behavior surprising.
- Deleting a root when its final route disappears risks irreversible data loss
  and races later reuse.
- Rereading live rows on retry can apply a different configuration under an
  already-audited operation UUID.
