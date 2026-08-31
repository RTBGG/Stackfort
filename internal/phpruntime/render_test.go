// SPDX-License-Identifier: AGPL-3.0-or-later

package phpruntime

import (
	"strings"
	"testing"
)

func TestRenderFPMConfigurationIsClosedAndAccountBound(t *testing.T) {
	t.Parallel()
	identity := testIdentity()
	spec := PoolSetSpec{
		Identity: identity, Versions: []string{"8.4"}, MaxChildren: 6, MemoryLimitMiB: 96,
	}
	profile, _ := ForDistribution("debian", "8.4")
	content, err := RenderFPMConfiguration(profile, spec)
	if err != nil {
		t.Fatalf("RenderFPMConfiguration: %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"user = " + identity.Username,
		"error_log = syslog",
		"syslog.ident = stackfort-php-200123",
		"listen = /run/stackfort-php/account-200123-php8.4.sock",
		"listen.owner = www-data",
		"listen.mode = 0600",
		"pm = ondemand",
		"pm.max_children = 6",
		"clear_env = yes",
		"security.limit_extensions = .php",
		"php_admin_value[memory_limit] = 96M",
		"php_admin_value[upload_tmp_dir] = " + identity.HomeDirectory + "/tmp",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("configuration omitted %q:\n%s", expected, text)
		}
	}
	for _, forbidden := range []string{"include =", "env[", "listen = 0.0.0.0", "daemonize = yes"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("configuration contains forbidden %q:\n%s", forbidden, text)
		}
	}
}

func TestRenderSystemdUnitUsesAccountSliceAndFixedBinary(t *testing.T) {
	t.Parallel()
	identity := testIdentity()
	spec := PoolSetSpec{
		Identity: identity, Versions: []string{"8.3"}, MaxChildren: 4, MemoryLimitMiB: 128,
	}
	profile, _ := ForDistribution("rocky", "8.3")
	content, err := RenderSystemdUnit(profile, spec)
	if err != nil {
		t.Fatalf("RenderSystemdUnit: %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"Type=notify",
		"NotifyAccess=main",
		"Slice=stackfort-accounts-200123.slice",
		"ExecStart=/usr/sbin/php-fpm --nodaemonize --fpm-config /etc/stackfort/php/account-200123-php8.3.conf",
		"CapabilityBoundingSet=CAP_CHOWN CAP_KILL CAP_SETGID CAP_SETUID",
		"ProtectSystem=strict",
		"NoNewPrivileges=yes",
		"ReadWritePaths=" + identity.HomeDirectory + " /run/stackfort-php",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("unit omitted %q:\n%s", expected, text)
		}
	}
}

func TestRenderRejectsProfileSubstitution(t *testing.T) {
	t.Parallel()
	identity := testIdentity()
	spec := PoolSetSpec{
		Identity: identity, Versions: []string{"8.4"}, MaxChildren: 4, MemoryLimitMiB: 128,
	}
	profile, _ := ForDistribution("debian", "8.4")
	profile.BinaryPath = "/tmp/evil"
	if _, err := RenderFPMConfiguration(profile, spec); err == nil {
		t.Fatal("substituted runtime profile was accepted")
	}
}
