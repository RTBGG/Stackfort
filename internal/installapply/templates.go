// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"fmt"
	"runtime"

	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

const managedHeader = "# Managed by Stackfort. Do not edit.\n"

const selinuxNGINXPanelPolicyPath = "/etc/stackfort/stackfort-nginx-panel.te"

func serviceUnits(distribution string) map[string]string {
	apiSandbox := ""
	if distribution == "debian" || distribution == "ubuntu" {
		apiSandbox = "AppArmorProfile=stackfort-api\n"
	}
	return map[string]string{
		"stackfort.slice": managedHeader + `[Unit]
Description=Stackfort service hierarchy
`,
		"stackfort-core.slice": managedHeader + `[Unit]
Description=Stackfort platform and control-plane services

[Slice]
CPUWeight=10000
IOWeight=10000
MemoryLow=20%
`,
		"stackfort-accounts.slice": managedHeader + fmt.Sprintf(`[Unit]
Description=Stackfort hosting account workloads

[Slice]
CPUQuota=%d%%
CPUQuotaPeriodSec=100ms
CPUWeight=100
IOWeight=100
MemoryHigh=75%%
MemoryMax=80%%
`, runtime.NumCPU()*80),
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
