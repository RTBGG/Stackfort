# Native core package qualification — Hyper-V — 2026-09-01

Phase 6's passive `stackfort-release` DEB/RPM carrier passed reproducibility and
package-lifecycle qualification on all three supported `amd64` guests.

## Qualified guests

| Guest | Kernel | Package manager | Result |
| --- | --- | --- | --- |
| Debian GNU/Linux 13 (trixie) | `6.12.107+deb13-cloud-amd64` | `dpkg 1.22.22` | DEB pass |
| Ubuntu 26.04 LTS | `7.0.0-29-generic` | `dpkg 1.23.7ubuntu1` | DEB pass |
| Rocky Linux 10.2 (Red Quartz) | `6.12.0-211.49.1.el10_2.x86_64` | `rpm 4.19.1.1` | RPM pass |

## Reproducible artifacts

Each format was built twice from the same `0.0.0-dev` release tree with
`SOURCE_DATE_EPOCH=0`. The independently built files were byte-identical:

| Format | Artifact | SHA-256 for both builds |
| --- | --- | --- |
| DEB | `stackfort-release_0.0.0~dev-1_amd64.deb` | `f19a54610d569183026eb0e6fda93ed7b331ec7afb5883ef0d4eef0a75c820cb` |
| RPM | `stackfort-release-0.0.0~dev-1.sf1.x86_64.rpm` | `eae4330828393008405ae6f97d2470be38ea2aabba90ad27f3daad10098d9e74` |

The source payload came from the existing Windows cross-build archive. Its
transport had removed the Linux execute bits; the hardened builder first
rejected that tree, proving the new fail-closed mode gate. Qualification then
restored the four `0755` modes required by `scripts/build-release.sh` and by
the official Linux release workflow before producing the hashes above.

## Lifecycle corpus

`packaging/core/test-native-package-lifecycle.sh` installed `0.0.0-dev`,
upgraded it to a synthetic `0.0.1-beta.1` carrier, and removed it. The second
tree changed only release-version metadata and existed solely to prove native
prerelease ordering and obsolete version-root retirement.

Every guest proved:

- the extracted payload owns only `/usr/lib/stackfort/releases/<version>`,
  `/usr/sbin/stackfort-install`, and package documentation/license paths;
- the DEBs contain no maintainer program and the RPMs contain no scriptlet;
- package installation does not configure or start Stackfort;
- upgrade removes the old versioned release root and selects the new root;
- the installed tree and wrapper are `root:root`, with the wrapper executable;
- the wrapper executes its embedded installer;
- removal retires only the carrier release root and wrapper;
- hashes and modes of active API, agent, scanner, and web payload files are
  identical before and after the entire lifecycle;
- `stackfort-release` is absent after cleanup.

Required markers:

```text
Debian: STACKFORT_QUALIFICATION native-package-deb-install-upgrade-remove=passed
Ubuntu: STACKFORT_QUALIFICATION native-package-deb-install-upgrade-remove=passed
Rocky:  STACKFORT_QUALIFICATION native-package-rpm-install-upgrade-remove=passed
```

## Scope

This record closes the first Phase 6 roadmap item: versioned core DEB/RPM
packages where appropriate. It does not qualify functional Stackfort upgrades,
migrations, health-gated activation, rollback, repositories, or automatic
update checks; those remain separate Phase 6 work.
