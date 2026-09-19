#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Destructive to a DISPOSABLE container only: install the exact built package
# through its declared dependencies, without prepare-build-host.sh or a compiler
# preinstalled. No host service, candidate archive or publication is modified.
set -euo pipefail
export LC_ALL=C
fail() { printf 'Vinyl runtime qualification failed: %s\n' "$1" >&2; exit 1; }
[[ ${EUID:-$(id -u)} -eq 0 && ${STACKFORT_DISPOSABLE_PACKAGE_TEST:-} == 1 ]] || fail 'explicit disposable-container root opt-in required'
[[ -f /.dockerenv || -f /run/.containerenv ]] || fail 'requires a disposable container, not a host installation'
[[ $# -eq 1 && -d $1 && ! -L $1 ]] || fail 'one built-package directory required'
if command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1; then
  fail 'compiler already present; runtime baseline is contaminated'
fi
directory=$(realpath "$1")
# shellcheck disable=SC1091
source /etc/os-release
shopt -s nullglob
case "${ID:-}:${VERSION_ID:-}" in
  debian:13|ubuntu:26.04)
    packages=("$directory"/vinyl-cache_*.deb)
    [[ ${#packages[@]} -eq 1 && ! -L ${packages[0]} ]] || fail 'expected exactly one native DEB'
    [[ $(dpkg-deb -f "${packages[0]}" Package) == vinyl-cache ]] || fail 'unexpected package identity'
    export DEBIAN_FRONTEND=noninteractive
    apt-get -o APT::Update::Error-Mode=any update
    apt-get install -y --no-install-recommends "${packages[0]}"
    for dependency in gcc libc6-dev; do
      [[ $(dpkg-query -W '-f=${db:Status-Abbrev}' "$dependency") == 'ii ' ]] || fail 'missing declared compiler/header dependency'
    done
    ;;
  rocky:10|rocky:10.*)
    packages=("$directory"/vinyl-cache-*.rpm)
    [[ ${#packages[@]} -eq 1 && ! -L ${packages[0]} ]] || fail 'expected exactly one native RPM'
    [[ $(rpm -qp --qf '%{NAME}' "${packages[0]}") == vinyl-cache ]] || fail 'unexpected package identity'
    # Same signed-repository prerequisite as the production installer, solely
    # for jemalloc. Do not install a development toolchain outside the RPM deps.
    dnf install -y epel-release
    dnf install -y --setopt=install_weak_deps=False "${packages[0]}"
    rpm -q gcc glibc-devel >/dev/null || fail 'missing declared compiler/header dependency'
    ;;
  *) fail 'unsupported runtime container' ;;
esac
fixture=$(mktemp -d /var/tmp/stackfort-vinyl-runtime.XXXXXXXX)
cleanup() {
  case "$fixture" in
    /var/tmp/stackfort-vinyl-runtime.????????) rm -rf -- "$fixture" ;;
    *) printf 'Unsafe runtime fixture cleanup refused.\n' >&2 ;;
  esac
}
trap cleanup EXIT
if ! timeout 60s /usr/sbin/vinyld -C -f /etc/vinyl-cache/stackfort.vcl >"$fixture/compile.log" 2>&1; then
  tail -n 12 "$fixture/compile.log" >&2
  fail 'installed managed VCL did not compile with declared runtime dependencies'
fi
printf 'STACKFORT_QUALIFICATION vinyl-minimal-runtime-vcl=passed\n'
