// SPDX-License-Identifier: AGPL-3.0-or-later

package phpruntime

import (
	"errors"
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestNativeRuntimeMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		distribution string
		version      string
		packageName  string
		binary       string
		unit         string
		nginxUser    string
	}{
		{"debian", "8.4", "php8.4-fpm", "/usr/sbin/php-fpm8.4", "php8.4-fpm.service", "www-data"},
		{"ubuntu", "8.5", "php8.5-fpm", "/usr/sbin/php-fpm8.5", "php8.5-fpm.service", "www-data"},
		{"rocky", "8.3", "php-fpm", "/usr/sbin/php-fpm", "php-fpm.service", "nginx"},
	}
	for _, test := range tests {
		profile, err := ForDistribution(test.distribution, test.version)
		if err != nil {
			t.Fatalf("ForDistribution(%s, %s): %v", test.distribution, test.version, err)
		}
		if profile.PackageName != test.packageName || profile.BinaryPath != test.binary ||
			profile.VendorUnit != test.unit || profile.NGINXUser != test.nginxUser {
			t.Errorf("profile = %#v", profile)
		}
	}
	for _, invalid := range [][2]string{{"debian", "8.5"}, {"ubuntu", "8.4"}, {"rocky", "8.4"}, {"", "8.4"}} {
		if _, err := ForDistribution(invalid[0], invalid[1]); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("ForDistribution(%q, %q) error = %v", invalid[0], invalid[1], err)
		}
	}
}

func TestPoolSetCanonicalizationAndFixedPaths(t *testing.T) {
	t.Parallel()
	identity := testIdentity()
	spec, err := Canonicalize(PoolSetSpec{Identity: identity, Versions: []string{"8.5", "8.3", "8.5"}})
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if !reflect.DeepEqual(spec.Versions, []string{"8.3", "8.5"}) ||
		spec.MaxChildren != DefaultMaxChildren || spec.MemoryLimitMiB != DefaultMemoryMiB {
		t.Fatalf("canonical spec = %#v", spec)
	}
	socket, _ := SocketPath(identity, "8.4")
	configuration, _ := ConfigurationPath(identity, "8.4")
	unit, _ := UnitName(identity, "8.4")
	if socket != "/run/stackfort-php/account-200123-php8.4.sock" ||
		configuration != "/etc/stackfort/php/account-200123-php8.4.conf" ||
		unit != "stackfort-php-8-4-200123.service" {
		t.Fatalf("paths = %q / %q / %q", socket, configuration, unit)
	}
}

func TestPoolSetRejectsUntrustedVersionsAndLimits(t *testing.T) {
	t.Parallel()
	identity := testIdentity()
	tests := []PoolSetSpec{
		{Identity: identity, Versions: []string{"8.4/../../x"}, MaxChildren: 4, MemoryLimitMiB: 128},
		{Identity: identity, Versions: []string{"8.4", "8.3"}, MaxChildren: 4, MemoryLimitMiB: 128},
		{Identity: identity, Versions: []string{"8.4", "8.4"}, MaxChildren: 4, MemoryLimitMiB: 128},
		{Identity: identity, Versions: []string{"8.4"}, MaxChildren: 0, MemoryLimitMiB: 128},
		{Identity: identity, Versions: []string{"8.4"}, MaxChildren: 4, MemoryLimitMiB: 8},
	}
	for _, spec := range tests {
		if !errors.Is(Validate(spec), ErrInvalidSpec) {
			t.Errorf("Validate(%#v) unexpectedly succeeded", spec)
		}
	}
}

func TestVersionInvocationValuesRoundTrip(t *testing.T) {
	t.Parallel()
	identity := testIdentity()
	values, err := VersionInvocationValues(identity, "8.4")
	if err != nil {
		t.Fatal(err)
	}
	parsed, version, err := SpecFromVersionInvocationValues(values)
	if err != nil || parsed != identity || version != "8.4" {
		t.Fatalf("round trip = %#v / %q / %v", parsed, version, err)
	}
	values[5] = "8.4;id"
	if _, _, err := SpecFromVersionInvocationValues(values); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("untrusted version error = %v", err)
	}
}

func testIdentity() hostingidentity.Spec {
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: 200123, GID: 200123, HomeDirectory: home,
	}
}
