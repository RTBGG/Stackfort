# Beta.13 conditional publication authorization

On 2026-09-26 RTBGG requested installation through the one-line installer and
explicitly confirmed the [restricted scope](2026-09-26-beta13-fresh-install-scope.md):
"Ja, gleicher eingeschränkter Test-Beta-Umfang".

This applies only to `0.1.0-beta.13`, source
`991f7df6b27093588a69d0dbea2e3eab7a7ceaae`, original build `36235431432`,
attempt 1/artifact `10904191959`, archive SHA-256
`b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91`.

Host checks completed at `2026-09-26T11:54:13Z`.
Evidence finalized at `2026-09-26T11:57:25Z`; this is not a new human review at that time.
See [candidate results](../../infra/host-tests/results/2026-09-26-beta13-candidate-qualification.md)
and [same-target removal](../../infra/host-tests/results/2026-09-26-beta13-os-removal.md).
Live CI/security/provenance gates remain mandatory. Publication is not claimed here.

## Approved limits

Experimental fresh disposable Debian 13 amd64 only, on qualified ext4/GRUB
primary-MBR/BIOS or GPT/UEFI. No production or important data. No independent
security review; bounded tests are not an exhaustive security guarantee.

No upgrades from Beta.10, Beta.11, Beta.12 or any installed version. No failed
upgrade result is relabelled and no predecessor is retired. Already shipped
predecessor updaters are unchanged.

Community-only GitHub issues/private-security-reporting support by RTBGG and
possible volunteers; no guaranteed response, fixes, SLA or support period.
No in-place uninstaller: removal requires full OS reinstallation and irreversibly
removes all server data, configuration and services. Passive package removal
does not remove the platform.

Hosting quotas share root storage. Aggregate capacity admission and a guaranteed
OS disk/inode reserve are absent; exhaustion may require reinstallation.

Frozen SECURITY.md describes the candidate as unpublished. Its exact digest
remains bound to readiness evidence; the historical tag is not rewritten.
Public support notices and the installer selector may advance only after
verified immutable publication of original packages and genuine attestation.
