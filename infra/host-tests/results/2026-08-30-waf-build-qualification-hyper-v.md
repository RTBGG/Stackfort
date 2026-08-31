# WAF build qualification on Hyper-V — 2026-08-30

> Historical evidence: the final runtime gate found that Ubuntu's upstream
> source-built connector could pass NGINX's module signature check yet stall a
> reused HTTP connection because the installed NGINX contained Canonical's
> patch series. The builder now uses Ubuntu's exact pinned source package and
> all three artifacts were rebuilt reproducibly. Current hashes and runtime
> evidence are in the
> [final runtime qualification](2026-08-31-waf-runtime-hyper-v.md).

## Scope

This result qualifies the K-010 rooted WAF payload builder on the three
supported amd64 guests. Native package qualification subsequently passed and
is recorded in the
[native package result](2026-08-30-waf-native-packages-hyper-v.md); this page
remains the lower-level source-build and NGINX-module ABI evidence.

Every run used `SOURCE_DATE_EPOCH=0`, `packaging/waf/sources.lock`, and
`packaging/waf/targets.lock`. The builder downloaded and verified each source,
applied the pinned YAJL security patches, compiled libModSecurity and the exact
NGINX connector, loaded the unstripped module with the distribution NGINX,
stripped only non-runtime symbols, loaded the staged module again, and emitted
the rooted file inventory. A second clean run used a different `mktemp`
workspace; `cmp` required the complete tarballs to be byte-identical.

## Results

| Guest | Locked NGINX package / source ABI | Final archive SHA-256 | Bytes | Result |
| --- | --- | --- | ---: | --- |
| Debian 13 | `1.26.3-3+deb13u7` / `1.26.3` | `63c42a1220fa3f9cf64534c9126e5eeb0d8e20e0335a44fafdef8a4358e52888` | 1,099,714 | Two byte-identical builds; both module loads passed |
| Ubuntu 26.04 | `1.28.3-2ubuntu1.10` / `1.28.3` | `727c6d1f074fb1792aa54d30e6d876c70dd54e5ffd8cc4886ae15a7b08ec9249` | 1,143,014 | Two byte-identical builds; both module loads passed |
| Rocky Linux 10.2 | `2:1.26.3-6.el10_2.6` / `1.26.3` | `00390de60115618f86665de62c3845491b3c49dc23ca78308f191510d61b5048` | 1,108,548 | Two byte-identical builds; both module loads passed |

In every load, NGINX reported ModSecurity-nginx `1.0.4` and libModSecurity
`3.0.16`. Each manifest also records OWASP CRS `4.25.1` and the Debian
security-patched YAJL `2.1.0-7` source layer. File-inventory verification,
dependency-license presence, fixed runtime RPATH, and absence of temporary
workspace paths in delivered ELF files passed on all guests.

## Fail-closed properties observed

- The builder requires an exact OS, architecture, NGINX source version, and
  native NGINX package revision match.
- Every network source is HTTPS and has a fixed SHA-256; no system
  ModSecurity/CRS fallback exists.
- Source archives with absolute or parent-traversal paths are rejected before
  extraction.
- The generated connector must load into the installed distribution NGINX
  before an artifact is emitted, both before and after symbol stripping.
- The payload carries a private fixed library path, rooted file checksums, the
  complete source/target locks, a manifest, and all dependency licenses.

## Gate status at the time

These were lower-level qualification artifacts and were not installer inputs.
The later native-package, release-installer, and final runtime gates are now
complete; follow the supersession note above for current evidence.
