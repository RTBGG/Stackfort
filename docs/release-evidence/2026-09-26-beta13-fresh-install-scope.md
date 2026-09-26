# Beta.13: maintainer-authorized fresh-install-only scope

Recorded 2026-09-26. RTBGG requested: "Ermögliche bitte die installation von beta 13 über den one liner."
After being asked explicitly about the same experimental fresh-Debian-13-only
scope, absent upgrade support/independent review, community support and complete
OS-reinstallation removal, RTBGG confirmed:
"Ja, gleicher eingeschränkter Test-Beta-Umfang".

This is conditional publication authority, not a technical pass or independent
security approval. It applies only to this frozen retained candidate:

- Version: `0.1.0-beta.13`.
- Source/tag target: `991f7df6b27093588a69d0dbea2e3eab7a7ceaae`.
- Archive SHA-256: `b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91`.
- Original build: run `36235431432`, attempt 1, artifact `10904191959`.
- Retained ZIP SHA-256: `38895891e3641466319f17e8b962780134e814d23fc7b60a0420e3cd1f6d8ea7`.

## Limits

Experimental fresh disposable Debian 13 amd64 test servers only, on qualified
ext4/GRUB primary-MBR/BIOS or GPT/UEFI storage. No production or important data.
No independent security review has been performed. Community-only support is
provided through GitHub issues and private security reporting, with no
guaranteed response, fixes, SLA or support period.

No upgrade from Beta.10, Beta.11, Beta.12 or any other installed release is
supported or qualified. Do not use the updater or run Beta.13 over an existing
installation. No predecessor is retired and no failed/unqualified upgrade cell
is converted into a pass. The historical predecessor asset-name transition
limitation is not repaired in already shipped predecessor updaters.

No in-place uninstaller exists. Removal requires complete OS reinstallation,
irreversibly removing all server data, configuration and services. Removing a
passive package does not uninstall the active platform. Hosting quotas share
root storage; aggregate capacity admission and a guaranteed OS disk/inode reserve
are absent.

## Technical requirements remain mandatory

The unchanged candidate readiness policy and validator must verify genuine
source CI/security, fresh installation, rerun/reboot, host security, resource and
tenant isolation, quota enforcement, WAF/cache, real rootless OCI, process-loss
containment and same-target complete OS reprovision removal. Missing or failed
checks block publication. Only upgrade qualification is explicitly out of scope;
the fresh-only publication path must disclose that absence.

Keep the original tag, packages and genuine tag-origin attestation unchanged.
Advance the public one-line selector only after immutable release assets exist
and their public downloads match the retained bytes.
