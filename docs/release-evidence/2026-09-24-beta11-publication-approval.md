# Beta.11 conditional publication authorization

RTBGG requested publication and explicitly confirmed the
[fresh-install-only scope](2026-09-24-beta11-fresh-install-scope.md), without
Beta.10 upgrades and conditional on completing the other technical tests.
This applies only to `0.1.0-beta.11`, source
`ec117da052b18e051870a224b89bee4b0ddf64e7`, original build `35991617289`,
attempt 1/artifact `10804961581`, archive SHA-256
`d6fec6d5a894d0c120aef5d83829b335a8875b68a181c45a7ad2bb9d139d1304`.

Remaining host tests completed at `2026-09-24T12:17:17Z`. The machine-readable
decision is finalized at `2026-09-24T12:22:37Z` under that conditional instruction;
this is evidence finalization, not a new human review at that moment. See the
[candidate results](../../infra/host-tests/results/2026-09-24-beta11-candidate-qualification.md)
and [same-target removal](../../infra/host-tests/results/2026-09-24-beta11-os-removal.md).
Live CI/security/provenance gates must still pass. Publication is not claimed yet.

## Approved limits and support

- Experimental fresh disposable Debian 13 amd64 servers only, using the qualified
  ext4/GRUB primary-MBR/BIOS or GPT/UEFI profile. No production or important data.
- No independent security review. Bounded tests are not a general security or
  exhaustive isolation guarantee.
- No upgrades from Beta.10 or any other installed version. Nine predecessor
  cells remain unqualified. The later updater fix is not in these packages.
- Community-only GitHub issues/private-security-reporting support by RTBGG and
  possible volunteers; no guaranteed response, fixes, SLA or support period.
- No in-place uninstaller. Removal requires complete OS reinstallation and
  irreversibly removes all server data, configuration and services. Removing a
  passive release package does not uninstall the platform.
- Quotas share the root filesystem. Aggregate admission and a guaranteed OS
  byte/inode reserve are absent; exhaustion may require reinstallation.

The frozen SECURITY.md still described Beta.11 as unpublished. Its exact digest
remains bound by readiness evidence; this later authorization changes publication
scope without rewriting the historical tag. Update the public support notice and
one-line selector only after verified publication. Original packages and genuine
tag-origin attestation remain unchanged.
