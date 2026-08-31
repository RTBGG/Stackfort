#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  printf 'Run this build-host preparation script as root.\n' >&2
  exit 1
fi

# shellcheck disable=SC1091
source /etc/os-release

case "${ID:-}" in
  debian|ubuntu)
    export DEBIAN_FRONTEND=noninteractive
    apt-get -o DPkg::Lock::Timeout=120 update
    apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends \
      autoconf automake binutils build-essential ca-certificates curl dpkg-dev \
      file libpcre2-dev libtool patch perl pkg-config xz-utils zlib1g-dev nginx
    ;;
  rocky)
    dnf install -y \
      autoconf automake binutils ca-certificates curl diffutils findutils gcc \
      gzip libtool make nginx patch pcre2-devel perl pkgconf-pkg-config rpm-build tar \
      which xz zlib-ng-compat-devel
    ;;
  *)
    printf 'Unsupported WAF build host: %s\n' "${ID:-unknown}" >&2
    exit 1
    ;;
esac
