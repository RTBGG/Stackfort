# ADR 0012: Relationship- and attribute-based authorization

- Status: Accepted
- Date: 2026-08-16

## Context

Stackfort has platform roles, account memberships, account lifecycle state, and
server-side sessions. Authentication proves which identity controls a session,
but role-only checks in individual HTTP handlers would be easy to omit and
would not prevent cross-account object substitution. Sensitive administration
also needs a freshness rule that callers cannot accidentally disable.

## Decision

1. Use explicit server-selected action constants and deny every unknown action.
2. Combine the platform role with the current account membership, account
   status, identity status, session validity, and authentication age.
3. Keep authorization subjects opaque outside the core package, seal them with
   an ephemeral per-process HMAC key, and construct valid subjects only from an
   authenticated server-side session.
4. Resolve authorization state on every protected request rather than embedding
   grants in browser-controlled or long-lived bearer claims.
5. Give platform administrators cross-account authority; give account owners
   full account authority except package assignment and host-wide functions.
   Define least-privilege forward-compatible member and auditor policies.
6. Mark sensitive actions in the central policy and require authentication no
   more than five minutes old.
7. Couple protected object reads with account-scoped lookup. Return the same
   resource-not-found response for missing and unauthorized account objects.
8. Require future application services to authorize the exact account and
   action adjacent to execution. UUID opacity is never an access control.

## Consequences

- A newly introduced action is unusable until its policy is explicitly added.
- Membership or platform-role removal takes effect on the next request without
  waiting for a browser claim to expire.
- Suspended accounts are read-only to account principals; archived accounts are
  accessible only to platform administrators.
- Sensitive endpoints can share one freshness rule and one HTTP error contract.
- The first protected account endpoint demonstrates the pattern; every future
  domain, file, database, backup, and OCI service still needs its own coupled
  resource query and negative cross-tenant tests.
- C-004 adds MFA metadata, factor management, and identity-scoped session
  control without weakening or replacing the action policy. Phishing-resistant
  step-up factors can extend the same boundary.
