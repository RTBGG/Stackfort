# ADR 0064: Root-managed panel hostname and automatic ACME

- Status: accepted
- Date: 2026-09-06

## Decision

Retain the 8443 bootstrap origin and add an optional named 443 origin through
`stackfort-installer panel`. The panel is not a tenant domain; root input paths
and certificate keys are never accepted through an unprivileged browser API.

Reuse the existing RFC 8555 registrar/issuer with production Let's Encrypt
HTTP-01, a separate root-only account and explicit terms/contact consent.
Verify issued/imported certificate identity, key match, validity and system
trust. Store immutable content-addressed PEM bundles and atomically switch one
generated include after candidate validation. Reload, HTTPS health, durable
rollback and the shared tenant-activation lock protect the live configuration.
Reserve exact names, aliases and overlapping wildcards against tenant activation.

Install an active-version installer binary and a randomized twice-daily renewal
timer. Renew during the last third of the actual lifetime, capped at 30 days
before expiry; retain a durable one-hour attempt cooldown. Importing a manual
certificate or disabling the hostname opts out without deleting ACME identity.

## Limits

The initial workflow is console-only. DNS/firewall changes, DNS-01, wildcard
panel names, contact editing, certificate pruning and expiry notifications are
not implemented. Root-console actions do not enter the API identity audit chain;
the private journal/systemd logs are operational evidence. Interrupted issuance
can start a new order with the persisted account after cooldown. Keep existing
public-beta release gates, including independent security review.

Commands and failure behavior: [Panel hostname](../panel-hostname.md).
