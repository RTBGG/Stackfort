# Account-owner Phase 1 flows

H-004 adds a distinct account-owner workspace to the authenticated application.
The login/bootstrap state machine remains shared with the administrator console,
but the server now discovers platform capability and account memberships before
the browser selects a workspace.

## Role-aware entry and switching

After `GET /api/v1/session` restores or creates a session, the browser requests
`GET /api/v1/me`. The response reports platform-administrator capability and up
to 100 active account memberships. A non-administrator enters the account
workspace directly. A platform administrator enters administration and can
switch to the account workspace only when that identity also has an explicit
active membership.

The owner-facing list never expands to every hosting account merely because the
identity is a platform administrator. This keeps account context explicit and
prevents the browser from treating broad platform privilege as an implicit
customer workspace.

## Self-service API

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/me` | Returns platform capability and active own memberships with the effective immutable package snapshot. |
| `PATCH` | `/api/v1/me/profile` | Updates the authenticated identity's email, display name, and locale. |
| `GET` | `/api/v1/sessions` | Lists up to 100 active sessions for the authenticated identity. |
| `DELETE` | `/api/v1/sessions/{sessionID}` | Revokes one session belonging to the authenticated identity. |
| `POST` | `/api/v1/sessions/revoke-all` | Revokes other sessions while optionally retaining the current session. |
| `GET` | `/api/v1/accounts/{accountID}/operations/{operationID}` | Returns bounded progress for one operation inside an authorized account. |
| `GET` | `/api/v1/accounts/{accountID}/php` | Returns host/package-intersected versions and bounded aggregate pool state for an authorized account. |

`GET /api/v1/me` derives its identity only from the authenticated authorization
subject. Each account entry includes its current membership role, account
status, package name/revision, effective limits, and the live non-removed domain
count. A non-sensitive `hostReady` flag keeps domain actions unavailable during
initial host provisioning. The response excludes Unix identity details,
superseded assignments, and accounts without an active membership.

Profile updates require CSRF proof and a session authenticated within the
recent-authentication window. The repository revalidates that subject inside
the write transaction and always uses the subject's identity as the target.
Email uniqueness remains a database-enforced conflict, and the audit event
records locale but not contact data.

## Account workspace

The responsive English/German workspace provides:

- an account dashboard with role, status, package revision, domain usage, and
  active TLS count;
- account selection when the identity belongs to multiple accounts;
- static/PHP-domain list, create, canonical/document-root/target edit,
  suspend/resume/remove, TLS state, expiry, issuance retry, and live durable
  operation progress;
- host/package-approved PHP selection and tenant-scoped pool state, configured
  domain count, memory, cumulative CPU time, and process count;
- on-demand non-secret certificate history with active/retired state, exact
  names, issuer, validity, renewal time, activation/retirement time, and
  SHA-256 fingerprint;
- package limits and the measured live domain count;
- own email, display-name, and persisted locale editing; and
- current/other browser session review, individual revocation, revoke-others,
  and sign-out.

The browser hides mutations that the membership cannot perform and makes a
suspended account read-only, while the server remains authoritative. Owners can
remove domains; owners and members can perform ordinary domain lifecycle work;
auditors receive read-only views according to the existing policy matrix.

Every domain or certificate mutation retains the returned operation ID and
polls the account-scoped status route until a terminal state. Success refreshes
the authoritative membership/domain snapshots; failure preserves the previous
visible configuration. The response intentionally omits payloads, results,
request IDs, idempotency keys, worker identities, leases, and attempt details.
Changing account/workspace, signing out, or unmounting the application cancels
the browser poll.

Certificate history is loaded only when expanded. An open history refreshes
with the domain snapshot when its certificate operation succeeds. The browser
never receives the public chain, private key, ACME URLs, or encrypted material.

Storage and traffic *limits* are shown, but consumption is deliberately not
invented. Only domain usage is currently backed by a measured control-plane
counter. Storage and bandwidth consumption remain labelled as unavailable
until accounting telemetry exists.

## Verification

Core tests verify membership-only discovery, current package snapshots, domain
counts, platform-capability separation, subject-bound profile writes, and the
recent-authentication gate. HTTP tests cover authenticated context responses,
omission of host internals, strict profile JSON, CSRF, request metadata, and
subject targeting. Frontend tests cover owner routing, domain-edit payloads,
profile/session actions, localization, accessibility, strict identifier
placement in API routes, and domain creation through terminal operation status
to the refreshed resource list. Additional flows cover PHP selection,
pool-health accessibility, active/retired history,
automatic post-issuance refresh, locale-aware dates, and secret-field omission.

Manual browser review covered every account page, administrator/account
switching, English and German copy, desktop and 320-pixel layouts, drawer focus
and inert state, and console output. The page had no horizontal overflow or
console warnings/errors. The review also generalized the profile control's
accessible name and normalized the progress-bar appearance.
