#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
sources_lock="$script_directory/sources.lock"
targets_lock="$script_directory/targets.lock"
patches_lock="$script_directory/patches.lock"
source_date_epoch="${SOURCE_DATE_EPOCH:-0}"

fail() {
  printf 'Stackfort WAF native packaging failed: %s\n' "$1" >&2
  exit 1
}

usage() {
  printf 'Usage: %s QUALIFICATION-BUNDLE.tar.gz [OUTPUT-DIRECTORY]\n' "${0##*/}" >&2
  exit 2
}

[[ "$#" -ge 1 && "$#" -le 2 ]] || usage
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || fail 'SOURCE_DATE_EPOCH must be a non-negative integer'
[[ "${EUID:-$(id -u)}" -ne 0 ]] || fail 'run this wrapper as an unprivileged build user'

bundle="$(realpath "$1")"
[[ -f "$bundle" ]] || fail "qualification bundle does not exist: $bundle"
[[ -f "$bundle.sha256" ]] || fail "qualification checksum is missing: $bundle.sha256"
output_directory="$(realpath -m "${2:-$PWD/dist}")"
mkdir -p "$output_directory"

for command in awk cmp find gzip install readlink realpath sed sha256sum sort stat tar touch; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
done

sidecar_name="$(awk 'NF == 2 { print $2 }' "$bundle.sha256")"
[[ "$(awk 'NF { count++ } END { print count + 0 }' "$bundle.sha256")" -eq 1 ]] || fail 'qualification checksum must contain exactly one record'
[[ "$sidecar_name" == "${bundle##*/}" ]] || fail 'qualification checksum must name only its adjacent bundle'
(
  cd "${bundle%/*}"
  sha256sum --check --strict "${bundle##*/}.sha256" >/dev/null
) || fail 'qualification bundle checksum does not match'

while IFS= read -r archive_path; do
  [[ "$archive_path" =~ ^[A-Za-z0-9._/+:-]+$ ]] || fail "archive contains an unsafe path: $archive_path"
  [[ "$archive_path" != /* ]] || fail "archive contains an absolute path: $archive_path"
  IFS='/' read -r -a archive_components <<<"$archive_path"
  for component in "${archive_components[@]}"; do
    [[ "$component" != '..' ]] || fail "archive contains parent traversal: $archive_path"
  done
done < <(tar -tzf "$bundle")

workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
tar -xzf "$bundle" -C "$workspace" --no-same-owner --no-same-permissions

mapfile -t bundle_roots < <(find "$workspace" -mindepth 1 -maxdepth 1 -type d -print)
[[ "${#bundle_roots[@]}" -eq 1 ]] || fail 'qualification archive must contain exactly one root directory'
bundle_root="${bundle_roots[0]}"
rootfs="$bundle_root/root"
manifest="$bundle_root/manifest.json"
[[ -d "$rootfs" && -f "$manifest" && -f "$bundle_root/FILES.SHA256" && -f "$bundle_root/PATCHES.lock" ]] || fail 'qualification archive is incomplete'
cmp -s "$sources_lock" "$bundle_root/SOURCES.lock" || fail 'qualification source lock differs from the packaging checkout'
cmp -s "$targets_lock" "$bundle_root/TARGETS.lock" || fail 'qualification target lock differs from the packaging checkout'
cmp -s "$patches_lock" "$bundle_root/PATCHES.lock" || fail 'qualification patch lock differs from the packaging checkout'
(
  cd "$bundle_root"
  sha256sum --check --strict PATCHES.lock >/dev/null
) || fail 'qualification connector patch does not match its lock'

manifest_value() {
  local key="$1"
  sed -nE "s/^[[:space:]]*\"$key\": \"([^\"]*)\",?[[:space:]]*$/\\1/p" "$manifest" |
    awk 'NR == 1 { value=$0 } END { if (NR != 1) exit 1; print value }'
}

source_version() {
  local name="$1"
  awk -F '|' -v name="$name" '$1 == name { if (found++) exit 2; value=$2 } END { if (found != 1) exit 1; print value }' "$sources_lock"
}

target_os="$(manifest_value os)"
target_version="$(manifest_value versionPrefix)"
target_architecture="$(manifest_value architecture)"
package_format="$(manifest_value packageFormat)"
nginx_source_version="$(manifest_value nginxSourceVersion)"
nginx_package_version="$(manifest_value nginxPackageVersion)"
worker_account="$(manifest_value nginxWorker)"
runtime_library_directory="$(manifest_value libraryDirectory)"
module_directory="$(manifest_value moduleDirectory)"
loader_path="$(manifest_value loaderPath)"

target_row="$(awk -F '|' -v os="$target_os" -v version="$target_version" -v architecture="$target_architecture" \
  '$1 == os && $2 == version && $3 == architecture { if (found++) exit 2; row=$0 } END { if (found != 1) exit 1; print row }' "$targets_lock")" ||
  fail 'qualification target is not uniquely locked'
IFS='|' read -r _ _ _ locked_nginx_source locked_nginx_package locked_format locked_worker locked_module_directory locked_loader_path <<<"$target_row"
[[ "$nginx_source_version" == "$locked_nginx_source" && "$nginx_package_version" == "$locked_nginx_package" ]] || fail 'qualification NGINX version differs from its target lock'
[[ "$package_format" == "$locked_format" && "$worker_account" == "$locked_worker" ]] || fail 'qualification package metadata differs from its target lock'
[[ "$module_directory" == "$locked_module_directory" && "$loader_path" == "$locked_loader_path" ]] || fail 'qualification installation paths differ from their target lock'

coraza_version="$(source_version coraza)"
libcoraza_version="$(source_version libcoraza)"
connector_version="$(source_version coraza-nginx)"
crs_version="$(source_version owasp-crs)"
go_toolchain_version="$(source_version go-toolchain)"
[[ "$(manifest_value coraza)" == "$coraza_version" && "$(manifest_value libCoraza)" == "$libcoraza_version" ]] || fail 'qualification Coraza versions differ from their source locks'
[[ "$(manifest_value corazaNGINX)" == "$connector_version" && "$(manifest_value owaspCRS)" == "$crs_version" ]] || fail 'qualification connector or CRS version differs from its source lock'
[[ "$(manifest_value connectorPatchSHA256)" == "$(awk 'NF == 2 { print $1 }' "$patches_lock")" ]] || fail 'qualification connector patch digest differs from its lock'
[[ "$(manifest_value goToolchain)" == "$go_toolchain_version" ]] || fail 'qualification Go toolchain differs from its source lock'
[[ "$runtime_library_directory" == "/usr/lib/stackfort/coraza-$libcoraza_version/lib" ]] || fail 'qualification private library path differs from the Coraza lock'

inventory_count=0
while read -r digest payload_path extra; do
  [[ "$digest" =~ ^[0-9a-f]{64}$ && -n "$payload_path" && -z "${extra:-}" ]] || fail 'qualification inventory contains a malformed record'
  [[ "$payload_path" == ./* && "$payload_path" != */../* && "$payload_path" != ../* ]] || fail "qualification inventory contains an unsafe path: $payload_path"
  case "$payload_path" in
    ".$loader_path"|".$module_directory/ngx_http_coraza_module.so") ;;
    ".$runtime_library_directory/"*) ;;
    "./usr/share/stackfort/coraza-$coraza_version/"*) ;;
    "./usr/share/stackfort/owasp-crs-$crs_version/"*) ;;
    "./usr/share/licenses/stackfort-waf/"*) ;;
    *) fail "qualification inventory contains an unowned path: $payload_path" ;;
  esac
  inventory_count=$((inventory_count + 1))
done <"$bundle_root/FILES.SHA256"
[[ "$inventory_count" -gt 0 ]] || fail 'qualification inventory is empty'

find "$rootfs" -type f -printf './%P\n' | sort >"$workspace/actual-payload-paths"
awk '{ print $2 }' "$bundle_root/FILES.SHA256" | sort >"$workspace/inventory-payload-paths"
cmp -s "$workspace/actual-payload-paths" "$workspace/inventory-payload-paths" || fail 'qualification inventory is not a complete payload file list'
[[ -z "$(find "$rootfs" -type f -links +1 -print -quit)" ]] || fail 'qualification payload contains hard-linked files'
[[ -z "$(find "$rootfs" ! -type d ! -type f ! -type l -print -quit)" ]] || fail 'qualification payload contains a special file'
(
  cd "$rootfs"
  sha256sum --check --strict ../FILES.SHA256 >/dev/null
) || fail 'qualification payload inventory does not match'

# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == "$target_os" && "${VERSION_ID:-}" == "$target_version"* ]] || fail 'build host does not match the qualification target'
[[ "$(uname -m)" == x86_64 && "$target_architecture" == amd64 ]] || fail 'build host architecture does not match the qualification target'
id "$worker_account" >/dev/null 2>&1 || fail "locked NGINX worker account is missing: $worker_account"

module_path="$rootfs$module_directory/ngx_http_coraza_module.so"
[[ -f "$module_path" && -f "$rootfs$loader_path" && -d "$rootfs$runtime_library_directory" ]] || fail 'qualification payload omits its module, loader, or private runtime libraries'

mapfile -t payload_symlinks < <(find "$rootfs" -type l -print | sort)
[[ "${#payload_symlinks[@]}" -eq 0 ]] || fail 'qualification payload must not contain symlinks'

case "$package_format" in
  deb)
    command -v dpkg-deb >/dev/null 2>&1 || fail 'dpkg-deb is unavailable; run prepare-build-host.sh'
    installed_nginx="$(dpkg-query -W -f='${Version}' nginx 2>/dev/null)" || fail 'the locked NGINX package is not installed'
    ;;
  rpm)
    for command in cpio rpm2cpio rpmbuild; do
      command -v "$command" >/dev/null 2>&1 || fail "$command is unavailable; run prepare-build-host.sh"
    done
    installed_nginx="$(rpm -q --qf '%{EPOCHNUM}:%{VERSION}-%{RELEASE}' nginx 2>/dev/null)" || fail 'the locked NGINX package is not installed'
    ;;
  *)
    fail "unsupported native package format: $package_format"
    ;;
esac
[[ "$installed_nginx" == "$nginx_package_version" ]] || fail "installed NGINX is $installed_nginx, lock requires $nginx_package_version"

package_release=1
package_version_deb="$libcoraza_version+nginx$connector_version+crs$crs_version-$package_release"
package_version_rpm="$libcoraza_version"
package_release_rpm="$package_release.sf1"
stage="$workspace/package-root"
mkdir -p "$stage"
cp -a "$rootfs/." "$stage/"
qualification_docs="$stage/usr/share/doc/stackfort-waf/qualification"
mkdir -p "$qualification_docs"
install -m 0644 "$manifest" "$qualification_docs/manifest.json"
install -m 0644 "$bundle_root/FILES.SHA256" "$qualification_docs/FILES.SHA256"
install -m 0644 "$bundle_root/SOURCES.lock" "$qualification_docs/SOURCES.lock"
install -m 0644 "$bundle_root/TARGETS.lock" "$qualification_docs/TARGETS.lock"
install -m 0644 "$bundle_root/PATCHES.lock" "$qualification_docs/PATCHES.lock"
install -m 0644 "$bundle_root/patches/0001-stackfort-sanitized-events.patch" "$qualification_docs/0001-stackfort-sanitized-events.patch"

standalone_module_test_script() {
  local module="$1"
  cat <<EOF
test_configuration=\$(mktemp)
trap 'rm -f -- "\$test_configuration"' EXIT HUP INT TERM
printf 'load_module $module;\\npid /run/stackfort-waf-package-test.pid;\\nerror_log stderr notice;\\nevents {}\\nhttp {}\\n' >"\$test_configuration"
/usr/sbin/nginx -t -q -e stderr -c "\$test_configuration" -p /var/cache/stackfort/coraza
rm -f -- "\$test_configuration"
trap - EXIT HUP INT TERM
EOF
}

if [[ "$package_format" == deb ]]; then
  command -v dpkg-shlibdeps >/dev/null 2>&1 || fail 'dpkg-shlibdeps is unavailable; run prepare-build-host.sh'
  command -v file >/dev/null 2>&1 || fail 'file is unavailable; run prepare-build-host.sh'
  mkdir -p "$workspace/debian/debian" "$stage/DEBIAN"
  cat >"$workspace/debian/debian/control" <<EOF
Source: stackfort-waf
Section: admin
Priority: optional
Maintainer: Stackfort Authors <noreply@stackfort.invalid>
Standards-Version: 4.7.2

Package: stackfort-waf
Architecture: amd64
Description: Stackfort private Coraza engine and OWASP CRS
EOF
  mapfile -t packaged_elfs < <(find "$stage$runtime_library_directory" "$stage$module_directory" -type f -exec file {} \; | awk -F: '$2 ~ /ELF/ { print $1 }' | sort)
  [[ "${#packaged_elfs[@]}" -eq 2 ]] || fail 'qualification payload must contain the Coraza module and one private runtime object'
  shlib_arguments=()
  for elf in "${packaged_elfs[@]}"; do
    shlib_arguments+=("-e$elf")
  done
  shlib_output="$(
    cd "$workspace/debian"
    dpkg-shlibdeps -O --ignore-missing-info -l"$stage$runtime_library_directory" "${shlib_arguments[@]}"
  )" || fail 'could not derive Debian shared-library dependencies'
  runtime_dependencies="${shlib_output#shlibs:Depends=}"
  [[ -n "$runtime_dependencies" && "$runtime_dependencies" != "$shlib_output" ]] || fail 'Debian shared-library dependency result is empty'
  installed_size="$(du -sk "$stage" | awk '{ print $1 }')"
  cat >"$stage/DEBIAN/control" <<EOF
Package: stackfort-waf
Version: $package_version_deb
Architecture: amd64
Maintainer: Stackfort Authors <noreply@stackfort.invalid>
Installed-Size: $installed_size
Section: admin
Priority: optional
Homepage: https://github.com/RTBGG/stackfort
Depends: nginx (= $nginx_package_version), $runtime_dependencies
Conflicts: libnginx-mod-http-modsecurity
Description: Stackfort private Coraza engine and OWASP CRS
 Hash-pinned Coraza, libcoraza, coraza-nginx, and OWASP Core Rule Set
 built for the exact NGINX package revision qualified by Stackfort.
EOF
  cat >"$stage/DEBIAN/postinst" <<EOF
#!/bin/sh
set -eu
install -d -o root -g root -m 0755 /var/cache/stackfort /var/cache/stackfort/coraza
install -d -o $worker_account -g $worker_account -m 0700 /var/cache/stackfort/coraza/data
$(standalone_module_test_script "$module_directory/ngx_http_coraza_module.so")
EOF
  chmod 0755 "$stage/DEBIAN/postinst"
  find "$stage" -exec touch --date="@$source_date_epoch" {} +
  artifact_name="stackfort-waf_${package_version_deb}_${target_os}${target_version}_amd64.deb"
  temporary_artifact="$workspace/$artifact_name"
  SOURCE_DATE_EPOCH="$source_date_epoch" dpkg-deb --root-owner-group -Zxz -z9 --build "$stage" "$temporary_artifact" >/dev/null
else
  rpm_top="$workspace/rpmbuild"
  mkdir -p "$rpm_top/BUILD" "$rpm_top/BUILDROOT" "$rpm_top/RPMS" "$rpm_top/SOURCES" "$rpm_top/SPECS" "$rpm_top/SRPMS"
  payload_archive="$rpm_top/SOURCES/stackfort-waf-payload.tar.gz"
  find "$stage" -exec touch --date="@$source_date_epoch" {} +
  tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner \
    -C "$workspace" -cf - package-root | gzip -n >"$payload_archive"
  spec="$rpm_top/SPECS/stackfort-waf.spec"
  cat >"$spec" <<EOF
%global debug_package %{nil}
%global _build_id_links none
%global __strip /bin/true
Name:           stackfort-waf
Version:        $package_version_rpm
Release:        $package_release_rpm%{?dist}
Summary:        Stackfort private Coraza engine and OWASP CRS
License:        Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause
URL:            https://github.com/RTBGG/stackfort
Source0:        stackfort-waf-payload.tar.gz
BuildArch:      x86_64
Requires:       nginx = $nginx_package_version
Conflicts:      mod_security

%description
Hash-pinned Coraza, libcoraza, coraza-nginx, and OWASP Core Rule Set built for
the exact NGINX package revision qualified by Stackfort.

%prep
%setup -q -c -T
tar -xzf %{SOURCE0}

%build

%install
mkdir -p %{buildroot}
cp -a package-root/. %{buildroot}/

%post
install -d -o root -g root -m 0755 /var/cache/stackfort /var/cache/stackfort/coraza
install -d -o $worker_account -g $worker_account -m 0700 /var/cache/stackfort/coraza/data
$(standalone_module_test_script "$module_directory/ngx_http_coraza_module.so")

%files
%config(noreplace) $loader_path
$module_directory/ngx_http_coraza_module.so
%dir /usr/lib/stackfort
%dir /usr/lib/stackfort/coraza-$libcoraza_version
$runtime_library_directory
%dir /usr/share/stackfort
/usr/share/stackfort/coraza-$coraza_version
/usr/share/stackfort/owasp-crs-$crs_version
%license /usr/share/licenses/stackfort-waf
%doc /usr/share/doc/stackfort-waf

%changelog
* Sat Jan 01 2000 Stackfort Authors <noreply@stackfort.invalid> - $package_version_rpm-$package_release_rpm
- Reproducible Stackfort WAF package.
EOF
  if ! SOURCE_DATE_EPOCH="$source_date_epoch" rpmbuild -bb "$spec" \
      --define "_topdir $rpm_top" \
      --define "_buildhost stackfort.invalid" \
      --define "use_source_date_epoch_as_buildtime 1" \
      --define "clamp_mtime_to_source_date_epoch 1" >"$workspace/rpmbuild.log" 2>&1; then
    cat "$workspace/rpmbuild.log" >&2
    fail 'rpmbuild failed'
  fi
  mapfile -t rpm_artifacts < <(find "$rpm_top/RPMS" -type f -name 'stackfort-waf-*.rpm' -print)
  [[ "${#rpm_artifacts[@]}" -eq 1 ]] || fail 'rpmbuild did not produce exactly one native package'
  temporary_artifact="${rpm_artifacts[0]}"
  artifact_name="${temporary_artifact##*/}"
fi

package_verification_root="$workspace/package-verification-root"
mkdir -p "$package_verification_root"
case "$package_format" in
  deb) dpkg-deb --extract "$temporary_artifact" "$package_verification_root" ;;
  rpm) rpm2cpio "$temporary_artifact" | (cd "$package_verification_root" && cpio --extract --make-directories --quiet) ;;
esac
(
  cd "$package_verification_root"
  sha256sum --check --strict "$bundle_root/FILES.SHA256" >/dev/null
) || fail 'native package changed the qualified payload bytes'
while read -r inventory_path; do
  relative_path="${inventory_path#./}"
  qualified_mode="$(stat -c '%a' "$rootfs/$relative_path")"
  packaged_mode="$(stat -c '%a' "$package_verification_root/$relative_path")"
  [[ "$qualified_mode" == "$packaged_mode" ]] || fail "native package changed qualified file mode: $inventory_path"
done <"$workspace/inventory-payload-paths"
find "$rootfs" -type l -printf '%P -> %l\n' | sort >"$workspace/qualified-symlinks"
find "$package_verification_root" -type l -printf '%P -> %l\n' | sort >"$workspace/packaged-symlinks"
cmp -s "$workspace/qualified-symlinks" "$workspace/packaged-symlinks" || fail 'native package changed qualified library symlinks'

artifact="$output_directory/$artifact_name"
install -m 0644 "$temporary_artifact" "$artifact"
artifact_digest="$(sha256sum "$artifact" | awk '{ print $1 }')"
artifact_bytes="$(stat -c '%s' "$artifact")"
case "$package_format" in
  deb) native_package_version="$(dpkg-deb -f "$artifact" Version)" ;;
  rpm) native_package_version="$(rpm -qp --qf '%{VERSION}-%{RELEASE}' "$artifact")" ;;
esac
(
  cd "$output_directory"
  sha256sum "$artifact_name" >"$artifact_name.sha256"
)
cat >"$output_directory/$artifact_name.release.json" <<EOF
{
  "schemaVersion": 1,
  "kind": "waf-native-package",
  "distribution": "$target_os",
  "versionPrefix": "$target_version",
  "architecture": "$target_architecture",
  "format": "$package_format",
  "path": "",
  "sha256": "$artifact_digest",
  "sizeBytes": $artifact_bytes,
  "packageName": "stackfort-waf",
  "packageVersion": "$native_package_version",
  "nginxPackageVersion": "$nginx_package_version",
  "corazaVersion": "$coraza_version",
  "libCorazaVersion": "$libcoraza_version",
  "corazaNGINXVersion": "$connector_version",
  "owaspCRSVersion": "$crs_version",
  "filename": "$artifact_name"
}
EOF
printf 'Built native Stackfort WAF package %s\n' "$artifact"
