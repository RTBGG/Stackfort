# Beta.12: maintainer-authorized fresh-install-only scope

Recorded 2026-09-24. RTBGG explicitly authorized continued Beta.12 release
qualification and publication, then confirmed: "Beta.12 ebenfalls nur für
frische Testserver veröffentlichen; keine Upgrades anbieten."
Publication remains conditional on the remaining technical checks passing.
This is not a fabricated test result or a waiver of a failed technical check.

The decision applies only to this frozen candidate:

- Version: `0.1.0-beta.12`.
- Source/tag target: `f4d1a7947af4ffbdc2cbf28d2f618c10833cefec`.
- Archive SHA-256: `446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9`.
- Original retained build: run `36005061649`, attempt 1, artifact `10811230131`.
- Retained ZIP SHA-256: `8cf13cd7084cc1a21497a1017bbd45225cdf924347fa4ee95fc66e6906dca485`.

## Limits

Experimental Debian 13 amd64 only, on fresh disposable test servers without
important data. Not for production. No independent security review has been
performed. Community support is through GitHub issues and private security
reporting, with no guaranteed response, fixes, SLA or support period.
Removal requires complete OS reinstallation, destroying all server data;
removing a passive package does not uninstall the active platform.

No upgrade from Beta.10, Beta.11 or another installed release is supported.
Do not select Beta.12 in the updater or run its installer over an older
installation. The predecessor asset-name mismatch remains an unresolved
transition limitation; the later source correction does not repair already
shipped predecessor updaters. No predecessor is retired and no failed or
unqualified upgrade-matrix cell is represented as passed.

## Publication requirements remain

The exact retained candidate must pass source CI/security and complete fresh
installation, rerun, reboot, installed-host security, tenant/resource isolation,
quota enforcement, WAF/cache, actual rootless OCI, process-loss containment and
same-target full-OS reprovision removal checks. Missing or failed results still
block publication. Preserve the original tag, archive and genuine provenance;
never rebuild or replace them under the same version.

A narrowly bound fresh-install-only publication path may apply this explicit
decision while retaining the unchanged candidate readiness validator and all
other technical gates. It must disclose the absence of upgrade support rather
than create a passing upgrade-matrix receipt. The public one-line default may
change only after immutable release assets exist and public downloads pass
verification.
