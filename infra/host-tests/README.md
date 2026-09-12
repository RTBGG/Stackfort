# Disposable host test foundation

Stackfort's host tests require full virtual machines. A container does not prove
systemd-as-PID-1 behavior, cgroup delegation, mount-level project quotas, or
SELinux/AppArmor behavior.

## Image sources

`images.json` records the official current cloud-image locations for Debian 13,
Ubuntu 26.04 LTS, and Rocky Linux 10 on `amd64`. The URLs intentionally point to
the vendor's current image within the supported release line. Every download
must be verified against the checksum document fetched from the same official
vendor, and the resolved image checksum must be retained with a test result.

On a Linux virtualization host, download and verify an image with:

```sh
bash infra/host-tests/prepare-image.sh debian-13
bash infra/host-tests/prepare-image.sh ubuntu-26.04
bash infra/host-tests/prepare-image.sh rocky-10
```

On an elevated Windows 11 Pro Hyper-V host, install `qemu-img`, then create and
start a node with:

```powershell
winget install --id SoftwareFreedomConservancy.QEMU --exact
powershell -ExecutionPolicy Bypass -File infra/host-tests/New-StackfortHyperVVm.ps1 debian-13
powershell -ExecutionPolicy Bypass -File infra/host-tests/Test-StackfortHyperVVm.ps1 debian-13
powershell -ExecutionPolicy Bypass -File infra/host-tests/Test-StackfortInstallerHyperVVm.ps1 debian-13
powershell -ExecutionPolicy Bypass -File infra/host-tests/Test-StackfortInstallerHyperVVm.ps1 debian-13 -RunPhase1Suite
powershell -ExecutionPolicy Bypass -File infra/host-tests/Test-StackfortCleanInstallersHyperV.ps1 -SkipBuild
powershell -ExecutionPolicy Bypass -File infra/host-tests/Remove-StackfortHyperVVm.ps1 stackfort-debian-13 -Force
```

The PowerShell harness applies the same vendor checksum policy as the Linux
download helper, converts the verified QCOW2 source into an immutable VHDX
base, and creates a differencing system disk, a NoCloud seed disk, and a
separate project-quota disk. Existing VMs and VM directories are never
overwritten. Runtime state and the dedicated SSH key live below
`C:\ProgramData\Stackfort\Hyper-V`, outside the repository.
The cloud-init login is `stackfort-test`; `stackfort` is reserved for the
installer's locked service identity.

For ordinary single-system-disk VPS onboarding, `New-StackfortHyperVVm.ps1
-SingleDisk` omits both the quota disk and its cloud-init formatting. The
separate [storage image experiment](../../docs/storage-image-prototype.md)
documents its fixed-name disposable VM and validation harness. It is not yet a
production installer path. `-ImageCacheRoot` selects an independent cache when
the vendor's rolling image changes; a cached image is never accepted against a
different current checksum. Converted bases include the requested disk size in
their cache key.

The separate [native ext4 quota experiment](../../docs/native-quota-prototype.md)
tests early-boot root preparation and automatic resume on a fresh, checkpointed
single-disk Debian VM. Its fixed-name harness is lab-only; it does not add a
production installer path.

Its [journal-bound boot follow-up](../../docs/native-quota-boot-handoff.md) uses
`Test-StackfortNativeJournalBootHyperVVm.ps1` to qualify a separate one-shot
initrd, unchanged normal boot artifacts, successful resume and terminal recovery
cases. Each sequence requires an explicit fresh-checkpoint restore; the harness
does not restore snapshots or retry failed preparation automatically.

The [durable release staging follow-up](../../docs/native-quota-release-staging.md)
tests a real retained candidate in a private mount namespace and verifies it in
a new process after bootstrap cleanup. It does not execute the candidate or
modify the actual host's storage/installation journal.

The [release-origin binding](../../docs/native-quota-release-origin.md) adds
actual signature checks and negative tests in private namespaces. The journal
boot harness's `Prepare -WithRelease` connects the same retained candidate and
its authentication receipt to a real one-shot conversion/resume. It executes
only the independently pinned verifier, not the candidate installer. Initial
trust-root discovery may require HTTPS; post-boot checks use retained evidence.

The additional [post-boot installation opt-in](../../docs/native-quota-install-continuation.md)
is `Prepare -WithRelease -WithInstallation`. It starts a separate post-mount
service that runs all real installer stages from the retained candidate, with
the native plan checked before every stage. `Validate` waits for package/service
completion; `NormalBoot` requires a verified already-installed rerun. This
remains a dedicated-VM experiment, not public installer activation.
See the [dated installation/reboot evidence](results/2026-09-09-native-install-continuation.md).

The [admission/recovery follow-up](../../docs/native-quota-service-admission.md)
adds a separate closed web gate and explicit two-digest recovery. Its optional
sealed `pause-services-once` fault supports `InterruptInstall`,
`InspectAdmission`, `RecoverInstallation`, `ValidateCurrent` and
`WebGateProbe` stages. Only the dedicated Debian VM is eligible; these stages
do not enable a public installer or interrupt a running package transaction.
The [operator follow-up](../../docs/native-installer-operator.md) also builds
and seals the actual installer CLI as a separate lab artifact. Recovery tests
exercise its status/approve/cancel commands and the supervisor's durable
single-consumption path. Existing candidate archives remain unchanged.

The [real installer runtime follow-up](../../docs/native-installer-runtime.md)
adds `Prepare -WithRelease -WithInstallation -WithRuntime`. This seals the
current installer as the post-ready dispatcher before creating storage state;
early gate, live verification, admission and quarantine units no longer invoke
the test binary. Only the first conversion boot uses the proof-conditioned lab
resumer. `RuntimeInterrupt` exercises SIGKILL during revalidation of an already
complete installation (never during apt/dpkg), followed by the same reviewed
`RecoverInstallation` flow. Do not combine this profile with the legacy
in-process `pause-services-once` test hook.

The [power-loss containment follow-up](../../docs/native-installer-power-loss.md)
adds `Test-StackfortNativePowerCutHyperVVm.ps1`. It explicitly starts and hard-stops
only the fixed disposable Debian VM, observes the real installer over serial,
and verifies recovery with an unmounted root. Restore the exact reviewed offline
armed checkpoint separately for each case; never run this on another VM.
This tests the boot safety stop, not sector-tear recovery or automatic repair.

The [deterministic crash-image follow-up](../../docs/native-installer-crash-replay.md)
adds `Test-StackfortNativeCrashReplayHyperVVm.ps1` on the already-running,
normally installed fixed Debian VM. It records real conversion writes through
dm-log-writes on new scratch images, checks write/sector prefixes and synthetic
half-sector tears, and verifies a complete file-image backup/restore. It never
converts root, mounts or repairs the crash images, restores a VM checkpoint or
changes VM power state. JSON/logs and raw images are retained. This does not yet
qualify external rescue and whole-system disk restoration.

The [whole-disk rescue follow-up](../../docs/native-installer-whole-disk-recovery.md)
adds `New-StackfortNativeRestoreLab.ps1`, the fixed Linux disk-restore/inspection/
boot integration tests and `Start-StackfortRestoredDisk.ps1`. It exports a complete
offline pre-conversion disk, restores only an independently identified empty
replacement from a separate rescue OS, compares every logical byte and boots
the restored disk twice. The original VM is never restored or booted. The clone
retains duplicate OS/network identity, so it must never run alongside the original.
All fixtures remain lab-only; the public installer has no automatic restore command.

The [recovery-policy handoff](../../docs/native-installer-recovery-policy.md)
adds read-only `native recovery-plan` advice. Its Linux
`TestDisposableNativeRecoveryInspection` uses an isolated mount namespace to
check unsafe/missing/busy state without changing the real installation. The
[dated result](results/2026-09-11-native-recovery-policy.md) also covers the actual
CLI against recorded Debian state. It does not authorize backup/restore.
The [subsequent decision-binding qualification](results/2026-09-11-native-recovery-choice.md)
adds `TestDisposableNativeRecoveryChoiceBoundaries`, an exclusive pre-prerequisite
receipt and reviewed host/source bindings through APT, sealing and offline boot.
Only the explicit fresh-disposable mode is accepted; external backup verification
and public consent/activation are not implemented.

The [package-guard follow-up](../../docs/native-installer-package-coordination.md)
adds `TestDisposableNativePackageGuard` (private mount namespace and scratch dpkg
database), real APT/dpkg lock-conflict tests, exact package-delta checks, and
`TestDisposableNativePackageBaseline` (read-only real package inventory with
private synthetic receipts). Full preparation/boot/normal-boot evidence is in the
[dated qualification](results/2026-09-12-native-package-guard.md). Process-lifetime
guards do not yet complete the persistent package/kernel coordination gate.

The [host eligibility follow-up](../../docs/native-installer-host-eligibility.md)
also exercises the same boot path starting from the exact offline
`native-host-missing-prerequisites` checkpoint, where only `quota` and `nftables`
were removed from the disposable fixture. The installer must install both,
complete the bound prerequisite receipt and retain unchanged normal boot pins
before arming. Isolated Linux tests cover read-only checks, reserved UDP ports,
prerequisite journal transitions and the public gate.

The [all-real boot preparation follow-up](../../docs/native-installer-preparation.md)
uses `Test-StackfortNativeBootHyperVVm.ps1` on the explicitly started disposable
Debian VM. Run `Prepare -AcceptDisposableReinstallationRisk`, `Arm`, `Validate`,
then `NormalBoot`. The driver supplies
the authenticated candidate and checks results; no test executable enters the
initramfs or finalization units. It refuses mixing a changed installer into a
sealed operation. Checkpoint restoration is a separate, explicit offline action.

Rocky's Windows download uses the catalogued Hochschule Esslingen HTTPS mirror
because the vendor CDN can be severely throttled on some routes. The mirror
payload is still accepted only when it matches Rocky Linux's checksum fetched
independently from `dl.rockylinux.org`.

The test command waits for the VM, resolving its address through Hyper-V KVP or
the exact virtual-adapter MAC as a first-boot fallback. It cross-builds the
read-only installer and opt-in Linux test binary, waits for Cloud-init, and runs
the capability gate and installer preflight before any package preparation,
followed by destructive quota/isolation checks and the real managed NGINX
baseline. The
NGINX phase also gives a typed static/redirect account include containing
adversarial dollar-prefixed URL literals to the vendor parser. The NGINX
preparation installs the distribution package and stops only an unmanaged
instance before first adoption. The static-domain phase runs the durable
create/shared/suspend/resume/remove workflow, verifies account-owned file modes,
POSIX default ACLs, NGINX symlink denial, non-destructive roots, and enforcing
Rocky SELinux contexts. The remove command validates that
the Hyper-V configuration is below the exact Stackfort VM directory before it
removes that disposable directory; shared verified images, immutable bases,
and the SSH key are retained.

The separate installer harness builds a release-shaped archive, transfers it
to a clean guest, and exercises the manual archive, exact production bootstrap,
or passive native-package route. It runs the production installer twice,
requires an unchanged journal and `alreadyInstalled=true` on the second run,
and independently checks
file metadata, service health, systemd sandbox properties, firewall
persistence, enforcing AppArmor/SELinux state, and the native WAF package's
database verification, module/loader metadata, private runtime ownership, and
live `nginx -t`. A local build uses
`infra/host-tests/work/waf-packages-coraza` and
`infra/host-tests/work/vinyl-packages` by default; pass
`-WafPackageDirectory` or `-VinylPackageDirectory` to select other complete
three-target record directories. Select `-InstallMethod archive`, `bootstrap`,
or `native`; the native route also requires `-NativePackagePath`. Use
`-SkipBuild` to reuse an existing matching archive in `dist`. A guest already
mutated by the integration or installer suite is not a clean input; restore a
known clean checkpoint first.

`Test-StackfortCleanInstallersHyperV.ps1` automates the complete three-guest by
three-method matrix. It restores the named immutable clean checkpoint before
every cell and stops each guest in a `finally` block. The bootstrap cell uses
the exact production script through its explicit root-owned local-fixture
qualification seam; normal bootstrap execution remains GitHub-Releases-only.
The recorded matrix is
[2026-09-01-clean-installer-matrix-hyper-v.md](results/2026-09-01-clean-installer-matrix-hyper-v.md).

Add `-RunPhase1Suite` to cross-build and run the complete destructive Phase 1
suite after installation. It covers account and filesystem isolation, byte and
inode quotas, systemd resource limits, managed NGINX reconciliation, static and
redirect domain lifecycles, ACME HTTP-01, TLS staging, cross-account operation
denial, recovery from an injected interrupted NGINX promotion, and bounded
loopback throughput/latency probes for static NGINX and the control API. It now
also qualifies the installed native PHP runtime, a real account-scoped FPM
worker/socket, cross-account PHP file denial, own-root writes, systemd slice
placement, Rocky SELinux write labels, and exact retirement after domain
removal. The suite requires explicit qualification markers, including
`php-account-pool-isolation=passed` and
`php-account-pool-observability=passed`, and both performance records in its
output; their absence fails the harness. Observability requires non-empty
aggregate systemd memory/CPU/task accounting for the active pool and a clean
`missing` state without metrics after retirement. The installed suite also
requires `mariadb-tenant-lifecycle=passed`: it provisions real read/write and
read-only principals for two accounts through the peer-authenticated agent,
proves own access and denied cross-account access/write escalation, replays the
same mutation safely, and verifies grant cleanup after deletion.

The local cache is ignored by Git. `prepare-image.sh` uses only HTTPS, refuses
unknown image IDs, resumes partial downloads, and moves an image into its final
cache path only after checksum verification.

`Test-StackfortFileManagerHyperVVm.ps1` is the focused K-001 qualification
wrapper. It builds one Linux integration binary, runs only the safe
file-manager navigation test on an existing disposable guest, verifies the
machine-readable qualification marker, and powers the VM off again when the
wrapper started it.

`Test-StackfortFileDownloadHyperVVm.ps1` is the focused K-002 qualification
wrapper. It builds the Linux integration test and production agent helper once,
copies both exact artifacts to each selected guest, checks the
`file-manager-download=passed` marker, and returns a VM it started to Off.

`Test-StackfortFileArchivesHyperVVm.ps1` is the focused K-005 qualification
wrapper. It builds one Linux integration binary and one production agent,
reuses those exact artifacts across all selected guests, exercises bounded ZIP
and tar.gz creation/extraction plus the hostile archive corpus, requires the
`file-manager-archives=passed` marker, and returns a VM it started to Off.

`Test-StackfortScheduledJobsHyperVVm.ps1` is the focused K-009 qualification
wrapper. It builds one Linux integration binary, reuses it unchanged on each
guest, verifies all closed calendars and generated units with the installed
systemd tools, executes real Shell and PHP jobs as a disposable hosting
identity, tests the account slice/sandbox and hostile-link fences, requires the
`scheduled-jobs=passed` marker, and returns a VM it started to Off.

`Test-StackfortOCIDeploymentHyperVVm.ps1` is the focused L-005 qualification
wrapper. It builds one Linux integration binary, prepares the guest's rootless
Podman helpers, and exercises the production Quadlet lifecycle with a real
digest-pinned workload: health-gated deploy, loopback-only ingress, replay,
bounded logs, suspend, resume, rollback, removal, secret retirement, and clean
identity teardown. It requires the `oci-deployment-lifecycle=passed` marker and
returns a VM it started to Off. The recorded focused result is
[2026-09-01-oci-deployment-lifecycle-hyper-v.md](results/2026-09-01-oci-deployment-lifecycle-hyper-v.md).

The Phase 6 native carrier qualification builds the passive
`stackfort-release` package twice per format and runs
`packaging/core/test-native-package-lifecycle.sh` as root in each guest. The
closed lifecycle checks package paths, empty maintainer-script/scriptlet
surfaces, prerelease upgrade ordering, ownership, wrapper execution, removal,
and non-interference with active Stackfort payload files. The recorded matrix
is [2026-09-01-native-core-packages-hyper-v.md](results/2026-09-01-native-core-packages-hyper-v.md).

`Test-StackfortUpdateTransactionHyperVVm.ps1` is the focused Phase 6 staged
transaction qualification. It runs the production persistent update engine and
root-owned file journal with a disposable native SQLite fixture. Success proves
the complete ordered transaction, migration commit, health-failure rollback,
and recovery from an interrupted durable journal. It intentionally does not add
a local-release bypass to the production GitHub attestation stager; the next
roadmap item owns complete published-release upgrade matrices.

`Test-StackfortWAFHyperVVm.ps1` is the focused K-010 runtime qualification
wrapper. It exercises WAF off, detection-only, and blocking PL1 through the
production domain lifecycle across static and PHP routes, verifies WAF ordering
before customer and canonical redirects, and proves the ACME bypass, TLS,
failed-candidate rollback, AppArmor/SELinux behavior, and comparable absolute
and relative performance records:

```powershell
.\infra\host-tests\Test-StackfortWAFHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

`Test-StackfortCacheHyperVVm.ps1` is the focused K-013/K-014 final wrapper. It
cross-builds one integration binary, verifies the exact release WAF/Vinyl
package hashes and guest MAC address, resolves a running guest through KVP or a
MAC/neighbor fallback, and executes one bounded cache test. Success requires
the Vinyl sandbox/loopback, personalization isolation, WAF ordering/exceptions,
scoped purge/metrics, and all nine performance markers: uncached PHP, NGINX
FastCGI cache, and Vinyl with WAF off, DetectionOnly, and Blocking PL1. The
FastCGI path uses the same access-log format and exact installed WAF profiles;
a generation marker and blocking SQLi probe prevent measurements through an
old NGINX worker. A VM started by the wrapper is returned to Off.

```powershell
.\infra\host-tests\Test-StackfortCacheHyperVVm.ps1 `
  -ImageId rocky-10 -VmName stackfort-rocky-10
```

Run the same command with `debian-13`/`stackfort-debian-13` and
`ubuntu-26.04`/`stackfort-ubuntu-26-04-v2` for the complete matrix. The recorded
result is [2026-08-31-vinyl-cache-hyper-v.md](results/2026-08-31-vinyl-cache-hyper-v.md).

`evaluate-pagespeed-nginx.sh` is a Debian 13 evaluation harness, not a release
qualification or installer. On a disposable guest with the externally
installed, signed `nginx-module-pagespeed` 1.15 package and its explicitly
listed prerequisites, it compares direct PHP, warm Vinyl, warm NGINX FastCGI
cache, and PageSpeed/Cyclone under all three WAF modes. It also proves that
HTTPS approach 2 rewrites through a fixed loopback origin and records the
resulting request/body-byte shape. See the
[evaluation record](results/2026-09-01-mod-pagespeed-nginx-evaluation.md) for
the exact package versions, licensing boundary, configuration, and command.

## Required VM shape

Each disposable VM must have at least:

- 2 virtual CPUs, 4 GiB RAM, and 20 GiB system storage;
- a separate test filesystem mounted at `/srv/stackfort-quota` with project
  quotas active;
- systemd as PID 1 and the unified cgroup v2 hierarchy;
- CPU, memory, I/O, and process-count cgroup controllers;
- AppArmor enabled on Debian/Ubuntu or SELinux enabled on Rocky;
- outbound HTTPS for package and release verification;
- no production data, credentials, SSH keys, or reused machine identity.

The installer accepts a nominal 4 GiB guest when Linux reports at least 3.5 GiB
usable `MemTotal`; firmware and kernel reservations differ between the three
distributions.

Run the capability gate inside a freshly booted VM:

```sh
sudo STACKFORT_EXPECTED_OS_ID=debian \
  STACKFORT_EXPECTED_VERSION_PREFIX=13 \
  STACKFORT_QUOTA_PATH=/srv/stackfort-quota \
  bash scripts/host-capabilities.sh
```

Change the expected ID/version to `ubuntu`/`26.04` or `rocky`/`10` for the
other images.

## GitHub runner boundary

The [release upgrade matrix](../../docs/upgrade-matrix.md) uses
`Test-StackfortUpgradeMatrixHyperV.ps1` to enumerate every supported predecessor
and restore the dedicated clean checkpoints explicitly. The per-VM driver tests
real release activation, health rollback, and subprocess interruption, and emits
archive-bound evidence for the release publication gate. Local unpublished
rehearsals are recorded separately and cannot satisfy that gate.

`.github/workflows/host-validation.yml` targets self-hosted runners carrying
the labels `ephemeral` and the exact distribution label. The runner must be
registered for one job only and the entire VM must be destroyed after the job,
even when validation fails. A long-lived shell runner is not an acceptable
substitute.

Provisioning and one-job runner registration are deliberately not automated
until the GitHub repository exists and its runner-group policy can be defined.
No registration token belongs in this repository or in a reusable VM image.
