# ADR 0030: Authenticated bounded administrator console

Status: Accepted

## Context

The Phase 1 host and domain primitives already have strict API boundaries, but
the browser previously mounted an unauthenticated shell with no package,
account, operation, or audit inventory. Building the administrator workflow must
not move authorization into JavaScript, expose privileged host identity data, or
weaken secure-cookie and CSRF controls for development convenience.

## Decision

1. Resolve bootstrap, session restoration, password login, and MFA as explicit
   browser states before mounting protected navigation.
2. Keep authorization server-side and select one fixed policy action for every
   administrator endpoint; sensitive mutations also require recent
   authentication and CSRF validation.
3. Add deterministic bounded list queries for current packages, hosting-account
   package summaries, recent operations, and recent audit events.
4. Exclude Unix identity internals, audit chain hashes, operation payloads, and
   operation results from administrator list responses.
5. Reuse the existing account-scoped, idempotent domain lifecycle API rather
   than creating a UI-specific mutation surface.
6. Preserve the production `Secure`/`HttpOnly` session cookie contract; plain
   HTTP development does not receive a weaker authentication mode.
7. Keep update status explicitly read-only until signed release and rollback
   behavior exists.

## Consequences

- UI visibility is not treated as an authorization decision.
- List responses remain predictable and cannot become unbounded database dumps.
- Later pagination can extend the same limits without changing the security
  boundary.
- Full local browser authentication needs an HTTPS development endpoint, while
  component and contract tests remain deterministic without production secrets.
- H-004 can reuse the entry state machine and account-scoped APIs while exposing
  a different navigation model after role-aware session discovery is added.
