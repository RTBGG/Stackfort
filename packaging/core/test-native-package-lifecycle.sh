#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

old_package="$(realpath -e "${1:-}")"
new_package="$(realpath -e "${2:-}")"

fail() {
  printf 'Stackfort native package lifecycle failed: %s\n' "$*" >&2
  exit 1
}

[[ "${EUID:-$(id -u)}" -eq 0 ]] || fail 'run the lifecycle test as root'
[[ -f "$old_package" && -f "$new_package" && ! -L "$old_package" && ! -L "$new_package" ]] ||
  fail 'two regular native package files are required'

case "$old_package:$new_package" in
  *.deb:*.deb) package_format=deb ;;
  *.rpm:*.rpm) package_format=rpm ;;
  *) fail 'both packages must use the same DEB or RPM format' ;;
esac

for command in basename cmp find grep mktemp realpath sha256sum stat; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
done
case "$package_format" in
  deb)
    for command in dpkg dpkg-deb dpkg-query; do
      command -v "$command" >/dev/null 2>&1 || fail "required DEB command is unavailable: $command"
    done
    if dpkg-query -W -f='${db:Status-Abbrev}' stackfort-release 2>/dev/null | grep -q '^ii'; then
      fail 'stackfort-release was installed before this test'
    fi
    ;;
  rpm)
    for command in cpio rpm rpm2cpio; do
      command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
    done
    if rpm -q stackfort-release >/dev/null 2>&1; then
      fail 'stackfort-release was installed before this test'
    fi
    ;;
esac

workspace="$(mktemp -d)"
installed=false
cleanup() {
  if [[ "$installed" == true ]]; then
    case "$package_format" in
      deb) dpkg --remove stackfort-release >/dev/null 2>&1 || true ;;
      rpm) rpm -e stackfort-release >/dev/null 2>&1 || true ;;
    esac
  fi
  case "$workspace" in
    /tmp/tmp.*) rm -rf -- "$workspace" ;;
    *) printf 'Refusing unsafe lifecycle workspace cleanup: %s\n' "$workspace" >&2 ;;
  esac
}
trap cleanup EXIT

active_fingerprint() {
  for path in \
    /usr/local/bin/stackfort-api \
    /usr/local/sbin/stackfort-agent \
    /usr/local/libexec/stackfort-trivy \
    /usr/share/stackfort/web/index.html; do
    if [[ -f "$path" && ! -L "$path" ]]; then
      sha256sum "$path"
      stat -c '%U:%G %a %n' "$path"
    else
      printf 'missing %s\n' "$path"
    fi
  done
}
active_fingerprint >"$workspace/active-before"

extract_package() {
  local package="$1"
  local destination="$2"
  mkdir -p "$destination"
  case "$package_format" in
    deb) dpkg-deb --extract "$package" "$destination" ;;
    rpm) rpm2cpio "$package" | (cd "$destination" && cpio --extract --make-directories --quiet) ;;
  esac
}

inspect_package() {
  local package="$1"
  local label="$2"
  local root="$workspace/$label"
  extract_package "$package" "$root"
  while IFS= read -r relative; do
    case "$relative" in
      usr | usr/lib | usr/lib/stackfort | usr/lib/stackfort/releases | usr/lib/stackfort/releases/* | \
        usr/sbin | usr/sbin/stackfort-install | \
        usr/share | usr/share/doc | usr/share/doc/stackfort-release | usr/share/doc/stackfort-release/* | \
        usr/share/licenses | usr/share/licenses/stackfort-release | usr/share/licenses/stackfort-release/*) ;;
      *) fail "$label package owns an unexpected path: /$relative" ;;
    esac
  done < <(cd "$root" && find . -mindepth 1 -printf '%P\n' | LC_ALL=C sort)
  mapfile -t release_roots < <(find "$root/usr/lib/stackfort/releases" -mindepth 1 -maxdepth 1 -type d -print)
  [[ "${#release_roots[@]}" -eq 1 ]] || fail "$label package does not contain exactly one release root"
  local release_version
  release_version="$(basename "${release_roots[0]}")"
  [[ -x "${release_roots[0]}/bin/stackfort-installer" ]] || fail "$label package omits its installer"
  [[ "$(<"${release_roots[0]}/VERSION")" == "$release_version" ]] ||
    fail "$label package release path and VERSION differ"
  grep -Fq "release_root='/usr/lib/stackfort/releases/$release_version'" \
    "$root/usr/sbin/stackfort-install" || fail "$label wrapper selects the wrong release"
  printf '%s\n' "$release_version"
}

old_release="$(inspect_package "$old_package" old)"
new_release="$(inspect_package "$new_package" new)"
[[ "$old_release" != "$new_release" ]] || fail 'upgrade packages contain the same Stackfort release'

case "$package_format" in
  deb)
    old_control="$workspace/old-control"
    new_control="$workspace/new-control"
    dpkg-deb --control "$old_package" "$old_control"
    dpkg-deb --control "$new_package" "$new_control"
    for control_root in "$old_control" "$new_control"; do
      [[ -z "$(find "$control_root" -maxdepth 1 -type f \
        ! -name control ! -name md5sums -print -quit)" ]] ||
        fail 'DEB contains a maintainer script or unexpected control program'
    done
    dpkg -i "$old_package" >/dev/null
    installed=true
    dpkg -i "$new_package" >/dev/null
    ;;
  rpm)
    [[ -z "$(rpm -qp --scripts "$old_package")" && -z "$(rpm -qp --scripts "$new_package")" ]] ||
      fail 'RPM contains an installation or removal scriptlet'
    rpm -Uvh "$old_package" >/dev/null
    installed=true
    rpm -Uvh "$new_package" >/dev/null
    ;;
esac

[[ ! -e "/usr/lib/stackfort/releases/$old_release" ]] || fail 'old release root survived package upgrade'
[[ -d "/usr/lib/stackfort/releases/$new_release" ]] || fail 'new release root is missing after package upgrade'
[[ -x /usr/sbin/stackfort-install ]] || fail 'installed package wrapper is not executable'
[[ "$(stat -c '%U:%G %a' /usr/sbin/stackfort-install)" == 'root:root 755' ]] ||
  fail 'installed package wrapper ownership or mode is unsafe'
[[ "$(stat -c '%U:%G %a' "/usr/lib/stackfort/releases/$new_release")" == 'root:root 755' ]] ||
  fail 'installed release root ownership or mode is unsafe'
/usr/sbin/stackfort-install version | grep -Fq 'stackfort-installer ' ||
  fail 'installed wrapper cannot execute its embedded installer'

case "$package_format" in
  deb) dpkg --remove stackfort-release >/dev/null ;;
  rpm) rpm -e stackfort-release >/dev/null ;;
esac
installed=false
[[ ! -e "/usr/lib/stackfort/releases/$new_release" && ! -e /usr/sbin/stackfort-install ]] ||
  fail 'package-owned release source survived removal'

active_fingerprint >"$workspace/active-after"
cmp "$workspace/active-before" "$workspace/active-after" >/dev/null ||
  fail 'carrier install, upgrade, or removal changed active Stackfort payload files'
printf 'STACKFORT_QUALIFICATION native-package-%s-install-upgrade-remove=passed\n' "$package_format"
