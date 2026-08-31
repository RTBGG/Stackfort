# ADR 0004: Use SQLite for panel state and MariaDB for hosted applications

Status: Accepted

## Context

The panel must remain operable enough to diagnose and repair the hosted database
service. Making its own authentication and desired-state records depend on the
customer-facing MariaDB daemon creates an unnecessary circular dependency.

## Decision

Store control-plane state in a private SQLite database using WAL mode and
explicit migrations. Manage a separate MariaDB service for hosted applications.
Back up panel state consistently as part of system recovery procedures.

Use the CGo-free `modernc.org/sqlite` driver so release binaries remain
self-contained on supported architectures. Require a bundled SQLite version
with the WAL-reset race fix (3.51.3 or newer), use `synchronous=FULL`, and route
application writes through a serialized immediate-transaction boundary.

## Consequences

- Panel state remains available when MariaDB is degraded.
- Installation has fewer bootstrap dependencies.
- SQLite write concurrency must be deliberately bounded through short
  transactions and a persisted job model.
- WAL state must remain on a local filesystem; live database-file copies are
  not a supported backup method.
- Multi-controller clustering will require a later storage abstraction or a
  different control-plane database.

Operational settings, migration invariants, and backup behavior are documented
in [Panel state persistence](../persistence.md).
