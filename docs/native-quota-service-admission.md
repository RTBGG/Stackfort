# Native installation admission and reviewed recovery

Status: **internal API and opt-in Debian laboratory boot units**. This does not
enable native-storage preparation in the public installer, publish a release,
or qualify general production onboarding.

This follows [post-boot installation continuation](native-quota-install-continuation.md).
Storage readiness, a complete package journal, and public service admission are
different conditions. Installed payload verification can fail even when the
package journal says complete.
The [dated Debian evidence](../infra/host-tests/results/2026-09-09-native-service-admission.md)
records the exact helper and authenticated fixture, interruption, recovery and
external-network checks.
The subsequent [installer operator interface](native-installer-operator.md)
adds recorded status and durable approve/cancel commands. Production boot
resume is still disabled.
The [real installer runtime follow-up](native-installer-runtime.md) replaces
post-ready lab service entry points with the sealed installer dispatcher while
retaining the lab-only initial conversion boundary.

## Closed until verified

`SourceStage.AdmitInstallation` holds the shared installer/storage lock and
wraps the now-private continuation method. The caller must establish the gate
before networking/consumer startup and provide independent process-loss cleanup.
An invalid invocation that fails before entering the coordinator is not itself
a replacement for that supervisor.

The Debian gate owns only `inet stackfort_install_admission`, marked with the
exact operation UUID. Its input hook drops non-loopback TCP and UDP traffic to
ports 80, 443 and 8443. The inet family covers IPv4 and IPv6. SSH and loopback
health checks are unaffected. This is separate from the ordinary Stackfort
firewall; it never flushes the global ruleset. Unknown rules, ownership markers
or chain settings are rejected rather than overwritten.

Closing, opening and rule creation use nftables transactions. Opening empties
only the previously verified owned chain; the marked table remains.
See the [Debian nft reference](https://manpages.debian.org/trixie/nftables/nft.8.en.html).

```text
early boot: close public web gate
  -> verify retained release + native storage
  -> hosting mount available
  -> admission coordinator
     -> persist checking
     -> verify installed intent / run or resume real package stages
     -> start managed services behind closed gate; check loopback health
     -> recheck source, storage, complete result and closed gate
     -> persist admitted; open and verify gate
failure / process loss -> close gate; stop fixed managed consumers
```

Completed-install checks may start already installed services for health
verification, but only after verifying their package, payload, configuration,
security and NGINX intent. This does not replay completed package stages.

The lab's early gate service runs before `network-pre.target`; the storage
resumer requires it. The separate installation service has no restart loop and
uses `ExecStopPost` quarantine, including after a killed main process. It does
not order after managed consumers, avoiding a stop-ordering deadlock when
cleanup stops them. See the
[systemd service reference](https://manpages.debian.org/trixie/systemd/systemd.service.5.en.html).

Quarantine checks/stops NGINX, API, agent, phpMyAdmin, Vinyl, MariaDB, the
distribution PHP 8.4 pool and panel renewal timer/service. It does not stop the
coordinator itself or the verified hosting mount, which can remain available
for operator recovery.

## Durable state, not a readiness flag

`/var/lib/stackfort-installer/installation-admission.json` is canonical,
root-private JSON. It binds the complete native plan, current boot UUID,
monotonic attempt counter, and `checking`, `admitted` or
`recovery-required` phase. Reads reject unsafe metadata, links, malformed or
noncanonical content. Writes use an exclusive temporary record, file sync,
atomic rename and parent-directory sync.

Every normal boot closes the actual network gate and checks installed state
again. A historical `admitted` record is never permission to skip this.
A killed process can leave `checking`; that is a recovery condition, not
permission for an automatic retry.

`InspectAdmission` returns read-only state and SHA-256 digests of the exact
admission record and canonical package journal. Missing package state has a
distinct fixed digest. Recovery requires both reviewed digests and runs under
the same lock. A changed record invalidates the authorization. A present but
incomplete package journal requires recovery even after historical admission.

Recovery does not reset journals, adopt a new source, bypass storage checks,
repair malformed evidence or skip completed-stage verification. The existing
engine retries only stages that genuinely need continuation. Ordinary
installer/updater constructors and public journal reads still reject native
storage state.

The operator follow-up queues these digests in a separate canonical approval.
The supervisor's `AdmitPendingInstallation` consumes it durably under the same
lock before calling this admission protocol. Merely queuing an approval does
not start services or authorize any check to be skipped.

## Disposable-VM reproduction

Use the exact Debian VM and offline pre-conversion checkpoint described in the
[origin guide](native-quota-release-origin.md). Prepare its authenticated fixture
and ensure nftables is installed **before** sealing the boot intent. These are
lab prerequisites, not an implemented public bootstrap.

Keep all Go sources and compiler settings unchanged through the sequence:

```powershell
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Prepare -WithRelease -WithInstallation -InstallationFault pause-services-once
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage InterruptInstall
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage InspectAdmission
# Review both digests and the failure before explicitly authorizing recovery:
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage RecoverInstallation -AdmissionStateSHA256 <reviewed-state-sha256> -AdmissionPackageSHA256 <reviewed-package-sha256>
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage ValidateCurrent
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage NormalBoot
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage WebGateProbe
# Probe HTTPS from the Windows host: it must fail while loopback remains healthy.
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage NormalBoot
```

The sealed fault pauses before the service stage, after package transactions
have finished. The interruption test kills only the coordinator main process
at this point. It does not kill apt, interrupt quota conversion, or simulate a
power cut. Omit the fault and use `Validate` for an uninterrupted conversion.
`ValidateCurrent` is intended for the initial conversion boot after recovery;
`NormalBoot` additionally proves the converter did not run again.

## Remaining production boundaries

- Package the early gate, boot backend, coordinator and explicit recovery UI/CLI;
  integrate bootstrap prerequisite ordering and retain public gates until qualified.
- Qualify all listeners, dynamic tenant PHP/OCI autostart, forwarding/published
  container ports and competing firewall services. The fixed web gate is not
  whole-machine or arbitrary-container isolation.
- Serialize external package/kernel/initrd work and older/already-running
  updaters; the shared lock protects only cooperating transactions.
- Add capacity/reserve admission, recovery policy and fresh provider-image
  onboarding for Debian, Ubuntu and Rocky.
- Build and sign a new candidate with the native runtime after these gates.
  Driving an authenticated older payload with current internal code does not
  turn that payload into a qualified native release.

The record and gate do not defend against hostile root replacing trusted
code or firewall state, continuously monitor storage, or qualify power loss
during filesystem metadata writes. A firewall conflict is retained for review;
fixed consumers are stopped, but unrelated listeners are outside this contract.
