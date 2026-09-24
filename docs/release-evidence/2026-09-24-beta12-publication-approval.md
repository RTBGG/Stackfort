# Beta.12 conditional publication authorization

RTBGG explicitly authorized continued Beta.12 checks and publication, then
confirmed the [fresh-install-only scope](2026-09-24-beta12-fresh-install-scope.md):
"Beta.12 ebenfalls nur für frische Testserver veröffentlichen; keine Upgrades anbieten."
The authorization is conditional on the remaining technical checks passing.
It applies only to `0.1.0-beta.12`, source
`f4d1a7947af4ffbdc2cbf28d2f618c10833cefec`, original build `36005061649`,
attempt 1/artifact `10811230131`, archive SHA-256
`446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9`.

Remaining host tests completed at `2026-09-24T15:06:39Z`. The machine-readable
decision is finalized at `2026-09-24T15:08:40Z` under that conditional instruction;
this is evidence finalization, not a new human review at that moment. See the
[candidate results](../../infra/host-tests/results/2026-09-24-beta12-candidate-qualification.md)
and [same-target removal](../../infra/host-tests/results/2026-09-24-beta12-os-removal.md).
Live CI/security/provenance gates must still pass. Publication is not claimed here.

## Approved limits and support

- Experimental fresh disposable Debian 13 amd64 servers only, using qualified
  ext4/GRUB primary-MBR/BIOS or GPT/UEFI. No production or important data.
- No independent security review. Bounded tests are not a general security or
  exhaustive isolation guarantee.
- No upgrades from Beta.10, Beta.11 or any other installed version. No older
  failed or unqualified matrix cell is converted into a pass, and no predecessor
  is retired. This release cannot repair already shipped predecessor updaters.
- Community-only GitHub issues/private-security-reporting support by RTBGG and
  possible volunteers; no guaranteed response, fixes, SLA or support period.
- No in-place uninstaller. Removal requires complete OS reinstallation and
  irreversibly removes all server data, configuration and services. Removing a
  passive release package does not uninstall the active platform.
- Hosting quotas share the root filesystem. Aggregate capacity admission and a
  guaranteed OS disk/inode reserve are absent; exhaustion may require reinstallation.

The frozen SECURITY.md described Beta.12 as unpublished. Its exact digest remains
bound by readiness evidence; this later authorization changes publication scope
without rewriting the historical tag. Update the public support notice and
one-line selector only after verified publication. Original packages and genuine
tag-origin attestation remain unchanged.
