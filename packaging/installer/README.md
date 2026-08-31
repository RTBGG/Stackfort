# Installer

The initial installer supports fresh Debian 13, Ubuntu 26.04 LTS, and Rocky
Linux 10 `amd64` hosts. It detects conflicting software and aborts safely rather
than assume ownership of an existing server.

The standalone binary provides both the I-001 read-only gate and I-002's
confirmed, journaled installation:

```sh
stackfort-installer preflight
stackfort-installer preflight --format=json
stackfort-installer install --source-dir=/absolute/release/path --yes
stackfort-installer install --source-dir=/absolute/release/path --yes --format=json
```

Exit status `0` means ready/complete, `2` means one or more actionable preflight
checks blocked installation, and `1` means invocation, inspection, source,
journal, apply, or verification failed. See
[Installer preflight](../../docs/installer-preflight.md) and
[Fresh-host installation](../../docs/installer-installation.md).

`install.sh` is the inspectable convenience bootstrap intended for the GitHub
raw URL. It never treats branch contents as the installation payload: it
selects a versioned GitHub Release, checks the archive against that release's
`SHA256SUMS`, constrains archive paths, and invokes the installer embedded in
the verified archive. The release workflow also generates GitHub build
attestations and refuses to replace an existing version's assets.
