#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
[[ "${EUID:-$(id -u)}" -eq 0 ]] || { printf 'Run as root.\n' >&2; exit 1; }
# shellcheck disable=SC1091
source /etc/os-release
case "${ID:-}" in
  debian|ubuntu)
    export DEBIAN_FRONTEND=noninteractive
    apt-get -o DPkg::Lock::Timeout=120 update
    apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends \
      autoconf automake autotools-dev build-essential ca-certificates cpio curl \
      dpkg-dev file libedit-dev libjemalloc-dev libncurses-dev libpcre2-dev \
      libtool pkg-config python3-docutils python3-sphinx
    ;;
  rocky)
    dnf install -y 'dnf-command(config-manager)'
    dnf config-manager --set-enabled crb
    # EL10 ships jemalloc-devel in EPEL. Install the Rocky-provided repository
    # definition before resolving Vinyl's upstream build dependencies.
    dnf install -y epel-release
    dnf install -y autoconf automake ca-certificates chrpath cpio curl diffutils file \
      gcc jemalloc-devel libedit-devel libtool make ncurses-devel pcre2-devel \
      pkgconf-pkg-config python3-docutils python3-sphinx rpm-build
    ;;
  *) printf 'Unsupported Vinyl build host: %s\n' "${ID:-unknown}" >&2; exit 1 ;;
esac
