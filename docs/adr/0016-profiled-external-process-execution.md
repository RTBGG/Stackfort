# ADR 0016: Profiled external process execution

Status: Accepted

## Context

Some host-agent duties require native distribution tools. Reimplementing every
package database, service manager, configuration validator, and lifecycle tool
would add risk, while a generic command runner would turn a validation mistake
into root-level command execution. Killing only the direct process is also
insufficient when a tool has created children or left output pipes open.

D-002 already needed three narrowly coded read-only subprocesses. Mutation
operations must not copy and gradually diverge from those initial controls.

## Decision

1. Place all native execution behind one internal `agentexec` runner. Do not
   expose it through RPC.
2. Let callers select only compiled-in profiles. Each profile owns one fixed
   absolute executable, direct argument template, exact semantic-value
   allowlist, timeout, output limits, and reap delay.
3. Provide no production registration API and no caller-selected executable,
   environment, directory, raw option list, or command string.
4. Execute without a shell, with `/` as the directory, no standard input, and a
   fixed minimal locale, timezone, and `PATH` environment.
5. Capture standard output and error independently. Kill the process group as
   soon as either bound is exhausted; never report truncation as success.
6. On Linux, start the direct process as a new process-group leader. Kill that
   group on cancellation, timeout, and output exhaustion, and reap the command
   through one `Wait` call with a bounded `WaitDelay`.
7. Fail closed outside Linux, where this process-group contract is not
   implemented.
8. Return ordinary non-zero program exits as bounded results. Return stable,
   argument-free classes for unsafe start, cancellation, timeout, output, and
   reap outcomes.
9. Allow profiles to identify sensitive semantic inputs and redact their values
   from both captured streams. Prefer avoiding command-line secrets entirely.
10. Migrate D-002's package and service probes to the shared runner before any
    command-backed mutation is introduced.

## Consequences

- A new external utility requires a code-reviewed profile rather than a caller
  supplying a path or options.
- Timeouts and output pressure clean up trusted utilities that create ordinary
  child processes in their inherited process group.
- Programs that deliberately create a new session or process group are not
  suitable profile targets without a stronger, separately reviewed containment
  mechanism. Production profiles therefore remain limited to trusted host
  utilities with understood behavior.
- Stable runner errors are less diagnostically detailed than raw `os/exec`
  errors. Detailed host diagnostics, when needed, must be generated separately
  without secrets and must not cross the RPC boundary by default.
- Linux runtime tests are required; cross-compilation alone cannot verify
  process-group cleanup.
