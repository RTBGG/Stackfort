# Native WAF package qualification on Hyper-V — 2026-08-30

> Historical evidence: later gates fixed Rocky's RPM post-processing and
> Ubuntu's need to compile against Canonical's exact patched source package.
> The common source lock consequently changed every native package's metadata
> and all packages were rebuilt twice reproducibly. Current hashes and runtime
> evidence are in the
> [final runtime qualification](2026-08-31-waf-runtime-hyper-v.md).

## Scope

This result qualifies the K-010 native `stackfort-waf` package wrapper on all
three supported amd64 guests. Each wrapper run consumed the previously
qualified rooted payload, used `SOURCE_DATE_EPOCH=0`, and revalidated the
adjacent SHA-256, rooted file inventory, embedded source and target locks, host
tuple, worker account, installation paths, and exact installed NGINX package.

The resulting packages remain unsigned qualification artifacts. Release
attestation and privileged Stackfort installer integration are separate gates.

## Reproducible artifacts

| Guest | Native artifact | SHA-256 | Bytes | Result |
| --- | --- | --- | ---: | --- |
| Debian 13 | `stackfort-waf_3.0.16+connector1.0.4+crs4.25.1-1_debian13_amd64.deb` | `82f6fa20da397c8ea2b69b501f1209a9d6388c6925469693353ff872848f3776` | 786,432 | Two byte-identical builds |
| Ubuntu 26.04 | `stackfort-waf_3.0.16+connector1.0.4+crs4.25.1-1_ubuntu26.04_amd64.deb` | `d7dfa47ee0430215222b4f29968353bc1d2b3145a4c76daccf94113f9e1749ba` | 815,668 | Two byte-identical builds |
| Rocky Linux 10.2 | `stackfort-waf-3.0.16-1.sf1.el10.x86_64.rpm` | `308dd25dcacbad6b937ca88c31a668bcf6a5a3813929459428fc33254f02bf5a` | 901,121 | Two byte-identical builds |

Both the package and its filename-only checksum sidecar were regenerated in a
second temporary directory. `cmp` accepted the complete native packages and
`sha256sum --check --strict` accepted every sidecar.

## Package and lifecycle checks

- Debian declares exact `nginx (= 1.26.3-3+deb13u7)` and Ubuntu exact
  `nginx (= 1.28.3-2ubuntu1.10)`. `dpkg-shlibdeps` derived their remaining
  distribution-specific ELF dependencies from the packaged objects.
- Rocky declares exact `nginx = 2:1.26.3-6.el10_2.6`. RPM dependency scanning
  recorded the system ELF requirements and the package's private
  `libmodsecurity.so.3` and `libyajl.so.2` provides.
- Each package retains its source locks, target locks, manifest, and rooted
  checksum inventory below
  `/usr/share/doc/stackfort-waf/qualification`.
- Native installation ran an isolated NGINX module-load test. Every guest
  reported ModSecurity-nginx `1.0.4` and libModSecurity `3.0.16`; the live
  distribution `nginx -t` then passed.
- `dpkg --verify` on Debian/Ubuntu and `rpm -V` on Rocky reported no package
  drift after installation.
- `/var/cache/stackfort/modsecurity` was root-owned mode `0755`; its `tmp` and
  `data` children were mode `0700` and owned by `www-data` on Debian/Ubuntu or
  `nginx` on Rocky.
- Native removal deleted the module, loader, private libraries, CRS, engine
  data, and qualification metadata. It deliberately retained the mutable
  runtime directories, so uninstalling a package cannot delete engine state.
- A qualification bundle changed after checksum creation was rejected before
  extraction or packaging. A separately rehashed bundle whose internally
  consistent inventory added an unowned `/etc` payload was also rejected by
  the fixed installation-path allowlist.

The package scripts do not reload live NGINX and do not own generated Stackfort
WAF profiles. That boundary prevents a native package transaction from
silently changing request handling; the privileged Stackfort installer must
stage, verify, activate, and roll back those changes explicitly.

All three qualification VMs were `Off` after the checks.

## Gate status at the time

The later release-manifest, transactional installer, and full three-guest
runtime matrix are now complete; follow the supersession note above for
current evidence.
