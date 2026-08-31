// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"slices"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/wafconfig"
)

func TestRockyPackagesIncludeSELinuxModuleToolchain(t *testing.T) {
	t.Parallel()

	packages := strings.Join(installerPackages("rocky"), " ")
	for _, required := range []string{"checkpolicy", "policycoreutils", "policycoreutils-python-utils"} {
		if !strings.Contains(" "+packages+" ", " "+required+" ") {
			t.Fatalf("Rocky packages lack %s: %s", required, packages)
		}
	}
}

func TestInstallerPackagesSelectNativePHPRuntime(t *testing.T) {
	t.Parallel()
	for distribution, runtimePackage := range map[string]string{
		"debian": "php8.4-fpm", "ubuntu": "php8.5-fpm", "rocky": "php-fpm",
	} {
		packages := " " + strings.Join(installerPackages(distribution), " ") + " "
		if !strings.Contains(packages, " "+runtimePackage+" ") {
			t.Fatalf("%s packages lack %s: %s", distribution, runtimePackage, packages)
		}
		if !strings.Contains(packages, " mariadb-server ") {
			t.Fatalf("%s package plan lacks MariaDB: %s", distribution, packages)
		}
		if !strings.Contains(packages, " logrotate ") {
			t.Fatalf("%s package plan lacks deterministic log retention: %s", distribution, packages)
		}
		for _, runtimePackage := range []string{"podman", "netavark", "aardvark-dns", "passt", "slirp4netns", "fuse-overlayfs"} {
			if !strings.Contains(packages, " "+runtimePackage+" ") {
				t.Fatalf("%s package plan lacks rootless runtime package %s: %s", distribution, runtimePackage, packages)
			}
		}
		mappingPackage := "uidmap"
		if distribution == "rocky" {
			mappingPackage = "shadow-utils-subid"
		}
		if !strings.Contains(packages, " "+mappingPackage+" ") {
			t.Fatalf("%s package plan lacks subordinate-ID helpers: %s", distribution, packages)
		}
		if !strings.Contains(packages, " cli ") && !strings.Contains(packages, "-cli ") {
			t.Fatalf("%s package plan lacks an explicit PHP CLI: %s", distribution, packages)
		}
		for _, extension := range []string{"curl", "gd", "intl", "mbstring", "mysql", "xml", "zip"} {
			if distribution == "rocky" && extension == "curl" {
				extension = "common"
			}
			if distribution == "rocky" && extension == "mysql" {
				extension = "mysqlnd"
			}
			if !strings.Contains(packages, extension) {
				t.Fatalf("%s package plan lacks PHP %s support: %s", distribution, extension, packages)
			}
		}
		if (distribution == "debian" || distribution == "ubuntu") && !strings.Contains(packages, " phpmyadmin ") {
			t.Fatalf("%s package plan lacks its patched native phpMyAdmin: %s", distribution, packages)
		}
		if distribution == "rocky" && strings.Contains(packages, " phpmyadmin ") {
			t.Fatalf("Rocky must use Stackfort's hash-pinned phpMyAdmin bundle: %s", packages)
		}
		profile, err := nativePHPProfile(distribution)
		if err != nil || profile.PackageName != runtimePackage || profile.VendorUnit == "" || profile.BinaryPath == "" {
			t.Fatalf("%s PHP profile = %#v / %v", distribution, profile, err)
		}
	}
}

func TestRockyPHPNGINXDropInContractIsExact(t *testing.T) {
	t.Parallel()
	if rockyPHPNGINXDropIn != "/etc/systemd/system/nginx.service.d/php-fpm.conf" ||
		rockyPHPNGINXDropInContent != "[Unit]\nWants=php-fpm.service\n\n" {
		t.Fatalf("Rocky PHP/NGINX package contract changed: %q / %q",
			rockyPHPNGINXDropIn, rockyPHPNGINXDropInContent)
	}
}

func TestSELinuxPolicyOutputParsing(t *testing.T) {
	t.Parallel()

	if !hasSELinuxModule("stackfort_nginx_panel\nforeign_module 2.0\n", "stackfort_nginx_panel") {
		t.Fatal("name-only SELinux module output was not recognized")
	}
	if hasSELinuxModule("stackfort_nginx_panel_extra\n", "stackfort_nginx_panel") {
		t.Fatal("prefix module was accepted")
	}
	ports := "http_port_t tcp 80, 443, 8443\nstackfort_api_port_t tcp 8080\n"
	if !hasSELinuxPortLabel(ports, "stackfort_api_port_t", "tcp", "8080") {
		t.Fatal("Stackfort port label was not recognized")
	}
	if hasSELinuxPortLabel(ports, "http_port_t", "tcp", "8080") {
		t.Fatal("port was attributed to the wrong SELinux type")
	}
}

func TestRockySELinuxContextsIncludeWritableWAFRuntime(t *testing.T) {
	t.Parallel()

	foundContext := false
	for _, item := range stackfortSELinuxFileContexts() {
		if item.expression == wafconfig.RuntimeRoot+"(/.*)?" && item.kind == "httpd_cache_t" {
			foundContext = true
			break
		}
	}
	if !foundContext {
		t.Fatalf("SELinux contexts omit the confined WAF runtime: %#v", stackfortSELinuxFileContexts())
	}
	if !slices.Contains(stackfortSELinuxRestorePaths(), wafconfig.RuntimeRoot) {
		t.Fatalf("SELinux restore paths omit %s", wafconfig.RuntimeRoot)
	}
}
