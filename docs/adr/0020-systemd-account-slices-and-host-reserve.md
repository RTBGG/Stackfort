# ADR 0020: Systemd account slices and a host-capacity reserve

Status: accepted

## Context

Package CPU, memory, swap, and process limits must cover PHP-FPM, scheduled
jobs, and future rootless application services as one account budget. The
agent must coexist with systemd, apply changes without killing healthy
workloads, preserve them across reboot, and leave enough capacity for the host
and Stackfort's control/data-plane services.

Package `nil` means unlimited/default. Swap also permits an explicit zero,
which must remain distinguishable because zero disables account swap.

## Decision

1. Use unified cgroup v2 through systemd-owned slices, not direct cgroupfs
   directory mutation.
2. Create `stackfort-core.slice` and `stackfort-accounts.slice` below the
   implicit `stackfort.slice`. Each account receives the immutable UID-derived
   child `stackfort-accounts-<UID>.slice`.
3. Cap the aggregate accounts slice at 80% of online CPU capacity and 80% of
   physical memory. Give the core sibling maximum CPU/I/O weights and a 20%
   best-effort memory protection. Platform service units must join the core
   slice.
4. Map package values to `CPUQuota`, `CPUWeight`, `MemoryMax`,
   `MemorySwapMax`, and `TasksMax`. Preserve absent versus explicit-zero state
   in the typed contract and database.
5. Write marker-owned persistent units atomically, then use one fixed
   `systemctl set-property --runtime` profile to apply every account property
   live. Refuse to adopt another file at a managed unit name.
6. Report success only after verifying the corresponding `cpu.max`,
   `cpu.weight`, `memory.max`, `memory.swap.max`, and `pids.max` files.
7. Place each managed `user@<UID>.service` through an exact root-owned drop-in
   below the same account slice. Reconcile resources before starting the
   rootless runtime; inspect live placement and migrate an already active user
   manager through fixed systemd profiles when necessary.

## Consequences

- PHP and job units explicitly use the account slice. Rootless Quadlets inherit
  it through the delegated per-account user manager; a UID alone still does
  not place a process in the resource boundary.
- Account limits remain hierarchical: an account's configured maximum may be
  further constrained by the aggregate customer ceiling under contention.
- `TasksMax` limits tasks/threads, not only process leaders.
- Lowering a live memory limit may invoke reclaim and then the account-local
  cgroup OOM behavior. The control plane must expose that operational impact.
- The 80/20 policy is the secure MVP default. A future administrator setting
  may revision it, but no installer or account may silently bypass a reserve.

## Rejected alternatives

- Direct writes to arbitrary cgroupfs paths would compete with systemd's
  ownership and complicate reboot/reload convergence.
- Per-login `user-<UID>.slice` limits do not reliably encompass system services,
  PHP pools, jobs, and rootless application units as one product-owned budget.
- A mandatory container per account adds isolation and operational overhead
  that is unnecessary for static/PHP hosting and does not remove the need for
  host-level service limits.
