// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"
)

func TestPHPMyAdminRuntimeIsDedicatedAndFailClosed(t *testing.T) {
	t.Parallel()
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		configuration, err := phpMyAdminFPMConfiguration(distribution)
		if err != nil {
			t.Fatalf("%s configuration: %v", distribution, err)
		}
		profile, _ := phpMyAdminPHPProfile(distribution)
		for _, required := range []string{
			"user = stackfort-pma", "group = " + profile.NGINXUser, "listen = " + phpMyAdminSocketPath,
			"listen.mode = 0660", "pm = ondemand",
			"error_log = syslog", "syslog.ident = stackfort-phpmyadmin",
			"clear_env = yes", "security.limit_extensions = .php", "session.cookie_secure] = on",
			"session.cookie_samesite] = Strict", "allow_url_include] = off", "open_basedir] = ",
		} {
			if !strings.Contains(configuration, required) {
				t.Errorf("%s FPM configuration lacks %q:\n%s", distribution, required, configuration)
			}
		}
		for _, forbidden := range []string{
			"listen = 127.0.0.1", "listen.mode = 0666", "display_errors] = on", "listen.owner = ",
			"error_log = /proc/self/fd/2",
		} {
			if strings.Contains(configuration, forbidden) {
				t.Errorf("%s FPM configuration contains %q", distribution, forbidden)
			}
		}

		unit := phpMyAdminServiceUnit(distribution)
		for _, required := range []string{
			"ExecStart=" + profile.BinaryPath + " --nodaemonize", "Slice=stackfort-core.slice",
			"User=stackfort-pma", "Group=" + profile.NGINXUser, "SupplementaryGroups=stackfort-pma",
			"NoNewPrivileges=yes", "ProtectSystem=strict", "IPAddressDeny=any",
			"IPAddressAllow=localhost", "ReadOnlyPaths=" + phpMyAdminConfigurationRoot,
		} {
			if !strings.Contains(unit, required) {
				t.Errorf("%s phpMyAdmin unit lacks %q:\n%s", distribution, required, unit)
			}
		}
	}
}

func TestPHPMyAdminDistributionSourcesAreExplicit(t *testing.T) {
	t.Parallel()
	for _, distribution := range []string{"debian", "ubuntu"} {
		root, err := phpMyAdminDocumentRoot(distribution)
		if err != nil || root != "/usr/share/phpmyadmin" {
			t.Fatalf("%s document root = %q / %v", distribution, root, err)
		}
		if _, native := phpMyAdminNativeConfigurationPath(distribution); !native {
			t.Fatalf("%s did not select its native package configuration", distribution)
		}
	}
	root, err := phpMyAdminDocumentRoot("rocky")
	if err != nil || root != "/usr/share/stackfort/phpmyadmin" {
		t.Fatalf("Rocky document root = %q / %v", root, err)
	}
	if _, native := phpMyAdminNativeConfigurationPath("rocky"); native {
		t.Fatal("Rocky unexpectedly selected a native phpMyAdmin package")
	}
}
