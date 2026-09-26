# Optional optical installation-media entries

Status: **published in experimental Beta.12 for fresh installations only**.
This correction remains in the current public Beta.13 bootstrap selection;
the earlier immutable releases are unchanged.

A provider's fresh Debian MBR image can retain these ordinary fstab entries
even when neither optical filesystem is mounted:

```fstab
/dev/sr1        /media/cdrom0   udf,iso9660 user,noauto     0       0
/dev/sr0        /media/cdrom1   udf,iso9660 user,noauto     0       0
```

Beta.11 rejects these as additional persistent filesystems. The correction
distinguishes inactive, optional optical declarations without deleting or
rewriting them. Root quota preparation still changes only the bound root row;
the original optical lines are retained byte-for-byte.

## Narrow exception

- Sources must be canonical direct `/dev/srN` paths; absent installation-media
  devices are allowed. Existing sources must be block devices, never regular
  files or symbolic-link aliases. Kernel device numbers must not be mounted
  under any source alias.
- Targets must be `/media/cdrom` or `/media/cdromN`. Existing target/parent
  paths must be real directories, not symlinks.
- Types are `udf`, `iso9660`, or exactly those two types in either order.
- An explicit `noauto` is mandatory. Only `user`, `users`, `ro`, `nosuid`,
  `nodev`, `noexec` and `nofail` may accompany it. Duplicate/unknown options,
  conflicting user policies, `auto`, `defaults`, bind/loop and systemd
  automount/dependency/initrd options are rejected. Dump/fsck fields must be zero.
- Complete live mountinfo must show no mounted optical filesystem, corresponding
  source, target, child or non-root ancestor mount, including autofs and hidden
  stacked mounts. Missing/malformed/changing inspection fails closed.
- Duplicate or overlapping fstab targets are rejected. Unrelated extra data or
  boot filesystems still require separate qualification. This is not a general
  exemption for `noauto` or `nofail`.

These rules follow the distinction between
[`mount`'s noauto option](https://manpages.debian.org/trixie/mount/mount.8.en.html)
and [systemd automount/dependency behavior](https://manpages.debian.org/trixie/systemd/systemd.mount.5.en.html):
`nofail` alone does not prevent mounting, and `x-systemd.automount` overrides
`noauto`. The checker does not claim to stop unrelated privileged software
from changing mount state after inspection.

## Evidence and release boundary

See the [actual MBR reproduction and Linux regression results](../infra/host-tests/results/2026-09-24-beta12-optical-regression.md).
The [exact Beta.12 qualification](../infra/host-tests/results/2026-09-24-beta12-candidate-qualification.md)
also verifies unchanged optical rows through installation and normal reboot.
No operator should remove fstab entries, reset installer journals, repartition,
or replace a released binary to bypass an eligibility failure. Use the published
Beta.12 on a fresh qualified test OS; it has its own release provenance and tests.
