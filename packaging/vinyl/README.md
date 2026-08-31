# Stackfort Vinyl Cache packaging contract

Stackfort builds Vinyl Cache 9.0.1 from the upstream security-release tarball
whose SHA-256 is fixed in `sources.lock`. One native amd64 package is built and
qualified on each supported target: Debian 13, Ubuntu 26.04, and Rocky Linux
10. No system Varnish/Vinyl package or unpinned source is used as a fallback.

The package listens only on `127.0.0.1:6081` and exposes its authenticated
management interface only on `127.0.0.1:6082`. NGINX remains the public TLS and
Coraza edge and forwards only eligible PHP requests to Vinyl; its private
origin listener is `127.0.0.1:9000`. Port 9000 is a stock `http_port_t` on
EL10, so enforcing SELinux permits NGINX to bind it without a broad local
policy exception. The installed VCL is generated from the
same closed Go policy used by the control plane. It never accepts customer VCL,
never exposes an HTTP purge method, bypasses every request with a cookie or
Authorization, and refuses to store Set-Cookie/private/no-store responses.
`managed-vcl.sha256` closes this package input even on a minimal build guest
without Go; repository tests additionally require it to remain byte-identical
to `cacheconfig.ManagedVCL()`.

On a disposable target VM:

```console
sudo bash packaging/vinyl/prepare-build-host.sh
bash packaging/vinyl/verify-locks.sh
SOURCE_DATE_EPOCH=0 bash packaging/vinyl/build-native-package.sh dist/native
```

The adjacent `*.release.json` record binds the package hash, version, target,
and upstream Vinyl version into Stackfort's release manifest. The privileged
installer verifies that record and the installed package before starting the
service.

On Rocky Linux 10, preparation enables CRB, installs `epel-release` from Rocky
Extras, and uses DNF to resolve Vinyl's jemalloc dependency while installing
the local RPM. The RPM removes redundant standard-library RUNPATH metadata and
must pass `rpm -V` unchanged. With SELinux enforcing, Stackfort installs one
exact rule allowing `httpd_t` to connect to `varnishd_port_t`; it never enables
a broad web-server network-connect boolean.
