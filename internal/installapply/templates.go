// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"runtime"
	"sort"

	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

const managedHeader = "# Managed by Stackfort. Do not edit.\n"

const phpRuntimeTmpfilesPath = "/etc/tmpfiles.d/stackfort-php.conf"

// The socket parent is shared by independently managed PHP pools and must not
// disappear when any one pool stops. Recreate only the parent at every boot.
func phpRuntimeTmpfiles() string {
	return managedHeader + "d " + phpruntime.RuntimeRoot + " 0755 root root - -\n"
}

const selinuxNGINXPanelPolicyPath = "/etc/stackfort/stackfort-nginx-panel.te"

// Use the rendered inventory for both installation and verification so newly
// added units cannot exist only as templates and be omitted from the host.
func serviceUnitNames(distribution string) []string {
	units := serviceUnits(distribution)
	if distribution == "rocky" {
		delete(units, "stackfort-firewall.service")
	}
	names := make([]string, 0, len(units))
	for name := range units {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func serviceUnits(distribution string) map[string]string {
	apiSandbox := ""
	if distribution == "debian" || distribution == "ubuntu" {
		apiSandbox = "AppArmorProfile=stackfort-api\n"
	}
	processorCount := runtime.NumCPU()
	if processorCount < 1 {
		// Never turn an invalid signed count into an enormous account quota.
		processorCount = 1
	}
	return map[string]string{
		"stackfort-panel-renew.service": managedHeader + `[Unit]
Description=Stackfort panel certificate renewal and interruption recovery
After=network-online.target nginx.service
Wants=network-online.target
ConditionPathExists=/etc/nginx/stackfort/.stackfort-managed

[Service]
Type=oneshot
User=root
Group=root
ExecStart=/usr/local/sbin/stackfort-installer panel renew --yes --format=json
TimeoutStartSec=5min
Slice=stackfort-core.slice
UMask=0077
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=full
ReadWritePaths=/etc/nginx/stackfort /etc/stackfort/panel-tls /var/lib/stackfort-agent/acme-http01
ProtectHome=yes
ProtectClock=yes
ProtectKernelLogs=yes
ProtectKernelModules=yes
ProtectKernelTunables=yes
LockPersonality=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
`,
		"stackfort-panel-renew.timer": managedHeader + `[Unit]
Description=Check the Stackfort panel certificate twice daily

[Timer]
OnBootSec=10min
OnCalendar=*-*-* 00,12:00:00
RandomizedDelaySec=1h
Persistent=yes

[Install]
WantedBy=timers.target
`,
		"stackfort.slice": managedHeader + `[Unit]
Description=Stackfort service hierarchy
`,
		"stackfort-core.slice":     hostingresources.CoreSliceUnit(),
		"stackfort-accounts.slice": hostingresources.AccountsSliceUnit(uint64(processorCount)),
		"stackfort-agent.service": managedHeader + `[Unit]
Description=Stackfort privileged host agent
After=local-fs.target
Before=stackfort-api.service
RequiresMountsFor=/srv/hosting

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/local/sbin/stackfort-agent
Restart=on-failure
RestartSec=2s
Slice=stackfort-core.slice
RuntimeDirectory=stackfort
RuntimeDirectoryMode=0750
UMask=0027
NoNewPrivileges=yes
PrivateDevices=yes
PrivateTmp=yes
ProtectClock=yes
ProtectControlGroups=yes
ProtectHome=yes
ProtectKernelLogs=yes
ProtectKernelModules=yes
ProtectKernelTunables=yes
ProtectSystem=full
LockPersonality=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
`,
		"stackfort-api.service": managedHeader + `[Unit]
Description=Stackfort control API
After=network-online.target stackfort-agent.service
Requires=stackfort-agent.service
Wants=network-online.target

[Service]
Type=simple
User=stackfort
Group=stackfort
ExecStart=/usr/local/bin/stackfort-api
EnvironmentFile=/etc/stackfort/stackfort.env
Restart=on-failure
RestartSec=2s
Slice=stackfort-core.slice
StateDirectory=stackfort
StateDirectoryMode=0750
UMask=0027
NoNewPrivileges=yes
PrivateDevices=yes
PrivateTmp=yes
ProtectClock=yes
ProtectControlGroups=yes
ProtectHome=yes
ProtectKernelLogs=yes
ProtectKernelModules=yes
ProtectKernelTunables=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/stackfort
LockPersonality=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
` + apiSandbox + `
[Install]
WantedBy=multi-user.target
`,
		"stackfort-update@.service": managedHeader + `[Unit]
Description=Stackfort staged update to %i
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=root
Group=root
ExecStartPre=/usr/bin/sleep 2
ExecStart=/usr/local/sbin/stackfort-updater apply --version=%i --yes --format=json
TimeoutStartSec=30min
Restart=no
Slice=stackfort-core.slice
UMask=0077
PrivateTmp=yes
ProtectClock=yes
ProtectHome=yes
ProtectKernelLogs=yes
ProtectKernelModules=yes
ProtectKernelTunables=yes
LockPersonality=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallArchitectures=native
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=6
`,
		phpMyAdminUnit: phpMyAdminServiceUnit(distribution),
		"stackfort-firewall.service": managedHeader + `[Unit]
Description=Stackfort dedicated nftables ingress rules
After=network-pre.target
Before=network.target
ConditionPathExists=/etc/stackfort/firewall.nft

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=-/usr/sbin/nft delete table inet stackfort
ExecStart=/usr/sbin/nft -f /etc/stackfort/firewall.nft
ExecReload=-/usr/sbin/nft delete table inet stackfort
ExecReload=/usr/sbin/nft -f /etc/stackfort/firewall.nft
ExecStop=-/usr/sbin/nft delete table inet stackfort
NoNewPrivileges=yes
PrivateDevices=yes
ProtectHome=yes
ProtectControlGroups=yes
ProtectKernelModules=yes
ProtectKernelTunables=yes
ProtectSystem=strict
ReadOnlyPaths=/etc/stackfort/firewall.nft
CapabilityBoundingSet=CAP_NET_ADMIN
RestrictAddressFamilies=AF_NETLINK
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
`,
	}
}

func environmentFile() string {
	return managedHeader + `STACKFORT_API_ADDRESS=127.0.0.1:8080
STACKFORT_STATE_PATH=/var/lib/stackfort/stackfort.db
STACKFORT_MASTER_KEY_PATH=/var/lib/stackfort/master.key
`
}

func logrotateFile() string {
	return hostinglogs.RetentionConfiguration()
}

func nftablesFile() string {
	return managedHeader + `table inet stackfort {
    chain input {
        type filter hook input priority -10; policy accept;
        tcp dport { 80, 443, 8443 } accept
    }
}
`
}

func appArmorProfile() string {
	return managedHeader + `#include <tunables/global>

profile stackfort-api /usr/local/bin/stackfort-api flags=(attach_disconnected) {
  #include <abstractions/base>
  #include <abstractions/nameservice>

  network inet stream,
  network inet6 stream,
  network unix stream,

  /usr/local/bin/stackfort-api mr,
  /var/lib/stackfort/ rw,
  /var/lib/stackfort/** rwk,
  /var/lib/stackfort-phpmyadmin-broker/ r,
  /var/lib/stackfort-phpmyadmin-broker/broker.key r,
  /run/stackfort/ r,
  /run/stackfort/agent.sock rw,
  /etc/ssl/certs/** r,
  /usr/share/ca-certificates/** r,
  /etc/ca-certificates.conf r,
  /proc/sys/net/core/somaxconn r,
}
`
}

func selinuxNGINXPanelPolicy() string {
	return `module stackfort_nginx_panel 1.1;

require {
  type httpd_t;
  type varnishd_port_t;
  attribute port_type;
  class tcp_socket name_connect;
}

type stackfort_api_port_t;
typeattribute stackfort_api_port_t port_type;

# Permit the confined NGINX worker to reach only ports carrying Stackfort's
# dedicated control-API label. The installer assigns that label only to
# TCP/8080; it does not enable broad httpd network-connect booleans.
allow httpd_t stackfort_api_port_t:tcp_socket name_connect;

# Permit NGINX to reach only Vinyl's distribution-defined listener type.
# No broad HTTP-daemon network-connect or network-relay boolean is enabled.
allow httpd_t varnishd_port_t:tcp_socket name_connect;
`
}
