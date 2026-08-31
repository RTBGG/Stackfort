# ADR 0006: Project identity and license

- Status: Accepted
- Date: 2026-08-14

## Context

Distributable identifiers must be stable before the Go and web workspaces,
packages, system services, container images, and installer are created. The
project also needs a license before accepting external contributions.

## Decision

The project identity is:

| Purpose | Identifier |
| --- | --- |
| Product name | Stackfort |
| GitHub repository | `RTBGG/stackfort` |
| Go module | `github.com/RTBGG/stackfort` |
| Frontend package | `@stackfort/web` |
| Container namespace | `ghcr.io/rtbgg/stackfort-*` |
| Control API executable | `stackfort-api` |
| Host agent executable | `stackfort-agent` |
| Administrative CLI | `stackfortctl` |
| systemd units | `stackfort-api.service`, `stackfort-agent.service` |
| Unprivileged service account | `stackfort` |
| Configuration directory | `/etc/stackfort` |
| State directory | `/var/lib/stackfort` |
| Runtime directory | `/run/stackfort` |

The API runs as the unprivileged `stackfort` system account. The narrowly scoped
host agent initially runs as root with systemd hardening and exposes only the
typed local interface defined by the security architecture. Hosting accounts
receive separate generated Linux identities and do not run as `stackfort`.

Stackfort is licensed under `AGPL-3.0-or-later`. Contributions are accepted
under the same license unless explicitly agreed otherwise before inclusion.

## Consequences

- Public identifiers no longer depend on a working title.
- Network users of modified hosted versions receive the AGPL source-availability
  protections.
- Distribution must include the license text and applicable third-party notices.
- Changing the project name or module path after the first release would be a
  compatibility event and requires a new ADR and migration plan.
