# Static-domain lifecycle

F-004 joins the transactional domain repository, durable operation runner,
privileged filesystem reconciliation, and F-003 NGINX activation into one
replay-safe static-site workflow.

## Browser API

All routes are account-scoped. Reads require an authenticated session; writes
also require the session-bound `X-CSRF-Token`, matching CSRF cookie, JSON where
applicable, and a non-empty `Idempotency-Key` header.
Logical removal additionally uses the destructive account action and therefore
requires an owner/platform administrator with recent authentication.

| Method and route | Result |
| --- | --- |
| `GET /api/v1/accounts/{accountID}/domains` | Lists visible live domains and typed target/TLS state. |
| `POST /api/v1/accounts/{accountID}/domains` | Queues a create operation; its operation UUID is also the future domain UUID. |
| `PATCH /api/v1/accounts/{accountID}/domains/{domainID}` | Queues a typed canonical-mode and/or target edit. |
| `POST .../{domainID}/suspend` | Removes the route while retaining domain and files. |
| `POST .../{domainID}/resume` | Reintroduces the retained route and confirms it active after activation. |
| `DELETE .../{domainID}` | Logically removes the domain and route; it does not delete content. |

Mutations return HTTP `202` with `operationId`, `domainId`, and the queued
status. Unknown JSON fields are rejected. The public shape has no NGINX source,
host path, executable, service, worker name, SELinux type, or raw configuration
field.

## Durable workflow

The in-process Linux worker claims `domain.lifecycle.apply` operations through
the existing fenced lease runner:

1. apply the typed domain mutation and its immutable operation marker in one
   serialized transaction;
2. capture one operation-linked, complete account desired-state document;
3. derive and ensure the sorted, unique document-root set;
4. reject activation if a newer desired revision now exists;
5. activate the immutable typed snapshot with the F-003 NGINX transaction;
6. record one operation-unique applied revision;
7. conditionally promote only still-matching pending targets to `active`;
8. complete the durable operation with stable, non-sensitive result fields.

The operation ID is the create domain ID, host revision ID, agent correlation,
and idempotency anchor. The desired document stores domain specs, renderer
options, document roots, and the exact pending `(domainId, targetId)` pairs.
Consequently, replay after any committed boundary is independent of later row
changes.

## Static-file access

The account retains ownership of its home, document roots, and files. Stackfort
adds an access ACL for only the distribution NGINX worker (`www-data` on
Debian/Ubuntu, `nginx` on Rocky): traverse-only at the account root and each
validated intermediate directory, then read/traverse at an enabled document
root. A default document-root ACL gives new account-owned files the same worker
entry, bounded by the file's effective mode mask. `0600` content is therefore
not served; restoring an effective read bit makes it available without changing
ownership.

On enforcing Rocky Linux guests, exact persistent SELinux file-context rules
label the account root, validated intermediate directories, and only the
selected document subtree as `httpd_sys_content_t`; bounded `restorecon` applies
the rule to existing content. POSIX ACLs remain the narrower per-worker
boundary. NGINX also rejects symbolic-link traversal below `$document_root`.

Neither suspend, logical removal, nor removal of the final sharing domain calls
a delete primitive. Root records, target history, TLS intent, and all non-empty
content remain for explicit file-management or later account archival.

## Verification

Unit and repository tests cover strict tagged payloads, create replay, immutable
desired-state replay, stale-revision refusal, conditional activation, full
create/edit/suspend/resume/remove behavior, package/domain conflicts, shared
roots, CSRF/idempotency transport, exact ACL/SELinux command profiles, and
absence of a delete call.

The disposable Hyper-V suite runs the production repository and operation
runner with the real host identity, project-quota filesystem, NGINX renderer,
and activator. Debian 13, Ubuntu 26.04, and enforcing Rocky Linux 10 each proved:

- an account-owned static file is served through its generated host;
- removing the worker's effective file read permission stops serving it;
- a symlink to `/etc/passwd` is not served;
- an edit switches service to a newly reconciled custom root;
- a second domain serves the same explicit shared root;
- suspend, resume, and logical removal change only routing;
- the shared/non-empty root survives removal of one and then all routes.

Upstream references:

- [NGINX `user` directive](https://nginx.org/en/docs/ngx_core_module.html#user)
- [NGINX `disable_symlinks` directive](https://nginx.org/en/docs/http/ngx_http_core_module.html#disable_symlinks)
- [`setfacl(1)` access and default ACLs](https://www.man7.org/linux/man-pages/man1/setfacl.1.html)
- [`semanage-fcontext(8)` persistent file contexts](https://www.man7.org/linux/man-pages/man8/semanage-fcontext.8.html)
- [`restorecon(8)` context application](https://www.man7.org/linux/man-pages/man8/restorecon.8.html)
