# ADR 0026: Transactional certificate lifecycle

Status: accepted

## Context

Certificate issuance crosses three independent state machines: the ACME
authority, Stackfort's SQLite control plane, and root-owned NGINX host state. A
crash can occur after any remote response or local write. Replacing NGINX's
currently valid key and chain before the candidate is verified would turn an
ordinary renewal failure into an outage. Accepting certificate paths or raw
NGINX configuration from the browser would also cross the privilege boundary.

## Decision

1. Model each issue or renewal as a retry-safe durable operation with at most
   four attempts. Persist an immutable certificate ID, exact sorted SAN set,
   envelope-encrypted P-256 key, purpose, and optional predecessor before
   creating an ACME order.
2. Persist the authority's order URL once. A replay retrieves that order,
   resumes pending authorizations, fetches an already-issued certificate, or
   finalizes a ready order with a CSR from the retained key.
3. Validate the returned leaf and chain before staging: the public key must
   match, the SAN set must be exact, every requested hostname must verify, the
   validity interval must be usable, server authentication must be allowed,
   and adjacent chain signatures must match.
4. Keep the new record in `staged` state. Send one bounded typed bundle to the
   agent. The bundle contains no path; its UUIDv7 certificate ID resolves only
   below `/etc/nginx/stackfort/certificates`. The agent revalidates names, PEM,
   P-256 key matching, ownership, modes, and symlink-free fixed paths.
5. Build a complete immutable account NGINX revision that references the
   staged ID. Port 80 retains the HTTP-01 location and otherwise redirects to
   HTTPS. NGINX syntax validation, atomic revision promotion, reload, health
   checking, and rollback reuse the F-003 transaction.
6. Mark the candidate `active` and its predecessor `retired` in one SQLite
   transaction only after the applied NGINX revision is recorded active. A
   failed issue, renewal, staging, or activation records a stable error but
   leaves the predecessor and its expiry metadata intact.
7. Automatically queue initial production issuance for eligible active
   HTTP-01 domains. Schedule the fallback renewal time once between 60% and 65%
   of certificate lifetime. After one four-attempt renewal round is exhausted,
   persist a later retry time rather than creating an unbounded hot loop.
8. Retire and detach an active certificate when its domain is removed or its
   exact TLS name/challenge intent changes. Retained history and root-only
   artifacts support audit and rollback; garbage collection is a separate
   future policy.

## Consequences

- The ACME order, certificate key, host artifact, NGINX revision, and database
  activation all have explicit replay boundaries.
- Users can observe status, issuer, fingerprint, validity, renewal time, and
  stable failure codes without receiving private keys or authority credentials.
- The current renewal fallback deliberately runs before one-third lifetime
  remains and spreads work across a five-percent window. ACME Renewal
  Information (ARI) remains a planned enhancement; when implemented, its
  authority-selected window takes precedence over this persisted fallback.
- Retired encrypted keys and root artifacts consume bounded storage until a
  separate retention/garbage-collection design is accepted.

## Rejected alternatives

- Running Certbot would add a second unmanaged account, renewal, filesystem,
  and reload state machine.
- Writing the candidate over the active key and chain would make validation or
  reload failure destructive.
- Storing a private key in an operation payload would expose it to progress,
  retry, and generic operation tooling.
- A fixed “renew every day” timer would synchronize hosts and ignore actual
  certificate lifetime.
