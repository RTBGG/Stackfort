# Authorization policy

C-003 adds the application-layer policy between an authenticated browser
session and every platform or account resource. Authentication alone grants no
resource access.

The policy follows four rules:

1. deny unknown actions and unmatched relationships by default;
2. resolve roles, memberships, account status, and session freshness from
   current server-side state on every protected request;
3. select the action in server code rather than accepting a permission name
   from the client; and
4. scope an object lookup by both its account ID and object ID.

## Server-derived subject

`AuthenticatedSession.AuthorizationSubject()` is the only public constructor
for an authorization subject. Its identity, session, and per-process HMAC seal
are private to the core package. The repository adds the seal only after
successful cookie-session resolution and verifies it before every decision.
HTTP handlers therefore cannot build an accepted subject from path, query,
JSON, header, or local-storage values, or from an unverified session-shaped
struct.

`Repository.Authorize` rechecks that the session is active, belongs to the
identity, has not reached its absolute or idle expiry, and that the identity is
active. Platform roles and active account memberships are read from SQLite for
the decision; they are not cached in a browser token.

## Policy matrix

An administrator may act across accounts. Account roles apply only to the
account named in the server-side authorization request.

| Action | Platform administrator | Owner | Member | Auditor | Recent authentication |
| --- | --- | --- | --- | --- | --- |
| View platform state | Allow | Deny | Deny | Deny | No |
| Manage platform state | Allow | Deny | Deny | Deny | Yes |
| View packages | Allow | Deny | Deny | Deny | No |
| Manage packages | Allow | Deny | Deny | Deny | Yes |
| Create accounts | Allow | Deny | Deny | Deny | Yes |
| View an account | Allow | Allow | Allow | Allow | No |
| Manage account settings | Allow | Allow | Deny | Deny | Yes |
| View memberships | Allow | Allow | Deny | Allow | No |
| Manage memberships | Allow | Allow | Deny | Deny | Yes |
| View the effective package | Allow | Allow | Allow | Allow | No |
| Assign a package | Allow | Deny | Deny | Deny | Yes |
| View account resources | Allow | Allow | Allow | Allow | No |
| Manage ordinary resources | Allow | Allow | Allow | Deny | No |
| View account access/error logs | Allow | Allow | Allow | Allow | No |
| View scheduled jobs | Allow | Allow | Allow | Allow | No |
| Manage scheduled jobs | Allow | Allow | Allow | Deny | No |
| View/create/verify local file backups | Allow | Allow | Allow | Deny | No |
| Restore local file backups | Allow | Allow | Deny | Deny | Yes |
| View account audit data | Allow | Allow | Deny | Allow | No |
| Perform destructive account actions | Allow | Allow | Deny | Deny | Yes |

Member and auditor records already exist for forward compatibility. Their
least-privilege policy is defined and tested, but team-management UI remains a
post-MVP feature.

Unknown action constants have no fallback permission. Adding a new protected
operation therefore requires an explicit policy entry and matrix test.

Identity-scoped actions derive their target from the sealed subject and do not
accept another identity ID:

| Action | Active authenticated identity | Recent authentication |
| --- | --- | --- |
| View own profile | Allow | No |
| Manage own profile | Allow | Yes |
| View own factors | Allow | No |
| Manage own factors | Allow | Yes |
| View own sessions | Allow | No |
| Manage own sessions | Allow | Yes |

## Recent authentication

The policy itself marks sensitive actions. Callers cannot opt out. A sensitive
decision requires `sessions.authenticated_at` to be no more than five minutes
old. A normal password login creates and rotates a session with a fresh
authentication timestamp; stale requests receive
`recent_authentication_required`.

C-004 adds TOTP, recovery, identity-scoped session review/revocation, and MFA
session metadata. Factor and session mutations use the same non-optional C-003
freshness gate, which is also mandatory for platform/package
changes, account creation, account settings, membership changes, package
assignment, and destructive account actions.

## Account state

- Active accounts use the complete matrix.
- Suspended accounts remain readable to active members, but their mutations are
  denied. A platform administrator can inspect and repair them.
- Archived accounts are inaccessible through account roles. A platform
  administrator retains recovery and audit access.
- Unknown or missing account states are denied.

## Object and HTTP boundary

`Repository.GetAuthorizedHostingAccount` resolves the current session,
platform role, account, and membership in one SQLite statement before returning
the account. This is the pattern for future domain, database, file, backup, and
application services: authorization and the account-scoped lookup must be
coupled in the application layer.

The first protected resource endpoint is:

- `GET /api/v1/accounts/{accountID}`

H-004 adds `GET /api/v1/me` for bounded active membership discovery and
`PATCH /api/v1/me/profile` for subject-bound profile changes. Platform
administrator capability is reported separately and never expands the
owner-facing membership list implicitly.

It accepts the session only from the strict host-only cookie. Missing or invalid
sessions return `401`. A missing account and an existing account outside the
subject's scope both return the same generic `404`; changing the path ID cannot
reveal or return another tenant's account.

The existing domain repository additionally requires both `accountID` and
`domainID`, and its composite relations prevent substituting an object from a
different account. Future HTTP domain handlers must first authorize
`account.resources.view` or `account.resources.manage` for that same account.

References:

- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- [OWASP Transaction Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html)
