# ADR 0008: Domain identity and retained routing history

Status: Accepted

## Context

Stackfort must accept internationalized domains, route with unambiguous host
keys, reserve `www` consistently, prevent tenant conflicts, support shared
document roots, and reconcile desired configuration without losing evidence of
what previously existed. Domain IDs alone do not prevent two accounts from
claiming the same public host. Mutable target rows would also erase the state an
operation or audit event referred to.

## Decision

1. Store an immutable normalized Unicode display name and lowercase IDNA ASCII
   routing name for every domain.
2. Configure a pinned, non-transitional UTS #46 IDNA profile explicitly and
   require a stable Unicode-to-ASCII round trip.
3. Model the domain as a base host without a leading `www`; one live base row
   globally reserves both base and `www`, while a canonical-mode field controls
   serving or redirect behavior.
4. Store domain targets and redirect rules as immutable revisions. Replacing a
   target supersedes the old row rather than updating it.
5. Store document roots as immutable account-owned relative paths. Explicit
   sharing reuses the root record; removing a domain never deletes that root.
6. Retain TLS intent and active-certificate metadata separately from target
   history. A changed intent does not erase a working certificate reference.
7. Link applied configuration to account-scoped desired revisions and
   operations using a SHA-256 digest, and logically remove domains instead of
   deleting their history.

## Consequences

- Host collision checks are straightforward and case-insensitive after IDNA
  conversion.
- A domain entered with or without `www` addresses one resource; independently
  hosting both with unrelated content is intentionally unsupported.
- Unicode homographs remain a human risk, so interfaces should show Unicode and
  ASCII forms together during sensitive review.
- Restrictive path syntax trades some filesystem naming flexibility for
  portable rendering and a smaller injection surface.
- Historical targets, roots, TLS state, and applied revisions consume modest
  additional SQLite space but support audit, rollback, and diagnosis.
- DNS, public-suffix policy, filesystem safety, rendering, and activation remain
  separate checks; persisted intent is not proof that a route is live.
