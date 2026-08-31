#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "$script_directory/../.." && pwd)"
sources_lock="$script_directory/sources.lock"
targets_lock="$script_directory/targets.lock"
patches_lock="$script_directory/patches.lock"
output_directory="${1:-$repository_root/dist/waf}"
source_date_epoch="${SOURCE_DATE_EPOCH:-0}"

fail() {
  printf 'WAF build failed: %s\n' "$1" >&2
  exit 1
}

[[ "$(uname -s)" == Linux ]] || fail 'the native WAF bundle must be built on Linux'
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || fail 'SOURCE_DATE_EPOCH must be Unix seconds'
bash "$script_directory/verify-locks.sh"

# shellcheck disable=SC1091
source /etc/os-release
case "$(uname -m)" in
  x86_64) architecture=amd64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

target_line="$(awk -F '|' -v os="${ID:-}" -v arch="$architecture" '
  $1 == os && $3 == arch { if (found++) exit 2; print }
  END { if (found != 1) exit 1 }
' "$targets_lock")" || fail "no unique locked target for ${ID:-unknown}/$architecture"
IFS='|' read -r target_os target_version architecture nginx_version nginx_package_version package_format worker_account module_directory loader_path <<<"$target_line"
case "${VERSION_ID:-}" in
  "$target_version"*) ;;
  *) fail "host version ${VERSION_ID:-unknown} does not match locked $target_os $target_version" ;;
esac

for command in awk autoreconf automake curl find gcc gzip libtoolize make patch pkg-config readelf sha256sum strings strip tar; do
  command -v "$command" >/dev/null 2>&1 || fail "missing build command $command; run prepare-build-host.sh"
done
[[ -x /usr/sbin/nginx ]] || fail 'NGINX is not installed at /usr/sbin/nginx'

case "$package_format" in
  deb)
    installed_nginx_package="$(dpkg-query -W -f='${Version}' nginx 2>/dev/null)" || fail 'cannot query the NGINX package'
    ;;
  rpm)
    installed_nginx_package="$(rpm -q --qf '%{EPOCHNUM}:%{VERSION}-%{RELEASE}' nginx 2>/dev/null)" || fail 'cannot query the NGINX package'
    ;;
  *) fail "unsupported package format $package_format" ;;
esac
[[ "$installed_nginx_package" == "$nginx_package_version" ]] || fail "NGINX package is $installed_nginx_package; target lock requires $nginx_package_version"
installed_nginx_version="$(/usr/sbin/nginx -v 2>&1)"
[[ "$installed_nginx_version" == "nginx version: nginx/$nginx_version"* ]] || fail "NGINX binary does not have locked source version $nginx_version"

source_field() {
  local name="$1" field="$2"
  awk -F '|' -v name="$name" -v field="$field" '$1 == name { if (found++) exit 2; value=$field } END { if (found != 1) exit 1; print value }' "$sources_lock"
}

workspace="$(mktemp -d)"
cleanup() { rm -rf -- "$workspace"; }
trap cleanup EXIT
download_directory="$workspace/downloads"
mkdir -p "$download_directory" "$output_directory"

download_source() {
  local name="$1" url digest filename archive
  url="$(source_field "$name" 4)" || fail "source $name has no unique URL"
  digest="$(source_field "$name" 3)" || fail "source $name has no unique digest"
  filename="${url##*/}"
  [[ "$filename" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || fail "source $name has an unsafe filename"
  archive="$download_directory/$filename"
  curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --output "$archive" "$url"
  printf '%s  %s\n' "$digest" "$archive" | sha256sum --check --status || fail "source hash mismatch for $name"
  printf '%s\n' "$archive"
}

extract_archive() {
  local archive="$1" destination="$2"
  if tar -tf "$archive" | awk '$0 ~ /^\// || $0 ~ /(^|\/)\.\.(\/|$)/ { bad=1 } END { exit !bad }'; then
    fail "archive contains an unsafe path: ${archive##*/}"
  fi
  mkdir -p "$destination"
  tar -xf "$archive" --strip-components=1 -C "$destination"
}

libcoraza_archive="$(download_source libcoraza)"
coraza_archive="$(download_source coraza)"
connector_archive="$(download_source coraza-nginx)"
crs_archive="$(download_source owasp-crs)"
go_archive="$(download_source go-toolchain)"

go_root="$workspace/go-toolchain"
libcoraza_source="$workspace/libcoraza-source"
coraza_source="$workspace/coraza-source"
connector_source="$workspace/connector-source"
extract_archive "$go_archive" "$go_root"
extract_archive "$libcoraza_archive" "$libcoraza_source"
extract_archive "$coraza_archive" "$coraza_source"
extract_archive "$connector_archive" "$connector_source"
patch --batch --forward --directory="$connector_source" -p1 <"$script_directory/patches/0001-stackfort-sanitized-events.patch"

go_binary="$go_root/bin/go"
[[ -x "$go_binary" ]] || fail 'pinned Go toolchain is incomplete'
[[ "$($go_binary version)" == "go version go$(source_field go-toolchain 2) linux/amd64" ]] || fail 'pinned Go toolchain reports an unexpected version'
grep -Fx 'module github.com/corazawaf/libcoraza' "$libcoraza_source/go.mod" >/dev/null || fail 'libcoraza module identity differs from the lock'
grep -Fx 'go 1.25.0' "$libcoraza_source/go.mod" >/dev/null || fail 'libcoraza minimum Go version differs from the reviewed source'
grep -F "github.com/corazawaf/coraza/v3 v$(source_field coraza 2)" "$libcoraza_source/go.mod" >/dev/null || fail 'libcoraza does not pin the reviewed Coraza engine'
grep -Fx 'module github.com/corazawaf/coraza/v3' "$coraza_source/go.mod" >/dev/null || fail 'Coraza source identity differs from the lock'

compiler_map="-ffile-prefix-map=$workspace=. -fdebug-prefix-map=$workspace=."
export PATH="$go_root/bin:$PATH"
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org
export GOSUMDB=sum.golang.org
export GOFLAGS='-trimpath -buildvcs=false -mod=readonly -ldflags=-buildid='
export CGO_ENABLED=1
export CGO_CFLAGS="-O2 $compiler_map"
export CGO_LDFLAGS='-Wl,--build-id=none'
(
  cd "$libcoraza_source"
  "$go_binary" mod verify
  ./build.sh
  CFLAGS="-O2 $compiler_map" ./configure --prefix="$workspace/libcoraza-install"
  make --silent --jobs "$(getconf _NPROCESSORS_ONLN)" coraza/coraza.h libcoraza.so
)
libcoraza_library="$libcoraza_source/libcoraza.so"
libcoraza_header="$libcoraza_source/coraza/coraza.h"
[[ -f "$libcoraza_library" && -f "$libcoraza_header" ]] || fail 'libcoraza shared library or generated header is missing'

nginx_source="$workspace/nginx-source"
if [[ "$target_os" == ubuntu ]]; then
  command -v dpkg-source >/dev/null 2>&1 || fail 'missing build command dpkg-source; run prepare-build-host.sh'
  nginx_orig_archive="$(download_source nginx-ubuntu-orig)"
  nginx_debian_archive="$(download_source nginx-ubuntu-debian)"
  nginx_dsc="$(download_source nginx-ubuntu-dsc)"
  [[ -f "$nginx_orig_archive" && -f "$nginx_debian_archive" ]] || fail 'Ubuntu NGINX source layers are incomplete'
  [[ "$(source_field nginx-ubuntu-orig 2)" == "$nginx_version" ]] || fail 'Ubuntu NGINX orig source version differs from target lock'
  [[ "$(source_field nginx-ubuntu-debian 2)" == "$nginx_package_version" ]] || fail 'Ubuntu NGINX Debian source version differs from target lock'
  [[ "$(source_field nginx-ubuntu-dsc 2)" == "$nginx_package_version" ]] || fail 'Ubuntu NGINX DSC version differs from target lock'
  dpkg-source --extract "$nginx_dsc" "$nginx_source"
else
  nginx_archive="$(download_source "nginx-$nginx_version")"
  extract_archive "$nginx_archive" "$nginx_source"
fi

runtime_library_directory="/usr/lib/stackfort/coraza-$(source_field libcoraza 2)/lib"
(
  cd "$nginx_source"
  ./configure --with-compat \
    --with-cc-opt="-O2 $compiler_map -Wno-error=unused-function -I$libcoraza_source" \
    --with-ld-opt="-Wl,-rpath,$runtime_library_directory" \
    --add-dynamic-module="$connector_source"
  make --silent --jobs "$(getconf _NPROCESSORS_ONLN)" modules
)
module="$nginx_source/objs/ngx_http_coraza_module.so"
[[ -f "$module" ]] || fail 'coraza-nginx module was not built'
readelf -d "$module" | grep -E '(RPATH|RUNPATH)' | grep -F "$runtime_library_directory" >/dev/null || fail 'coraza-nginx lacks the locked runtime library path'
if readelf -d "$module" | grep -F 'Shared library: [libcoraza.so]' >/dev/null; then
  fail 'coraza-nginx must load libcoraza only after the worker fork'
fi

bundle_version="$(source_field libcoraza 2)-$(source_field coraza-nginx 2)-$(source_field owasp-crs 2)"
bundle_name="stackfort-waf-$bundle_version-$target_os$target_version-nginx$nginx_version-$architecture"
bundle_root="$workspace/$bundle_name"
rootfs="$bundle_root/root"
private_library_stage="$rootfs$runtime_library_directory"
mkdir -p "$private_library_stage" "$rootfs$module_directory" "$(dirname "$rootfs$loader_path")" \
  "$rootfs/usr/share/stackfort" "$bundle_root"
cp "$libcoraza_library" "$private_library_stage/libcoraza.so"
cp "$module" "$rootfs$module_directory/ngx_http_coraza_module.so"
printf 'load_module %s/ngx_http_coraza_module.so;\n' "$module_directory" >"$rootfs$loader_path"

engine_data_root="$rootfs/usr/share/stackfort/coraza-$(source_field coraza 2)"
mkdir -p "$engine_data_root"
cp "$coraza_source/coraza.conf-recommended" "$engine_data_root/coraza.conf-recommended"
cp "$libcoraza_source/go.mod" "$engine_data_root/libcoraza-go.mod"
cp "$libcoraza_source/go.sum" "$engine_data_root/libcoraza-go.sum"
crs_root="$rootfs/usr/share/stackfort/owasp-crs-$(source_field owasp-crs 2)"
extract_archive "$crs_archive" "$crs_root"
cp "$crs_root/crs-setup.conf.example" "$crs_root/crs-setup.conf"

license_root="$rootfs/usr/share/licenses/stackfort-waf"
mkdir -p "$license_root"
for license_source in \
  "$libcoraza_source/LICENSE:libcoraza-LICENSE" \
  "$coraza_source/LICENSE:coraza-LICENSE" \
  "$connector_source/LICENSE:coraza-nginx-LICENSE" \
  "$crs_root/LICENSE:owasp-crs-LICENSE" \
  "$nginx_source/LICENSE:nginx-LICENSE" \
  "$go_root/LICENSE:go-LICENSE"; do
  source_path="${license_source%%:*}"
  destination_name="${license_source##*:}"
  [[ -f "$source_path" ]] || fail "dependency license is missing: $source_path"
  cp "$source_path" "$license_root/$destination_name"
done

strip --strip-unneeded "$private_library_stage/libcoraza.so"
strip --strip-unneeded "$rootfs$module_directory/ngx_http_coraza_module.so"
chmod 0755 "$private_library_stage/libcoraza.so" "$rootfs$module_directory/ngx_http_coraza_module.so"

# nginx -t cannot detect a missing libcoraza: the connector intentionally calls
# dlopen() only after workers fork. Start a disposable worker and send a request.
worker_test_root="$workspace/worker-test"
worker_test_socket="$worker_test_root/nginx.sock"
worker_test_configuration="$worker_test_root/nginx.conf"
worker_test_error_log="$worker_test_root/error.log"
mkdir -p "$worker_test_root"
cat >"$worker_test_configuration" <<EOF
load_module $rootfs$module_directory/ngx_http_coraza_module.so;
pid $worker_test_root/nginx.pid;
error_log $worker_test_error_log notice;
worker_processes 1;
events {}
http {
  access_log off;
  client_body_temp_path $worker_test_root/client-body;
  proxy_temp_path $worker_test_root/proxy;
  fastcgi_temp_path $worker_test_root/fastcgi;
  uwsgi_temp_path $worker_test_root/uwsgi;
  scgi_temp_path $worker_test_root/scgi;
  server {
    listen unix:$worker_test_socket;
    coraza on;
    coraza_transaction_id \$request_id;
    coraza_rules '
      SecRuleEngine DetectionOnly
      SecRule ARGS:probe "@streq stackfort-sensitive-value" "id:920999,phase:1,pass,log,severity:2"
    ';
    location / { return 204; }
  }
}
EOF
LD_LIBRARY_PATH="$private_library_stage" /usr/sbin/nginx -c "$worker_test_configuration" -p "$worker_test_root" -e "$worker_test_error_log"
worker_started=0
for _ in $(seq 1 100); do
  if [[ -S "$worker_test_socket" ]] && curl --fail --silent --show-error --unix-socket "$worker_test_socket" 'http://localhost/?probe=stackfort-sensitive-value' >/dev/null; then
    worker_started=1
    break
  fi
  sleep 0.05
done
LD_LIBRARY_PATH="$private_library_stage" /usr/sbin/nginx -s quit -c "$worker_test_configuration" -p "$worker_test_root" -e "$worker_test_error_log" || true
[[ "$worker_started" -eq 1 ]] || { cat "$worker_test_error_log" >&2; fail 'coraza-nginx worker did not serve the qualification request'; }
grep -F 'coraza: libcoraza.so loaded via dynlib_open' "$worker_test_error_log" >/dev/null || fail 'coraza-nginx did not load libcoraza in the worker'
grep -F 'coraza: Stackfort event [id "920999"] [severity "CRITICAL"]' "$worker_test_error_log" >/dev/null || fail 'coraza-nginx did not emit the sanitized detection-only event'
if grep -F 'stackfort-sensitive-value' "$worker_test_error_log" >/dev/null; then
  strings "$worker_test_error_log" | grep -nF 'stackfort-sensitive-value' |
    sed 's/stackfort-sensitive-value/[REDACTED]/g' >&2
  fail 'coraza-nginx leaked match data into its sanitized event log'
fi

find "$rootfs" -type d -exec chmod 0755 {} +
find "$rootfs" -type f -exec chmod 0644 {} +
chmod 0755 "$private_library_stage/libcoraza.so" "$rootfs$module_directory/ngx_http_coraza_module.so"
(
  cd "$rootfs"
  find . -type f -print0 | sort -z | xargs -0 sha256sum >"$bundle_root/FILES.SHA256"
)

cat >"$bundle_root/manifest.json" <<EOF
{
  "schema": 1,
  "target": {
    "os": "$target_os",
    "versionPrefix": "$target_version",
    "architecture": "$architecture",
    "packageFormat": "$package_format",
    "nginxSourceVersion": "$nginx_version",
    "nginxPackageVersion": "$nginx_package_version",
    "nginxWorker": "$worker_account"
  },
  "components": {
    "coraza": "$(source_field coraza 2)",
    "libCoraza": "$(source_field libcoraza 2)",
    "corazaNGINX": "$(source_field coraza-nginx 2)",
    "connectorPatchSHA256": "$(awk 'NF == 2 { print $1 }' "$patches_lock")",
    "owaspCRS": "$(source_field owasp-crs 2)",
    "goToolchain": "$(source_field go-toolchain 2)"
  },
  "runtime": {
    "libraryDirectory": "$runtime_library_directory",
    "moduleDirectory": "$module_directory",
    "loaderPath": "$loader_path"
  }
}
EOF
cp "$sources_lock" "$bundle_root/SOURCES.lock"
cp "$targets_lock" "$bundle_root/TARGETS.lock"
cp "$patches_lock" "$bundle_root/PATCHES.lock"
mkdir -p "$bundle_root/patches"
cp "$script_directory/patches/0001-stackfort-sanitized-events.patch" "$bundle_root/patches/"
find "$bundle_root" -exec touch --date="@$source_date_epoch" {} +

artifact="$output_directory/$bundle_name.tar.gz"
tar --sort=name --mtime="@$source_date_epoch" --owner=0 --group=0 --numeric-owner \
  -C "$workspace" -cf - "$bundle_name" | gzip -n >"$artifact"
(
  cd "$output_directory"
  sha256sum "${artifact##*/}" >"${artifact##*/}.sha256"
)
printf 'Built and worker-tested %s\n' "$artifact"
