# ADR 0033: Journaled idempotent fresh-host installation

Status: Accepted

Date: 2026-08-24

## Context

Stackfort's preflight describes a substantial privileged mutation: packages,
an operating-system identity, immutable release files, systemd units, mandatory
access-control policy, firewall state, NGINX, and two application services. A
lost SSH connection, process termination, package-manager failure, or power
loss can occur between any two of those changes. Replaying an unrecorded shell
transcript would risk replacing unrelated host state or duplicating a partially
completed action.

The initial installer is deliberately limited to fresh `amd64` hosts. It is not
an upgrade, uninstall, or general repair mechanism.

## Decision

1. A release is an extracted, root-owned, non-writable tree with a semantic
   `VERSION`, required metadata, bounded regular files, and static 64-bit ELF
   API and agent binaries for the running architecture. The installer computes
   a deterministic SHA-256 tree digest and binds the journal to both version
   and digest.
2. Installation uses one fixed ordered stage list: packages, service identity,
   release payload, system configuration, security policy, NGINX baseline, and
   services plus health checks. There is no caller-controlled command or path
   dispatch.
3. `/var/lib/stackfort-installer/install-state.json` is an atomic, fsynced,
   root-owned `0600` journal below a root-owned `0700` directory. A non-following
   `0600` lock file and `flock` exclude concurrent installers.
4. The engine records `applying` before a stage mutates the host and records
   `complete` only after the stage-specific verifier succeeds. A failed or
   interrupted `applying` stage is converged again on the next invocation;
   already completed stages are verified and skipped. Package installation is
   not rolled back because package-manager rollback is less reliable than
   convergent resumption.
5. A complete journal causes a verification-only run. It does not rewrite the
   journal or increment attempt counters and reports `alreadyInstalled=true`
   and `changed=false`. A different release or distribution is refused; that
   transition belongs to the future updater.
6. Installer-owned files are created atomically with exact ownership and modes.
   Generated configuration may replace only content carrying Stackfort's
   managed header. Immutable binaries and web assets conflict rather than
   overwrite when their content differs.
7. The API runs as locked user `stackfort`; the agent remains root but both are
   placed in `stackfort-core.slice` and hardened with systemd sandbox settings.
   Debian and Ubuntu load an enforcing API AppArmor profile and a dedicated
   Stackfort nftables table. Rocky keeps SELinux enforcing, installs persistent
   narrow file contexts, and adds only TCP 80/443 to firewalld.
8. The convenience bootstrap selects a versioned GitHub Release, verifies the
   archive against that release's `SHA256SUMS`, extracts into a private temporary
   directory, and invokes the bundled installer. An existing journal pins a
   retry to its original version. Release workflows publish versioned assets
   without replacing an existing release and attach GitHub build provenance.

## Consequences

- Killing the installer can leave a stage partially applied, but the journal
  records exactly which stage must converge. It never guesses that an
  unverified stage completed.
- Foreign files, symlinks, identities, services, or release content stop the
  installation instead of being silently adopted.
- A user must retain or redownload the exact release while an installation is
  incomplete. The bootstrap does this automatically from the journal version.
- Drift after a completed installation is reported as a verification failure;
  automatic repair and version upgrades require separate, explicitly designed
  workflows.
- Package-manager changes and externally created data are not removed by this
  ticket. A production uninstaller therefore remains out of scope.

## Verification

Pure engine tests inject stage failures and verify restart attempts, immutable
source binding, no-op completion, and preflight-without-journal behavior. The
real release installer was also exercised on disposable Debian 13, Ubuntu
26.04, and Rocky Linux 10 Hyper-V guests. Debian resumed after identity and
health-probe failures; all three guests then passed a byte-identical-journal
second run plus external ownership, systemd, firewall, AppArmor/SELinux, and
service-health assertions.
