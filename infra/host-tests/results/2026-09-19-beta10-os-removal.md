# Beta.10 same-target full OS reprovision removal

Result: **passed**, completed `2026-09-19T16:15:51Z`.
Method: direct deployment of an authenticated complete Debian vendor OS image,
not an interactive installer, in-place uninstaller, snapshot rollback or secure
erasure. The active server was completely replaced with a fresh independent OS;
detached former disks/checkpoints remain preserved as external lab evidence.

## Exact candidate and target

- Version: `0.1.0-beta.10`; source
  `0096faed0ae77aef3326ce629db7591a433bb37f`.
- Archive: `stackfort-0.1.0-beta.10-linux-amd64.tar.gz`, SHA-256
  `8e7ce111d6cefda172e74bd7a71380c181e66a726d8f157f6bd1af8944e93263`.
- Original build35451346082, attempt1, artifact10586044694, ZIP SHA-256
  `4d68a705d86a58df3d1c8519c01e0d4c39cc8142bb3b184859c0a0a4ff061896`.
- Same Hyper-V target before/after:
  `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`,
  `stackfort-native-quota-debian-13`.
- Same DMI before/after: `6365bd88-5141-4f15-b3f8-2ba9996baad2`.

The [full candidate qualification](2026-09-19-beta10-candidate-qualification.md)
records the genuine tag-authenticated candidate actively installed and admitted,
original setup/login, hosting functionality, same-release rerun, normal reboot,
resource enforcement and OCI execution. The final fault test then deliberately
quarantined that same installation. Immediately before removal, guarded read-only
checks again verified DMI/boot, three exact archive binaries, unchanged complete
package and ready-storage records, the expected interrupted-admission hash, and
the actual successful quarantine receipt. It was gracefully powered off, not
force-stopped or repaired back into admission. Removal did not claim that a
quarantined service was still active.

## Media and complete independent OS disks

The cached official Debian13 amd64 generic-cloud image came from
[Debian's official image service](https://cloud.debian.org/images/cloud/trixie/latest/).
Its origin/integrity were checked using official HTTPS and published SHA512SUMS;
**no detached-signature verification is claimed**.

- Image SHA-256:
  `85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`.
- Image SHA-512:
  `8ea9faae810043a0b35b0149f05014f26705c2339ffb11ead308f33e844a87cc3ef46ec81d5262b38817b6a88af404874d48a5857ebe072ef6a31dfb6e371f50`.
- Independent 50 GiB system disk `6790b3ca-d3ab-414e-bb36-2cbcee750d90`,
  initial SHA-256
  `3864c5c0d71541b1dab8f43d6c6b658e1c3453e633e0a7b358dc19d8bca5e726`.
- Independent 64 MiB seed disk `2a724d2f-e00a-4f4e-8cfc-637aab1db352`,
  initial SHA-256
  `26c8e1d6a786a3bea42f479e29ace631f1326d3ed23c5363923817c694d9e758`.

These reserved disks were freshly deployed from the vendor image, not copied from
the installed system or its old seed. Before attachment, the guarded host script
verified both exact VHD IDs, hashes, sizes, absence of differencing parents and
reparse points, target VM ID/off state and preserved previous disk chains.
The only two old active SCSI attachments were detached and both independent disks
attached. Their new exact paths were rechecked before booting the same VM; no old
system, seed or data disk remains attached. No old disk or checkpoint was deleted,
and no snapshot was restored. The duplicate-identity rescue VM remained off.

## Fresh boot and absence checks

The new seed's VM/instance/nonce/disk-bound Hyper-V KVP report authenticated the
new SSH public key before changing only this VM's known-host entry. Fingerprint:
`SHA256:ZQ7WLsrQjgf5pmtRnud0b6xAgGpeKb/b41pqD9KhmL0`.
New boot: `dac83dbb-4322-4dde-a946-826549539171`.

The read-only guest verifier passed:

- Debian13 amd64, cloud-init complete with no errors in all four stages;
- a fresh writable plain-ext4 root without project/quota features or mount flags;
- no Stackfort installer/admission/state directory, application database/config
  directory, API/agent/installer binaries, hosting tree or former smoke account;
- no Stackfort/Vinyl managed unit files or loaded units, service/tenant identities,
  hosting mount unit, API AppArmor profile, WAF module or platform share directory;
- no platform TCP/UDP listeners at 80/443/8443/8080/6081/6082/3306/9000, and no
  remaining UDP qualification listener at49173.

The independent `stackfort-test` SSH lab operator was deliberately provisioned
by the new seed; it is not a leftover platform identity. Two read-only verifier
drafts stopped before a result: systemd returns1 for an empty filtered unit-file
list, and a broad identity-prefix check mistakenly included that lab operator.
The final verifier uses successful complete unit-file inventory plus exact
platform/tenant identity checks. No guest cleanup or product change was made to
obtain a pass.

Receipt `work/candidates/35452066167-attempt1/os-removal-result.json`, SHA-256:
`48dc244d7a4fd780161d6df9a08eb3a37936f181569307002087fa6a238e0154`.
The separate retained `final-removal-attachment-20260919.json` binds host-side
old/new disk graphs to that same VM; SHA-256
`ab3a46938efa7029c8466c486e2bb79da8f634312b853324e99f637bef180da3`.
Guest path absence alone is not taken as
proof of independent disk replacement.

## User-facing scope

This validates the authorized experimental removal method: **complete OS
reinstallation removes all data and services from the active server**, not just
Stackfort. It does not offer an in-place uninstaller, backup guarantee, provider
recovery promise or forensic erasure. External snapshots/evidence copies are not
removed. Production systems and important data remain outside the beta's scope;
support is community-only and no independent security review has been performed.
