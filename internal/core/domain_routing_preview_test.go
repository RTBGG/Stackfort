// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"errors"
	"testing"
)

func TestDomainRoutingPreviewCanonicalHostModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode        CanonicalMode
		apexAction  DomainRouteAction
		wwwAction   DomainRouteAction
		destination string
	}{
		{CanonicalPreferApex, DomainRouteServe, DomainRouteRedirect,
			"https://example.test/example/path?source=preview"},
		{CanonicalPreferWWW, DomainRouteRedirect, DomainRouteServe,
			"https://www.example.test/example/path?source=preview"},
		{CanonicalServeBoth, DomainRouteServe, DomainRouteServe, ""},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.mode), func(t *testing.T) {
			t.Parallel()
			preview, err := PreviewDomainRouting(DomainRoutingPreviewParams{
				Name: "WWW.Example.Test", CanonicalMode: test.mode,
				Target: DomainTargetSpec{Type: DomainTargetStatic},
			})
			if err != nil {
				t.Fatal(err)
			}
			if preview.Name.ASCII != "example.test" || len(preview.Routes) != 2 ||
				preview.Routes[0].SourcePattern != "example.test" ||
				preview.Routes[1].SourcePattern != "www.example.test" ||
				preview.Routes[0].Action != test.apexAction || preview.Routes[1].Action != test.wwwAction {
				t.Fatalf("preview = %#v", preview)
			}
			for _, route := range preview.Routes {
				if route.Action == DomainRouteRedirect &&
					(route.StatusCode != RedirectPermanent || route.DestinationURL != test.destination ||
						!route.PreservePath || !route.PreserveQuery) {
					t.Fatalf("canonical redirect = %#v", route)
				}
			}
		})
	}
}

func TestDomainRoutingPreviewRedirectPathAndQueryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		preservePath  bool
		preserveQuery bool
		expected      string
	}{
		{"fixed only", false, false, "https://destination.example/base?fixed=1"},
		{"path", true, false, "https://destination.example/base/example/path?fixed=1"},
		{"query", false, true, "https://destination.example/base?fixed=1&source=preview"},
		{"path and query", true, true, "https://destination.example/base/example/path?fixed=1&source=preview"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			preview, err := PreviewDomainRouting(DomainRoutingPreviewParams{
				Name: "redirect.example", CanonicalMode: CanonicalPreferApex,
				Target: DomainTargetSpec{Type: DomainTargetRedirect, Redirect: &RedirectSpec{
					StatusCode: RedirectTemporary, TargetURL: "https://destination.example/base?fixed=1",
					HostMode: RedirectHostWWWOnly, PreservePath: test.preservePath,
					PreserveQuery: test.preserveQuery,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Routes) != 2 || preview.Routes[0].Action != DomainRouteInactive ||
				preview.Routes[1].Action != DomainRouteRedirect ||
				preview.Routes[1].StatusCode != RedirectTemporary ||
				preview.Routes[1].DestinationURL != test.expected {
				t.Fatalf("preview = %#v", preview)
			}
		})
	}
}

func TestDomainRoutingPreviewRejectsLoopsUnsafeTargetsAndAmbiguousWildcardScope(t *testing.T) {
	t.Parallel()
	tests := []RedirectSpec{
		{StatusCode: RedirectTemporary, TargetURL: "http://destination.example"},
		{StatusCode: RedirectTemporary, TargetURL: "https://user:secret@destination.example"},
		{StatusCode: RedirectTemporary, TargetURL: "https://destination.example/#fragment"},
		{StatusCode: RedirectTemporary, TargetURL: "https://www.source.example/loop"},
		{StatusCode: 307, TargetURL: "https://destination.example"},
		{StatusCode: RedirectTemporary, TargetURL: "https://destination.example", HostMode: "unknown"},
		{StatusCode: RedirectTemporary, TargetURL: "https://destination.example",
			HostMode: RedirectHostApexOnly, WildcardSubdomains: true},
		{StatusCode: RedirectTemporary, TargetURL: "https://child.source.example",
			HostMode: RedirectHostBoth, WildcardSubdomains: true},
	}
	for _, redirect := range tests {
		redirect := redirect
		if _, err := PreviewDomainRouting(DomainRoutingPreviewParams{
			Name: "source.example", Target: DomainTargetSpec{
				Type: DomainTargetRedirect, Redirect: &redirect,
			},
		}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("PreviewDomainRouting(%#v) error = %v", redirect, err)
		}
	}
}
