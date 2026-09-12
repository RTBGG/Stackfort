# Experimental beta removal

> [!WARNING]
> There is no in-place Stackfort uninstaller for the experimental beta.
> Removal requires a complete operating-system reinstallation. It irreversibly
> removes **all server data, configuration and services**, not just Stackfort.
> Use only fresh disposable test servers without important data.

RTBGG explicitly approved this limited removal scope on 2026-09-12. It is a
product-policy decision, not a claim that a particular release has passed its
removal test. No final candidate-specific removal qualification is recorded yet.
Community-only support does not guarantee recovery or provider reinstallation.

## Operator boundary

Removal is performed outside Stackfort using the hosting provider's full OS
reinstallation facility or an authenticated distribution installer that
completely reprovisions the test server's OS disk. Confirm the exact server,
disk and destructive scope in that operator-controlled workflow. Do not mistake
an update, repair installation or filesystem-preserving reinstall for removal.

Direct deployment of an authenticated complete vendor OS image also qualifies
as OS reinstallation. It must create a fresh independent OS disk, not inherit
from or depend on the former installed disk, and detach every former system,
seed and data disk. Record the image's actual authentication mechanism and do
not describe image deployment as an interactive installer run.

Removing the passive `stackfort-release` DEB/RPM only removes its packaged source
files. It does not uninstall the active platform, undo native quota changes or
remove hosted data. Stopping services, deleting installer journals or individual
directories, and restoring a snapshot are not the supported removal procedure.
Stackfort does not expose a force/reset/wipe shortcut or automatically invoke a
provider reinstall after a failure.

This removal policy is not a backup/restore promise or forensic secure-erasure
guarantee. External backups, provider snapshots and third-party credentials are
outside the local OS reinstall; their existence does not preserve data on the
reinstalled server or make this procedure safe for important data.

## Required candidate qualification

The `experimental-beta` release gate substitutes
`full-system-reprovision-removal` for `active-uninstall`, keeping all other
technical, source/provenance and approval gates. The removal report must show:

1. The exact candidate commit, version, archive SHA and original build artifact
   identity actively installed and admitted on the identified disposable test
   target. Record the installation and service checks before removal.
2. Authentication of the distribution installation media and its exact SHA-256.
   Record the supported distribution/version/architecture and how its origin
   and integrity were verified; a filename alone is not authentication.
3. Actual complete OS disk provisioning and installation on **that same target**,
   using the authenticated installer. For Hyper-V, retain the same VM ID through
   both phases. Snapshot rollback alone or creating another clean VM does not
   demonstrate this removal method.
4. A fresh OS boot afterward, with Stackfort state, managed services and hosting
   data absent. Check the former installer/runtime/admission records, platform
   database/configuration, managed systemd units and hosting paths. Confirm no
   Stackfort service or listener remains; normal distribution services such as
   SSH are not Stackfort leftovers.
5. Real completion timestamps and retained, sanitized observations in a
   digest-bound report, with identical before/after target identity. Never place
   tokens, passwords, database contents or private keys into the report.

The exact machine-readable fields are in the
[readiness contract](../packaging/releases/README.md#evidence-object-fields).
The record and candidate-specific support/publication decisions must explicitly
acknowledge `full-system-reprovision`. No policy default, checked-in example or
synthetic validator fixture supplies a real passing removal result.

Reviewed releases and any ordinary future release class still require tested
active-installation uninstall; this experimental exception does not silently
weaken that requirement or introduce production support.
