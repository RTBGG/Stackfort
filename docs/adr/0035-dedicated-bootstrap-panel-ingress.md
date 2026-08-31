# ADR 0035: Dedicated bootstrap panel ingress

Status: accepted

## Context

The release already installed the web assets and a loopback-only API, but no
virtual host connected them. Secure host-only cookies meant that exposing the
API as plain HTTP was not a valid browser flow. A fresh machine also has no
panel hostname, publicly trusted certificate, or registered ACME account, while
customer ports 80/443 must retain strict unknown-host rejection.

## Decision

1. Publish initial management on a distinct public TCP port, `8443`, through a
   fixed root-owned NGINX configuration.
2. Generate one local P-256 certificate/key bundle atomically during
   installation and retain it across idempotent runs while it remains valid.
3. Keep the customer-facing default on port `443` unchanged with
   `ssl_reject_handshake on`; the self-signed certificate is used only by the
   explicit management listener.
4. Serve the immutable SPA directly and proxy only `/api/` to the existing
   loopback API, preserving one origin for strict cookies and CSRF.
5. Include port `8443` in preflight, firewall reconciliation, mandatory-access-
   control policy, installed-system health checks, and the declared plan.
6. On enforcing Rocky hosts, install a local SELinux module that permits
   `httpd_t` to connect only to a dedicated `stackfort_api_port_t` assigned to
   TCP `8080`; do not enable broad HTTPD network-connect booleans.
7. Treat public panel hostname/certificate provisioning and trusted client
   address attribution as explicit follow-up work.

## Consequences

- A new installation is operable from a real browser without making the API a
  public listener or weakening its cookie attributes.
- Initial access produces a certificate warning; operators must verify the
  server address. This is encryption with local identity, not Web PKI identity.
- The management port is independent of customer SNI routing and can be
  restricted separately by an external firewall.
- Local accounts still cannot write panel assets, configuration, or key
  material.
- Rocky gains one small host-local policy module and persistent port label;
  qualification verifies both while global HTTPD connect/relay booleans remain
  off.

## Rejected alternatives

- Serving bootstrap/login over plain HTTP would discard the `Secure` cookie
  boundary.
- Reusing the port-443 catch-all would undo the rejecting unknown-host policy
  and present the local certificate to unrelated SNI names.
- Binding the Go API publicly would bypass the NGINX request and static-content
  boundary.
- Requiring a public panel domain before first login would make clean-host
  installation circular and complicate recovery when DNS is unavailable.
