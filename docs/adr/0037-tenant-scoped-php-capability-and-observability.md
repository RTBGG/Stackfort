# ADR 0037: Derive PHP controls from host and immutable package policy

- Status: Accepted
- Date: 2026-08-25

## Context

J-001 can safely reconcile a distribution-approved account PHP-FPM pool, but a
browser needs a selectable version and health view. Returning raw systemd or
process data would disclose privileged host structure, while trusting a
browser-supplied version would bypass host and package policy.

## Decision

1. Derive the host-approved list only from the supported distribution, usable
   systemd capability, and exact installed native FPM package.
2. Store administrator selections in immutable package limits and offer an
   account only the intersection of that snapshot and current host approval.
3. Add an account-authorized status service that completes authorization before
   contacting the agent or loading tenant host state.
4. Add the read-only `php.fpm-pools.inspect` operation. It derives unit and
   cgroup identity inside the agent and returns only state plus optional
   aggregate memory, CPU-time, and task counters.
5. Keep pool sizing on the fixed reviewed J-001 preset for this slice; do not
   expose arbitrary FPM directives or limits in the browser.
6. Fail the status request closed when live inspection is malformed or
   unavailable, while representing unsupported host capability as typed data.

## Consequences

- Package and account forms cannot offer an untrusted or unavailable version.
- Tenant status is useful without exposing units, sockets, PIDs, process
  arguments, cgroup paths, or another account.
- Removing package permission immediately removes the version from future UI
  choices, while an immutable assignment remains historically stable.
- Live health depends on the local agent; the UI reports a temporary
  unavailability instead of inventing telemetry.
- Additional side-by-side runtimes require an explicit future expansion of the
  approved host matrix and installer, not merely a new browser option.
