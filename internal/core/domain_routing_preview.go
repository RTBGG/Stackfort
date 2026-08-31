// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"net/url"
)

const (
	domainPreviewPath     = "/example/path"
	domainPreviewRawQuery = "source=preview"
)

type DomainRouteAction string

const (
	DomainRouteServe    DomainRouteAction = "serve"
	DomainRouteRedirect DomainRouteAction = "redirect"
	DomainRouteInactive DomainRouteAction = "inactive"
)

type DomainRoutingPreviewParams struct {
	Name          string
	CanonicalMode CanonicalMode
	Target        DomainTargetSpec
}

type DomainRoutingPreview struct {
	Name          NormalizedDomainName
	CanonicalMode CanonicalMode
	TargetType    DomainTargetType
	SamplePath    string
	SampleQuery   string
	Routes        []DomainRoutePreview
}

type DomainRoutePreview struct {
	SourcePattern  string
	SourceURL      string
	Action         DomainRouteAction
	StatusCode     RedirectStatusCode
	DestinationURL string
	PreservePath   bool
	PreserveQuery  bool
}

// PreviewDomainRouting validates transient domain intent with the same
// normalization and redirect-loop boundary used by persistence, then produces
// deterministic examples for the UI. It performs no account or host mutation.
func PreviewDomainRouting(params DomainRoutingPreviewParams) (DomainRoutingPreview, error) {
	name, err := normalizeDomainBase(params.Name)
	if err != nil {
		return DomainRoutingPreview{}, err
	}
	canonicalMode, err := normalizeCanonicalMode(params.CanonicalMode)
	if err != nil {
		return DomainRoutingPreview{}, err
	}
	target, err := prepareDomainTarget(params.Target, name)
	if err != nil {
		return DomainRoutingPreview{}, err
	}
	preview := DomainRoutingPreview{
		Name: name, CanonicalMode: canonicalMode, TargetType: target.spec.Type,
		SamplePath: domainPreviewPath, SampleQuery: domainPreviewRawQuery,
	}
	if target.spec.Type == DomainTargetRedirect {
		preview.Routes = previewRedirectRoutes(name.ASCII, target)
		return preview, nil
	}
	preview.Routes = previewCanonicalRoutes(name.ASCII, canonicalMode)
	return preview, nil
}

func previewCanonicalRoutes(base string, mode CanonicalMode) []DomainRoutePreview {
	apex := previewServeRoute(base)
	www := previewServeRoute("www." + base)
	switch mode {
	case CanonicalPreferApex:
		www = previewCanonicalRedirectRoute("www."+base, base)
	case CanonicalPreferWWW:
		apex = previewCanonicalRedirectRoute(base, "www."+base)
	}
	return []DomainRoutePreview{apex, www}
}

func previewRedirectRoutes(base string, target preparedDomainTarget) []DomainRoutePreview {
	redirect := target.spec.Redirect
	routes := []DomainRoutePreview{
		previewInactiveRoute(base),
		previewInactiveRoute("www." + base),
	}
	activate := func(index int) {
		routes[index] = previewCustomerRedirectRoute(routes[index].SourcePattern, routes[index].SourceURL, target)
	}
	switch redirect.HostMode {
	case RedirectHostApexOnly:
		activate(0)
	case RedirectHostWWWOnly:
		activate(1)
	case RedirectHostBoth:
		activate(0)
		activate(1)
	}
	if redirect.WildcardSubdomains {
		sourceHost := "preview." + base
		route := previewCustomerRedirectRoute("*."+base, previewSourceURL(sourceHost), target)
		routes = append(routes, route)
	}
	return routes
}

func previewServeRoute(host string) DomainRoutePreview {
	return DomainRoutePreview{SourcePattern: host, SourceURL: previewSourceURL(host), Action: DomainRouteServe}
}

func previewInactiveRoute(host string) DomainRoutePreview {
	return DomainRoutePreview{SourcePattern: host, SourceURL: previewSourceURL(host), Action: DomainRouteInactive}
}

func previewCanonicalRedirectRoute(sourceHost, destinationHost string) DomainRoutePreview {
	return DomainRoutePreview{
		SourcePattern: sourceHost, SourceURL: previewSourceURL(sourceHost), Action: DomainRouteRedirect,
		StatusCode:     RedirectPermanent,
		DestinationURL: "https://" + destinationHost + domainPreviewPath + "?" + domainPreviewRawQuery,
		PreservePath:   true, PreserveQuery: true,
	}
}

func previewCustomerRedirectRoute(sourcePattern, sourceURL string, target preparedDomainTarget) DomainRoutePreview {
	redirect := target.spec.Redirect
	return DomainRoutePreview{
		SourcePattern: sourcePattern, SourceURL: sourceURL, Action: DomainRouteRedirect,
		StatusCode: redirect.StatusCode,
		DestinationURL: previewRedirectDestination(
			target.redirectURL, redirect.PreservePath, redirect.PreserveQuery,
		),
		PreservePath: redirect.PreservePath, PreserveQuery: redirect.PreserveQuery,
	}
}

func previewSourceURL(host string) string {
	return "https://" + host + domainPreviewPath + "?" + domainPreviewRawQuery
}

func previewRedirectDestination(target string, preservePath, preserveQuery bool) string {
	parsed, _ := url.Parse(target)
	rawQuery := parsed.RawQuery
	parsed.RawQuery = ""
	destination := parsed.String()
	if preservePath {
		destination += domainPreviewPath
	}
	if rawQuery != "" {
		destination += "?" + rawQuery
	}
	if preserveQuery {
		separator := "?"
		if rawQuery != "" {
			separator = "&"
		}
		destination += separator + domainPreviewRawQuery
	}
	return destination
}
