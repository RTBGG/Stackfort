# ADR 0031: Explicit self-service account context

Status: Accepted

## Context

An authenticated session identifies a person but previously did not tell the
browser whether that identity administers the platform or which hosting-account
memberships it may present. Requiring the browser to know account UUIDs would
make navigation brittle, while returning every account to administrators in an
owner-facing response would blur platform and customer contexts. H-004 also
needs package usage, own-profile updates, and session control without adding a
generic identity/account query surface.

## Decision

1. Add one authenticated `GET /api/v1/me` read model that reports platform
   capability separately from active explicit account memberships.
2. Limit the membership list to 100 deterministically ordered entries and
   include only the current immutable package snapshot and measured domain
   count needed by the account UI.
3. Do not let platform-administrator status implicitly expand the owner-facing
   account list; switching to the account workspace requires membership.
4. Select the initial browser workspace from server-derived context: platform
   administrators enter administration and other identities enter account
   self-service.
5. Keep every account resource mutation on the existing account-scoped,
   authorization-coupled domain and TLS APIs.
6. Add a strict own-profile mutation whose target is the authorization subject,
   with CSRF and recent-authentication enforcement and transactional session
   revalidation.
7. Reuse identity-scoped session endpoints and never expose bearer/CSRF
   material in the session list.
8. Display only usage that is backed by current measurements; present other
   package values as limits until accounting telemetry exists.

## Consequences

- The browser has a deterministic role-aware entry point without accepting
  account identifiers from local state or user input.
- Account-owner navigation is explicit even for identities that also hold a
  broad platform role.
- Package revisions and usage can be displayed without exposing host identity
  internals or creating an unbounded inventory endpoint.
- UI visibility improves usability but does not replace server-side policy.
- Email/display-name/locale self-service is available; password-credential
  rotation remains a separate security-sensitive lifecycle rather than being
  folded into a generic profile patch.
- Storage and bandwidth consumption remain intentionally absent until they can
  be measured accurately.
