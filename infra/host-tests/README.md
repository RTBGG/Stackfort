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
to a clean guest, runs the production installer twice, requires an unchanged
journal and `alreadyInstalled=true` on the second run, and independently checks
file metadata, service health, systemd sandbox properties, firewall
persistence, enforcing AppArmor/SELinux state, and the native WAF package's
database verification, module/loader metadata, private runtime ownership, and
live `nginx -t`. A local build uses
`infra/host-tests/work/waf-packages` by default; pass `-WafPackageDirectory`
to select another complete three-target record directory. Use `-SkipBuild` to
reuse an existing matching archive in `dist`. A guest already mutated by the integration
or installer suite is not a clean input; restore a known clean checkpoint
first.

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

`.github/workflows/host-validation.yml` targets self-hosted runners carrying
the labels `ephemeral` and the exact distribution label. The runner must be
registered for one job only and the entire VM must be destroyed after the job,
even when validation fails. A long-lived shell runner is not an acceptable
substitute.

Provisioning and one-job runner registration are deliberately not automated
until the GitHub repository exists and its runner-group policy can be defined.
No registration token belongs in this repository or in a reusable VM image.
