# ADR 0059: Keep native core packages passive and versioned

- Status: accepted
- Date: 2026-09-01
- Extends: [ADR 0033](0033-journaled-idempotent-fresh-host-installation.md)

## Context

Phase 6 requires versioned DEB and RPM artifacts without creating a second,
package-manager-specific host mutation path. A package that writes active
Stackfort binaries, configuration, identities, or services directly would
bypass the existing preflight, journal, convergence checks, and later staged
update/rollback design. Debian packages also must not place owned files below
`/usr/local`, while Stackfort's fresh-host installer currently owns active
files there deliberately.

Coraza and Vinyl cannot be folded into one portable core package: their native
payload and dependencies are qualified against exact distribution revisions.

## Decision

1. Publish one `amd64` `stackfort-release` DEB for Debian 13 and Ubuntu 26.04,
   plus one `x86_64` RPM for Rocky Linux 10.
2. Install the complete, already manifested release tree at
   `/usr/lib/stackfort/releases/<semantic-version>` and a thin administrator
   entry point at `/usr/sbin/stackfort-install`.
3. Keep both packages free of maintainer scripts and RPM scriptlets. Package
   installation must not create identities, start services, change
   configuration, or touch customer/runtime state. The wrapper invokes the
   embedded, journaled installer only after the operator separately requests
   preflight or confirmed installation.
4. Keep distribution-bound WAF and Vinyl artifacts inside the manifested
   release tree. The installer selects and verifies the exact host package.
5. Normalize timestamps and ownership, disable RPM payload mutation, extract
   every built package, and compare its complete release tree, modes, wrapper,
   metadata, and checksums before publication. Map semantic prereleases to the
   native `~` ordering convention.
6. Removing `stackfort-release` removes only the carrier source and wrapper. It
   is not a Stackfort uninstaller and must never remove configured services or
   customer data.

## Consequences

- DEB/RPM users get native inventory, integrity verification, version ordering,
  and a short manual entry point without weakening the fresh-host safety model.
- Installing or upgrading the carrier alone does not change a running
  Stackfort instance. A future updater can stage and activate releases with its
  own migration, health, and rollback journal.
- Package-manager removal is intentionally non-destructive. A separately
  designed uninstaller remains required for host configuration and retained
  data.
- The carrier temporarily duplicates release files before a fresh install;
  this storage cost buys a stable verified source and clean package ownership.

## References

- <https://www.debian.org/doc/debian-policy/ch-opersys.html>
- <https://www.debian.org/doc/debian-policy/ch-maintainerscripts.html>
- <https://www.debian.org/doc/debian-policy/ch-files.html#configuration-files>
- <https://rpm.org/docs/4.20.x/manual/spec.html>
- <https://rpm.org/docs/latest/manual/file_triggers.html>
- <https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/html-single/packaging_and_distributing_software/index>
