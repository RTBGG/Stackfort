# Beta.13 public release and installation

Date: 2026-09-26. Status: **published; public downloads and literal README
installation passed**. This report distinguishes public transport
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

**Passed at `2026-09-26T12:17:36Z`.** The same GPT/UEFI VM used for
[full OS-removal qualification](2026-09-26-beta13-os-removal.md) had an independently
freshly provisioned Debian 13 vendor-image disk graph, not an installed-state
checkpoint rollback. Plain ext4 without project quotas and absence of Stackfort
state were checked before running the actual README command:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh | sudo bash
```

No `STACKFORT_VERSION`, local asset fixture, alternate origin or verification
bypass was provided. The real interactive storage review was identity-checked
and acknowledged only for this disposable VM. Automatic storage preparation,
authorized reboot and installation completed. Original setup redemption, setup
replay rejection and administrator login succeeded. Credentials and raw terminal
transcripts were retained only in process memory, not exported or persisted.

- VM: `stackfort-mbr-builder-debian-13`, ID `55729cf8-11e3-4744-be5b-ddde3824c0e4`;
  DMI `a5db08d7-6c4e-47e0-a381-9bd594b2731a`.
- Fresh pre-install checkpoint: `6c0f9f17-faca-4688-849b-2f1e0f608001`.
- Initial boot: `8aefd50b-f37f-4568-b801-2f1907dadebb`;
  conversion boot: `f9762bfe-a758-4371-8fb1-78861fe37cf5`;
  final normal-reboot boot: `3fee9f21-a6e5-4160-9e15-77f9ab949181`.
- Native operation: `fef758e8-85e5-483c-aa6b-ee9da6aca0a2`.
- Public bootstrap commit: `3ed5aa454094beda09d8b4b9c9dd2eb6aa89d695`;
  SHA-256 `a2a1f6ff2c3d79f11815b1d46d01e6ee0d5cae3929f893256daa903738bfe783`.
  An unauthenticated re-download after completion returned identical bytes.

All seven installed API smoke groups passed:

1. Hosting package/account provisioning.
2. Static/PHP delivery and file upload/download.
3. Domain creation idempotency.
4. Database wizard and resulting inventory.
5. Document-root backup and restore.
6. Per-domain FastCGI enable/disable, HIT/bypass and purge.
7. WAF off/detection/blocking, with blocking enforced before a cache response.

The same-release completed rerun and a subsequent normal reboot both preserved
the original session, package/account/domain records, file and backup digests,
database inventory and static/PHP/WAF/FastCGI behavior. No setup code was reissued
or renewed. The completed rerun is **not** an upgrade from an older release.

The qualification-only external driver was restricted to this exact fresh VM,
source, public bootstrap commit and README pipeline. The API/persistence helper
was unchanged; no installed binary was replaced. Local sanitized receipts are
retained in `work/publication-beta13-20260926/` and
`work/beta13-candidate-20260926/`:

| Evidence | SHA-256 |
| --- | --- |
| Fresh preflight receipt | `8adbeebf96f1470ded066fc732fb1da0a7ef35fdfdecf5517dd107bf0296caee` |
| Public onboarding receipt | `7c1924fe3c33ae09fe4bcc0ecd295dec83a298e1498243237ad8484f0b6bf8b6` |
| All-asset download receipt | `2c1e56bef0c8394a2e44b2066b44630980059e2621f26d452018595ff4757c49` |
| Restricted public onboarding driver | `2310eb776397ce290e72ad20c78065a673860aec6c6f7a3862ccb4e6bf4d751b` |
| Exact-VM wrapper | `55d3d186be269c5117703124b691a17be34524ade35e47742ec298fd37b37045` |
| Unchanged installed API helper | `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196` |

The public transport test does not independently cover MBR, large generic-kernel
inventories, tenant isolation, SQL credential/phpMyAdmin use, OCI deployment,
performance benchmarking or full-account/database backups. The separate exact
retained-candidate MBR/GPT large-inventory, resource/isolation, OCI and failure
containment tests remain attributed to their candidate report, not this run.

## Bootstrap regression and automation

Before advancing the public selector, all eight shell bootstrap qualification
groups passed in a private mount namespace on the still-fresh disposable GPT
guest: production lock, pinned experimental channel, local fixture, native routing,
pinned rerun, fail-closed native evidence, archive boundary and download errors.
The test script SHA-256 was
`f6abfaf5393775f055ebcfc29fc126baf4fb506409859b2d3a7d6608921a9e0b`.
Documentation version contracts and all 968 local Markdown links also passed.

For selector commit `3ed5aa4`, [Security run 36241144553](https://github.com/RTBGG/Stackfort/actions/runs/36241144553)
passed. The Go, Web and Workflow hygiene jobs of
[CI run 36241144477](https://github.com/RTBGG/Stackfort/actions/runs/36241144477)
passed. Its auxiliary artifact build is not used as publication evidence and
does not replace any published candidate byte or its already-passed original
CI/security/provenance evidence. This statement is about those three jobs,
not an unverified overall result of the follow-up workflow.

## Limits

Fresh disposable Debian 13 amd64 qualified GPT/UEFI or primary MBR/BIOS ext4/GRUB
hosts only. No upgrades, production use, important data or independent security
review. Support is community-only; removal requires complete OS reinstallation.
Interrupted older installations must not have journals reset or be retried.
The release does not provide public conversion resume or automatic recovery.
Shared-root disk/inode exhaustion remains an availability limitation. These
checks do not qualify every provider image or the default Ubuntu/Rocky root path.
