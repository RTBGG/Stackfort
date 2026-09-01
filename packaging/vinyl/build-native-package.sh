#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
output_directory="$(realpath -m "${1:-$PWD/dist/native}")"
source_date_epoch="${SOURCE_DATE_EPOCH:-0}"

fail() { printf 'Stackfort Vinyl packaging failed: %s\n' "$1" >&2; exit 1; }
[[ "${EUID:-$(id -u)}" -ne 0 ]] || fail 'run as an unprivileged build user'
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || fail 'SOURCE_DATE_EPOCH must be a non-negative integer'
bash "$script_directory/verify-locks.sh"
for command in awk curl find make realpath sed sha256sum sort tar touch; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
done

# shellcheck disable=SC1091
source /etc/os-release
target_row="$(awk -F '|' -v os="${ID:-}" -v version="${VERSION_ID:-}" '$1==os && index(version,$2)==1 { if(found++) exit 2; row=$0 } END { if(found!=1) exit 1; print row }' "$script_directory/targets.lock")" || fail 'build host is not a uniquely supported target'
IFS='|' read -r target_os version_prefix architecture package_format <<<"$target_row"
[[ "$(uname -m)" == x86_64 && "$architecture" == amd64 ]] || fail 'only the locked amd64 target is supported'

source_row="$(awk -F '|' '$1=="vinyl-cache" { if(found++) exit 2; row=$0 } END { if(found!=1) exit 1; print row }' "$script_directory/sources.lock")" || fail 'Vinyl source is not uniquely locked'
IFS='|' read -r _ vinyl_version vinyl_sha256 vinyl_url <<<"$source_row"
[[ "$vinyl_url" == https://* ]] || fail 'source URL is not HTTPS'
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
archive="$workspace/vinyl-cache.tgz"
curl --fail --location --proto '=https' --tlsv1.2 --output "$archive" "$vinyl_url"
printf '%s  %s\n' "$vinyl_sha256" "$archive" | sha256sum --check --status || fail 'source checksum does not match'
if tar -tzf "$archive" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then fail 'source archive contains an unsafe path'; fi
source_root="$workspace/source"
mkdir -p "$source_root"
tar -xzf "$archive" --strip-components=1 --no-same-owner --no-same-permissions -C "$source_root"
[[ -z "$(find "$source_root" -type l -print -quit)" ]] || fail 'source archive contains a symbolic link'

case "$package_format" in
  deb)
    command -v dpkg-deb >/dev/null 2>&1 || fail 'dpkg-deb is unavailable; run prepare-build-host.sh'
    lib_directory=/usr/lib/x86_64-linux-gnu
    ;;
  rpm)
    command -v rpmbuild >/dev/null 2>&1 || fail 'rpmbuild is unavailable; run prepare-build-host.sh'
    command -v chrpath >/dev/null 2>&1 || fail 'chrpath is unavailable; run prepare-build-host.sh'
    lib_directory=/usr/lib64
    ;;
  *) fail 'unsupported native package format' ;;
esac

build_root="$workspace/build"
stage="$workspace/root"
mkdir -p "$build_root" "$stage"
(
  cd "$source_root"
  ./configure --quiet --prefix=/usr --bindir=/usr/bin --sbindir=/usr/sbin \
    --libdir="$lib_directory" --sysconfdir=/etc --localstatedir=/var \
    CFLAGS="-O2 -g0 -ffile-prefix-map=$workspace=. -fdebug-prefix-map=$workspace=."
  make -s -j"$(getconf _NPROCESSORS_ONLN)"
  # Upstream's top-level install-data-local target does not apply DESTDIR to
  # localstatedir. Override the make-time installation variable only; configure
  # has already compiled the production /var runtime path into the binaries.
  make -s DESTDIR="$stage" localstatedir="$stage/var" install
)

# Libtool records the standard EL library directory as a RUNPATH when it
# relinks installed command-line tools. RPM correctly rejects that redundant
# path. Remove it from staged executables instead of weakening RPM QA checks.
if [[ "$package_format" == rpm ]]; then
  while IFS= read -r -d '' executable; do
    if chrpath -l "$executable" >/dev/null 2>&1; then
      chrpath -d "$executable"
    fi
  done < <(find "$stage/usr/bin" "$stage/usr/sbin" -type f -print0)
fi

install -D -m 0644 "$script_directory/stackfort.vcl" "$stage/etc/vinyl-cache/stackfort.vcl"
install -D -m 0644 "$script_directory/vinyl.service" "$stage/usr/lib/systemd/system/vinyl.service"
install -D -m 0755 "$script_directory/stackfort-vinyl-reload" "$stage/usr/libexec/stackfort-vinyl-reload"
install -D -m 0755 "$script_directory/stackfort-vinyl-validate" "$stage/usr/libexec/stackfort-vinyl-validate"
install -D -m 0644 "$script_directory/sources.lock" "$stage/usr/share/doc/vinyl-cache/stackfort/SOURCES.lock"
install -D -m 0644 "$script_directory/targets.lock" "$stage/usr/share/doc/vinyl-cache/stackfort/TARGETS.lock"
install -D -m 0644 "$script_directory/managed-vcl.sha256" "$stage/usr/share/doc/vinyl-cache/stackfort/managed-vcl.sha256"
install -D -m 0644 "$script_directory/stackfort.vcl" "$stage/usr/share/doc/vinyl-cache/stackfort/stackfort.vcl"
license_file="$(find "$source_root" -maxdepth 1 -type f -iname 'LICENSE*' -print -quit)"
[[ -n "$license_file" ]] || fail 'upstream source does not contain a license file'
install -D -m 0644 "$license_file" "$stage/usr/share/licenses/vinyl-cache/LICENSE"
find "$stage" -exec touch -h -d "@$source_date_epoch" {} +
mkdir -p "$output_directory"

package_version="$vinyl_version-1sf1"
case "$package_format" in
  deb)
    mkdir -p "$stage/DEBIAN"
    installed_size="$(du -sk "$stage" | awk '{print $1}')"
    cat >"$stage/DEBIAN/control" <<EOF
Package: vinyl-cache
Version: $package_version
Architecture: amd64
Maintainer: Stackfort Authors <noreply@stackfort.invalid>
Installed-Size: $installed_size
Section: web
Priority: optional
Homepage: https://vinyl-cache.org/
Depends: libc6, libedit2, libjemalloc2, libpcre2-8-0, openssl, systemd
Conflicts: varnish
Provides: varnish
Description: Stackfort-qualified Vinyl Cache 9 edge
 Hash-pinned Vinyl Cache with a loopback-only, personalization-safe Stackfort VCL.
EOF
    cat >"$stage/DEBIAN/preinst" <<'EOF'
#!/bin/sh
set -e
if ! getent group vinyl >/dev/null; then groupadd --system vinyl; fi
if ! getent passwd vinyl >/dev/null; then useradd --system --gid vinyl --home-dir /var/lib/vinyl-cache --shell /usr/sbin/nologin vinyl; fi
EOF
    cat >"$stage/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
install -d -o vinyl -g vinyl -m 0750 /etc/vinyl-cache /var/lib/vinyl-cache /var/cache/vinyl-cache
if [ -L /etc/vinyl-cache/secret ] || { [ -e /etc/vinyl-cache/secret ] && [ ! -f /etc/vinyl-cache/secret ]; }; then
  echo 'Refusing unsafe Vinyl secret path' >&2
  exit 1
fi
if [ ! -s /etc/vinyl-cache/secret ]; then
  umask 077
  temporary_secret="/etc/vinyl-cache/.secret.$$"
  trap 'rm -f -- "$temporary_secret"' EXIT HUP INT TERM
  openssl rand -hex 32 >"$temporary_secret"
  [ "$(wc -c <"$temporary_secret")" -eq 65 ]
  chown vinyl:vinyl "$temporary_secret"
  chmod 0600 "$temporary_secret"
  mv -f -- "$temporary_secret" /etc/vinyl-cache/secret
  trap - EXIT HUP INT TERM
fi
chown vinyl:vinyl /etc/vinyl-cache/secret
chmod 0600 /etc/vinyl-cache/secret
systemctl daemon-reload >/dev/null 2>&1 || true
systemctl enable vinyl.service >/dev/null 2>&1 || true
EOF
    chmod 0755 "$stage/DEBIAN/preinst" "$stage/DEBIAN/postinst"
    package_filename="vinyl-cache_${package_version}_debian-${target_os}${version_prefix}_amd64.deb"
    dpkg-deb --build --root-owner-group "$stage" "$output_directory/$package_filename" >/dev/null
    ;;
  rpm)
    top="$workspace/rpmbuild"
    mkdir -p "$top"/{BUILD,BUILDROOT,RPMS,SOURCES,SPECS,SRPMS}
    tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner -C "$stage" -czf "$top/SOURCES/payload.tar.gz" .
    cat >"$top/SPECS/vinyl-cache.spec" <<EOF
Name: vinyl-cache
Version: $vinyl_version
Release: 1.sf1%{?dist}
Summary: Stackfort-qualified Vinyl Cache 9 edge
License: BSD-2-Clause
URL: https://vinyl-cache.org/
Source0: payload.tar.gz
BuildArch: x86_64
Requires: openssl systemd
Conflicts: varnish
Provides: varnish

%description
Hash-pinned Vinyl Cache with a loopback-only, personalization-safe Stackfort VCL.

%prep
%build
%install
mkdir -p %{buildroot}
tar -xzf %{SOURCE0} -C %{buildroot}

%pre
getent group vinyl >/dev/null || groupadd --system vinyl
getent passwd vinyl >/dev/null || useradd --system --gid vinyl --home-dir /var/lib/vinyl-cache --shell /sbin/nologin vinyl

%post
install -d -o vinyl -g vinyl -m 0750 /etc/vinyl-cache /var/lib/vinyl-cache /var/cache/vinyl-cache
if [ -L /etc/vinyl-cache/secret ] || { [ -e /etc/vinyl-cache/secret ] && [ ! -f /etc/vinyl-cache/secret ]; }; then
  echo 'Refusing unsafe Vinyl secret path' >&2
  exit 1
fi
if [ ! -s /etc/vinyl-cache/secret ]; then
  umask 077
  temporary_secret="/etc/vinyl-cache/.secret.\$\$"
  trap 'rm -f -- "\$temporary_secret"' EXIT HUP INT TERM
  openssl rand -hex 32 >"\$temporary_secret"
  [ "\$(wc -c <"\$temporary_secret")" -eq 65 ]
  chown vinyl:vinyl "\$temporary_secret"
  chmod 0600 "\$temporary_secret"
  mv -f -- "\$temporary_secret" /etc/vinyl-cache/secret
  trap - EXIT HUP INT TERM
fi
chown vinyl:vinyl /etc/vinyl-cache/secret
chmod 0600 /etc/vinyl-cache/secret
systemctl daemon-reload >/dev/null 2>&1 || :
systemctl enable vinyl.service >/dev/null 2>&1 || :

%files
%attr(0750,vinyl,vinyl) %dir /etc/vinyl-cache
%attr(0644,root,root) /etc/vinyl-cache/stackfort.vcl
/usr/bin/vinyl*
/usr/bin/vtest
/usr/include/vinyl-cache
/usr/lib/systemd/system/vinyl.service
/usr/lib64/libvinylapi.so*
/usr/lib64/pkgconfig/vinylapi.pc
/usr/lib64/vinyl-cache
/usr/libexec/stackfort-vinyl-*
/usr/sbin/vinyld
/usr/share/aclocal/vinyl*.m4
/usr/share/doc/vinyl-cache
/usr/share/licenses/vinyl-cache
/usr/share/man/man1/vinyl*.1*
/usr/share/man/man3/vmod_*.3*
/usr/share/man/man7/*.7*
/usr/share/vinyl-cache
EOF
    rpmbuild --define "_topdir $top" --define "_source_date_epoch $source_date_epoch" -bb "$top/SPECS/vinyl-cache.spec" >/dev/null
    built="$(find "$top/RPMS" -type f -name '*.rpm' -print -quit)"
    [[ -n "$built" ]] || fail 'rpmbuild did not produce a package'
    package_version="$(rpm -qp --qf '%{VERSION}-%{RELEASE}' "$built")"
    package_filename="vinyl-cache-${vinyl_version}-1.sf1.rocky${version_prefix}.x86_64.rpm"
    cp "$built" "$output_directory/$package_filename"
    ;;
esac

package_path="$output_directory/$package_filename"
package_sha256="$(sha256sum "$package_path" | awk '{print $1}')"
package_size="$(stat -c '%s' "$package_path")"
cat >"$package_path.release.json" <<EOF
{
  "schemaVersion": 1,
  "kind": "vinyl-native-package",
  "distribution": "$target_os",
  "versionPrefix": "$version_prefix",
  "architecture": "$architecture",
  "format": "$package_format",
  "path": "",
  "sha256": "$package_sha256",
  "sizeBytes": $package_size,
  "packageName": "vinyl-cache",
  "packageVersion": "$package_version",
  "nginxPackageVersion": "",
  "corazaVersion": "",
  "libCorazaVersion": "",
  "corazaNGINXVersion": "",
  "owaspCRSVersion": "",
  "vinylVersion": "$vinyl_version",
  "filename": "$package_filename"
}
EOF
printf 'Created %s\n' "$package_path"
