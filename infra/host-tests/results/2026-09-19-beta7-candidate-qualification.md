# Beta.7 exact candidate qualification — 2026-09-19

Status: **native storage preparation passed; installation failed at Vinyl;
not publishable**.

The [beta.6 prerequisite failure](2026-09-19-beta6-candidate-qualification.md)
is fixed in frozen source `ecd7be3c451fe4c38526b2dc3bc3b9e20d5ee22e`.
The failed beta.6 installation was not retried or repaired. All old disks remain
retained, with failure checkpoint `5f7c0511-5a4a-45b3-a49e-f10fbba6aa4f`
(`native-beta6-prerequisite-failure-20260919`).

## Retained build and tag

- Version: `0.1.0-beta.7`.
- [Original build](https://github.com/RTBGG/Stackfort/actions/runs/35439337197):
  run `35439337197`, attempt `1`, success; all six WAF/Vinyl target jobs passed.
- Aggregate artifact `10582719791`, 313,151,870 bytes.
- ZIP SHA-256: `a3b70e1727f988c7ad976431475f982856132980406b7afdfbdd18badabb1bbf`.
- TAR: `stackfort-0.1.0-beta.7-linux-amd64.tar.gz`, 117,607,242 bytes.
- TAR SHA-256: `874cd2ed1ff1843da92e69bc1d88c0e37a596fc9ef4791c61164c4e66f3828d1`.
- Installer SHA-256: `0af658e55fea8fe762bf014a6cb99702cd4a25ff3f73b83b1b9c7658b169e8d6`.
- API SHA-256: `4cd243c9011adc6fde8122cc73dc4b81ce7c32a4186deb3595fad1deba82f5ba`.
- Agent SHA-256: `046eaf1f73bd4cf49f4b9e0f30a2e55d46d5069d9f54d4106b7c0af65cba5b02`.
- Bootstrap SHA-256: `61338f5ec07930bad50a30679d978f1d2ff88de39c7075a30b595e1621b4b5bb`.

The complete seven-artifact API inventory and successful original run passed
mechanical promotion validation. Strict extraction checked the ZIP digest, fixed
ten-file inventory, CRC/size bounds, all aggregate and carrier checksums, SPDX
metadata, exact TAR source/version and standalone/archived installer equality.

Selection commit `0844f001f84673afe56a3e295fc01d74c985ad8e` records that original
artifact. Annotated tag `v0.1.0-beta.7`, object
`f1904b3952e9f20930d6eae82c8f1160f3b8db68`, points to the frozen source, not
the later selection/harness commit. The [tag run](https://github.com/RTBGG/Stackfort/actions/runs/35439799936)
reused the original bytes without rebuilding and produced exact-tag provenance.
It stopped at the missing readiness record; publication steps were skipped.

The twelve-file tag artifact `10582844905` (313,159,875 bytes) passed local
integrity checks and all ten original payloads are byte-identical. Tag ZIP
SHA-256: `73bd56c6f73c6baeeb06647f03eb2e7bea901dbce450782c0fcbbdd28e4935c3`;
attestation bundle: `1c38f370bf90937e618be8cbe4d870528208d75344a1b963a27c11a9a94c5df4`;
checksums: `43056972900167b42ae4ab7ee3dcb50d5df37f7cdff2315d891c34b464bf7449`.
The extraction helper checks integrity; the actual installer performed its
unchanged cryptographic origin validation during onboarding.

Source [Security run 35439304105](https://github.com/RTBGG/Stackfort/actions/runs/35439304105)
passed. Source [CI run 35439304029](https://github.com/RTBGG/Stackfort/actions/runs/35439304029)
passed on attempt 2: the initial successful Go/Web/hygiene jobs were retained
and its reproducibility job was rerun after a later main push cancelled it.
The original candidate build was not rerun or changed.

The external host driver now requires an explicit canonical `0.1.0-beta.N`
version, in addition to its mandatory exact commit and asset hashes. It no
longer silently defaults to an older candidate. Its local contract tests passed;
this harness-only change does not replace any installed product binary.
The frozen bootstrap's seven isolated Linux routing/integrity checks also passed.

## New disposable baseline

The same exact VM `4361f439-15e9-4f9e-a690-9a8e44b6cbd3` was shut down gracefully
before changing its two disk attachments. New independent vendor-OS/system and
cloud-init seed disks were checked against their initial hashes/IDs. No disk
was copied from an installed system, restored, overwritten or deleted. The
duplicate-identity rescue VM remained off. This is baseline preparation, not
the required post-install full-OS removal test.

Vendor image SHA-256:
`85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`.
Authentication: official HTTPS and published SHA512SUMS; no independent detached
signature claim. Cloud-init completed with empty error lists. Its new SSH public
key was checked through the instance/nonce/image/VM-bound Hyper-V KVP report
before updating the strict SSH pin. No private key was read.

Baseline: Debian 13.6, kernel `6.12.107+deb13-cloud-amd64`, Secure Boot on, cgroup
v2, 50 GiB GPT/ext4 disk, 256-byte inodes, approximately 47 GiB free and 3.24
million free inodes. No Stackfort state or project-quota feature was present.
Root enumerated as `/dev/sdb1`; the seed as `/dev/sda1`. A read-only diagnostic
initially targeted the former baseline's device name and failed safely; the
correct root was then verified through `findmnt` and `lsblk`. The actual driver
resolves the root dynamically and does not hardcode either disk name.

Initial boot: `7ccd8a75-9e5e-4746-99f8-3e99c9357f15`.
Fresh checkpoint: `bdbf15de-8fcf-44f8-975a-67bd33549973`
(`native-beta7-vendor-fresh-20260919`). Read-only APT simulation on this fresh
baseline reproduces the six reviewed additions covered by the regression test.

## Actual onboarding result

The exact retained-tag transport reached and verified the real-terminal review,
fresh-disposable/reboot acknowledgements and original `SAVED` setup commitment.
The six prerequisite additions completed with a valid post-state, including
`libjansson4`. The sealed native boot was armed exactly once. Automatic offline
storage preparation reached `ready` after the authorized reboot:
`481f9541-4544-4f9b-b5c4-b66a2b5fc9cf`. No manual filesystem conversion, replay,
setup-code recovery or replacement installer was used.

The ensuing installation completed `packages` and `waf-native-package`, then
failed at `vinyl-native-package` at `2026-09-19T11:26:37Z`. Its recorded error was
`installed Vinyl could not compile the managed VCL`. The installer automatically
removed only its newly installed `vinyl-cache` package during rollback; residual
configuration remained, and no whole-platform rollback or successful uninstall
is claimed. The native service remained failed. The Windows qualification
driver was stopped after observing this terminal host failure, rather than
waiting its full polling deadline; no guest reset or repair followed.

Neither GCC/CC nor C headers were installed on the minimal host. The exact
Debian Vinyl package (SHA-256
`b1062a176fd2cd6ae27e3fbdb6c2fdaf436956f9d751e94ff6a37da8ca38632f`)
does not declare them as dependencies. A compile-only diagnostic extracted
that same authenticated package into a new temporary directory, using its
own VCL/VMOD paths without installing it or reading the management secret.
It failed with `exec: gcc: not found` and compiler exit 127 (Vinyl exit 2).
No installer receipt, package database or host service was changed by that probe.

The next source corrects native package runtime dependencies and introduces
separate compiler-free container checks before aggregate candidate creation.
Its focused metadata regression first failed against the old declarations and
passes with the fix. This does not repair or qualify beta.7; its tag stays fixed.

## Remaining qualification

Successful complete onboarding, original setup redemption and product smoke,
same-release rerun/reboot, live isolation/resource/security checks, actual OCI
build/scan/deploy, process-loss quarantine and same-target full OS removal remain
required. Public GitHub download testing follows publication, not this retained
fixture run. Candidate-specific publication approval is not recorded here.

Test-only overlays were prepared from the frozen source; all 1,058 tracked raw
Git blobs matched. The external test ELF SHA-256 is
`901630519ca84ba871fe908de6b1662db80942cfc3b701205d890adc5c2fbee4`.
Compilation/source verification is not execution or installed-host qualification.
