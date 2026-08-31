# TOTP, recovery codes, and session management

C-004 adds optional multi-factor authentication and identity-scoped session
control without changing the deny-by-default authorization boundary from C-003.

## Authentication profile

Stackfort generates a unique 160-bit TOTP secret and uses the widely supported
HMAC-SHA-1, six-digit, 30-second application profile. Verification accepts the
current time step and one adjacent step in either direction. The active factor
stores its highest accepted counter, and an equal or older counter is rejected,
so a valid code cannot be replayed. SHA-1 is used only inside HMAC for RFC
4226/6238 authenticator interoperability, not for signatures or collision
resistance.

Setup produces an encrypted, ten-minute challenge bound to the current identity
and session. The factor becomes active only after Stackfort verifies a code from
the new secret. A setup challenge permits five attempts. Replacing an existing
factor additionally requires a valid current TOTP or recovery code before the
new setup challenge is issued.

Activating a factor creates ten recovery codes. Each contains 128 random bits,
is returned exactly once, and is stored only as a SHA-256 digest. A successful
recovery login atomically marks that code used. Recovery and TOTP failures share
a persistent identity limit: five failures in ten minutes cause a 15-minute
block. Codes, provisioning secrets, and challenge values are excluded from
application and audit logs.

## Secret storage

TOTP setup and active-factor secrets use AES-256-GCM envelope encryption. Each
record receives an independent 256-bit data-encryption key. A separate
AES-256-GCM operation wraps that key with the host master key; authenticated
associated data binds the ciphertext to its purpose, record UUID, identity UUID,
and key version.

The 256-bit host master key lives outside SQLite. The API creates it beside the
database as `master.key`, or uses the absolute path in
`STACKFORT_MASTER_KEY_PATH`. The file must be a private, regular, non-symlink
file and is created with mode `0600`. A database backup without this key cannot
recover TOTP secrets; a copied key without the database is also insufficient.
Both therefore belong in the protected disaster-recovery set, stored separately
where practical.

## Two-phase login

When no factor is active, `POST /api/v1/login` creates the password session as
before. With an active factor, the same endpoint verifies the password but does
not create or rotate a session. It creates a five-minute challenge, persists
only its SHA-256 digest, and returns `202` with non-secret expiry metadata. The
browser receives the challenge in `__Host-sf-mfa`, with `Secure`, `HttpOnly`,
`SameSite=Strict`, `Path=/`, and no `Domain`.

`POST /api/v1/login/mfa` accepts a TOTP or recovery code. Successful verification
atomically consumes the challenge and factor proof, rotates a previously
presented session, and creates new independent session/CSRF values. The session
records whether authentication used `totp` or `recovery` and its MFA time.
Challenge, session, and CSRF values never appear in JSON.

## Authenticated HTTP surface

| Method and path | Purpose |
| --- | --- |
| `GET /api/v1/mfa/totp` | Read non-secret factor status and unused recovery-code count. |
| `POST /api/v1/mfa/totp/setup` | Begin an encrypted, session-bound setup challenge. |
| `POST /api/v1/mfa/totp/setup/{challengeID}/confirm` | Prove and activate the new authenticator; return recovery codes once. |
| `DELETE /api/v1/mfa/totp` | Verify the current factor and remove TOTP. |
| `GET /api/v1/sessions` | List up to 100 active sessions for the current identity. |
| `DELETE /api/v1/sessions/{sessionID}` | Revoke one session owned by the identity. |
| `POST /api/v1/sessions/revoke-all` | Revoke all sessions, optionally retaining the current one. |

All unsafe endpoints require the same-session CSRF cookie/header proof. Factor
and session mutations also require authentication within the previous five
minutes. A foreign session UUID is denied without confirming whether it exists.
Factor activation, replacement, and removal revoke every session for the
identity. Replacing the password credential uses the same global revocation
rule. The browser clears its local authentication cookies after factor changes
and must log in again under the new factor state.

## Verification coverage

Automated tests cover the RFC time-based vector, encrypted-at-rest setup and
factor secrets, associated-data and ciphertext tampering, same-key restart and
wrong-key rejection, proof-before-activation, TOTP replay, challenge replay,
single-use recovery, absence of a session after password-only MFA login,
identity-scoped single/all-session revocation, cookie attributes, CSRF, and an
HTTP enrollment/recovery-login/revocation flow.

References:

- [RFC 6238: TOTP](https://www.rfc-editor.org/rfc/rfc6238.html)
- [NIST SP 800-63B-4 authenticator requirements](https://pages.nist.gov/800-63-4/sp800-63b/authenticators/)
- [OWASP Multifactor Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Multifactor_Authentication_Cheat_Sheet.html)
