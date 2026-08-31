# ADR 0032: Read-only installer preflight and explicit plan

- Status: Accepted
- Date: 2026-08-24

## Context

A one-line installer is a high-trust boundary. Stackfort initially supports
only fresh hosts and must not discover incompatibility after it has already
changed packages, users, storage, service state, security modules, or firewall
rules. The existing agent capability detector already provides bounded typed
observations, but it did not define installer readiness, resource minimums, an
actionable failure contract, or the complete planned mutation surface.

## Decision

1. Ship `stackfort-installer` as a separate versioned release binary. I-001
   exposes only `preflight`, JSON/text output, and build provenance; it has no
   mutating command.
2. Reuse the fixed host-capability detector for OS, architecture, systemd,
   cgroup, quota filesystem, mandatory access control, ports, packages, and
   service state. Add bounded CPU, `/proc/meminfo`, and `statfs` observations.
3. Require the documented disposable-host minimums plus 5 GiB free on the
   already provisioned `/srv/hosting` quota filesystem. Stackfort will not
   repartition a disk or silently alter root mount flags.
4. Treat unknown inspection as a blocker. Active future data-plane services,
   occupied ingress ports, and existing Stackfort units are also blockers;
   missing packages and an existing distribution firewall are planned inputs.
5. Always return the complete distribution-specific plan. Each normalized
   failed check carries a stable ID/reason code and remediation. Exit `2`
   distinguishes a safely inspected but incompatible host from execution error
   `1`.
6. Keep plan output declarative. I-002 must implement idempotent, journaled
   stages, repeat capability checks at mutation boundaries, and verify signed
   immutable release artifacts before using them.

## Consequences

- Operators can inspect both the host decision and intended ownership surface
  before granting mutation authority.
- The preflight can be integrated into automation without parsing prose.
- Hosts without a prepared project-quota filesystem fail early with an exact
  remediation instead of receiving a risky storage rewrite.
- An upgrade/repair path must deliberately distinguish an existing Stackfort
  installation; I-001's fresh-install gate rejects it.
- Distribution package lists and planned paths are now compatibility-sensitive
  and require tests and documentation when changed.
