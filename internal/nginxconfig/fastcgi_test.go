// SPDX-License-Identifier: AGPL-3.0-or-later

package nginxconfig

import (
	"bytes"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestFastCGICacheClosedRenderingAndNamespace(t *testing.T) {
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "fastcgi.example.test", "public_html")
	domain.Target.Type, domain.Target.PHPVersion = core.DomainTargetPHP, "8.4"
	domain.Cache = core.DomainCachePolicy{Preset: core.CachePresetFastCGIRespectOrigin, Generation: core.ID(rendererTestAccountID)}
	domain.WAF.Mode = core.WAFModeBlockingPL1
	first, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"fastcgi_cache StackfortFastCGI;", "fastcgi_cache_bypass $stackfort_fastcgi_request_bypass $stackfort_fastcgi_path_bypass;",
		"fastcgi_no_cache $stackfort_fastcgi_request_bypass $stackfort_fastcgi_path_bypass $stackfort_fastcgi_response_bypass $stackfort_fastcgi_origin_bypass;",
		"fastcgi_ignore_headers X-Accel-Expires;", "fastcgi_cache_lock on;", "coraza on;",
		"fastcgi_cache_use_stale off;", "add_header X-Stackfort-Cache $upstream_cache_status always;",
		identity.AccountID + ":fastcgi.example.test:" + string(domain.Cache.Generation) + ":$scheme:$host:$request_uri",
	} {
		if !bytes.Contains(first.Content, []byte(required)) {
			t.Errorf("missing %q", required)
		}
	}
	if bytes.Contains(first.Content, []byte("6081")) || bytes.Contains(first.Content, []byte("9000")) {
		t.Fatal("FastCGI must not stack with Vinyl")
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(roundtrip.Content, first.Content) {
		t.Fatalf("wire roundtrip differs: %v", err)
	}
	for _, invalid := range []string{"", "../other", "019c1234-5678-4abc-8def-0123456789ad", "x; fastcgi_cache off"} {
		specs[0].CacheGeneration = invalid
		if _, err := RenderSpecs(identity, specs, DefaultOptions()); err == nil {
			t.Errorf("accepted generation %q", invalid)
		}
	}
	domain.Cache.Preset = core.CachePresetFastCGIWordPress
	wordpress, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || strings.Contains(string(wordpress.Content), "$stackfort_fastcgi_origin_bypass") {
		t.Fatalf("WordPress fallback invalid: %v", err)
	}
	domain.Cache.Preset = core.CachePresetDisabled
	disabled, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || bytes.Contains(disabled.Content, []byte("fastcgi_cache ")) {
		t.Fatalf("disabled cache rendered: %v", err)
	}
}
