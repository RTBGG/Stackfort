# Stackfort WAF packaging contract

The production bundle contains distribution-specific amd64 packages built from
exactly:

| Component | Version | Upstream |
| --- | ---: | --- |
| Coraza | 3.7.0 | `corazawaf/coraza` |
| libcoraza | 1.7.0 | `corazawaf/libcoraza` |
| NGINX connector | 0.20.0 | `corazawaf/coraza-nginx` |
| OWASP Core Rule Set | 4.25.1 | `coreruleset/coreruleset` |
| Go build toolchain | 1.25.12 | `go.dev/dl` |

The connector is compiled against the exact NGINX ABI of each supported
distribution. It carries a fixed private RUNPATH and opens the Stackfort-owned
`/usr/lib/stackfort/coraza-1.7.0/lib/libcoraza.so` only after the NGINX worker
fork. CRS is installed below `/usr/share/stackfort/owasp-crs-4.25.1` and is
never replaced in place. Coraza build material and licenses live below the
versioned `/usr/share/stackfort/coraza-3.7.0` tree. Private persistent engine
data is created below `/var/cache/stackfort/coraza` for the distribution NGINX
worker.

No installer may fall back to a differently versioned system CRS, connector,
library, or Go toolchain. If the tuple, NGINX ABI, package revision, hash,
inventory, ELF contract, or real worker check cannot be verified, packaging
fails closed before live NGINX activation.

`sources.lock` pins every downloaded byte. Debian and Rocky use the exact
upstream NGINX source version matching their package ABI. Ubuntu pins the exact
distribution DSC, original source tarball, and Debian patch tarball; the builder
applies that package's patch series before compiling the connector.
`targets.lock` additionally pins the native NGINX package revision, worker,
module directory, and loader path. A distribution security update therefore
fails the builder closed until Stackfort deliberately qualifies and records its
new ABI.

`patches.lock` also pins Stackfort's narrow connector patch. The patch wires
libcoraza's matched-rule callback to the active NGINX request and emits a fixed,
sanitized event record containing only rule ID, severity, request correlation,
method, and the normalized path without its query string. Match data, rule
messages, headers, query strings, and bodies are never requested from
libcoraza. The disposable worker test proves both Detection-only visibility and
non-disclosure before an artifact can be produced.

The exact Go archive is verified and used with `GOTOOLCHAIN=local`; the build
does not depend on whichever Go version happens to be installed on the guest.
Go module checksums remain enforced. Reproducibility flags remove workspace
paths and build IDs, and `SOURCE_DATE_EPOCH` controls every archive/package
timestamp.

Prepare a disposable build guest and create its qualification bundle with:

```console
sudo bash packaging/waf/prepare-build-host.sh
SOURCE_DATE_EPOCH=0 bash packaging/waf/build-bundle.sh
bash packaging/waf/build-native-package.sh \
  dist/stackfort-waf-*.tar.gz dist/native
```

The builder downloads only HTTPS sources whose SHA-256 appears in
`sources.lock`, rejects unsafe archive paths, compiles libcoraza and the
connector against the target's exact NGINX tree, and verifies both ELF objects.
Because `nginx -t` does not execute Coraza's after-fork initialization, the
builder starts a disposable NGINX worker, sends a Unix-socket request, and
requires a fixed inline rule to return HTTP 403. It also proves that the worker
actually opened `libcoraza.so`.

The deterministic tarball contains a rooted payload, machine-readable
qualification manifest, licenses, exact Go module files, and a checksum
inventory. It is consumed only by the native-package wrapper. The finished
DEB/RPM is the sole WAF installer input; release assembly binds it in the closed
manifest before GitHub artifact attestation.

`build-native-package.sh` rechecks the adjacent SHA-256, complete inventory,
fixed payload-path allowlist, lock files, host tuple, exact installed NGINX
package, file modes, and absence of payload symlinks. The package declares its
exact NGINX dependency, preserves qualification metadata below
`/usr/share/doc/stackfort-waf/qualification`, creates runtime directories for
the locked worker, and performs an isolated syntax/module-load check during
installation. The privileged Stackfort installer then starts the real worker
and proves a deny request before accepting the package. The native package does
not reload the live server or own Stackfort's generated
WAF policy; those actions belong to the privileged transactional installer.
An adjacent strict `*.release.json` record is emitted for release assembly.

An amd64 release requires exactly one valid record and package for Debian 13,
Ubuntu 26.04, and Rocky Linux 10. `scripts/build-release.sh` copies those
artifacts below `packages/waf`, writes `RELEASE-MANIFEST.json`, and fails a
non-development build if the directory is absent or incomplete. A development
archive may carry an explicit incomplete arm64 manifest for cross-build
reproducibility, but source inspection rejects it before journal creation or
host mutation. Public non-development archives remain amd64-only until arm64
WAF packaging is qualified.
