# Account systemd slices and cgroup-v2 limits

E-003 maps the resource portion of an account's immutable package-assignment
snapshot to a systemd slice and verifies the cgroup-v2 state that the kernel
actually exposes. Migration `010_hosting_account_resources.sql` stores desired
and applied values, a monotonically increasing revision, capability status,
the correlated operation, and the last confirmed application time.
[ADR 0020](adr/0020-systemd-account-slices-and-host-reserve.md) records the
hierarchy and reserve decision.

## Hierarchy and host reserve

Systemd's dash hierarchy produces these cgroups:

```text
stackfort.slice
├── stackfort-core.slice
└── stackfort-accounts.slice
    └── stackfort-accounts-<UID>.slice
        ├── stackfort-php-<UID>-*.service
        ├── stackfort-job-<UID>-*.service
        └── user@<UID>.service
            └── app.slice/... (rootless Quadlets)
```

`stackfort-accounts.slice` receives `CPUQuota=<online CPUs × 80>%` and
`MemoryMax=80%`, with a 75% soft memory threshold. Customer workloads therefore
cannot consume the final 20% of CPU or physical memory through this hierarchy.
`stackfort-core.slice` receives maximum CPU/I/O weights and `MemoryLow=20%`.
Stackfort API, agent, edge, cache, database, and other platform service units
must set `Slice=stackfort-core.slice`; their service-specific cache and memory
budgets remain separate configuration.

The aggregate ceiling and each package limit are hierarchical. For example, a
250% account CPU quota permits two and a half cores while capacity exists, but
all accounts together still remain below the parent ceiling.

## Package mapping

| Package value | Persistent/live systemd property | cgroup-v2 verification |
| --- | --- | --- |
| CPU quota `N` | `CPUQuota=N%`, fixed 100 ms period | `cpu.max` quota and period |
| CPU quota absent | `CPUQuota=` (explicit reset; `infinity` is invalid for this systemctl property) | `cpu.max` begins with `max` |
| CPU weight `N` | `CPUWeight=N` | `cpu.weight=N` |
| CPU weight absent | systemd default `100` | `cpu.weight=100` |
| Memory bytes `N` | `MemoryMax=N` | `memory.max=N` (kernel page rounding is accepted only downward by less than one page) |
| Memory absent | `MemoryMax=infinity` | `memory.max=max` |
| Swap bytes `N`, including `0` | `MemorySwapMax=N` | `memory.swap.max=N` |
| Swap absent | `MemorySwapMax=infinity` | `memory.swap.max=max` |
| Process limit `N` | `TasksMax=N` | `pids.max=N` |
| Process limit absent | `TasksMax=infinity` | `pids.max=max` |

`TasksMax` counts kernel tasks/threads rather than only process leaders. The
kernel documents `memory.max` as the hard memory boundary, `memory.swap.max` as
the independent swap boundary, and limits as hierarchical. See the
[Linux cgroup-v2 documentation](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html).

## Reconciliation boundary

The API sends only `hosting.resources.reconcile` with a complete validated
resource spec and audit correlation. The caller cannot select a unit name,
property, executable, raw argument, or path. The agent:

1. rechecks systemd, unified cgroup v2, and CPU/memory/PIDs controllers;
2. renders the two platform slices, the UID-derived account slice, and one
   exact `user@<UID>.service.d/50-stackfort-account-boundary.conf` drop-in;
3. refuses an existing symlink, insecure/oversized file, or file without the
   Stackfort ownership marker;
4. replaces changed units atomically, reloads systemd, starts the account
   slice, and applies all live properties in one fixed `set-property` call; and
5. inspects the account user manager, restarts it through a fixed derived
   profile only when it is active outside the boundary, and verifies its exact
   cgroup; and
6. reads `cpu.max`, `cpu.weight`, `memory.max`, `memory.swap.max`, and
   `pids.max`, returning success only when every value matches.

The provisioning sequence is identity, project-backed filesystem, resources,
then rootless runtime. Consequently the first user-manager start is already
below the account slice. An unconditional bounded daemon reload closes the
write-before-reload crash gap, while inactive managers are not started merely
to apply resource intent.

Systemd documents that `set-property` applies supported resource settings
immediately and can set multiple properties together; the runtime overlay is
paired with Stackfort's persistent unit so the same state returns after reboot.
See the upstream [`systemctl` source documentation](https://github.com/systemd/systemd/blob/main/man/systemctl.xml).

## Verification

Unit tests cover optional-value semantics, fixed command templates, unit
rendering, unlimited CPU reset semantics, capability failure, protocol validation, revision fencing, and
applied/blocked persistence. The disposable root integration test runs on
Debian 13, Ubuntu 26.04, and Rocky Linux 10. It verifies a live limit change,
observes a rejected fork in `pids.events`, and observes an over-limit memory
probe killed inside the account cgroup through `memory.events`.
The Phase 5 matrix additionally verifies that the rootless user manager and
container PID are descendants of the same account path, triggers the parent
`pids.max` from inside a live OCI container, and confirms placement after a
real reboot. See the
[Phase 5 result](../infra/host-tests/results/2026-09-01-oci-phase5-exit-matrix-hyper-v.md)
and systemd's upstream
[`Slice=` resource-control contract](https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html).
