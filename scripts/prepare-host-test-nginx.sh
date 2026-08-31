#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later

set -eu

if [ "$(id -u)" -ne 0 ]; then
    echo "This disposable-host preparation must run as root." >&2
    exit 1
fi

# shellcheck disable=SC1091
. /etc/os-release

case "${ID:-}" in
    debian|ubuntu)
        export DEBIAN_FRONTEND=noninteractive
        apt-get -o DPkg::Lock::Timeout=120 update
        apt-get -o DPkg::Lock::Timeout=120 install -y --no-install-recommends nginx acl
        ;;
    rocky)
        dnf install -y nginx acl policycoreutils-python-utils
        ;;
    *)
        echo "Unsupported disposable-host distribution: ${ID:-unknown}" >&2
        exit 1
        ;;
esac

# Package post-install scripts may start NGINX. The baseline deliberately does
# not adopt a live unmanaged service, so make the disposable image quiescent on
# first use. An already Stackfort-managed service stays live for idempotency.
if [ ! -f /etc/nginx/stackfort/.stackfort-managed ]; then
    systemctl stop nginx.service
fi

test -x /usr/sbin/nginx
test -x /usr/bin/setfacl
if [ "${ID:-}" = "rocky" ]; then
    test -x /usr/sbin/semanage
    test -x /usr/sbin/restorecon
fi
test "$(systemctl show --property=LoadState --value nginx.service)" = "loaded"
