# Public one-line installation readiness

Audit date: 2026-09-12. Status: **not ready for public native installation**.
The public GitHub Releases API returned HTTP 200 with an empty inventory.
Unpublished workflow candidates are not public releases.

The [exact beta.4 onboarding attempt](../infra/host-tests/results/2026-09-12-beta4-onboarding-php-capability-failure.md)
completed automatic quota preparation, installation and original setup redemption,
but failed the product smoke because the installed PHP package was not detected.
That tag remains unchanged and is not approved for publication. Beta.5 must be
built and qualified separately after the detector correction.

## Required execution order

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
   the new fresh-host firewall eligibility still needs its real positive-host
   check and the exact candidate's complete ingress/reboot qualification.
4. Qualify the implemented public `onboard` dispatcher against the exact candidate.
   Bootstrap now selects that path for eligible Debian preflight blockers or
   existing native state; it authenticates native staging, binds real-terminal
   fresh-disposable/reboot consent and refuses partial-state shortcuts. Completed
   reruns use the sealed runtime with systemd-supervised live admission checks.
   The seven isolated shell-routing markers pass; real tagged onboarding remains
   pending. `storageprep.CheckInactive` and strict tag origin remain enforced.
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

The implemented native conversion profile is Debian 13 amd64, fresh disposable
plain GPT/ext4 root with GRUB. Ubuntu/Rocky native conversion, LVM/RAID, separate
persistent boot/state filesystems and retained-data conversion are not qualified.
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

The bare bootstrap now explicitly selects `0.1.0-beta.5` for a fresh invocation,
not GitHub's latest-stable channel. Explicit versions and existing journal pins
remain supported; a beta is never relabeled stable. The public handler is
registered, but final tagged-candidate qualification and public release assets
are still pending. The [installation guide](installer-installation.md) documents
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
