# Native Stackfort release package

`build-native-package.sh` converts an already assembled `amd64` release tree
into a reproducible `stackfort-release` DEB or RPM. The package is a passive,
versioned release carrier; the journaled Stackfort installer remains the only
component allowed to configure a host.

```sh
SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)" \
  bash packaging/core/build-native-package.sh \
  /absolute/path/to/stackfort-0.1.0-linux-amd64 deb dist

SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)" \
  bash packaging/core/build-native-package.sh \
  /absolute/path/to/stackfort-0.1.0-linux-amd64 rpm dist
```

The DEB requires `dpkg-deb`; the RPM requires `rpmbuild`, `rpm`, `rpm2cpio`,
and `cpio`. Each build extracts its result and compares the complete release
tree, file modes, wrapper, documentation, and package metadata before
publishing the artifact. Adjacent `.sha256` and `.release.json` records make
the package independently inventoryable.

The same DEB supports Debian 13 and Ubuntu 26.04. The RPM supports Rocky Linux
10. Native WAF and Vinyl packages remain separate because their compiled
payloads and dependencies are tied to each qualified distribution revision.
