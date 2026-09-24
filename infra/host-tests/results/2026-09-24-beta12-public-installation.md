# Beta.12 immutable publication and public installation

Publication and public-download verification **passed** on 2026-09-24. The final
fresh public README-command installation is a separate check and is still
pending at this record's initial publication. Experimental fresh installations
only, not production approval or upgrade support.

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

RTBGG approved [fresh-only scope](../../../docs/release-evidence/2026-09-24-beta12-fresh-install-scope.md)
and [conditional publication](../../../docs/release-evidence/2026-09-24-beta12-publication-approval.md).
The [exact-candidate report](2026-09-24-beta12-candidate-qualification.md) includes
both MBR/optical and GPT installation, resources, WAF/cache, OCI and containment;
[same-target removal](2026-09-24-beta12-os-removal.md) used a completely fresh OS.
No independent security review, production/important data, upgrade guarantee or
in-place uninstaller. Community support only; removal requires full OS reinstallation.
