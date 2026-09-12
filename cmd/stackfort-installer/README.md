# Stackfort installer

`stackfort-installer` provides preflight inspection, interactive native onboarding,
prepared-storage installation, panel hostname/certificate management, and native
operator commands. Public `onboard` dispatch is implemented; the exact beta.4
tagged archive, public downloads and clean-host flow are **not yet qualified or
published**. Source implementation is not a release-readiness claim.

```sh
stackfort-installer preflight --format=json
stackfort-installer native-host --format=json
stackfort-installer version
stackfort-installer install --source-dir=/absolute/extracted/release --yes
stackfort-installer panel status
stackfort-installer native status --format=json
stackfort-installer native recovery-plan --format=json
```

The [one-line bootstrap](../../docs/installer-installation.md#github-bootstrap)
currently selects the explicitly pinned `0.1.0-beta.5` and supplies the native
selection arguments when appropriate:

```sh
stackfort-installer onboard \
  --source-dir=/absolute/extracted/release \
  --archive=/absolute/release.tar.gz \
  --attestations=/absolute/build-attestation.jsonl \
  --version=0.1.0-beta.5
```

Only the exact build version/commit with authentic tag-release provenance is
accepted. Fresh native setup is limited to the eligible Debian 13 amd64 profile
and requires a real controlling root terminal, exact fresh-disposable/no-data
consent, separate `REBOOT` consent and `SAVED` acknowledgement after terminal-only
setup-code delivery. There is no public noninteractive `--yes`, consent/device
override or laboratory origin. Successful sealed preparation and arming requests
the explicitly authorized reboot; an error does not silently reset the operation.
The setup code expires one hour after post-install activation and is never
persisted in raw form. See [setup handoff](../../docs/native-installer-bootstrap-handoff.md).

A completed native rerun verifies the same sealed installation through systemd
supervision and live admission checks. It does not repeat conversion/installation,
reissue the setup code or schedule another reboot. Partial/recovery state is not
automatically resumed.

The native operator commands inspect recorded state and manage explicit recovery
approvals. They do **not** enable native quota preparation, start services, open
web ports, or reboot. A pending approval is not a completed recovery.
`native recovery-plan` gives read-only next-step advice and limited evidence;
exit 2 means review is needed, exit 1 means inspection/output failed. It never
verifies a backup or authorizes disk repair, restore or provider reinstallation.
The [recovery policy](../../docs/native-installer-recovery-policy.md) also documents
the operation-bound fresh-disposable decision now collected by `onboard` before
preparation. Neither operator approval nor onboarding verifies an external backup
or authorizes automatic provider reinstallation.

See [installer preflight](../../docs/installer-preflight.md),
[native operator commands](../../docs/native-installer-operator.md), and
[the native admission design](../../docs/native-quota-service-admission.md).

The internal `native-service` dispatcher and deterministic systemd units now
provide [post-ready boot verification, admission and process-loss quarantine](../../docs/native-installer-runtime.md)
in the current installer binary. They require the exact pre-sealed native plan;
the package wrapper does not expose an activation/resume shortcut.

The internal `native-boot` dispatcher additionally implements the initial
[offline preparation and finalization](../../docs/native-installer-preparation.md)
for the separately sealed Debian profile. There is no standalone public
prepare/device/reset option; only the higher-level interactive `onboard` flow
requests a reboot after explicit consent and successful arming.

New internal boot intents additionally use the
[power-loss containment guard](../../docs/native-installer-power-loss.md):
an uncertain restart stays in initramfs without automatically repairing or
mounting root. This is not public crash-recovery qualification.

The read-only [native host eligibility check](../../docs/native-installer-host-eligibility.md)
reports conservative fresh-host conflicts and missing prerequisites. Native
preparation installs only the exact approved new packages before boot sealing;
an internal APT guard validates the real transaction. Read-only `native-host`
does not itself install packages, grant consent or perform recovery. Public
availability remains subject to the [release gates](../../docs/one-line-installation-readiness.md).
