// SPDX-License-Identifier: AGPL-3.0-or-later

package cacheconfig

import (
	"os"
	"strings"
	"testing"
)

func TestManagedVCLContainsIndependentPersonalizationBarriers(t *testing.T) {
	t.Parallel()
	vcl := ManagedVCL()
	for _, required := range []string{
		`req.http.Authorization || req.http.Cookie`,
		`req.method != "GET" && req.method != "HEAD"`,
		`req.http.Content-Length && req.http.Content-Length != "0"`,
		`beresp.http.Set-Cookie`, `(private|no-store|no-cache)`,
		`hash_data(req.http.host)`, `hash_data(req.url)`,
		`X-Stackfort-Cache = "BYPASS"`,
	} {
		if !strings.Contains(vcl, required) {
			t.Fatalf("managed VCL omitted %q", required)
		}
	}
	if strings.Contains(vcl, "return (synth(200") {
		t.Fatal("managed VCL contains a public purge endpoint")
	}
}

func TestPackagedVCLIsGeneratedPolicyByteForByte(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("../../packaging/vinyl/stackfort.vcl")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != ManagedVCL() {
		t.Fatal("packaged VCL drifted from the closed control-plane policy")
	}
}

func TestClosedPresetAndPurgePathValidation(t *testing.T) {
	t.Parallel()
	for _, preset := range []Preset{"", PresetDisabled, PresetRespectOrigin, PresetWordPress} {
		if _, err := NormalizePreset(preset); err != nil {
			t.Fatalf("NormalizePreset(%q): %v", preset, err)
		}
	}
	if _, err := NormalizePreset("vcl { return(pass); }"); err == nil {
		t.Fatal("accepted free-form cache profile")
	}
	for _, path := range []string{"/", "/news/2026", "/shop/item-1"} {
		if normalized, err := NormalizePurgePath(path); err != nil || normalized != path {
			t.Fatalf("NormalizePurgePath(%q) = %q / %v", path, normalized, err)
		}
	}
	for _, path := range []string{"/.*", "/x?secret=y", "/../other", "//evil", "/x' || true"} {
		if _, err := NormalizePurgePath(path); err == nil {
			t.Fatalf("accepted unsafe purge path %q", path)
		}
	}
}
