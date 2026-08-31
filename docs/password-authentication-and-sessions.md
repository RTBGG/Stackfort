# Password authentication and browser sessions

C-002 turns the administrator credential created by C-001 into a bounded
browser-authentication surface. It does not grant access to account or
administrator resources by itself; C-003 separately authorizes each protected
request.

## HTTP surface

The password/session control API exposes:

- `POST /api/v1/login` with one `application/json` email/password object;
- `GET /api/v1/session` to resolve the current cookie session; and
- `POST /api/v1/logout` with the current CSRF proof.

When TOTP is enabled, login becomes the two-phase flow documented in
[TOTP, recovery codes, and session management](totp-recovery-and-session-management.md).

Login rejects simple form content types, unknown JSON fields, trailing JSON
values, and bodies above 4 KiB. Unsafe requests carrying
`Sec-Fetch-Site: cross-site` are rejected before a service method runs. The API
does not enable cross-origin credential sharing.

Unknown identities, identities without a credential, suspended identities, and
wrong passwords all execute one bounded password-derivation path and return the
same `401 authentication_failed` body. A fixed dummy salt/hash is used when no
credential exists; the submitted password is still processed with Argon2id.
Stored verification parameters are range-checked before allocation, and this
version accepts the Stackfort 32-byte Argon2id result only.

## Persistent login limits

Migration 006 stores pressure state in SQLite, so restarting the API does not
reset it. Identity keys are SHA-256 digests of normalized email addresses; raw
email addresses and passwords never enter the limit table.

| Scope | Counting | Window and limit | Block |
| --- | --- | --- | --- |
| Direct source | Every login reaching the service | 10 per minute | 5 minutes on the 11th attempt |
| Global | Every login reaching the service | 120 per minute | 1 minute on the 121st attempt |
| Identity key | Failed password attempts | 5 per 10 minutes | 15 minutes after the fifth failure |

Global and direct-source capacity is consumed before Argon2id. The identity
block is also checked before Argon2id and is cleared by a successful login.
Unknown identities receive the same hashed-key counter behavior, so the rate
response does not prove that an account exists. At most two password
derivations run concurrently in one API process.

The API currently uses the direct TCP peer and does not trust forwarded-address
headers. Behind the local NGINX panel proxy, remote clients therefore share its
source bucket until an authenticated proxy-source boundary is implemented. The
identity and global limits remain independent.

## Session and cookie contract

Every successful completed login generates independent 256-bit random session and CSRF
values. SQLite stores only their SHA-256 digests. The identity, session,
optional revocation of the previously presented session, cleared identity
failure counter, and login audit event commit in one transaction after the
credential is revalidated. A concurrent password change prevents session
creation.

The browser receives two host-only session cookies:

| Cookie | Attributes | Purpose |
| --- | --- | --- |
| `__Host-sf-id` | `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, no `Domain` | Opaque session bearer |
| `__Host-sf-csrf` | `Secure`, `SameSite=Strict`, `Path=/`, no `Domain` | Same-session synchronizer value readable by the UI |

Neither cookie has `Expires` or `Max-Age`, so a browser normally discards it on
close. The server remains authoritative and enforces a 30-minute inactivity
timeout and a 12-hour absolute timeout. `last_seen_at` is persisted at most once
per minute to avoid a write for every read.

The CSRF cookie is not authentication. For an unsafe authenticated request, the
UI must copy it into `X-CSRF-Token`. The service requires the cookie and header
to match the SHA-256 digest stored on that exact session, using constant-time
comparison. A token from another live session fails. SameSite and Fetch
Metadata are defense in depth, not substitutes for this check.

A login that presents an existing Stackfort session cookie revokes it with
`login_rotation` before creating the new session. Logout requires the bound
CSRF proof, revokes the server row, expires both cookies, and returns
`Clear-Site-Data` for cache, cookies, and storage. Replayed rotated, logged-out,
idle-expired, or absolute-expired tokens return the same authentication-required
response.

## Audit and remaining boundaries

Successful login, login rotation, explicit logout/revocation, and server-side
expiry append hash-chained audit events without bearer, CSRF, password, email,
or request-body material. Failed guesses are retained only as bounded pressure
state so an attacker cannot grow the immutable audit chain at the full request
rate.

C-003 applies session resolution, platform/account policy, tenant scope, and
recent authentication to protected resources; see
[Authorization policy](authorization-policy.md). C-004 adds session
review/revoke-all, TOTP, recovery codes, and factor-driven global revocation.
The Vue login/session screens will consume these APIs in the later UI slice.

References:

- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
