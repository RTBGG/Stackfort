# Scheduled account jobs

K-009 provides a deliberately closed scheduled-job lifecycle for account-owned
Shell and PHP scripts. The browser never submits a command line, executable,
systemd unit name, environment map, working directory, or raw calendar
expression.

## Product contract

An owner or member can create, edit, enable, disable, and remove jobs for a
host-ready active account. Auditors can view jobs but cannot mutate them.
Platform administrators retain their existing account-scoped override. The
effective immutable package revision controls `maxScheduledJobs`; zero disables
the feature and the absolute implementation ceiling is 1,000 jobs per account.

Each job contains only:

- an account-facing name;
- `shell` with an account-relative `.sh` path, or `php` with an
  account-relative `.php` path and package-approved PHP version;
- one fixed UTC schedule: every 5, 15, or 30 minutes; hourly at a selected
  minute; daily; or weekly; and
- an enabled flag.

Script paths use the same canonical segment grammar as document roots:
ASCII letters, digits, dot, underscore, and hyphen, separated by single `/`
characters. Absolute paths, dot segments, whitespace, `%`, `$`, backslashes,
control text, and shell punctuation are rejected. This keeps the rendered
`ExecStart` free of systemd specifier, environment, and quoting semantics.

Schedules are UTC by design. The UI labels every entered time as UTC rather
than silently applying the browser or server timezone. Interval jobs have a
fixed 30-second randomized delay and systemd's persistent timer behavior so a
missed run can be caught after downtime. A oneshot service cannot overlap with
itself; systemd leaves the already active invocation running.

## Host boundary

The control plane persists the closed definition and a revision-fenced durable
mutation. The privileged agent independently validates it and derives all host
details:

- `/bin/sh`, Debian `/usr/bin/php8.4`, Ubuntu `/usr/bin/php8.5`, or Rocky
  `/usr/bin/php` from the detected supported distribution;
- `stackfort-job-<account UID>-<job UUID without hyphens>.service/.timer`;
- the immutable account username, group, home, and
  `stackfort-accounts-<UID>.slice`; and
- a canonical `OnCalendar` expression.

Before writing units, the Linux manager opens the account home and every script
component descriptor-relatively with `O_NOFOLLOW`. Every directory and the
regular script must be owned by the exact account UID/GID and remain on the
account filesystem. Scripts with multiple hard links, unreadable scripts, and
scripts larger than 16 MiB are rejected. A script remains ordinary
account-owned content and may be edited later; it never executes as root.

The root-owned `0644` service/timer pair is written atomically below
`/etc/systemd/system`. Stackfort refuses to adopt an existing file without its
exact managed marker. Promotion, daemon reload, enable/disable, verification,
rollback, persistent timer-state cleanup, and removal are replay-safe. Only
fixed `systemctl` profiles receive redundant account identity and job ID
values; neither the script nor a unit name crosses that subprocess boundary.

The service runs as the account identity inside the account resource slice and
uses a five-minute runtime ceiling, 64-task limit, low CPU weight, restrictive
umask, no privileges or capabilities, private devices and temporary storage,
read-only system paths, namespace/kernel/proc protection, and socket-bind
denial. Only the account home is explicitly writable. Standard input and all
output are discarded to prevent unattended jobs from filling the journal or
exposing secrets through the panel. Per-job output/history and notifications
are intentionally outside K-009.

## Durable lifecycle and API

Create, update, and delete return `202 Accepted` with a durable operation. A
UUIDv7 job ID is also the create-operation ID. Idempotency keys, immutable
mutation records, optimistic revisions, the current package snapshot, host
readiness, and exact agent-response verification prevent replay or
cross-account substitution from applying different intent.

The browser API is:

- `GET|POST /api/v1/accounts/{accountID}/jobs`;
- `PUT|DELETE /api/v1/accounts/{accountID}/jobs/{jobID}`; and
- `GET /api/v1/accounts/{accountID}/job-operations/{operationID}`.

Mutations require the browser CSRF token and an idempotency key. The account UI
polls only the scoped operation resource, then reloads authoritative jobs.
Failed operations retain their immutable mutation for diagnosis and safe
worker retry rather than claiming host convergence.

## Verification

Unit and repository tests cover invalid unions and calendars, path injection,
package/PHP limits, authorization, cross-account isolation, idempotency,
optimistic revisions, exact worker-to-agent intent, agent response
substitution, host rollback, and create/update/disable/delete replay.

The focused Hyper-V test runs the same Linux integration binary on Debian 13,
Ubuntu 26.04, and Rocky Linux 10. It gives all schedule forms to
`systemd-analyze calendar`, gives rendered units to `systemd-analyze verify`,
executes real Shell and distribution PHP jobs as a disposable account, checks
the resource slice and sandbox properties, proves `PrivateTmp`, rejects
symlinked and hard-linked scripts, and verifies exact disable/removal cleanup.
See the [qualification result](../infra/host-tests/results/2026-08-30-scheduled-jobs-hyper-v.md).
