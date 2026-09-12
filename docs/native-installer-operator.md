# Native installer operator commands

Status: **operator inspection/approval and the public interactive native path are
implemented; exact-candidate publication qualification is pending**. Current
source registers `onboard` for an authenticated `tag-release` on an eligible
fresh, disposable Debian 13 `amd64` host. It includes terminal consent, guarded
offline preparation and supervised installation; this is not production support
or a claim that a matching public candidate has been qualified and published.
See the [current installation guide](installer-installation.md#interactive-native-setup).
Automatic recovery and public resume of partial state remain disabled.

This follows [service admission](native-quota-service-admission.md). The
[dated qualification](../infra/host-tests/results/2026-09-09-native-installer-operator.md)
distinguishes CLI tests, laboratory continuation and outstanding production work.

## Inspect without changing installation state

```sh
sudo stackfort-installer native status
sudo stackfort-installer native status --format=json
# The native package wrapper forwards the same command:
sudo /usr/sbin/stackfort-install native status
```

Inspection opens only an existing, trusted installer state directory and existing
shared lock. It does not create them. A missing directory means no native state;
an existing directory with a missing, unsafe or busy lock is an error.
There is no unlocked-read fallback.

The report includes recorded storage/admission phases, admission attempt and
boot, journal digests, and the most recent approval and its digest. A stored
`admitted` phase is historical, not a live health or firewall assertion.
JSON therefore explicitly reports `liveReadinessVerified: false` and
`publicResumeEnabled: false`. Successful status inspection exits 0 even if
the recorded installation requires recovery.

Malformed, linked, unsafe, noncanonical or cross-plan records are rejected,
not normalized. A package journal must match its original native binding.
Status does not hash every retained release file; approval and actual admission
perform the retained-source checks.

For an actionable but strictly read-only handoff, current source also provides
`native recovery-plan [--format=text|json]`. It classifies the complete locked
snapshot and emits limited evidence; inspection errors never become a partially
trusted recovery plan. Exit 2 requests operator review, exit 1 indicates failed
inspection/output, and exit 0 is not live readiness or installation permission.
See the [backup/reinstallation policy](native-installer-recovery-policy.md) for
the enforced preparation decision and implemented terminal consent boundary.
Status exposes only the recovery mode and reviewed/record digests, not the full
host snapshot. A consumed preparation decision never authorizes restore/reimage.

The public `onboard` rerun can separately supervise live verification of an
already complete, admitted installation from the exact same authenticated release.
That is not this read-only inspection command and is not recovery: incomplete
admission, pending approval or mismatched state is rejected without adoption,
another setup code or another conversion attempt.

## Review and approve once

First diagnose and correct the cause without resetting journals or weakening
the checks. Inspect the current state again, then explicitly supply both exact
reviewed digests:

```sh
sudo stackfort-installer native approve-recovery --yes \
  --state-sha256=<reviewed-admission-sha256> \
  --package-sha256=<reviewed-package-journal-sha256>
```

Approval requires recorded storage `ready`, recoverable admission, matching
snapshots and a still-valid authenticated retained source/manifest. It cannot
repair native storage recovery, replace release inputs, adopt a foreign package
journal or approve an already admitted complete installation.
It also never authorizes filesystem repair, disk restore or provider reinstallation.

The command queues a root-private canonical
`/var/lib/stackfort-installer/native-recovery-approval.json`. It contains a
new request UUID, complete native plan, exact journal digests and `pending`
status. A repeat of the same pending request is idempotent. It never starts a
service, selects a caller-supplied unit/backend, opens the web gate or reboots.
Exit 0 means **approval queued**, not **recovery complete**.

The qualified supervisor calls `SourceStage.AdmitPendingInstallation` while
holding the shared lock and a verified closed web gate. It checks the snapshots
again and durably changes the record to `consumed` **before** continuing
installation. All source, storage, installed-intent and health checks still
apply. No public CLI `resume`, `force` or `reset` bypass is exposed.

If execution is lost after consumption but before admission, the approval stays
consumed and cannot be automatically used again. The operator must inspect the
failure and explicitly create a new approval. Do not automate blind repeated
approval commands. The most recent consumed/cancelled receipt is retained until
a new explicit approval replaces it; this is not an append-only audit history.

## Cancel a pending approval

```sh
sudo stackfort-installer native cancel-recovery --yes \
  --approval-sha256=<reviewed-pending-approval-sha256>
```

Cancellation changes only the matching pending record to `cancelled`; it does
not delete journals or undo an installation. A stale cancellation digest is
rejected. A different still-pending approval cannot be silently overwritten:
cancel that exact record first. Corrupt records are preserved for investigation,
not forcibly repaired by these commands.

## Historical qualification and current release boundary

The original Debian operator qualification sealed two separate artifacts: the integration
helper and actual installer CLI. The CLI is not substituted into the older
authenticated candidate archive. The lab supervisor consumes the same durable
approval format as the installer creates; the old ad-hoc lab request file is
no longer used or silently adopted.

That real test flow covered status, repeated approval, wrong and correct
cancellation, reapproval with a new UUID, supervised consumption, replay rejection,
and subsequent verified boot. The package wrapper forwards native
commands rather than treating them as installation arguments.

The subsequent [real boot-service integration](native-installer-runtime.md)
moved post-ready verification, admission and independent quarantine into the
installer binary. Current source also implements the initial offline backend,
prerequisites, authenticated public dispatch, controlling-terminal fresh-server
and reboot consent, and sealed setup-code delivery/activation. The
[setup handoff](native-installer-bootstrap-handoff.md) describes these boundaries.
Older laboratory payloads and separate source/namespace tests are not proof of
the final tagged archive's public installation flow.

Before publication, that exact candidate still needs the applicable fresh-host,
reboot, setup, completed-rerun, failure-boundary and full-OS removal evidence under
the [release-readiness gates](one-line-installation-readiness.md). The initial
native scope is Debian 13 only; fresh-default Ubuntu/Rocky support is not
advertised. Shared-root capacity is an explicit experimental limitation, not a
guaranteed OS reserve. No inspection, approval or failure state authorizes
automatic repair, filesystem reset or server reinstallation.
