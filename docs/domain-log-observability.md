# Domain log observability

K-008 adds account-scoped access and error views while minimizing what is
persisted and bounding the privileged reader. These diagnostics are distinct
from audit events, general metrics, and future WAF/OCI application logs.

## Capture and storage

The fixed NGINX `stackfort_redacted` JSON format contains exactly:

- RFC 3339 timestamp, client address, normalized host, and method;
- `$uri`, which excludes the query string;
- response status and body byte count; and
- request duration.

It does not reference `$args`, `$request`, authorization/cookie headers,
referrer, or user agent. Each generated domain server writes access and native
NGINX error output to:

```text
/var/log/stackfort/accounts/<account UUID>/
└── domain-<SHA-256 of ASCII domain>.(access|error).log
```

`/var/log/stackfort` is root-owned `0750`; `accounts` and each account child are
root-owned `0700`; files are root-owned `0640`. The domain itself is not a
filename. NGINX's root master opens the files and workers inherit descriptors.
Rocky labels the complete account-log subtree `httpd_log_t` while SELinux stays
enforcing.

## Retention

The installer requires `logrotate` and owns `/etc/logrotate.d/stackfort`.
Rotation is daily or when an active file reaches 8 MiB, whichever applies;
seven rotations and at most seven days are retained. Numbered files use gzip
with one delayed uncompressed rotation. After rename/create, `USR1` asks NGINX
to reopen descriptors. `copytruncate` is intentionally absent.

The account-facing reader considers only the active file and `.1`. Older gzip
rotations remain root-only for an administrator's local incident response but
are not decompressed from attacker-influenced traffic by the privileged API.

## Privileged read boundary

`hosting.logs.read` accepts a derived hosting identity, normalized domain,
closed `access`/`error` kind, maximum-50 limit, and optional canonical
`inode:offset` cursor. It accepts no filesystem path. Root verifies all three
fixed parent directories and opens only the derived regular file with
`O_NOFOLLOW`, then checks root ownership, `0640`, link count one, and inode.
A stale cursor fails as a conflict after rotation instead of continuing in an
unrelated file.

Each newest-first page scans at most 256 KiB per retained source. Access JSON is
parsed into typed fields. Malformed input becomes a fixed withheld record.
Native error lines remain message records. Before either crosses RPC, the
reader normalizes control/invalid text, removes query tails, credential-like
assignments, and values in sensitive path segments, then caps paths at 512
bytes and messages at 768 bytes.

Redaction is defense in depth, not proof that arbitrary application path names
are non-sensitive. The UI says which request data is never captured and that
displayed records are redacted; operators must still avoid placing secrets in
URLs.

## Authorization and UI

The public read is:

```text
GET /api/v1/accounts/{accountID}/logs?domainId={domainID}&kind=access|error&cursor={optional}
```

The service rechecks account host readiness and `account.logs.view`, loads the
domain through the account-scoped repository list, and derives the agent
identity/domain. Administrators, owners, members, and auditors can view the
records; an outsider cannot substitute another account or domain. The English
and German account page provides domain/kind selection, typed access/error
tables, older-page loading, and the current privacy/retention notice.

## Qualification

Unit and component tests cover protocol unions, parsing/redaction, renderer
privacy, authorization/service scoping, strict HTTP queries, translated UI,
and accessibility. One final Linux integration binary additionally sent an
actual NGINX request containing unique query, authorization, cookie, referrer,
and user-agent secrets; verified their absence from raw access storage; checked
the second error-log redaction, cursor pagination, cross-account denial,
symlink rejection, modes, SELinux type, and two real delayed-compression
rotations on Debian 13, Ubuntu 26.04, and Rocky Linux 10. See the
[qualification evidence](../infra/host-tests/results/2026-08-29-domain-log-redaction-retention-hyper-v.md).
