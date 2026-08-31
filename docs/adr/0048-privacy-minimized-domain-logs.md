# ADR 0048: Privacy-minimized domain logs and bounded account views

- Status: accepted and implemented by K-008
- Date: 2026-08-29

## Context

Access and error logs are useful for diagnosing routing and application
failures, but common combined formats persist query strings, referrers, user
agents, cookies, or complete request lines. Native error messages can still
contain request-derived text. Letting a privileged API accept paths or stream
unbounded compressed history would also widen the root-agent attack surface.

## Decision

1. Capture only the minimum structured access fields needed for domain health
   and traffic diagnosis. Exclude query strings and request headers in NGINX,
   before data reaches disk.
2. Derive one opaque SHA-256 filename per normalized domain under a fixed
   root-only account directory. Root creates files before transactional NGINX
   activation; caller-controlled paths and raw configuration remain absent.
3. Apply a second parser/redactor in the root agent. Only typed capped records
   may cross RPC; malformed access records are withheld rather than echoed.
4. Read only active and delay-compressed `.1` files, newest first, through
   no-follow descriptors. Limit pages to 50 records and 256 KiB of scanning and
   bind continuation to canonical inode/offset state.
5. Retain seven numbered daily rotations with a per-active-file 8 MiB trigger,
   delayed gzip, and NGINX descriptor reopen. Never use `copytruncate`.
6. Authorize a domain by its persisted account-scoped ID in the control plane,
   then send only the server-derived identity and normalized name to the agent.
   Permit auditors because the result is a non-mutating diagnostic view.

## Consequences

Useful request status, volume, latency, and bounded error context are available
without deliberately collecting common credential-bearing request data. Some
client IP/path data remains personal or potentially sensitive and therefore
has short local retention and explicit UI disclosure. The reader deliberately
does not expose older compressed rotations or arbitrary searches; broader
incident tooling requires a separate administrator-only design. WAF events,
transfer aggregation, anonymization, export, and OCI application logs remain
separate future features.
