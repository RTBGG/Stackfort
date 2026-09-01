#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

export GOTOOLCHAIN=local
version="${VERSION:-0.0.0-dev}"
version="${version#v}"
commit="${COMMIT:-$(git rev-parse --verify HEAD 2>/dev/null || printf 'unknown')}"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || printf '0')}"
waf_package_directory="${STACKFORT_WAF_PACKAGE_DIR:-}"
vinyl_package_directory="${STACKFORT_VINYL_PACKAGE_DIR:-}"
native_package_formats="${STACKFORT_NATIVE_PACKAGE_FORMATS:-deb,rpm}"

if [[ ! "$version" =~ ^[0-9]+.[0-9]+.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  printf 'VERSION is not a supported semantic version: %s\n' "$version" >&2
  exit 1
fi
if [[ ! "$source_date_epoch" =~ ^[0-9]+$ ]]; then
  printf 'SOURCE_DATE_EPOCH must contain seconds since the Unix epoch.\n' >&2
  exit 1
fi
if [[ -z "$waf_package_directory" && "$version" != '0.0.0-dev' ]]; then
  printf 'STACKFORT_WAF_PACKAGE_DIR is required for a release build.\n' >&2
  exit 1
fi
if [[ -z "$vinyl_package_directory" && "$version" != '0.0.0-dev' ]]; then
  printf 'STACKFORT_VINYL_PACKAGE_DIR is required for a release build.\n' >&2
  exit 1
fi
case "$native_package_formats" in
  deb,rpm) ;;
  none)
    if [[ "$version" != '0.0.0-dev' ]]; then
      printf 'Production releases may not omit native release packages.\n' >&2
      exit 1
    fi
    ;;
  *)
    printf 'STACKFORT_NATIVE_PACKAGE_FORMATS must be deb,rpm or development-only none.\n' >&2
    exit 1
    ;;
esac

build_date="$(date --utc --date="@$source_date_epoch" '+%Y-%m-%dT%H:%M:%SZ')"
linker_flags="-s -w -buildid= -X github.com/RTBGG/stackfort/internal/buildinfo.Version=$version -X github.com/RTBGG/stackfort/internal/buildinfo.Commit=$commit -X github.com/RTBGG/stackfort/internal/buildinfo.BuildDate=$build_date"
output_root="$repository_root/dist"
phpmyadmin_version="5.2.3"
phpmyadmin_sha256="12ba1c425fa4071abbd4e7668c9ebdeac0b0755a467a6d6d5026122bb47c102b"
phpmyadmin_workspace="$(mktemp -d)"
trivy_version="0.74.0"
trivy_workspace="$(mktemp -d)"
trap 'rm -rf -- "$phpmyadmin_workspace" "$trivy_workspace"' EXIT
phpmyadmin_archive="$phpmyadmin_workspace/phpmyadmin.tar.gz"
phpmyadmin_root="$phpmyadmin_workspace/root"

curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$phpmyadmin_archive" \
  "https://files.phpmyadmin.net/phpMyAdmin/$phpmyadmin_version/phpMyAdmin-$phpmyadmin_version-all-languages.tar.gz"
printf '%s  %s\n' "$phpmyadmin_sha256" "$phpmyadmin_archive" | sha256sum --check --status
if tar -tzf "$phpmyadmin_archive" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
  printf 'phpMyAdmin archive contains an unsafe path.\n' >&2
  exit 1
fi
mkdir -p "$phpmyadmin_root"
tar -xzf "$phpmyadmin_archive" --strip-components=1 -C "$phpmyadmin_root"
if find "$phpmyadmin_root" -type l -print -quit | grep -q .; then
  printf 'phpMyAdmin archive contains a symbolic link.\n' >&2
  exit 1
fi
rm -rf -- "$phpmyadmin_root/setup" "$phpmyadmin_root/examples"
cp packaging/phpmyadmin/config.inc.php "$phpmyadmin_root/config.inc.php"

rm -rf -- "$output_root"
mkdir -p "$output_root"

(
  cd web
  npm ci
  npm run build
)

architectures=(amd64)
if [[ "$version" == '0.0.0-dev' ]]; then
  architectures+=(arm64)
fi

prepare_trivy() {
  local architecture="$1"
  local destination="$2"
  local license_destination="$3"
  local archive_name checksum
  case "$architecture" in
    amd64)
      archive_name="trivy_${trivy_version}_Linux-64bit.tar.gz"
      checksum="2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a"
      ;;
    arm64)
      archive_name="trivy_${trivy_version}_Linux-ARM64.tar.gz"
      checksum="b94ce1976bbf3c15b514b605ee88be7c6d94a29be2302847ff01cb794d47aad5"
      ;;
    *)
      printf 'Unsupported Trivy architecture: %s\n' "$architecture" >&2
      return 1
      ;;
  esac
  local archive="$trivy_workspace/$archive_name"
  local extract_root="$trivy_workspace/$architecture"
  curl --fail --location --proto '=https' --tlsv1.2 \
    --output "$archive" \
    "https://github.com/aquasecurity/trivy/releases/download/v$trivy_version/$archive_name"
  printf '%s  %s\n' "$checksum" "$archive" | sha256sum --check --status
  if tar -tzf "$archive" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
    printf 'Trivy archive contains an unsafe path.\n' >&2
    return 1
  fi
  rm -rf -- "$extract_root"
  mkdir -p "$extract_root"
  tar -xzf "$archive" --no-same-owner -C "$extract_root" trivy LICENSE
  if [[ ! -f "$extract_root/trivy" || -L "$extract_root/trivy" ||
        ! -f "$extract_root/LICENSE" || -L "$extract_root/LICENSE" ]]; then
    printf 'Trivy archive does not contain the expected regular executable and license.\n' >&2
    return 1
  fi
  cp "$extract_root/trivy" "$destination"
  cp "$extract_root/LICENSE" "$license_destination"
}

for architecture in "${architectures[@]}"; do
  bundle_name="stackfort-$version-linux-$architecture"
  stage_root="$output_root/$bundle_name"
  mkdir -p "$stage_root/bin" "$stage_root/web" "$stage_root/phpmyadmin" \
    "$stage_root/phpmyadmin-integration" "$stage_root/third-party-licenses"

  GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$linker_flags" -o "$stage_root/bin/stackfort-api" ./cmd/stackfort-api
  GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$linker_flags" -o "$stage_root/bin/stackfort-agent" ./cmd/stackfort-agent
  GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$linker_flags" -o "$stage_root/bin/stackfort-installer" ./cmd/stackfort-installer
  GOOS=linux GOARCH="$architecture" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$linker_flags" -o "$output_root/stackfort-installer-$version-linux-$architecture" ./cmd/stackfort-installer
  prepare_trivy "$architecture" "$stage_root/bin/stackfort-trivy" \
    "$stage_root/third-party-licenses/trivy-LICENSE"

  cp -R web/dist/. "$stage_root/web/"
  cp -R "$phpmyadmin_root"/. "$stage_root/phpmyadmin/"
  cp packaging/phpmyadmin/config.inc.php packaging/phpmyadmin/signon.php \
    packaging/phpmyadmin/stackfort-launch.php "$stage_root/phpmyadmin-integration/"
  cp LICENSE COPYRIGHT.md README.md "$stage_root/"
  printf '%s\n' "$version" >"$stage_root/VERSION"
  printf '%s\n' "$commit" >"$stage_root/COMMIT"
  manifest_arguments=(
    --destination "$stage_root"
    --version "$version"
    --architecture "$architecture"
  )
  if [[ "$architecture" == amd64 && -n "$waf_package_directory" && -n "$vinyl_package_directory" ]]; then
    manifest_arguments+=(--package-dir "$waf_package_directory" --vinyl-package-dir "$vinyl_package_directory")
  else
    manifest_arguments+=(--allow-incomplete)
  fi
  go run ./cmd/stackfort-release-manifest "${manifest_arguments[@]}"
  chmod 0755 "$stage_root/bin/stackfort-api" "$stage_root/bin/stackfort-agent" \
    "$stage_root/bin/stackfort-installer" "$stage_root/bin/stackfort-trivy" \
    "$output_root/stackfort-installer-$version-linux-$architecture"
  find "$stage_root" -type f ! -path '*/bin/*' -exec chmod 0644 {} +
  find "$stage_root" -type d -exec chmod 0755 {} +
  find "$stage_root" -exec touch --date="@$source_date_epoch" {} +

  archive_tar="$output_root/$bundle_name.tar"
  tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner \
    --mode='u+rwX,go+rX,go-w' --exclude="$bundle_name/bin/*" \
    -C "$output_root" -cf "$archive_tar" "$bundle_name"
  tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner \
    --mode=0755 -C "$output_root" -rf "$archive_tar" \
    "$bundle_name/bin/stackfort-api" "$bundle_name/bin/stackfort-agent" \
    "$bundle_name/bin/stackfort-installer" "$bundle_name/bin/stackfort-trivy"
  gzip -n -c "$archive_tar" >"$output_root/$bundle_name.tar.gz"
  rm -f -- "$archive_tar"
  if [[ "$architecture" == amd64 && "$native_package_formats" == deb,rpm ]]; then
    SOURCE_DATE_EPOCH="$source_date_epoch" \
      bash packaging/core/build-native-package.sh "$stage_root" deb "$output_root"
    SOURCE_DATE_EPOCH="$source_date_epoch" \
      bash packaging/core/build-native-package.sh "$stage_root" rpm "$output_root"
  fi
  rm -rf -- "$stage_root"
done

(
  cd "$output_root"
  artifacts=(./*.tar.gz ./stackfort-installer-*)
  if [[ "$native_package_formats" == deb,rpm ]]; then
    artifacts+=(./*.deb ./*.rpm ./*.release.json)
  fi
  sha256sum --text "${artifacts[@]}" | sort -k2 >SHA256SUMS
)

printf 'Release artifacts created in %s\n' "$output_root"
