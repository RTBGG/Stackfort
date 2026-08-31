#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
sources_lock="$script_directory/sources.lock"
targets_lock="$script_directory/targets.lock"
patches_lock="$script_directory/patches.lock"

fail() {
  printf 'WAF lock verification failed: %s\n' "$1" >&2
  exit 1
}

[[ "$(awk 'NF { count++ } END { print count + 0 }' "$patches_lock")" -eq 1 ]] ||
  fail 'patches.lock must contain exactly one connector patch'
(
  cd "$script_directory"
  sha256sum --check --strict "${patches_lock##*/}" >/dev/null
) || fail 'connector patch does not match patches.lock'

verify_common_line() {
  local file="$1" line_number="$2" line="$3"
  [[ "$line" != *$'\r'* ]] || fail "$file:$line_number contains CRLF data"
  [[ "$line" != *' '* && "$line" != *$'\t'* ]] || fail "$file:$line_number contains whitespace"
}

locked_version() {
  local component="$1"
  awk -F '|' -v component="$component" '$1 == component { if (found++) exit 2; value=$2 } END { if (found != 1) exit 1; print value }' "$sources_lock"
}

declare -A source_names=()
line_number=0
# shellcheck disable=SC2094
while IFS='|' read -r name version digest url extra; do
  line_number=$((line_number + 1))
  [[ -n "$name" && "${name:0:1}" != '#' ]] || continue
  verify_common_line "$sources_lock" "$line_number" "$name|$version|$digest|$url${extra:+|$extra}"
  [[ -z "${extra:-}" ]] || fail "$sources_lock:$line_number has too many fields"
  [[ "$name" =~ ^[a-z0-9.-]+$ ]] || fail "$sources_lock:$line_number has an invalid name"
  [[ "$version" =~ ^[0-9][0-9A-Za-z.+~-]*$ ]] || fail "$sources_lock:$line_number has an invalid version"
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || fail "$sources_lock:$line_number has an invalid SHA-256"
  [[ "$url" =~ ^https://[^/?#]+/[^[:space:]]+$ ]] || fail "$sources_lock:$line_number has a non-HTTPS or malformed URL"
  [[ -z "${source_names[$name]:-}" ]] || fail "$sources_lock contains duplicate source $name"
  source_names[$name]=1
done <"$sources_lock"

for required in libcoraza coraza coraza-nginx owasp-crs go-toolchain nginx-1.26.3 \
  nginx-ubuntu-orig nginx-ubuntu-debian nginx-ubuntu-dsc; do
  [[ -n "${source_names[$required]:-}" ]] || fail "$sources_lock omits $required"
done

ubuntu_target="$(awk -F '|' '$1 == "ubuntu" && $2 == "26.04" && $3 == "amd64" { print; found++ } END { if (found != 1) exit 1 }' "$targets_lock")" || fail 'Ubuntu target lock is not unique'
IFS='|' read -r _ _ _ ubuntu_nginx_version ubuntu_nginx_package _ <<<"$ubuntu_target"
[[ "$(locked_version nginx-ubuntu-orig)" == "$ubuntu_nginx_version" ]] || fail 'Ubuntu NGINX orig source differs from target ABI'
[[ "$(locked_version nginx-ubuntu-debian)" == "$ubuntu_nginx_package" ]] || fail 'Ubuntu NGINX Debian source differs from target package'
[[ "$(locked_version nginx-ubuntu-dsc)" == "$ubuntu_nginx_package" ]] || fail 'Ubuntu NGINX DSC differs from target package'

policy_file="$script_directory/../../internal/wafconfig/policy.go"
if [[ -f "$policy_file" ]]; then
  policy_version() {
    local constant="$1"
    awk -F '"' -v constant="$constant" '$1 ~ "^[[:space:]]*" constant "[[:space:]]*=" { if (found++) exit 2; value=$2 } END { if (found != 1) exit 1; print value }' "$policy_file"
  }
  [[ "$(policy_version LibCorazaVersion)" == "$(locked_version libcoraza)" ]] || fail 'libcoraza lock differs from wafconfig policy'
  [[ "$(policy_version CorazaVersion)" == "$(locked_version coraza)" ]] || fail 'Coraza lock differs from wafconfig policy'
  [[ "$(policy_version CorazaNGINXVersion)" == "$(locked_version coraza-nginx)" ]] || fail 'coraza-nginx lock differs from wafconfig policy'
  [[ "$(policy_version GoToolchainVersion)" == "$(locked_version go-toolchain)" ]] || fail 'Go toolchain lock differs from wafconfig policy'
  [[ "$(policy_version CRSVersion)" == "$(locked_version owasp-crs)" ]] || fail 'OWASP CRS lock differs from wafconfig policy'
fi

declare -A target_names=()
line_number=0
# shellcheck disable=SC2094
while IFS='|' read -r os_id version_prefix architecture nginx_version package_version format worker module_directory loader_path extra; do
  line_number=$((line_number + 1))
  [[ -n "$os_id" && "${os_id:0:1}" != '#' ]] || continue
  verify_common_line "$targets_lock" "$line_number" "$os_id|$version_prefix|$architecture|$nginx_version|$package_version|$format|$worker|$module_directory|$loader_path${extra:+|$extra}"
  [[ -z "${extra:-}" ]] || fail "$targets_lock:$line_number has too many fields"
  [[ "$os_id" =~ ^(debian|ubuntu|rocky)$ ]] || fail "$targets_lock:$line_number has an invalid OS"
  [[ "$version_prefix" =~ ^[0-9]+([.][0-9]+)?$ ]] || fail "$targets_lock:$line_number has an invalid OS version"
  [[ "$architecture" == amd64 ]] || fail "$targets_lock:$line_number is not amd64"
  if [[ "$os_id" == ubuntu ]]; then
    [[ "$(locked_version nginx-ubuntu-orig)" == "$nginx_version" ]] || fail "$targets_lock:$line_number references a different Ubuntu NGINX source"
  else
    [[ -n "${source_names[nginx-$nginx_version]:-}" ]] || fail "$targets_lock:$line_number references an unlocked NGINX source"
  fi
  [[ "$format" =~ ^(deb|rpm)$ ]] || fail "$targets_lock:$line_number has an invalid package format"
  [[ "$worker" =~ ^[a-z_][a-z0-9_-]*$ ]] || fail "$targets_lock:$line_number has an invalid worker account"
  [[ "$module_directory" == /* && "$loader_path" == /* ]] || fail "$targets_lock:$line_number contains a relative installation path"
  key="$os_id-$version_prefix-$architecture"
  [[ -z "${target_names[$key]:-}" ]] || fail "$targets_lock contains duplicate target $key"
  target_names[$key]=1
done <"$targets_lock"

for required in debian-13-amd64 ubuntu-26.04-amd64 rocky-10-amd64; do
  [[ -n "${target_names[$required]:-}" ]] || fail "$targets_lock omits $required"
done

printf 'WAF source and target locks are valid.\n'
