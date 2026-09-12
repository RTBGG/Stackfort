// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingresources"
)

func TestInstallerSharesPlatformSlicesWithAccountRuntime(t *testing.T) {
	t.Parallel()
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		units := serviceUnits(distribution)
		if units["stackfort-core.slice"] != hostingresources.CoreSliceUnit() ||
			units["stackfort-accounts.slice"] != hostingresources.AccountsSliceUnit(uint64(runtime.NumCPU())) {
			t.Fatal("installer platform slice differs from account runtime", distribution)
		}
	}
}

func TestPHPRuntimeTmpfilesIsSharedAndNonDestructive(t *testing.T) {
	t.Parallel()
	if phpRuntimeTmpfilesPath != "/etc/tmpfiles.d/stackfort-php.conf" ||
		phpRuntimeTmpfiles() != managedHeader+"d /run/stackfort-php 0755 root root - -\n" {
		t.Fatal("PHP runtime must recreate only the root-owned parent without cleanup or recursive changes")
	}
}

func TestInstalledServiceInventoryIncludesRenewalUnits(t *testing.T) {
	t.Parallel()
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		names := serviceUnitNames(distribution)
		if !slices.IsSorted(names) || len(slices.Compact(slices.Clone(names))) != len(names) {
			t.Fatal("unit installation inventory must be deterministic and unique")
		}
		for _, required := range []string{"stackfort-panel-renew.service", "stackfort-panel-renew.timer"} {
			if !slices.Contains(names, required) {
				t.Fatalf("%s installation omits %s", distribution, required)
			}
		}
		for name, content := range serviceUnits(distribution) {
			wanted := distribution != "rocky" || name != "stackfort-firewall.service"
			if content == "" || slices.Contains(names, name) != wanted {
				t.Fatalf("%s installation/template inventory disagrees for %s", distribution, name)
			}
		}
	}
}

func TestServiceUnitsContainRequiredSandboxAndOwnershipContract(t *testing.T) {
	t.Parallel()

	debian := serviceUnits("debian")
	api := debian["stackfort-api.service"]
	agent := debian["stackfort-agent.service"]
	for _, required := range []string{
		"User=stackfort\n", "Group=stackfort\n", "NoNewPrivileges=yes\n", "PrivateDevices=yes\n",
		"PrivateTmp=yes\n", "ProtectSystem=strict\n", "Slice=stackfort-core.slice\n",
		"AppArmorProfile=stackfort-api\n", "ExecStart=/usr/local/bin/stackfort-api\n",
	} {
		if !strings.Contains(api, required) {
			t.Fatalf("API unit lacks %q:\n%s", required, api)
		}
	}
	for _, required := range []string{
		"User=root\n", "NoNewPrivileges=no\n", "PrivateDevices=no\n", "PrivateTmp=yes\n",
		"ProtectSystem=yes\n", "ProtectControlGroups=no\n", "Slice=stackfort-core.slice\n",
		"ProtectHome=no\n", "InaccessiblePaths=/home /root\n", "ReadWritePaths=/etc\n",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK\n",
		"ExecStart=/usr/local/sbin/stackfort-agent\n",
	} {
		if !strings.Contains(agent, required) {
			t.Fatalf("agent unit lacks %q:\n%s", required, agent)
		}
	}
	rockyAPI := serviceUnits("rocky")["stackfort-api.service"]
	if strings.Contains(rockyAPI, "AppArmorProfile=") {
		t.Fatal("Rocky API unit references AppArmor")
	}
	updater := debian["stackfort-update@.service"]
	for _, required := range []string{
		"Type=oneshot\n", "User=root\n", "ExecStartPre=/usr/bin/sleep 2\n", "ExecStart=/usr/local/sbin/stackfort-updater apply --version=%i --yes --format=json\n",
		"TimeoutStartSec=30min\n", "UMask=0077\n", "PrivateTmp=yes\n", "ProtectHome=yes\n",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6\n", "Restart=no\n",
	} {
		if !strings.Contains(updater, required) {
			t.Fatalf("updater unit lacks %q:\n%s", required, updater)
		}
	}
	pma := debian[phpMyAdminUnit]
	for _, required := range []string{
		"User=stackfort-pma\n", "Group=www-data\n", "SupplementaryGroups=stackfort-pma\n",
		"NoNewPrivileges=yes\n", "ProtectSystem=strict\n", "CapabilityBoundingSet=\n",
		"RestrictAddressFamilies=AF_UNIX AF_INET\n", "IPAddressDeny=any\n", "IPAddressAllow=localhost\n",
	} {
		if !strings.Contains(pma, required) {
			t.Fatalf("phpMyAdmin unit lacks %q:\n%s", required, pma)
		}
	}
}

func TestFirewallAndMandatoryAccessControlTemplatesAreNarrow(t *testing.T) {
	t.Parallel()

	firewall := nftablesFile()
	if !strings.Contains(firewall, "table inet stackfort") ||
		!strings.Contains(firewall, "tcp dport { 80, 443, 8443 } accept") ||
		strings.Contains(firewall, "flush ruleset") {
		t.Fatalf("unsafe nftables template:\n%s", firewall)
	}
	profile := appArmorProfile()
	if !strings.Contains(profile, "profile stackfort-api /usr/local/bin/stackfort-api") ||
		!strings.Contains(profile, "/var/lib/stackfort/** rwk,") ||
		strings.Contains(profile, "\n  /** rw") {
		t.Fatalf("unsafe AppArmor template:\n%s", profile)
	}
	selinuxPolicy := selinuxNGINXPanelPolicy()
	for _, required := range []string{
		"type stackfort_api_port_t;",
		"typeattribute stackfort_api_port_t port_type;",
		"allow httpd_t stackfort_api_port_t:tcp_socket name_connect;",
		"allow httpd_t varnishd_port_t:tcp_socket name_connect;",
	} {
		if !strings.Contains(selinuxPolicy, required) {
			t.Fatalf("SELinux panel policy lacks %q:\n%s", required, selinuxPolicy)
		}
	}
	for _, forbidden := range []string{"unreserved_port_t", "httpd_can_network_connect", "httpd_can_network_relay"} {
		if strings.Contains(selinuxPolicy, forbidden) {
			t.Fatalf("SELinux panel policy contains broad permission %q:\n%s", forbidden, selinuxPolicy)
		}
	}
}

func TestSliceTemplatesAvoidRemovedAccountingSwitches(t *testing.T) {
	t.Parallel()

	units := serviceUnits("ubuntu")
	for _, name := range []string{"stackfort.slice", "stackfort-core.slice", "stackfort-accounts.slice"} {
		for _, removed := range []string{"CPUAccounting=", "IOAccounting=", "MemoryAccounting=", "TasksAccounting="} {
			if strings.Contains(units[name], removed) {
				t.Fatalf("%s contains removed systemd option %s", name, removed)
			}
		}
	}
	firewall := units["stackfort-firewall.service"]
	if strings.Count(firewall, "ExecReload=") != 2 ||
		!strings.Contains(firewall, "CapabilityBoundingSet=CAP_NET_ADMIN\n") ||
		!strings.Contains(firewall, "RestrictAddressFamilies=AF_NETLINK\n") {
		t.Fatalf("firewall reload or capability boundary is incomplete:\n%s", firewall)
	}
}

func TestLogRetentionTemplateIsBoundedAndReopensNGINX(t *testing.T) {
	t.Parallel()
	content := logrotateFile()
	for _, required := range []string{
		"/var/log/stackfort/accounts/*/*.access.log", "daily\n", "rotate 7\n", "maxage 7\n",
		"maxsize 8M\n", "compress\n", "delaycompress\n", "nodateext\n", "create 0640 root root\n",
		"su root root\n", `kill -USR1 "$(cat /run/nginx.pid)"`,
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("log retention template omits %q:\n%s", required, content)
		}
	}
	for _, forbidden := range []string{"copytruncate", "rotate 0", "create 0644"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("log retention template contains %q:\n%s", forbidden, content)
		}
	}
}

func TestPanelRenewalUnitsUseInstalledBinaryAndBoundedSchedule(t *testing.T) {
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		units := serviceUnits(distribution)
		service := units["stackfort-panel-renew.service"]
		for _, required := range []string{"User=root\n", "ExecStart=/usr/local/sbin/stackfort-installer panel renew --yes --format=json\n", "TimeoutStartSec=5min\n", "ProtectSystem=full\n", "UMask=0077\n", "ReadWritePaths=/etc/nginx/stackfort /etc/stackfort/panel-tls /var/lib/stackfort-agent/acme-http01\n"} {
			if !strings.Contains(service, required) {
				t.Errorf("renewal service missing %q", required)
			}
		}
		for _, required := range []string{"OnCalendar=*-*-* 00,12:00:00\n", "RandomizedDelaySec=1h\n", "Persistent=yes\n", "WantedBy=timers.target\n"} {
			if !strings.Contains(units["stackfort-panel-renew.timer"], required) {
				t.Errorf("renewal timer missing %q", required)
			}
		}
	}
}
