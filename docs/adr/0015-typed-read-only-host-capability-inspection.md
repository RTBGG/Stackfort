# ADR 0015: Typed read-only host capability inspection

Status: Accepted

## Context

Stackfort must explain whether account isolation, quotas, service management,
and the web stack can work on a particular node. Treating host inspection as a
single pass/fail script loses partial information and encourages callers to
parse command output. Allowing callers to choose probe commands, package names,
units, ports, or paths would also widen the privileged agent boundary created
in ADR 0014.

## Decision

1. Add the explicit version-1 operation `host.capabilities.inspect` with an
   empty request payload and a bounded typed response.
2. Represent each feature as `available`, `unavailable`, `unsupported`, or
   `unknown`, with a stable reason code for every non-available state.
3. Fix the managed filesystem target at `/srv/hosting`, public port checks at
   80/443, and package/service roles in agent-owned allowlists.
4. Read kernel state only from fixed, size-bounded files. Parse `/etc/os-release`,
   mountinfo, cgroup v2 controllers, security-module state, and TCP listener
   tables without evaluating shell syntax.
5. Query packages and systemd state only through fixed absolute executables and
   fixed argument shapes. Use no shell, caller environment, or returned raw
   output. Bound each command to one second and 32 KiB, and the complete report
   to eight seconds.
6. Validate the complete report on both sides of the RPC boundary, including
   exact role counts, unique allowlisted keys, bounded strings, known states,
   and response-operation correlation.
7. Keep distribution fixtures for Debian 13, Ubuntu 26.04, and Rocky Linux 10.
   Full VM tests remain the authority for systemd, quota, and LSM behavior.
8. Do not expose this read-only probe as a general process API. Consolidate its
   native execution into D-003's profiled runner before adding command-backed
   mutation operations. That migration is complete in ADR 0016.

## Consequences

- The API can explain individual unavailable features while still showing the
  rest of the host report.
- No request-controlled path or process input crosses the privileged boundary.
- Fixed package and unit mappings must be maintained as supported distribution
  versions evolve.
- `/proc/net/tcp*` identifies port conflicts but deliberately does not expose
  process ownership; richer conflict attribution requires a separately reviewed
  typed capability.
- Fixture tests prove parsing and classification. Disposable full VMs are still
  required to prove real kernel and service-manager behavior.
