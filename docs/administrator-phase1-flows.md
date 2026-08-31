# Administrator Phase 1 flows

H-003 turns the shared application shell into an authenticated administrator
console. It uses the existing bootstrap, password/MFA session, domain lifecycle,
and host-capability APIs and adds bounded administrator inventory endpoints for
packages, hosting accounts, operations, and audit events.

## Entry state machine

The browser resolves its initial state in this order:

1. `GET /api/v1/bootstrap` determines whether the one-time administrator form
   must be shown.
2. When bootstrap is complete, `GET /api/v1/session` restores an authenticated
   browser session or selects the login form after a stable 401 response.
3. `POST /api/v1/login` either returns a session or advances to the MFA form;
   `POST /api/v1/login/mfa` completes the same flow.
4. Only a returned/restored session mounts the administrator navigation and
   requests protected inventory.

Bootstrap validates password confirmation in the browser but the server remains
authoritative for capability expiry, single use, password policy, conflicts, and
rate limits. API error codes are stable inputs to local English/German messages;
server-provided English detail is not rendered as localized UI.

The session bearer is a `Secure`, `HttpOnly`, `SameSite=Strict`, host-only cookie.
JavaScript reads only the separate synchronizer CSRF cookie and repeats it in
`X-CSRF-Token` for mutations. Therefore a real local login flow must be served
through HTTPS; tests use controlled API responses and never weaken production
cookie attributes for plain HTTP development.

## Administrator API

The following endpoints require an authenticated platform administrator:

| Method | Endpoint | Authorization | Purpose |
| --- | --- | --- | --- |
| `GET` | `/api/v1/admin/packages` | `packages.view` | Current immutable package revisions and limits. |
| `POST` | `/api/v1/admin/packages` | `packages.manage` | Create a package. |
| `GET` | `/api/v1/admin/accounts` | `platform.view` | Hosting-account and assigned-package summaries. |
| `POST` | `/api/v1/admin/accounts` | `accounts.create` | Create an isolated account and queue durable host provisioning; omitted owner means the current administrator. |
| `GET` | `/api/v1/admin/operations?limit=N` | `platform.view` | Newest durable operation checkpoints. |
| `GET` | `/api/v1/admin/audit-events?limit=N` | `platform.view` | Newest append-only audit records without hash bytes. |
| `GET` | `/api/v1/admin/host/capabilities` | `platform.view` | Host services plus the exact approved native PHP version list. |

Mutations require a valid CSRF binding and recent authentication. List limits
default to 50 and reject values outside 1 through 200. Repository queries use
deterministic ordering; package responses contain the current revision, account
summaries contain the assigned immutable revision, and privileged Unix identity
details are not exposed. Account responses include only `hostReady` plus the
creation response's provisioning operation ID/status. Domain controls remain
disabled until the server confirms every account host boundary.

Domain creation, suspension, resumption, and removal continue to use the
account-scoped lifecycle API. The browser generates separate request and
idempotency identifiers and sends only the strict typed request body. Removal
retains the existing non-destructive file contract.

## Views

The administrator shell now provides:

- an inventory and operation dashboard;
- package creation with only host-approved PHP versions and isolated
  hosting-account creation;
- account-selected static/PHP-domain creation and lifecycle actions;
- service/operating-system inspection from the privileged agent;
- durable operation progress and audit history;
- session identity and sign-out;
- fixed-environment Let's Encrypt production-account registration with
  explicit terms acceptance and non-secret state; and
- an honest update-status placeholder showing only the installed build.

The updater view performs no release discovery or mutation. Signed channels,
staging, health verification, and rollback remain assigned to the updater
milestone.

## Verification

Core tests exercise deterministic current-revision inventories, account package
summaries, recent operations, descending audit events, and list bounds. HTTP
tests verify selected authorization actions, CSRF enforcement, current-admin
ownership, strict bodies, and early limit rejection. Frontend tests cover
bootstrap validation/completion, password-to-MFA progression, session entry,
API headers/bodies, localization, accessibility, focus, and responsive drawer
behavior.

The manual browser flow reviewed representative data in every administrator
view in English and German. Desktop and 320-pixel layouts remained usable; the
mobile drawer retained its inert/focus contract and the console had no warnings
or errors. That review exposed and fixed both a scrollbar-width overflow and a
route-only account identifier that would otherwise have violated the strict
domain JSON contract.

The installed flow now has a same-origin HTTPS entry point on port `8443`.
The real in-app browser reached the unmocked bootstrap state through the
installed API; submitting bootstrap credentials remains an explicit sensitive
browser action rather than part of read-only automated review. See
[Installed panel ingress](installed-panel-ingress.md).
