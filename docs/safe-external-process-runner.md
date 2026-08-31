# Safe external process runner

D-003 provides the one process-execution boundary inside the privileged host
agent. It is internal infrastructure for typed agent operations, not an RPC
operation and not a general command runner.

## Interface boundary

Callers select an agent-owned profile and provide only the semantic values that
the profile declares. A profile fixes:

- an absolute installation-owned executable path;
- the complete option template and an exact allowlist or typed semantic-value
  validator;
- a hard runtime deadline;
- independent standard-output and standard-error limits; and
- the maximum wait period used while reaping a terminated process.

The production registry contains the read-only D-002 package/systemd queries
and E-001's fixed `groupadd`, `useradd`, `usermod`, `userdel`, and `groupdel`
profiles. E-002 adds one fixed `/usr/sbin/setquota` project profile whose mount
target and options are compiled in. E-003 adds fixed `systemctl` daemon-reload,
account-slice start, and live property profiles; the unit name and every
property are derived from a validated typed resource specification. Account
profiles accept only a complete valid UUID-derived Stackfort
identity; they cannot target an arbitrary local name, ID, or home. The registry
cannot be extended by an RPC caller.

F-001 adds fixed profiles for the NGINX version/configuration test and for only
enable/disable/restart/reload/stop of `nginx.service`; neither the
main-configuration path nor the unit is caller-selectable.

The managed PHP slice adds fixed distribution/version profiles for FPM syntax
tests and for show/enable/disable/restart of only a derived account pool unit.
The caller supplies one complete validated Stackfort identity and an approved
version; executable, configuration, service, PID, and socket paths remain
registry-owned. J-002's read-only show profile requests only fixed systemd
state/accounting properties. The host adapter validates the derived account
cgroup and returns only bounded state plus aggregate memory, CPU-time, and task
counters to the typed protocol.

There is no executable parameter, raw argument-array parameter, shell command,
environment parameter, or working-directory parameter in the public runner
interface.

L-003 adds only fixed rootless image profiles. Pull enforces digest/TLS policy;
build fixes no-network, no-cache, CPU, memory, process, file-descriptor, time,
and output limits; save runs through fixed `prlimit` arguments for a 2-GiB
archive ceiling; inspect/remove accept only the derived image target. The Trivy
profile owns the bundled executable and scanner flags and returns JSON through
a 16-MiB capture. Podman processes drop to the persisted account UID/GID with
derived HOME and XDG runtime paths; failed-image cleanup uses no force flag and
cannot remove containers. Trivy remains root-owned and receives no
engine socket.

The capability adapter also verifies the complete legacy probe shape before it
selects a profile. This deliberate second check means both the operation and the
shared runner must accept a request before an external program can start.

## Execution controls

Every command is started directly with Go's `os/exec` package. Stackfort never
uses `/bin/sh -c`, expands a command string, or searches `PATH` for these
programs. The child receives only:

```text
LANG=C
LC_ALL=C
PATH=/usr/sbin:/usr/bin:/sbin:/bin
TZ=UTC
```

Its working directory is `/`, standard input is closed, and output capture is
bounded separately to 32 KiB per stream. Read-only probes use one-second
deadlines and account mutations use ten seconds for the account-database lock.
Reaching
either limit is an execution failure, not a truncated successful result.

On Linux, every child becomes the leader of a new process group. Cancellation,
profile timeout, and output exhaustion send `SIGKILL` to that complete group,
then call `Wait` exactly once. Go's `Cmd.WaitDelay` provides a final bound when
a terminated command or inherited pipe would otherwise delay reaping. Go
documents the `CommandContext`, `Cancel`, and `WaitDelay` behavior in
[`os/exec`](https://pkg.go.dev/os/exec); Linux documents negative process IDs as
process-group targets in [`kill(2)`](https://man7.org/linux/man-pages/man2/kill.2.html).

Stackfort fails closed on non-Linux platforms because the required process-group
isolation is part of the runner contract.

## Results and secrets

An ordinary non-zero exit status is a bounded typed result so an operation can
classify such states as “not installed” or “not found.” Start, cancellation,
timeout, output, and reap failures use stable error classes. Their text never
contains the executable arguments, captured output, or the underlying operating
system error.

Profiles for future secret-consuming utilities must mark their sensitive
semantic inputs. Every occurrence is replaced with `[REDACTED]` in both captured
streams before a result can leave the runner. Profile reviews must still prefer
non-secret file descriptors or protected files over command-line secrets,
because command arguments can be visible elsewhere on the host.

## Verification

Cross-platform tests inspect every production executable, template, allowlist,
environment, timeout, output bound, and redaction primitive. Linux-only helper
process tests create a real child process and prove that:

- a parent context cancellation is classified without starting work;
- a profile timeout kills both the direct process and its child;
- output exhaustion immediately kills both processes;
- the child is no longer addressable after the runner returns; and
- a delayed child side effect never occurs.

CI executes these tests with the race detector on Linux. Windows development
tests exercise validation and compile the Linux runner/tests for both `amd64`
and `arm64`, but they cannot substitute for the Linux runtime checks.
