# Beta.15 public release and default installation

Status: immutable public assets verified; literal README installation pending.
This record distinguishes completed exact-candidate qualification from the final
public-default transport test; it does not claim a pending test has passed.

## Publication and anonymous downloads

- Release: [v0.1.0-beta.15](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.15), GitHub release ID `397350853`.
- Frozen runtime source: `d93c4bfb9fb527e80a648eeb8482b159d0076b29`.
- Publication rules/evidence commit: `55996b1458249e24028f0935e5ed999eca32c5e7`.
- Verification-only workflow: [36262605666](https://github.com/RTBGG/Stackfort/actions/runs/36262605666), successful attempt 1.
- Publication workflow: [36262976996](https://github.com/RTBGG/Stackfort/actions/runs/36262976996), successful attempt 1.
- Publication receipt artifact: `10912783490`, ZIP SHA-256 `9b288879860012d2f5e8603e529617f9528509a7e2d9163bccec0af23a3529a8`.
- Runtime archive SHA-256: `b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`.
- Public bootstrap SHA-256 after selection: `441d139ca2795a8bfb2ebebe41961e23ded90b7d4c9bc4021e31c455e16a6838`.

The release is an immutable prerelease, not stable/latest. All 14 files were
downloaded anonymously and matched their published size and SHA-256. All 12
original tag-qualified files also matched the retained local bytes. GitHub's
DEB/RPM filename normalization changes names, not content; local verification
retains the inventory's original names. Readiness binds the correct source,
archive and evidence commit; upgrade support is explicitly empty.
The publication runner passed all 162 Node contract tests and both bounded
extractor suites. Original tag provenance and live CI/security were rechecked
against the original policy, with no rebuilt package or altered frozen tag.

## Default selector and fresh-host test

The bootstrap and current installation/support documents select Beta.15.
Eight bootstrap shell-contract groups passed in a private mount namespace on
the fresh disposable GPT VM before installation, without writing production
installer state. The default-selection test expects Beta.15; explicit versions,
existing journal pins, native routing, archive checks and fail-closed download
errors remain covered.

The same-target full-OS removal check left an independently fresh vendor Debian
13 GPT/UEFI system. Its pre-installation boot is
`332efe43-49d6-49ae-975f-e9c5fb4c9c02`; its fresh checkpoint is
`6f55414a-7686-4ebd-beb2-d89624afcc10`. The final public test will use the literal
README pipeline, without `STACKFORT_VERSION`, alternate repository, local fixture
or installed-binary repair. Completion evidence will be added after the test.

## Limits

Fresh disposable Debian 13 amd64 only, within qualified ext4/GRUB GPT/UEFI and
primary-MBR/BIOS profiles. No upgrades, production, valuable data, independent
security review or guaranteed support. Removal requires full OS reinstallation.
Private-CA/real-NGINX tests are not public Let's Encrypt issuance for user DNS.
See [exact-candidate evidence](2026-09-26-beta15-candidate-qualification.md),
[OS removal](2026-09-26-beta15-os-removal.md) and the
[approved fresh-only scope](../../../docs/release-evidence/2026-09-26-beta15-fresh-install-scope.md).
