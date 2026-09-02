// SPDX-License-Identifier: AGPL-3.0-or-later

package updatecheck

import "testing"

func TestParseReleaseVersionAcceptsOnlyStableAndBetaTags(t *testing.T) {
	accepted := map[string]string{
		"v0.0.0": "0.0.0", "v1.2.3": "1.2.3", "v2.0.0-beta.4": "2.0.0-beta.4",
	}
	for tag, wantVersion := range accepted {
		_, version, ok := parseReleaseVersion(tag)
		if !ok || version != wantVersion {
			t.Errorf("parseReleaseVersion(%q) = %q, %t", tag, version, ok)
		}
	}
	for _, tag := range []string{
		"1.2.3", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2", "v1.2.3-rc.1",
		"v1.2.3-beta.0", "v1.2.3-beta.01", "v1.2.3+build", "v18446744073709551616.0.0",
	} {
		if _, _, ok := parseReleaseVersion(tag); ok {
			t.Errorf("parseReleaseVersion(%q) unexpectedly succeeded", tag)
		}
	}
}

func TestCompareVersionsOrdersBetasAndStable(t *testing.T) {
	parse := func(tag string) semanticVersion {
		t.Helper()
		version, _, ok := parseReleaseVersion(tag)
		if !ok {
			t.Fatalf("parseReleaseVersion(%q) failed", tag)
		}
		return version
	}
	tests := []struct {
		left, right string
		want        int
	}{
		{"v1.2.3-beta.1", "v1.2.3-beta.2", -1},
		{"v1.2.3-beta.9", "v1.2.3", -1},
		{"v1.2.4-beta.1", "v1.2.3", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.2.3", "v1.2.3", 0},
	}
	for _, test := range tests {
		if got := compareVersions(parse(test.left), parse(test.right)); got != test.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}
