# Post-boot package and service continuation

Status: **internal coordinator and opt-in Debian lab; not a public native-storage
installer feature or a new release**.

This builds on the [authenticated release/boot binding](native-quota-release-origin.md).
Storage `ready` means verified storage preparation, not a completed Stackfort
installation. Package installation has a separate, source-bound journal.
The current [admission/recovery layer](native-quota-service-admission.md) wraps
this continuation with a closed public-web gate and reviewed recovery.
The [dated Debian evidence](../infra/host-tests/results/2026-09-09-native-install-continuation.md)
records the exact helper, authenticated payload, fresh installation, normal
reboot, development failures and remaining production boundaries.

## Internal boundary

`SourceStage.AdmitInstallation` calls a private continuation while keeping the existing shared installer lock for
the entire installation. It requires the exact manifest-bound `ready` storage
state; `planned`, `awaiting-reboot`, `verifying` and recovery states cannot use
this API to arm or resume a conversion.

Before installation, each engine stage apply/verification and the final or
already-installed verification pass, it checks the
retained source, archive, attestation receipt and boot manifest, then asks the
typed backend to verify live host identity, boot artifacts, managed storage and
actual quota enforcement. A cached ready flag is insufficient. Completed
installation reruns also perform these checks before verifying installed state.

The coordinator persists a canonical root-private
`/var/lib/stackfort-installer/native-install-binding.json` before the first
package journal. This binding contains the complete storage plan, including its
operation and manifest digest. Existing package state without the matching
binding cannot be adopted, even if its release version matches. Partial,
conflicting, linked or noncanonical records are rejected without automatic
repair or deletion.

Only a private adapter may read the package journal while native state exists.
It enforces the binding, canonical JSON and the original stage/source/platform
validation. Ordinary `NewLinuxRunner`, `NewLinuxUpdateRunner` and
`FileStore.Load` retain their native-journal rejection. There is no CLI flag,
environment variable or exported runner constructor that skips those gates.

The existing installation engine executes its nine real stages:

1. Distribution packages.
2. Exact Coraza/NGINX native package.
3. Exact Vinyl native package.
4. Service identities.
5. Release payload.
6. System configuration.
7. Security policy.
8. NGINX baseline.
9. Services and health checks.

The retained candidate installer executable is not launched. Current coordinator
code drives the existing Linux runner using the authenticated candidate's
unchanged payload. This distinction matters: the candidate's bundled binaries
predate the new native-storage code and are not a newly qualified native release.

## Laboratory boot graph

The filesystem resumer must not wait for package installation: starting the
agent from inside that resumer would deadlock against its hosting-mount
dependency. The lab therefore separates the steps:

```text
quota conversion / normal boot; early public-web gate closed
  -> native quota resumer: source + storage checks
  -> srv-hosting.mount
     -> separate native-install service: verified continuation, then web admission
     -> managed consumers: depend on the verified mount
```

`Prepare -WithRelease -WithInstallation` binds the opt-in into the immutable lab
manifest, enables a separate `stackfort-native-install.service`, and installs
lab-only `BindsTo`/`After=srv-hosting.mount` drop-ins for MariaDB, Vinyl, the
agent, API, phpMyAdmin and panel renewal service. NGINX keeps its strict
foreign-drop-in rejection: the mount's `RequiredBy=nginx.service` link and
`Before=nginx.service` ordering establish its dependency without adding a
foreign NGINX configuration override. The install unit starts
after the mount and network-online target, without making its own managed
services wait for installation completion. The admission follow-up now verifies
installed intent and starts missing consumers behind a closed network gate
before checking their health. It removes ordering after consumers so
supervisor quarantine can stop them without a stop-ordering deadlock.
It has no automatic restart loop.

The reboot test exposed a separate installer defect: the shared PHP socket
parent `/run/stackfort-php` was created once but not recreated after reboot.
Installation now writes and verifies a root-owned
`/etc/tmpfiles.d/stackfort-php.conf` with a directory-only rule, mode `0755`,
without age-based cleanup, recursive changes or socket deletion. A shared
parent should outlive individual PHP pool services; see the
[tmpfiles reference](https://manpages.debian.org/trixie/systemd/tmpfiles.d.5.en.html).
This small general installer fix does not enable native storage preparation.
Installer and live account reconciliation now also use the same platform-slice
renderers. Previously account/OCI operations reintroduced obsolete accounting
switches into shared slices, making the next immutable installation check fail.
CPU and memory reserves remain unchanged.

The original dated run blocked the filesystem resumer for incomplete package
state. The admission follow-up separates those concerns: verified storage may
remain mounted for operator recovery, but public web traffic stays blocked and
fixed managed consumers are stopped. Incomplete admission requires explicit
authorization bound to both durable admission and package snapshots. Within
that invocation, the engine retains its stage verification and retry behavior.

This graph checks storage/source admission at boot and propagates a mount unit
stop to consumers. NGINX uses `Requires`, not the stronger `BindsTo` semantics
for unexpected unit deactivation; see the [systemd dependency reference](https://manpages.debian.org/trixie/systemd/systemd.unit.5.en.html).
This is not continuous monitoring of quota enforcement and it
does not make package installation atomic. Vendor package scripts may start
services before the final installation stage; the new fixed web gate keeps
those listeners inaccessible from outside while local checks run.
A complete journal does not prove that every installed file still matches:
the later installed-payload verification can fail after consumers start.
The new admission supervisor closes the web gate and stops fixed consumers
on that failure. General production admission (including dynamic tenant/OCI
ports and competing firewall services) still needs qualification.

## Reproduction

Use only the dedicated Debian VM, explicitly restored from the offline
`native-quota-before-conversion` checkpoint. Prepare the exact root-owned
candidate fixture described in the [origin guide](native-quota-release-origin.md).
The wrapper never restores checkpoints or downloads candidate inputs itself.

```powershell
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Prepare -WithRelease -WithInstallation
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Arm
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage Validate
./infra/host-tests/Test-StackfortNativeJournalBootHyperVVm.ps1 -Stage NormalBoot
```

Keep Go sources/compiler settings unchanged throughout the sequence. The helper
pins its executable. `Validate` waits for the actual installation service, then
checks the complete package journal, storage dependencies and quota/OCI probes.
`NormalBoot` additionally requires an unchanged, already-installed result.
Fault tests require a separately saved successful checkpoint; never edit a
journal or retained source on a real host to force a retry.

## Remaining production gates

The lab backend and systemd files are not production boot infrastructure.
Public preparation/continuation, a fresh signed candidate containing the native
runtime and current guards, production recovery UX and broader interruption tests,
external package/kernel/initrd serialization, OS reserve/admission and ordinary
Debian/Ubuntu/Rocky provider-image onboarding remain open.

The shared lock excludes cooperating installer/storage transactions, not
unrelated root package managers. The updater has its own separate lock; its
current constructor rejects native state, but already-constructed or older
updaters are not globally serialized by this lock. The bootstrap/compiler/host
identity and artifact pins are
not a defense against a hostile root rewriting all trusted state. Nothing here
qualifies arbitrary power loss during ext4 metadata writes.
