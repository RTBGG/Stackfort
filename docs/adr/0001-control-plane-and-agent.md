# ADR 0001: Separate unprivileged control API from privileged host agent

Status: Accepted

## Context

The product needs to create Linux identities, apply quotas, render service
configuration, manage databases, and control systemd services. A conventional
panel running its entire web application as root would turn any web compromise
into an immediate host compromise.

## Decision

Run the browser-facing control API as an unprivileged user. Introduce a small
local host agent for privileged mutations. Communicate over a protected Unix
socket using a versioned, typed, allowlisted protocol. Do not expose arbitrary
command execution. Make operations idempotent and auditable.

## Consequences

- More protocol and reconciliation work is required.
- Privileged code is substantially smaller and easier to review.
- UI/API vulnerabilities do not automatically grant unrestricted root command
  execution.
- Host operations can be tested against desired state and failure injection.
- A future remote-node transport can reuse the operation model without allowing
  the browser to reach agents directly.

