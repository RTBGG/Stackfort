#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  printf 'Stackfort host validation requires Linux.\n' >&2
  exit 1
fi

# shellcheck disable=SC1091
source /etc/os-release

expected_id="${STACKFORT_EXPECTED_OS_ID:-}"
expected_version="${STACKFORT_EXPECTED_VERSION_PREFIX:-}"
quota_path="${STACKFORT_QUOTA_PATH:-/srv/stackfort-quota}"
failures=0

pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1" >&2; failures=$((failures + 1)); }
info() { printf 'INFO  %s\n' "$1"; }

info "distribution=${ID:-unknown} version=${VERSION_ID:-unknown} kernel=$(uname -r) architecture=$(uname -m)"

if [[ -n "$expected_id" && "${ID:-}" != "$expected_id" ]]; then
  fail "distribution ID is ${ID:-unknown}; expected $expected_id"
else
  pass "distribution ID"
fi
if [[ -n "$expected_version" && "${VERSION_ID:-}" != "$expected_version"* ]]; then
  fail "distribution version is ${VERSION_ID:-unknown}; expected prefix $expected_version"
else
  pass "distribution version"
fi

if [[ "$(cat /proc/1/comm)" == "systemd" ]]; then
  pass "systemd is PID 1"
else
  fail "systemd is not PID 1"
fi

if [[ -f /sys/fs/cgroup/cgroup.controllers ]]; then
  pass "cgroup v2 unified hierarchy"
  controllers="$(cat /sys/fs/cgroup/cgroup.controllers)"
  for controller in cpu io memory pids; do
    if [[ " $controllers " == *" $controller "* ]]; then
      pass "cgroup controller $controller"
    else
      fail "cgroup controller $controller is unavailable"
    fi
  done
else
  fail "cgroup v2 unified hierarchy is unavailable"
fi

if [[ ! -e "$quota_path" ]]; then
  fail "quota test path does not exist: $quota_path"
else
  filesystem="$(findmnt -n -o FSTYPE --target "$quota_path")"
  mount_options="$(findmnt -n -o OPTIONS --target "$quota_path")"
  info "quota_path=$quota_path filesystem=$filesystem options=$mount_options"
  if [[ "$filesystem" == "xfs" && ",$mount_options," == *,prjquota,* ]] ||
     [[ "$filesystem" == "xfs" && ",$mount_options," == *,pquota,* ]] ||
     [[ "$filesystem" == "ext4" && ",$mount_options," == *,prjquota,* ]]; then
    pass "project quota mount option"
  else
    fail "project quotas are not active on $quota_path"
  fi
fi

case "${ID:-}" in
  rocky|rhel|almalinux)
    if command -v getenforce >/dev/null && [[ "$(getenforce)" != "Disabled" ]]; then
      pass "SELinux is enabled"
    else
      fail "SELinux is disabled or unavailable"
    fi
    ;;
  debian|ubuntu)
    if [[ -r /sys/module/apparmor/parameters/enabled ]] && grep -q '^Y' /sys/module/apparmor/parameters/enabled; then
      pass "AppArmor is enabled"
    else
      fail "AppArmor is disabled or unavailable"
    fi
    ;;
  *)
    fail "unsupported distribution ID: ${ID:-unknown}"
    ;;
esac

if [[ -r /proc/sys/kernel/unprivileged_userns_clone ]] && [[ "$(cat /proc/sys/kernel/unprivileged_userns_clone)" != "1" ]]; then
  fail "unprivileged user namespaces are disabled"
else
  pass "user namespace capability"
fi

if (( failures != 0 )); then
  printf '%d required host capabilities failed.\n' "$failures" >&2
  exit 1
fi

printf 'Stackfort host capability validation passed.\n'
