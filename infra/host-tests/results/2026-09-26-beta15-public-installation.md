# Beta.15 public release and default installation

Status: **passed**. Immutable public assets and the literal README installation,
original setup/login, installed hosting/API smoke, same-release rerun and normal
reboot persistence all passed. This public transport test is separate from the
earlier retained exact-candidate host qualification.

## Publication and anonymous downloads

- Release: [v0.1.0-beta.15](https://github.com/RTBGG/Stackfort/releases/tag/v0.1.0-beta.15), GitHub release ID `397350853`.
- Frozen runtime source: `d93c4bfb9fb527e80a648eeb8482b159d0076b29`.
- Publication rules/evidence commit: `55996b1458249e24028f0935e5ed999eca32c5e7`.
- Verification-only workflow: [36262605666](https://github.com/RTBGG/Stackfort/actions/runs/36262605666), successful attempt 1.
- Publication workflow: [36262976996](https://github.com/RTBGG/Stackfort/actions/runs/36262976996), successful attempt 1.
- Publication receipt artifact: `10912783490`, ZIP SHA-256 `9b288879860012d2f5e8603e529617f9528509a7e2d9163bccec0af23a3529a8`.
- Runtime archive SHA-256: `b5ac6c02a8f13bcabb72b527c677844ba007fb8b2c8ff435a231ddad510ba617`.
- Public bootstrap SHA-256 after selection: `441d139ca2795a8bfb2ebebe41961e23ded90b7d4c9bc4021e31c455e16a6838`.
- Public selection commit: `780575eee2dfe455d1bb6c685ed3cc2a187774f9`.
- Published at `2026-09-26T18:34:34Z`; anonymous download verification completed at `2026-09-26T18:36:05Z`.
- Anonymous verification receipt SHA-256: `ae0aaa40b0943410c136a26e025e08332a1d1bf30140a6b19e84c493eba5cd9e`.
- Public readiness SHA-256: `c62e0a7e39728b564308c85524aaae85738e645c2b624fd282c19679ac06d27b`.
- Public fresh-only scope SHA-256: `a89f5cb2e5f4ae9d89dc3bc5ac067e3254932de14fdcf10dbd2bcff94ca52591`.

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

The exact public-selection commit also passed [CI 36263227754](https://github.com/RTBGG/Stackfort/actions/runs/36263227754)
(workflow/bootstrap hygiene, Web, Go and reproducible artifacts) and
[Security 36263227788](https://github.com/RTBGG/Stackfort/actions/runs/36263227788)
(all applicable push jobs; dependency review is PR-only). These development
artifacts were not substituted for the frozen released packages. The final
completion edit changes documentation only; its documentation contracts, all
275 Markdown documents/1,000 local links and whitespace checks passed locally.

The same-target full-OS removal check left an independently fresh vendor Debian
13 GPT/UEFI system. Its pre-installation boot is
`332efe43-49d6-49ae-975f-e9c5fb4c9c02`; its fresh checkpoint is
`6f55414a-7686-4ebd-beb2-d89624afcc10`. Read-only checks confirmed plain ext4
without quota/project features, no Stackfort identities/state/services/listeners
and error-free completed cloud-init. Strict SSH host-key verification and the
exact Hyper-V disk graph/DMI bound the target. The MBR faulted fixture was not reset.

The public test ran the literal README pipeline under a clean environment,
without `STACKFORT_VERSION`, alternate repository, local fixture selection or
installed-binary repair. Separate expected-file staging only bound the reviewed
archive/installer hash; the invoked bootstrap fetched every payload from public
GitHub. The branch-hosted bootstrap matched the selection commit and had the
same SHA-256 before and after the complete test.

- VM ID: `55729cf8-11e3-4744-be5b-ddde3824c0e4`; DMI: `a5db08d7-6c4e-47e0-a381-9bd594b2731a`.
- Native operation: `7ddb13ae-b9f9-416c-912e-80fc8dd5b098`.
- Conversion boot: `cb0b15b7-51a8-485b-a2a3-f347be34d5c5`.
- Final normal boot: `e879a730-22c9-4e9d-8a4d-3c71980e97e7`.
- Healthy public-install checkpoint: `df6e2b1e-439c-4276-bb60-088801c27b31`, `beta15-public-readme-qualified-20260926`; prior disks and fault evidence are retained.
- Completed: `2026-09-26T18:46:08Z`.
- Sanitized result SHA-256: `013257325e1ddca01171c1ed9ea545e633c56ba226b338d3d45faefb176e4d50`.
- Public driver SHA-256: `f6c25585d7f19ee95be3bb48c5a1a2429de6832d92c36abd02f7d2436e24570d`; guarded wrapper: `1086c41ff75cb432a26ccaca9073b5b03db74282810916e5709e5ba9bbbcbc87`.
- Shared installed-API helper SHA-256: `49ead41676443e181c3a2af63642e87cbe8b614bdb5464c790f9af60f228e196`.

The original terminal-only setup code was redeemed once; replay was rejected
and the newly created administrator logged in. Credentials, cookies and raw
terminal output were kept only in process memory, not retained as artifacts.
Seven installed API groups passed: account/package provisioning; static/PHP
upload/download and delivery; domain idempotency; database wizard; document-root
backup/restore; FastCGI toggle/hit/bypass/purge; and WAF off/detection/blocking
before cache. The ordinary same-release pipeline rechecked admission without
reconversion, reinstallation, setup reissue or reboot. A separately requested
normal reboot preserved the session, package/account/domain state, file and
backup digests, database inventory, PHP, WAF and cache behavior, with no repair.

This smoke does not itself cover SQL credentials/phpMyAdmin, OCI API workflows,
full-account/database backup, performance, or tenant isolation; the applicable
separate exact-candidate checks remain in their linked report. It is not a
browser-rendering or public CA/DNS test.

## Limits

Fresh disposable Debian 13 amd64 only, within qualified ext4/GRUB GPT/UEFI and
primary-MBR/BIOS profiles. No upgrades, production, valuable data, independent
security review or guaranteed support. Removal requires full OS reinstallation.
Private-CA/real-NGINX tests are not public Let's Encrypt issuance for user DNS.
See [exact-candidate evidence](2026-09-26-beta15-candidate-qualification.md),
[OS removal](2026-09-26-beta15-os-removal.md) and the
[approved fresh-only scope](../../../docs/release-evidence/2026-09-26-beta15-fresh-install-scope.md).
