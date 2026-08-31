# ADR 0011: Server-side password sessions and bound CSRF proofs

Status: Accepted

## Context

The panel requires browser authentication without exposing password or bearer
material to JavaScript storage, URLs, logs, or SQLite backups. Password hashing
is deliberately expensive and therefore needs persistent pressure controls.
Cookie authentication also makes unsafe requests vulnerable to CSRF, while
session theft, fixation, stale credentials, and API restarts must not restore
invalid authority.

## Decision

1. Verify existing Argon2id credentials with their stored parameters, strict
   runtime allocation bounds, and a dummy Argon2id path for missing/inactive
   identities. Return one generic authentication failure.
2. Persist global, direct-source, and SHA-256 identity-key limits. Consume
   global/source capacity and check identity blocks before password derivation.
3. Generate independent 256-bit session and CSRF values and store only SHA-256
   digests. Revalidate the credential before atomically inserting the session.
4. Exchange the session only in a host-only `Secure`, `HttpOnly`,
   `SameSite=Strict` cookie. Use a second host-only readable cookie for the
   server-stored synchronizer CSRF value; unsafe calls must echo it in a custom
   header and match the same session digest.
5. Reject cross-site unsafe Fetch Metadata and simple-content login requests as
   defense in depth. Do not enable credentialed CORS.
6. Use non-persistent browser cookies, a 30-minute server-side idle limit, and a
   12-hour server-side absolute limit. Persist activity no more than once per
   minute.
7. Revoke a presented prior session during login, recheck credential state
   after hashing, and invalidate sessions on logout/expiry with transactional
   audit events.
8. Keep authentication separate from authorization. C-003 scopes every
   protected lookup and enforces role/recent-authentication policy.

## Consequences

- Database or backup disclosure does not directly yield reusable session or
  CSRF values, while a stolen live cookie remains equivalent to authentication
  until revocation or timeout.
- Missing-account and wrong-password paths have comparable expensive work, at
  the cost of controlled CPU/memory use; global/source limits and a two-slot
  derivation semaphore bound that cost.
- The readable CSRF cookie is intentionally not `HttpOnly`; it grants no
  authority without the separate `HttpOnly` bearer and is checked against the
  session-side digest.
- Strict cookies can make local plain-HTTP browser development inconvenient,
  but production security is not weakened by a development default.
- Clients behind the initial local reverse proxy share the direct-peer bucket
  until a trusted client-address mechanism is explicitly configured.
- Login rotation invalidates a browser's presented session. Automatic periodic
  renewal and factor/recovery session management remain later work.
