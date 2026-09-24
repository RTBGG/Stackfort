# Beta.12 immutable publication and public installation

Publication, public-download verification and the fresh public README-command
installation **passed** on 2026-09-24. The public installation completed at
`15:24:00Z`, including original setup/login, installed hosting/API checks,
same-release rerun and normal reboot/session persistence. Experimental fresh
installations only, not production approval or upgrade support.

## Unchanged candidate and public assets

- [Public release](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.12):
  ID `395812409`, published `2026-09-24T15:14:32Z`, draft=false,
  prerelease=true, immutable=true. It is not promoted to stable/latest.
- [Verification-only run 36018366725](https://github.com/RTBGG/Stackfort/actions/runs/36018366725)
  passed before [publication run 36018591090](https://github.com/RTBGG/Stackfort/actions/runs/36018591090),
  attempt 1, job `107697395810`, passed all steps including release immutability
  and `gh release verify`.
- Publication/evidence commit: `08994c9e924429a99679c7a662e75c8314af6a85`.
  Frozen source/tag: `f4d1a7947af4ffbdc2cbf28d2f618c10833cefec`.
- Original build `36005061649`/attempt 1/artifact `10811230131`; genuine retained
  tag artifact `10810852574` from run `36007265703`/attempt 1. No rebuild,
  tag movement, replacement or substituted provenance.
- All 162 publication-contract tests and both extractors passed on Linux.
  Original readiness validator/policy enforced every remaining technical gate
  against the digest-bound host reports and live exact-source CI/security jobs.
- Cryptographic verification required the original repository/release workflow,
  tag ref, source/signer commit and GitHub-hosted runner.
- `upgrade-support.json` records no supported incoming predecessors and
  `upgradeQualificationPassed=false`; no passing upgrade matrix is invented.

All **14 public assets** were downloaded over HTTPS and verified against exact
GitHub sizes/SHA-256 digests. All ten original build files remain byte-identical.
GitHub normalizes six carrier filenames from `~beta.12` to `.beta.12`; package
bytes/metadata are unchanged and the archive used by the one-line installer is
unaffected. The manual guide explains restoring original inventory filenames.
Receipts are retained under `work/publication-beta12-20260924/`.

| Public input | SHA-256 |
| --- | --- |
| Archive | `446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9` |
| SHA256SUMS | `2a8c1ec7926eafecef293846e64ab585a1a035228fc51e82f1d899ab0d5e70a4` |
| Tag attestation | `67307840e6be9d9ff659fa22d6cb965098f4edc3742b4929a956448ef0659555` |
| Installer | `9a5ff2e3f6db4916c063fd37903cfb30a3975b1d4c3b1f8ca05e74d0e14c8650` |

## Fresh public README-command installation

The same disposable GPT/UEFI VM used for the preceding
[OS removal proof](2026-09-24-beta12-os-removal.md) was independently checked
again as fresh Debian 13 amd64, plain ext4 without quota features or Stackfort
state. The test used a new vendor-image disk graph, not an installed-state
checkpoint rollback. Its fresh checkpoint was retained before this invocation:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh | sudo bash
```

No `STACKFORT_VERSION`, local asset fixture, transport override or verification
bypass was provided. The real interactive storage review was checked and
acknowledged on this disposable machine; automatic preparation, reboot and
installation then completed. The original setup code was used once, its replay
was rejected, and administrator login succeeded. No credentials or raw terminal
transcripts were exported.

- VM: `stackfort-mbr-builder-debian-13`, ID
  `55729cf8-11e3-4744-be5b-ddde3824c0e4`, DMI
  `a5db08d7-6c4e-47e0-a381-9bd594b2731a`.
- Fresh pre-install checkpoint: `dd9deeed-4ae6-4231-951e-abf8ffac5d14`.
- Initial boot: `a7dd89e1-ec3a-4bce-8d0a-32de3349f70a`;
  conversion boot: `8e03398d-2b53-4a01-8fc5-c9a4b655700f`;
  final normal-reboot boot: `5869bac9-3b35-4287-9380-1e36975b7356`.
- Native operation: `7a94a7fc-e498-4ec2-a58e-e5267fc3a495`.
- Public main bootstrap commit: `0e0f2c8a6459c333735a99c67665798af681ecfd`;
  SHA-256 `a85c86093048569791cfad17c83dcc56a20840175d135c8b9f707a57db4f1782`,
  fetched again after completion with identical bytes. The source/tag and payload
  remain `f4d1a7947af4ffbdc2cbf28d2f618c10833cefec` and the archive hash above.

All seven installed API smoke groups passed:

1. Hosting package/account provisioning.
2. Static/PHP delivery and file upload/download.
3. Domain creation idempotency.
4. Database wizard and resulting inventory.
5. Document-root backup and restore.
6. Per-domain FastCGI enable/disable, HIT/bypass and purge.
7. WAF off/detection/blocking, with blocking enforced before a cache response.

The completed same-release rerun and subsequent normal reboot both preserved
the original session, account/domain/package records, file and backup digests,
database inventory, and static/PHP/WAF/FastCGI behavior. This is a same-version
rerun check, **not** an upgrade from an older release.

The external onboarding driver was a reviewed copy restricted to this fresh
GPT VM, the real README pipeline and the independently pinned public bootstrap
commit. The API/persistence helper was unchanged. Neither helper replaced an
installed binary. Local redacted receipts are retained under
`work/publication-beta12-20260924/`:

| Evidence | SHA-256 |
| --- | --- |
| Fresh preflight receipt | `28becbec60fb2ded25f6b29ab4aaf17d72b939cdfb3840e02d7abde3532ad1f8` |
| Public onboarding receipt | `a38eafc61e9259c7d2b8d8ab2ddec144d78f590f97efea5a4058bec2b5ab3ba5` |
| Public asset comparison receipt | `93a4b253ade51c07eca16b8e1570b7e8fda7ea123ddef8a07a5a58024b5db75e` |
| External onboarding driver | `99ae9f817d713ff1ea6871945e30e119ba0f07719b1d4baabaee51b2a6ad1657` |
| Unchanged installed API helper | `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196` |

The public transport test does not independently cover MBR, tenant isolation,
SQL credential/phpMyAdmin use, OCI deployment, performance benchmarking or
full-account/database backups. The distinct retained-candidate MBR/optical,
resource/isolation, OCI and containment checks are recorded in the candidate
report below; these results are not silently attributed to the public test.

## Follow-up bootstrap regression

Pinning the public default to Beta.12 exposed three stale Beta.11 expectations
in the bootstrap test fixture in
[CI run 36019179005](https://github.com/RTBGG/Stackfort/actions/runs/36019179005).
Only those fixture/expected-version values were corrected; no production
bootstrap behavior, release payload, tag or asset was changed. The complete
bootstrap regression then passed at `15:27:09Z` in a private mount namespace on
the disposable MBR lab, with all eight qualification groups passing. Before and
after hashes confirmed the installed journals and installer/API/agent binaries
were unchanged; its prior quarantine was not lifted.

- Test script SHA-256:
  `6b9a0a4fe220c5def7fafd95ee544891465a728525aa0f2438104fa07ea93e6f`.
- Redacted `bootstrap-regression.json` SHA-256:
  `a8cb84d5cc6fe816e33993579c927bdc10f67d65460c99d7a221487eea13426a`.

## Scope and remaining limitations

RTBGG approved [fresh-only scope](../../../docs/release-evidence/2026-09-24-beta12-fresh-install-scope.md)
and [conditional publication](../../../docs/release-evidence/2026-09-24-beta12-publication-approval.md).
The [exact-candidate report](2026-09-24-beta12-candidate-qualification.md) includes
both MBR/optical and GPT installation, resources, WAF/cache, OCI and containment;
[same-target removal](2026-09-24-beta12-os-removal.md) used a completely fresh OS.
No independent security review, production/important data, upgrade guarantee or
in-place uninstaller. Community support only; removal requires full OS reinstallation.
