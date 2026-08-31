# ADR 0049: Closed systemd scheduled account jobs

- Status: accepted and implemented by K-009
- Date: 2026-08-30

## Context

A hosting panel needs cron-like execution, but a conventional command field is
also a durable remote-command interface to a privileged control plane. Raw
crontabs complicate ownership, update rollback, resource accounting, missed-run
behavior, and safe removal. Container-per-account scheduling would add a second
runtime and storage boundary even for native static/PHP accounts.

## Decision

Use one root-owned systemd service/timer pair per job. Persist and transmit only
a closed account-relative Shell/PHP script definition and one fixed UTC
schedule. Derive executables, unit names, calendar text, account identity,
working directory, and resource slice inside trusted code.

Run every service as the immutable account UID/GID below its existing cgroup-v2
slice. Validate scripts descriptor-relatively and refuse symlinks, hard links,
foreign ownership, cross-device traversal, oversized files, and unsafe unit
collisions. Reconcile units transactionally with durable control-plane
operations and optimistic revisions. Discard job output in this slice.

## Consequences

- The feature reuses systemd's process supervision, resource hierarchy,
  non-overlap, persistent timers, and sandboxing on all supported hosts.
- A compromised browser session cannot choose a binary, arbitrary command,
  environment, systemd directive, or calendar expression.
- Times are UTC and the initial cadence is intentionally limited; custom cron,
  sub-five-minute execution, output/history, notifications, and OCI commands
  require separate reviewed designs.
- Account scripts remain mutable account content. Post-reconciliation edits can
  change what that account executes, but cannot turn the service into a root
  process or escape the account identity/resource boundary.
