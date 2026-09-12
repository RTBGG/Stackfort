# Release readiness and exact-promotion validation — 2026-09-12

Result: **the local Linux validator/extraction suites passed**. These are tests
of publication machinery using synthetic isolated fixtures, not passing release
evidence, actual GitHub CI results, independent security review or candidate
publication approval. No promotion/evidence record or public release was created
by this qualification.

## Executed checks

On the Debian 13 Linux qualification host:

```console
node --test scripts/verify-release-readiness.test.mjs scripts/promote-release-candidate.test.mjs
python3 -B scripts/extract-release-candidate.test.py
```

- Node: **110 tests passed, zero failures and zero skips**. Both actual Bash
  wrappers ran against temporary Git repositories and controlled fake GitHub
  responses, using the real local Bash, Git, jq and validators. No real API
  mutation or publication was performed.
- Python: **four tests passed**, including negative subcases for missing,
  changed, duplicate or unexpected files; ZIP digests, unsafe members/checksums,
  source identity, SPDX presence and preservation of an existing destination.
- Windows cross-check: 108 Node tests passed, with only the two Linux wrapper
  tests skipped; the same four Python tests passed. Bash syntax, pinned workflow
  actionlint, local Markdown links and `git diff --check` passed.

The Node tests cover exact repository/source/workflow/run-attempt/artifact
identity, complete API inventories, missing/expired/deleted/changed candidates,
retained ZIP and archive hashes, no overwrite/rebuild fallback, same-commit CI
and security jobs, report hashes, installation cells and explicit human decisions.
The experimental branch rejects invented independent-review claims and keeps
technical tests and candidate-specific approval mandatory.

Tag workflow ordering is also checked: retain and attest the exact candidate,
upload genuine tag-provenance qualification material before publication gates,
then require readiness and upgrade evidence before creating a GitHub Release.
This is static and synthetic workflow qualification, not a claim that an actual
GitHub tag promotion or download has already succeeded.

## Retained evidence and tested implementation

The local Node TAP log is ignored work evidence at
`infra/host-tests/work/native-oneline-release-validation.log`, SHA-256
`d9b99efb8bb6722744cffc284bcedd80ff2918dd1624f98a8a8ff8c7e170ebf1`.
It records the complete 110-test result. The Python outcome was separately
reported by the executing coordinator; it is not embedded in that Node log.

| Validated implementation file | SHA-256 |
| --- | --- |
| `scripts/verify-release-readiness.sh` | `24acc0c55c32466d95b063e1f405d30f4eadcc95d0e0d2a9adacdc3f1ca51bdc` |
| `scripts/verify-release-readiness.mjs` | `0d9de1ea2a759ef4da3c5437fee35b1ada51be78807d5712907ffdf0b9fd750d` |
| `scripts/promote-release-candidate.sh` | `23e9046cecec2192d19666de0a90e6d68c27145e161a10be95b32d5e6201d54c` |
| `scripts/promote-release-candidate.mjs` | `7c6fe5cf903a6ee805e3ca5108e28a27d19318fe099bf49a8f4838f8b5943b14` |
| `scripts/extract-release-candidate.py` | `ed2fa9e5a2bf31ce9508de72f898a3da6e75092dea492471f9fa3445464af103` |

## Bootstrap routing follow-up

The current shell bootstrap explicitly selects `0.1.0-beta.4` rather than
querying latest stable. The Linux shell fixture run reports all seven markers
passed: production transport/repository lock, pinned experimental channel, local
fixture boundary, native routing, journal-pinned rerun, fail-closed native
evidence and archive boundary. Native dispatch never passes generic `--yes` or
falls back to ordinary installation after a native-handler rejection.

This tests the real shell against isolated fixture handlers; it does not prove
that beta.4 assets are published, that genuine tag attestation succeeded or that
the public archived installer performed a real fresh-host boot/setup cycle.

At this follow-up, the observed local hashes were:

| File | SHA-256 |
| --- | --- |
| `packaging/installer/install.sh` | `fcefe96faa159f6da36811de9eff03525fbbf7925fff575040e50714f1ceab3f` |
| `packaging/installer/test-bootstrap.sh` | `74cfe2e0107e342016f530d7ba8091298cc3dc08b00c31a7236d9533ca3fcdf5` |
| `infra/host-tests/work/native-oneline-bootstrap-routing.log` | `a265c4a7da2cd3d5450c7698bcb26c2a5a92dcad334eadad1afeddf72d38fa65` |

The ignored work-log path is reusable, not immutable published evidence. A later
rerun needs its own recorded hash or a separately retained artifact; these local
markers cannot be submitted as invented exact-candidate install approvals.

## First-release upgrade gate

The checked-in support catalog is empty and the public release inventory was
empty at the 2026-09-12 audit. The actual tag job must fetch the complete fresh
inventory again. An authenticated successful empty response permits zero
predecessor cells; an API error, absent/null inventory or omitted published
predecessor does not. `CheckEvidence` still requires a valid target archive hash.
No invented prior-release qualification is necessary when no predecessor exists.

`go test ./cmd/stackfort-upgrade-matrix ./internal/upgradematrix` passed during
this review, including the first-release missing/null-inventory negative case.
Unpublished Actions/tag-qualification artifacts do not add GitHub Releases and
therefore do not create a predecessor dependency. The two-phase flow has no
first-release upgrade-evidence cycle.

For a later release, the existing upgrade policy currently creates three OS
cells times three recovery scenarios per supported predecessor. It does not
yet express release-specific native deployment scope. Review that versioned
policy before the next candidate; do not silently waive cells or describe the
Debian-only first native beta as qualified on Ubuntu/Rocky.

Actual candidate build metadata, retained artifact availability, genuine tag
attestation verification, the exact archived installer's clean-host tests, live
CI/security results and RTBGG's candidate-specific decision remain separate
requirements. See the [promotion contract](../../../packaging/releases/PROMOTION.md)
and [readiness contract](../../../packaging/releases/README.md).
