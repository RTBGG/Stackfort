# Beta.13 feedback fixes and panel hostname UI (development qualification)

This records **development-worktree qualification**, not a released
artifact, public CA validation, upgrade qualification or independent audit.
The user's VPS and its ACME account were not accessed or modified. Beta.13 and
the public one-line release selector remain unchanged.

## Findings

- Reproduced the ACME button failure with a mounted App/browser-API test: the
  child emitted `registerACMEAccount`, but Vue's kebab-case listener resolves to
  `registerAcmeAccount`. The regression failed before the event-name fix and
  passed afterwards. Normalized the corresponding WAF event names as well.
- Added bounded registration observation and explicit retry for incomplete
  registration; a missing/invalid ACME account now produces the actionable
  `tls.acme_account_required` certificate failure rather than a generic state error.
- Replaced duplicated database wizard numbers with named, localized steps.
- The access-log 404 for `/.__stackfort_activation_probe__` is an internal
  NGINX revision/header probe on an absent resource, not an HTTP-01 challenge
  failure. It does not identify the user's public DNS/CA failure by itself.
- Service presence is distinct from runtime state. Global PHP-FPM and rootful
  Podman inactivity is explained; Debian/Ubuntu now inspect
  `stackfort-firewall.service`, not the deliberately unused vendor nftables unit.
- Found the previous panel CLI also refused **completed** native installations,
  because the generic installer store intentionally rejects every native journal.
  Added a panel-only installed-state guard; the generic installer remains blocked.

## Panel feature boundaries

Admin GET status and CSRF-protected POST issuance use a closed, typed agent
contract. Platform-manage authorization requires a recent login. Both terms and
origin-change confirmations are required. The durable operation permits one
attempt; root file paths, shell commands, foreign CA URLs and tenant-scoped
correlations are rejected. The existing fixed production ACME transport,
one-hour cooldown, NGINX transaction/rollback, renewal timer and port-8443
fallback are reused. No private key or raw root/CA error crosses the API.

## Checks on 2026-09-26

- Go unit suite and frontend build/typecheck; frontend flow, component,
  localization and accessibility tests.
- Cross-compiled Linux installer/API/agent and root-host NGINX tests.
- On the already-installed disposable Debian 13 VM:
  `TestPanelManagementCompletedNativeHost` passed, including retained shared
  locking and rejection by the ordinary installer loader.
- Eight root-fixture panel tests passed: configuration/rotation/reservation,
  validation/reload/health rollback, interrupted recovery, unsafe file rejection,
  tenant conflicts, issuance/renewal/no-op, ACME failure cleanup and transport
  restrictions.
- `TestPanelDisposableLiveNGINX` passed with a **private fixture CA**: real HTTP-01
  delivery, trusted HTTPS UI/API, wrong-Host rejection, renewal no-op and cleanup.
  No live Let's Encrypt registration was made.
- Read-only status passed with the renewal unit's PrivateDevices/ProtectSystem/
  ProtectHome and explicit writable-directory restrictions. An initial diagnostic
  omitted those writable paths and correctly failed read-only; the actual
  configured paths passed.
- After tests the custom endpoint was disabled again; NGINX, API, agent and
  managed firewall remained active. Installed release binaries were not replaced.
- Browser visual inspection of the production frontend build with synthetic,
  loopback-only API data confirmed the German settings, wizard and service view.
  No console warnings/errors were observed.
- Candidate preparation reran all Go tests and `go vet ./...`, 73 frontend tests,
  production build/type checks, localization and documentation checks. The
  localization gate caught a literal hostname placeholder; it now uses the
  EN/DE catalog. `npm audit --audit-level=high` reported zero vulnerabilities.
- A separate development agent, using the installed agent's sandbox properties
  and a separate Unix socket, passed `TestPanelCompletedNativeAgentSocket` as the
  real unprivileged `stackfort` service identity. It inspected the completed
  native host without changing its panel, installed binaries or active agent.
  Initial fixture locations correctly failed because PrivateTmp hid `/var/tmp`
  and `/run` was mounted noexec; the root-owned `/opt` fixture passed without
  relaxing either protection. The temporary service was stopped afterwards.

Public issuance for the user's actual DNS name still requires correct A/AAAA
records and reachable ports 80/443. A new release must be built/qualified before
these changes are available through the public installer.
