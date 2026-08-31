# ADR 0010: Single-use administrator bootstrap capability

Status: Accepted

## Context

An unattended installer cannot safely ship a universal password, derive a
predictable password, or persist a reusable setup secret. The first public API
mutation is exposed before normal login, sessions, CSRF defenses, or an
administrator identity exist. Password hashing is intentionally expensive, so
capability guessing must also be rate-limited before Argon2id work begins.

## Decision

1. Generate 32 random bytes locally and encode them as a prefixed URL-safe
   bearer capability. Display the raw value only in the creation command.
2. Persist only its SHA-256 digest and lifecycle metadata. Permit one active
   capability and exactly one terminal transition: consumption, expiry, or
   explicit replacement.
3. Keep capability creation local to the state-owning operating-system identity;
   expose no remote endpoint that generates or retrieves one.
4. Apply persistent per-direct-source and global fixed-window limits before
   Argon2id derivation. Do not trust forwarded client-address headers until the
   reverse-proxy trust boundary is implemented explicitly. Permit at most two
   concurrent password derivations in one API process.
5. Return one generic denial for absent, expired, consumed, and incorrect
   capabilities, and compare the stored digest in constant time.
6. Revalidate after password derivation, then atomically create the identity,
   Argon2id credential, platform role, consumption record, and audit event.
7. Disable both capability creation and redemption once a platform
   administrator exists. Do not create a login session during bootstrap.

## Consequences

- There is no default or recoverable setup password and a database disclosure
  does not reveal the raw bootstrap capability.
- A leaked live capability grants one high-impact action until expiry, so the
  operator handoff must avoid URLs and logs and keep the default lifetime short.
- Persistent limiting survives restarts and protects the expensive hash, but a
  local NGINX proxy initially shares one direct-source bucket for remote clients.
- A failed or racing final transaction does not consume the capability unless
  all administrator records commit together.
- Normal authentication, sessions, CSRF protection, password verification,
  breached-password screening, and recovery remain C-002/C-004 concerns.
