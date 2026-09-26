# Public one-line installation readiness

Audit date: 2026-09-26. Status: **experimental public Debian 13 beta available**.
[Beta.13](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.13) is the
current immutable fresh-install-only prerelease. All 14 public assets were
downloaded and verified; the 12 tag-qualified original files are byte-identical.
It fixes rejection of [larger valid initramfs inventories](native-installer-initrd-inventory.md).
Exact-candidate MBR/BIOS and GPT/UEFI onboarding, installed-host/resource/isolation,
WAF/cache, real OCI, process-loss containment, same-target OS removal, original
live CI/security and provenance gates passed. No upgrades from any installed release.
The final literal README command test against public GitHub is pending; retained
candidate tests are not represented as that transport test.
See [publication and public installation evidence](../infra/host-tests/results/2026-09-26-beta13-public-installation.md).
Stopped older installations must not be reset or blindly retried. Historical
qualification does not establish support for every provider image.

### Earlier public Beta.12

[Beta.12](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.12) was published
on 2026-09-24 with the narrow inactive optional optical-media exception.
Its [public README installation, setup, API, rerun and reboot test](../infra/host-tests/results/2026-09-24-beta12-public-installation.md)
passed on its fresh GPT/UEFI fixture. A later field report exposed its 64 KiB
initramfs-inventory limit, corrected by the current release without enabling
recovery or upgrades.

### Earlier public Beta.11

[Beta.11](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.11)
is an earlier immutable **fresh-install-only** prerelease. All 14 public assets
were downloaded and verified; all 10 original build files remain byte-identical.
It supports the qualified primary MBR/BIOS and GPT/UEFI ext4/GRUB profiles below.
No upgrade from Beta.10 or another installed release is supported.
The [public-GitHub fresh installation, setup, API, rerun and reboot test](../infra/host-tests/results/2026-09-24-beta11-public-installation.md)
also passed on a fresh GPT/UEFI test host using the unchanged release.

### Earlier public release

[Beta.10](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.10)
is a public immutable prerelease. All 14 assets were downloaded and verified;
the [public-GitHub fresh installation, setup, rerun and reboot test](../infra/host-tests/results/2026-09-24-beta10-public-installation.md)
passed. This is not production approval or qualification of arbitrary hosts.

The [exact beta.10 candidate](../infra/host-tests/results/2026-09-19-beta10-candidate-qualification.md)
has now passed fresh Debian13 native onboarding with genuine tag provenance,
original setup/login, installed hosting/API checks, same-release rerun, normal
reboot/session persistence, host security, CPU/RAM/PID/byte/inode enforcement,
installed-broker rootless build/Trivy/deploy, real process-loss quarantine with
external IPv4/link-local-IPv6 TCP/UDP controls, and
[same-target full OS reprovision removal](../infra/host-tests/results/2026-09-19-beta10-os-removal.md).
These used the exact retained archive, not repaired installed binaries.
Candidate-specific [RTBGG publication approval](release-evidence/2026-09-24-beta10-publication-approval.md)
and the live release gates passed before publication. The public transport test
then passed against the unchanged archive. No independent review or production
suitability is implied. The report explicitly identifies narrower coverage, including no
global IPv6, I/O-rate enforcement or public-control-API OCI workflow claim.

## Earlier candidate findings

The [2026-09-24 Beta.11 qualification](../infra/host-tests/results/2026-09-24-beta11-candidate-qualification.md)
passed complete fresh Debian MBR/BIOS and GPT/UEFI onboarding, original setup/login,
installed API smoke, rerun and normal reboot. All three predecessor
upgrade attempts stopped at Beta.10's incorrect expectations for GitHub-normalized
DEB/RPM filenames, before any upgrade scenario. The stager correction is in later
source only, not in Beta.11. RTBGG authorized
[fresh-only publication without upgrade support](release-evidence/2026-09-24-beta11-fresh-install-scope.md).
All remaining technical gates subsequently passed: host security, resource and
tenant isolation, WAF/cache, rootless OCI, process-loss quarantine on both profiles,
and actual same-target GPT OS reprovision/removal. Beta.11 was published unchanged.
No predecessor was globally retired, no upgrade success was claimed, and no
remaining technical gate was waived.

The [exact beta.4 onboarding attempt](../infra/host-tests/results/2026-09-12-beta4-onboarding-php-capability-failure.md)
completed automatic quota preparation, installation and original setup redemption,
but failed the product smoke because the installed PHP package was not detected.
That tag remains unchanged and is not approved for publication. The subsequent
[exact beta.5 attempt](../infra/host-tests/results/2026-09-12-beta5-onboarding-agent-sandbox-failure.md)
passed PHP detection but failed its first hosting-account operation because the
privileged agent could not update Linux account files. Follow-up installed-agent
diagnostics also exposed umask-sensitive hosting/transaction traversal and an
inherited read-only cgroup view preventing a real Containerfile `RUN`. These
findings are corrected in source with regression tests; the modified diagnostic
installation is not release qualification. Beta.5 remains unpublished and its
tag unchanged. The [exact beta.6 candidate](../infra/host-tests/results/2026-09-19-beta6-candidate-qualification.md)
passed its retained build and tag attestation, but fresh onboarding stopped
before reboot at prerequisite planning. The minimal vendor image exposed a
missing `libjansson4` dependency allowance and an invalid recovery-record update
after a rejected package plan. Both have focused regression fixes; beta.6 is
not publishable and its tag remains unchanged. The subsequent
[beta.7 attempt](../infra/host-tests/results/2026-09-19-beta7-candidate-qualification.md)
completed prerequisite installation and automatic storage preparation, but
failed during Vinyl VCL compilation because its package omitted the runtime
C compiler and headers. Beta.7 also remains unpublished with an unchanged tag.
The [beta.8 attempt](../infra/host-tests/results/2026-09-19-beta8-candidate-qualification.md)
passed those new compiler-free checks and completed fresh automatic storage
preparation, all installation stages, original setup redemption/login and API
account provisioning. Its first domain activation failed because NGINX's default
server-name hash bucket was too small for the valid hostname and its `www` alias.
The defect is reproduced and fixed in subsequent source with real-NGINX boundary
tests, without modifying the installed candidate. Beta.8 remains unpublished and
its tag unchanged. The [beta.9 attempt](../infra/host-tests/results/2026-09-19-beta9-candidate-qualification.md)
passed native installation, original setup/login, account provisioning and both
domain activations, then exposed a staged-upload permission defect: its private
staging inode did not inherit the document root's default ACL when renamed, so
NGINX could not read the uploaded file. Its installed state and tag remain
unchanged. The correction covers upload/copy/archive staging and preservation of
live directory policies during backup restore, with real Linux ACL regressions.
Beta.10 subsequently passed the full fresh-host sequence recorded above;
installation-stage success alone was not accepted as working hosting functionality.

The [2026-09-19 installed-broker diagnostic](../infra/host-tests/results/2026-09-19-oci-layout-scan-diagnostic.md)
also corrected the OCI TAR/directory mismatch at the Trivy boundary. A real
rootless build, unchanged Trivy scan, source-bound replay/conflict, non-root
deployment and test-owned cleanup now pass on the modified diagnostic host.
This closes that concrete scanner defect, not the exact-candidate installation,
resource/isolation, reboot, failure-recovery or OS-removal qualification gates.

The subsequent [resource/isolation diagnostic](../infra/host-tests/results/2026-09-19-resource-isolation-diagnostic.md)
passed actual CPU throttling, PID exhaustion, account OOM enforcement, byte/inode
quota exhaustion and fixed unprivileged cgroup/kernel-interface open denials.
It used the installed broker but remains modified-host evidence; the same
checks were subsequently repeated against the exact beta.10 fresh installation.
The implementation commit's GitHub CI and Security workflows also passed.

## Qualification sequence and remaining boundaries

The following sequence governs publication. Exact-candidate evidence is
linked above; this checklist does not broaden those reports' tested scope or
waive remaining production, recovery, capacity and browser-review limitations.

1. Retain and extend native boot-safety qualification: isolated initramfs construction,
   subprocess containment, operation-bound boot authorization, pre-write offline
   artifact checks, guarded finalization and normal post-install kernel updates.
   Ordinary prior-OS package writers cannot survive the reboot into the unmounted
   conversion initramfs; checked boot drift rejects before metadata writes.
   A persistent global maintenance fence is not an established filesystem-safety
   prerequisite. Quiescing maintenance can avoid failed boots/quarantine; retain
   drift and process-loss tests without claiming hermetic privileged-hook safety.
   The [internal private-image/kernel cycle](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md)
   passed using beta.3 payloads plus a separately pinned installer. This does not
   qualify the final archived installer/public onboarding path.
2. Verify the native installation headroom check (at least 8 GiB free root space
   and 100,000 free inodes) and actual account project-quota enforcement. Disclose
   that this is not a durable OS disk/inode reserve: aggregate account admission,
   bounded unlimited requests and platform/cache/backup/image usage remain a
   production blocker. The explicitly disposable, nonproduction experimental
   beta may expose this documented availability limitation; it must not promise
   shared-root capacity protection or treat CPU/RAM reservations as disk reserves.
3. Qualify the actual listener/firewall boundary at startup, failure, reboot and
   nftables reload. Preserve loopback-only managed OCI publication; reject
   unsupported existing firewall/container configurations explicitly.
   The isolated dedicated-table/admission start/reload/stop checks now pass;
   Beta.10 additionally passed the real positive-host, ingress/reboot and
   process-loss checks documented in the exact-candidate report.
4. Qualify the implemented public `onboard` dispatcher against the exact candidate.
   Bootstrap now selects that path for eligible Debian preflight blockers or
   existing native state; it authenticates native staging, binds real-terminal
   fresh-disposable/reboot consent and refuses partial-state shortcuts. Completed
   reruns use the sealed runtime with systemd-supervised live admission checks.
   The seven isolated shell-routing markers and real Beta.10 tagged onboarding
   pass. `storageprep.CheckInactive` and strict tag origin remain enforced.
5. Qualify the implemented terminal-only setup handoff end to end: `SAVED`
   acknowledgement before preparation, digest-only operation/release binding and
   one-hour activation after installed-payload/service checks. Reboots/reruns
   never reissue or renew the code. Source tests are not real tagged browser
   redemption evidence; retain completed-rerun and update qualification too.
6. Build one immutable complete candidate with its actual archived installer,
   checksums, SBOM and provenance. Qualify that exact archive end to end on each
   advertised installation profile; separately retain failure/recovery evidence.
7. Satisfy the public-beta policy: independent security review or the expressly
   authorized experimental-beta contract with honest missing-review disclosure, exact
   released-version/deployment scope under community-only support, remaining product
   and class-specific removal qualification,
   and explicit publication approval. Automated or agent reviews do not invent
   an independent approval or support promise.
8. Publish the tested immutable assets, verify public downloads/provenance/channel
   discovery, and exercise the actual README command on a fresh disposable host.

## Scope

The public Beta.13 native conversion profile is Debian 13 amd64, fresh disposable
plain GPT/UEFI or active primary MBR/BIOS ext4 root with GRUB.
Ubuntu/Rocky native conversion, LVM/RAID, separate
persistent boot/state filesystems and retained-data conversion are not qualified.
See the exact [primary MBR/BIOS limits](native-installer-mbr.md).
Beta.10 remains GPT-only; its historical evidence does not qualify MBR.
Existing installation on already quota-prepared storage is a separate path; do
not describe those tests as native fresh-root qualification.

### Experimental capacity limitation

Accounts and platform services share the root filesystem. Individually valid
account limits, unlimited requests, database/log/cache/backup/image growth or
aggregate tenant usage can exhaust root disk space or inodes and make the server
unavailable. The initial headroom check does not prevent this during operation.
Durable OS reserves, aggregate admission and bounded platform usage must be
implemented and qualified before a production multi-tenant capacity claim.
The experimental release notes and deployment/support limits must disclose this
known limitation; disposable-only policy does not waive quota, isolation,
fail-closed boot, firewall or exact-candidate functional tests.

### Bootstrap selection and availability

The bare bootstrap now explicitly selects `0.1.0-beta.13` for a fresh invocation,
not GitHub's latest-stable channel. Explicit versions and existing journal pins
remain supported; a beta is never relabeled stable. The public handler is
registered; Beta.13's exact tagged-candidate qualification and public immutable
release assets are complete. Upgrades are not supported.
The [installation guide](installer-installation.md) documents
the real-terminal consent/reboot/setup flow and completed-rerun restrictions.

The current carrier wrapper also cannot be advertised as native conversion:
native eligibility rejects an installed `stackfort-release` package. Forwarding
read-only `native-host` does not authorize adopting the carrier or existing data.

## Kernel lifecycle issue resolved internally

The original permanent conversion-era kernel/initrd/GRUB pins have been replaced
by an explicit [completed-conversion lifecycle](native-installer-kernel-lifecycle.md).
Pre-ready pins remain exact; only a recorded verified conversion can delegate
normal boot artifacts to the OS while retaining live storage/service checks.
The real Debian kernel update/reboot passed in the internal qualification above,
with no changed storage journal, reconversion or reinstallation. Public activation
still requires the exact tagged candidate's tests, not disabling OS updates or
silently removing readiness checks.

## Evidence and policy

The maintainer clarified that no professional paid audit is funded. Support is
community-only via GitHub by RTBGG and possible future contributors, without
guaranteed responses, fixes or support periods. This is not independent review
evidence. On 2026-09-12 the maintainer separately authorized an experimental beta
without independent review, only for fresh disposable test servers without
important data and never production. The [versioned contract](../packaging/releases/README.md)
records that policy, requires explicit `not-performed` disclosure and actual
candidate-specific approval, and retains every technical gate. No candidate
approval or independent reviewer is invented. Voluntary independent review
remains welcome.

The maintainer separately approved complete OS reinstallation as the experimental
beta's only removal method on 2026-09-12. It destroys all server data,
configuration and services and provides no in-place uninstall. The
[removal contract](experimental-beta-removal.md) requires a real same-target
full-OS reprovision after exact-candidate activation; package removal or snapshot
rollback alone cannot pass. Reviewed releases retain active-uninstall tests.
This policy change requires a new source candidate and build; the earlier
`74feaf628e39e8a809bc9a8899fc0af0bb127ff0` candidate is rehearsal only, not evidence
that the revised contract passed.

The first native fresh-default beta scope is explicitly Debian 13 amd64 only;
Ubuntu/Rocky native paths remain unadvertised until qualified. The
[two-phase tag promotion](../packaging/releases/PROMOTION.md) retains exact build
artifacts and produces unpublished tag provenance before qualification gates,
avoiding both a public-release prerequisite and nondeterministic package rebuilds.

- [Current boot/package boundaries](native-installer-package-coordination.md)
- [Private-image and kernel-lifecycle internal qualification](../infra/host-tests/results/2026-09-12-native-private-image-kernel-lifecycle.md)
- [Linux release gate and exact-promotion tests](../infra/host-tests/results/2026-09-12-release-readiness-validation.md)
- [Earlier process-lifetime package-guard qualification](../infra/host-tests/results/2026-09-12-native-package-guard.md)
- [Recovery policy](native-installer-recovery-policy.md)
- [Maintainer release checklist](release-checklist.md)
- [Security release gates](security.md#6-security-release-gates)

This audit is a work checklist, not an authorization to convert arbitrary hosts,
publish unqualified assets, reset interrupted storage operations, or waive release
policy. Completed implementation items require their own retained test evidence.
