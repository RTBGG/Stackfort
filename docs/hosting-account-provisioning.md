# Durable hosting-account provisioning

E-004 connects hosting-account persistence to the three privileged boundaries
implemented by E-001 through E-003. Creating an account now queues one durable
`hosting.account.reconcile` operation instead of leaving host setup to an
independent manual step.

## Immutable operation snapshot

The application service authorizes `accounts.create`, persists the account and
its immutable package assignment, then snapshots:

- the UUID-derived Linux username, UID/GID, and canonical home directory;
- the filesystem revision, project ID, and byte/inode limits; and
- the resource revision and CPU, memory, swap, and process limits.

The operation uses the account plus both revision numbers as its deterministic
idempotency key and is retry-safe with three attempts. A startup/periodic scan
queues the same key for active accounts whose identity, filesystem, or resource
state is still pending. This closes the process-crash gap between the account
transaction and operation creation without duplicating work.

## Reconciliation order and readiness

The Linux worker applies the immutable snapshot in this order:

1. reconcile the exact local user, group, and account root;
2. create the fixed directory layout and apply the project quota;
3. reconcile the deterministic systemd account slice and cgroup limits; and
4. conditionally confirm both captured revisions and the Linux identity.

Every agent call carries the durable operation correlation and a stage-specific
idempotency key. Repository confirmations reject superseded revisions. Replays
are convergent, including an already-confirmed identity.

An account is host-ready only while it is active and all three boundaries are
confirmed as reconciled/applied and available. Domain mutations check this
server-side before queuing. Administrator and account-owner APIs expose only
the boolean `hostReady`; they do not expose Unix identity or host paths. The
bilingual UI labels provisioning accounts and disables domain actions until a
refreshed response reports readiness.

## Verification

Unit and repository tests cover immutable payload construction, crash-gap
repair, negative-limit rejection, ordered replay-safe execution, readiness
queries, and the domain gate. The disposable Linux suite now creates the real
user, project-quota layout, and systemd slice through this durable handler
before running the complete static-domain lifecycle.

See [ADR 0034](adr/0034-durable-hosting-account-provisioning.md),
[Hosting-account Unix identity](hosting-account-unix-identity.md),
[Hosting filesystem layout and project quota](hosting-filesystem-layout-and-quota.md),
and [Account systemd slices and cgroup-v2 limits](account-resource-control.md).
