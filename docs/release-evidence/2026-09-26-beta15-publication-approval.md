# Beta.15 conditional publication authorization

On 2026-09-26 RTBGG requested: "fahre damit fort und veröffentliche anschließend
für den one-line-installer." The [fresh-only scope](2026-09-26-beta15-fresh-install-scope.md)
records the prior experimental limitations and the newly frozen candidate.

This applies only to `0.1.0-beta.15`, source
`d93c4bfb9fb527e80a648eeb8482b159d0076b29`, original build `36260323807`,
attempt 1/artifact `10911968743`, archive SHA-256
`b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`.

Host qualification completed at `2026-09-26T18:22:18Z`.
Evidence finalized at `2026-09-26T18:26:07Z`; this is evidence finalization under
the earlier conditional authorization, not a new human review at that time.
See [candidate results](../../infra/host-tests/results/2026-09-26-beta15-candidate-qualification.md)
and [same-target removal](../../infra/host-tests/results/2026-09-26-beta15-os-removal.md).
Live CI/security/provenance gates remain mandatory. Publication is not claimed here.

Experimental fresh disposable Debian 13 amd64 only, using qualified ext4/GRUB
primary-MBR/BIOS or GPT/UEFI. No production or important data. No independent
security review; bounded tests are not an exhaustive security guarantee.

No upgrades from Beta.10, Beta.11, Beta.12, Beta.13, Beta.14 or any installed
version. No failed upgrade result is relabelled and no predecessor is retired.
Already shipped predecessor updaters are unchanged.

Community-only GitHub issues/private-security-reporting support by RTBGG and
possible volunteers; no guaranteed response, fixes, SLA or support period.
No in-place uninstaller: removal requires full OS reinstallation and irreversibly
removes all server data, configuration and services. Passive package removal
does not remove the platform.

Hosting quotas share root storage. Aggregate capacity admission and a guaranteed
OS disk/inode reserve are absent; exhaustion may require reinstallation.
The original frozen SECURITY.md remains bound to the readiness evidence; its
unpublished notice and historical tag are not rewritten. Public support notices
and the selector advance only after verified immutable publication.
