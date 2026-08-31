# ADR 0025: Encrypted ACME accounts and fixed HTTP-01 routing

Status: accepted

## Context

An ACME account private key is a reusable platform credential. Losing it can
break renewal continuity, while exposing it to a hosting account lets that
tenant act as Stackfort toward the certificate authority. HTTP-01 also has to
remain reachable on port 80 even when the managed domain normally performs a
canonical-host or customer redirect. Allowing users to supply an ACME directory,
challenge path, NGINX fragment, or host filesystem path would turn both flows
into network or configuration injection surfaces.

## Decision

1. Support only the compiled-in Let’s Encrypt staging and production ACME v2
   environments. The browser selects an enum; it cannot submit a directory URL.
2. Create one P-256 account key per environment. Store its PKCS#8 bytes with an
   independent AES-256-GCM data key wrapped by the external Stackfort host
   master key. Bind both layers to the account record and secret purpose through
   authenticated associated data.
3. Register through a replay-safe global operation. Persist the encrypted key
   before contacting the authority. A retry reuses that key and recovers an
   already-created authority account rather than generating another identity.
4. Require platform-administrator authorization, recent authentication, CSRF,
   an idempotency key, and explicit terms acceptance. Account users have no
   route to ACME account metadata; even administrator responses omit key
   material, its envelope, thumbprint, and authority-internal account URLs.
5. Render a fixed `/.well-known/acme-challenge/` location for every enabled
   HTTP-01 domain. Put customer and canonical redirects in the lower-priority
   `/` location. The challenge response is marked `no-store`, permits only GET
   and HEAD, disables auto-indexing and symlink traversal, and never passes
   through Vinyl.
6. Add one typed agent mutation for present/cleanup. It accepts only an RFC
   8555-shaped base64url token and matching key authorization. The Linux agent
   writes an exact root-owned mode-`0644` file atomically below
   `/var/lib/stackfort-agent/acme-http01`; no caller-selected path exists.
7. On Rocky Linux, persist a narrow `httpd_sys_content_t` mapping for that
   fixed subtree and apply it with `restorecon`. SELinux stays enforcing.

## Consequences

- ACME account registration can survive an API crash after the authority has
  accepted the key but before the result is committed locally.
- HTTP-01 tokens are public by protocol design, but hosting accounts cannot
  create, replace, or remove them on the host.
- A common root-owned token directory is safe because RFC 8555 tokens have at
  least 128 bits of entropy and the CA also validates the requested DNS name.
- G-001 establishes accounts and challenge routing only. Orders,
  authorizations, certificate keys, activation, renewal, and retirement belong
  to G-002.

## Rejected alternatives

- `certbot --webroot` or another unrestricted subprocess would create a second
  state machine and broaden command/path authority.
- Account-owned challenge directories would let tenants answer challenges
  under Stackfort’s ACME identity.
- A server-level redirect executes too early to guarantee that the HTTP-01
  location wins.
- Accepting arbitrary RFC-compatible directory URLs would permit privileged
  server-side requests to untrusted endpoints.
