# Beta.13 public release and installation

Date: 2026-09-26. Status: **published; public downloads verified; final literal
README installation test pending**. This report distinguishes public transport
checks from the completed [exact-candidate host qualification](2026-09-26-beta13-candidate-qualification.md).

## Publication and unchanged identity

- Version/tag: `0.1.0-beta.13` / `v0.1.0-beta.13`.
- Frozen source: `991f7df6b27093588a69d0dbea2e3eab7a7ceaae`; neither rebuilt nor retagged.
- Evidence/publication implementation: `0f8d904d563313c608defa932b830a7606b36805`.
- [Verification-only run 36240549529](https://github.com/RTBGG/Stackfort/actions/runs/36240549529)
  passed all unchanged technical gates, all 162 Node contracts and six Python extractor tests.
- [Publication run 36240644409](https://github.com/RTBGG/Stackfort/actions/runs/36240644409)
  completed successfully after the same validation.
- [Public release](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.13):
  ID `397220223`, published `2026-09-26T12:02:43Z`, not draft, prerelease, immutable.
- Explicit [fresh-only approval](../../../docs/release-evidence/2026-09-26-beta13-publication-approval.md)
  and [scope](../../../docs/release-evidence/2026-09-26-beta13-fresh-install-scope.md)
  are retained. No upgrade gate is represented as passed or any other technical gate waived.

## Public downloads

Unauthenticated HTTPS downloads of all 14 published assets passed exact filename,
size and SHA-256 checks against GitHub's uploaded-asset metadata. All 12 original
tag-qualified files remain byte-identical, including the original ten build
files, promotion record and tag-bound attestation. The two publication receipts
bind this exact candidate and evidence commit with fresh-only restrictions.
GitHub-normalized carrier names were saved with their checksum inventory names;
no package bytes or signed/checksummed inventories were changed.

| Item | SHA-256 |
| --- | --- |
| Linux amd64 archive | `b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91` |
| Standalone installer | `ce214102248c7e5ab8069ef7927542b5b63cf7a6ddea3744afd3d38891ad36d2` |
| SHA256SUMS | `b26b565cbc3a6f07edf5a4878c8b1113e22f3535019c1c362cecc3df0bda6f77` |
| Tag-bound attestation | `7c2219d067fe4cd6471ba5b70c7b65e86b61c9c07ca2de41fbc4dba2db06d6ea` |

The redacted local receipt `public-download-verification.json` retains all 14
asset hashes, sizes and checks. Provenance verification and original live CI
checks also passed before publication; downloading a file alone is not treated
as proof of its origin.

## Literal public installation

Pending: run the README's `main` bootstrap URL on the independently freshly
reprovisioned GPT/UEFI Debian 13 fixture, without `STACKFORT_VERSION` or fixture
origin overrides. Do not count the retained-fixture transport runs as this test.
Original setup/login, installed hosting/API smoke, same-release rerun and normal
reboot/persistence must pass before this section records completion.

## Limits

Fresh disposable Debian 13 amd64 qualified GPT/UEFI or primary MBR/BIOS ext4/GRUB
hosts only. No upgrades, production use, important data or independent security
review. Support is community-only; removal requires complete OS reinstallation.
Interrupted older installations must not have journals reset or be retried.
The release does not provide public conversion resume or automatic recovery.
Shared-root disk/inode exhaustion remains an availability limitation. These
checks do not qualify every provider image or the default Ubuntu/Rocky root path.
