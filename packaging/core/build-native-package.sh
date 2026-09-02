#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source_argument="${1:-}"
package_format="${2:-}"
output_argument="${3:-$PWD/dist}"
source_date_epoch="${SOURCE_DATE_EPOCH:-0}"

fail() {
  printf 'Stackfort core packaging failed: %s\n' "$*" >&2
  exit 1
}

[[ -n "$source_argument" ]] || fail 'an assembled release directory is required'
[[ "$package_format" == deb || "$package_format" == rpm ]] || fail 'package format must be deb or rpm'
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || fail 'SOURCE_DATE_EPOCH must be a non-negative integer'

for command in awk basename cat cmp cp du find install mktemp realpath sed sha256sum sort stat tar touch wc xargs; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
done
case "$package_format" in
  deb)
    command -v dpkg-deb >/dev/null 2>&1 || fail 'dpkg-deb is unavailable'
    ;;
  rpm)
    for command in cpio gzip rpm rpm2cpio rpmbuild; do
      command -v "$command" >/dev/null 2>&1 || fail "required RPM command is unavailable: $command"
    done
    ;;
esac

source_root="$(realpath -e "$source_argument")"
output_directory="$(realpath -m "$output_argument")"
[[ -d "$source_root" && ! -L "$source_argument" ]] || fail 'release source must be a real directory, not a symbolic link'
case "$output_directory/" in
  "$source_root/"*) fail 'output directory must be outside the release source' ;;
esac

version_file="$source_root/VERSION"
manifest_file="$source_root/RELEASE-MANIFEST.json"
[[ -f "$version_file" && ! -L "$version_file" ]] || fail 'release VERSION is unavailable'
[[ -f "$manifest_file" && ! -L "$manifest_file" ]] || fail 'release manifest is unavailable'
IFS= read -r version <"$version_file" || fail 'release VERSION is empty'
[[ "$(wc -l <"$version_file")" -eq 1 && "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]] ||
  fail 'release VERSION is not a single supported semantic version'

json_string_field() {
  local field="$1"
  # Artifact records deliberately reuse keys such as architecture. The first
  # exact top-level occurrence is the release header emitted by the manifest
  # assembler; consume the full stream and retain only that value.
  sed -nE 's/^[[:space:]]*"'"$field"'"[[:space:]]*:[[:space:]]*"([^"]+)",?[[:space:]]*$/\1/p' \
    "$manifest_file" | sed -n '1p'
}

mapfile -t manifest_versions < <(json_string_field version)
mapfile -t manifest_architectures < <(json_string_field architecture)
[[ "${#manifest_versions[@]}" -eq 1 && "${manifest_versions[0]}" == "$version" ]] ||
  fail 'release manifest version does not match VERSION'
[[ "${#manifest_architectures[@]}" -eq 1 && "${manifest_architectures[0]}" == amd64 ]] ||
  fail 'native Stackfort core packages currently require an amd64 release tree'

for path in \
  bin/stackfort-api bin/stackfort-agent bin/stackfort-updater bin/stackfort-gh \
  bin/stackfort-installer bin/stackfort-trivy \
  web/index.html phpmyadmin/index.php phpmyadmin/config.inc.php \
  phpmyadmin-integration/config.inc.php phpmyadmin-integration/signon.php \
  phpmyadmin-integration/stackfort-launch.php COMMIT COPYRIGHT.md LICENSE README.md \
  RELEASE-MANIFEST.json VERSION; do
  [[ -f "$source_root/$path" && ! -L "$source_root/$path" ]] || fail "required release file is unavailable: $path"
done
for path in bin/stackfort-api bin/stackfort-agent bin/stackfort-updater bin/stackfort-gh bin/stackfort-installer bin/stackfort-trivy; do
  [[ -x "$source_root/$path" ]] || fail "release executable has no execute permission: $path"
done
[[ -z "$(find "$source_root" ! -type d ! -type f -print -quit)" ]] ||
  fail 'release source contains a symbolic link or special file'
[[ -z "$(find "$source_root" -perm /022 -print -quit)" ]] ||
  fail 'release source contains a group/world-writable entry'

numeric_version="${version%%[-+]*}"
version_suffix="${version#"$numeric_version"}"
package_version_base="$version"
if [[ "$version_suffix" == -* ]]; then
  package_version_base="$numeric_version~${version_suffix#-}"
fi
deb_version="$package_version_base-1"
rpm_version="${package_version_base//-/_}"

workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
package_root="$workspace/package-root"
release_destination="$package_root/usr/lib/stackfort/releases/$version"
mkdir -p "$release_destination"
cp -a -- "$source_root/." "$release_destination/"
install -D -m 0755 /dev/null "$package_root/usr/sbin/stackfort-install"
sed "s|@STACKFORT_VERSION@|$version|g" "$script_directory/stackfort-install.in" \
  >"$package_root/usr/sbin/stackfort-install"
install -D -m 0644 "$source_root/LICENSE" "$package_root/usr/share/licenses/stackfort-release/LICENSE"
install -D -m 0644 "$source_root/README.md" "$package_root/usr/share/doc/stackfort-release/README.md"
install -D -m 0644 "$script_directory/PACKAGE.md" "$package_root/usr/share/doc/stackfort-release/PACKAGE.md"
find "$package_root" -exec touch --date="@$source_date_epoch" {} +

temporary_artifact=''
package_version=''
case "$package_format" in
  deb)
    mkdir -p "$package_root/DEBIAN"
    installed_size="$(du -sk "$package_root/usr" | awk '{ print $1 }')"
    cat >"$package_root/DEBIAN/control" <<EOF
Package: stackfort-release
Version: $deb_version
Architecture: amd64
Maintainer: Stackfort Authors <noreply@stackfort.invalid>
Installed-Size: $installed_size
Section: admin
Priority: optional
Homepage: https://github.com/RTBGG/stackfort
Description: Immutable Stackfort $version release carrier
 Contains the complete, verified Stackfort release source and a thin manual
 installer entry point. Package installation does not configure or start
 Stackfort and never modifies customer data.
EOF
    find "$package_root/DEBIAN" -exec touch --date="@$source_date_epoch" {} +
    temporary_artifact="$workspace/stackfort-release_${deb_version}_amd64.deb"
    SOURCE_DATE_EPOCH="$source_date_epoch" dpkg-deb --root-owner-group -Zxz -z9 \
      --build "$package_root" "$temporary_artifact" >/dev/null
    package_version="$(dpkg-deb -f "$temporary_artifact" Version)"
    [[ "$package_version" == "$deb_version" && "$(dpkg-deb -f "$temporary_artifact" Architecture)" == amd64 ]] ||
      fail 'DEB metadata differs from the requested release'
    ;;
  rpm)
    rpm_top="$workspace/rpmbuild"
    mkdir -p "$rpm_top/BUILD" "$rpm_top/BUILDROOT" "$rpm_top/RPMS" \
      "$rpm_top/SOURCES" "$rpm_top/SPECS" "$rpm_top/SRPMS"
    tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner \
      -C "$workspace" -cf - package-root | gzip -n >"$rpm_top/SOURCES/stackfort-release-payload.tar.gz"
    spec="$rpm_top/SPECS/stackfort-release.spec"
    cat >"$spec" <<EOF
%global debug_package %{nil}
%global _build_id_links none
%global __strip /bin/true
%global __os_install_post %{nil}
Name:           stackfort-release
Version:        $rpm_version
Release:        1.sf1
Summary:        Immutable Stackfort $version release carrier
License:        AGPL-3.0-or-later
URL:            https://github.com/RTBGG/stackfort
Source0:        stackfort-release-payload.tar.gz
BuildArch:      x86_64
AutoReqProv:    no
Requires:       /bin/sh

%description
Contains the complete, verified Stackfort release source and a thin manual
installer entry point. Package installation does not configure or start
Stackfort and never modifies customer data.

%prep
%setup -q -c -T
tar -xzf %{SOURCE0}

%build

%install
mkdir -p %{buildroot}
cp -a package-root/. %{buildroot}/

%files
%dir /usr/lib/stackfort
%dir /usr/lib/stackfort/releases
/usr/lib/stackfort/releases/$version
/usr/sbin/stackfort-install
%license /usr/share/licenses/stackfort-release/LICENSE
%doc /usr/share/doc/stackfort-release/README.md
%doc /usr/share/doc/stackfort-release/PACKAGE.md

%changelog
* Sat Jan 01 2000 Stackfort Authors <noreply@stackfort.invalid> - $rpm_version-1.sf1
- Reproducible Stackfort $version release carrier.
EOF
    if ! SOURCE_DATE_EPOCH="$source_date_epoch" rpmbuild -bb "$spec" \
        --define "_topdir $rpm_top" \
        --define "_buildhost stackfort.invalid" \
        --define "use_source_date_epoch_as_buildtime 1" \
        --define "clamp_mtime_to_source_date_epoch 1" >"$workspace/rpmbuild.log" 2>&1; then
      cat "$workspace/rpmbuild.log" >&2
      fail 'rpmbuild failed'
    fi
    mapfile -t rpm_artifacts < <(find "$rpm_top/RPMS" -type f -name 'stackfort-release-*.rpm' -print)
    [[ "${#rpm_artifacts[@]}" -eq 1 ]] || fail 'rpmbuild did not produce exactly one package'
    temporary_artifact="${rpm_artifacts[0]}"
    package_version="$(rpm -qp --qf '%{VERSION}-%{RELEASE}' "$temporary_artifact")"
    [[ "$package_version" == "$rpm_version-1.sf1" &&
      "$(rpm -qp --qf '%{ARCH}' "$temporary_artifact")" == x86_64 ]] ||
      fail 'RPM metadata differs from the requested release'
    ;;
esac

verification_root="$workspace/verification-root"
mkdir -p "$verification_root"
case "$package_format" in
  deb) dpkg-deb --extract "$temporary_artifact" "$verification_root" ;;
  rpm) rpm2cpio "$temporary_artifact" | (cd "$verification_root" && cpio --extract --make-directories --quiet) ;;
esac
verified_release="$verification_root/usr/lib/stackfort/releases/$version"
[[ -d "$verified_release" ]] || fail 'native package omitted the versioned release root'

inventory_tree() {
  local root="$1"
  (cd "$root" && find . -printf '%y %m %P\n' | LC_ALL=C sort)
}
checksum_tree() {
  local root="$1"
  (cd "$root" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum)
}

cmp <(inventory_tree "$source_root") <(inventory_tree "$verified_release") >/dev/null ||
  fail 'native package changed the release tree shape or modes'
cmp <(checksum_tree "$source_root") <(checksum_tree "$verified_release") >/dev/null ||
  fail 'native package changed the release payload bytes'
for path in \
  usr/sbin/stackfort-install \
  usr/share/licenses/stackfort-release/LICENSE \
  usr/share/doc/stackfort-release/README.md \
  usr/share/doc/stackfort-release/PACKAGE.md; do
  cmp "$package_root/$path" "$verification_root/$path" >/dev/null ||
    fail "native package changed packaged file: /$path"
  [[ "$(stat -c '%a' "$package_root/$path")" == "$(stat -c '%a' "$verification_root/$path")" ]] ||
    fail "native package changed file mode: /$path"
done

mkdir -p "$output_directory"
artifact_name="$(basename "$temporary_artifact")"
artifact="$output_directory/$artifact_name"
install -m 0644 "$temporary_artifact" "$artifact"
artifact_digest="$(sha256sum "$artifact" | awk '{ print $1 }')"
artifact_size="$(stat -c '%s' "$artifact")"
(
  cd "$output_directory"
  printf '%s  %s\n' "$artifact_digest" "$artifact_name" >"$artifact_name.sha256"
)
cat >"$output_directory/$artifact_name.release.json" <<EOF
{
  "schemaVersion": 1,
  "kind": "core-native-package",
  "format": "$package_format",
  "architecture": "amd64",
  "stackfortVersion": "$version",
  "packageName": "stackfort-release",
  "packageVersion": "$package_version",
  "releaseRoot": "/usr/lib/stackfort/releases/$version",
  "filename": "$artifact_name",
  "sha256": "$artifact_digest",
  "sizeBytes": $artifact_size
}
EOF
printf 'Built verified Stackfort core package %s\n' "$artifact"
