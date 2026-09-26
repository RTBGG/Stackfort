# Beta.15: maintainer-authorized fresh-install-only scope

Recorded 2026-09-26. RTBGG requested: "fahre damit fort und veröffentliche
anschließend für den one-line-installer." This continues the approved Beta.14
product fixes and authorizes completing the package-handoff fix, qualifying a
new candidate and publishing it for the one-line installer. The new frozen
candidate is Beta.15; the previous Beta.14 tag is not moved or replaced.

This is conditional publication authority under the previously confirmed
experimental test-beta limits, not a technical pass or independent security
approval. It applies only to this frozen retained candidate:

- Version: `0.1.0-beta.15`.
- Source/tag target: `d93c4bfb9fb527e80a648eeb8482b159d0076b29`.
- Archive SHA-256: `b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`.
- Original build: run `36260323807`, attempt 1, artifact `10911968743`.
- Retained ZIP SHA-256: `309ae1aa1cb4711eaa0f39c4a4730df871e0d3d6a6a280427fceb80144ea620b`.

## Limits

Experimental fresh disposable Debian 13 amd64 test servers only, on qualified
ext4/GRUB primary-MBR/BIOS or GPT/UEFI storage. No production or important data.
No independent security review has been performed. Community-only support is
provided through GitHub issues and private security reporting, with no
guaranteed response, fixes, SLA or support period.

No upgrade from Beta.10, Beta.11, Beta.12, Beta.13, Beta.14 or any other installed
release is supported or qualified. Do not use the updater or run Beta.15 over an
existing installation. No predecessor is retired and no failed/unqualified
upgrade cell is converted into a pass. The historical predecessor asset-name
transition limitation is not repaired in already shipped predecessor updaters.

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
