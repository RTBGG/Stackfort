// SPDX-License-Identifier: AGPL-3.0-or-later

package nginxconfig

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

const rendererTestAccountID = "019c1234-5678-7abc-8def-0123456789ad"

func TestRenderAccountStaticGolden(t *testing.T) {
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "Example.Test", "public_html")

	rendered, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatalf("RenderAccount() error = %v", err)
	}
	expected := `# Managed by Stackfort. Do not edit.
# Account: 019c1234-5678-7abc-8def-0123456789ad

server {
    listen 80;
    listen [::]:80;
    server_name www.example.test;
    access_log "/var/log/stackfort/accounts/019c1234-5678-7abc-8def-0123456789ad/domain-9b263fbcb589853137b33ddcafa5bcc5403464ead4da766d0f819348bf8d472c.access.log" stackfort_redacted;
    error_log "/var/log/stackfort/accounts/019c1234-5678-7abc-8def-0123456789ad/domain-9b263fbcb589853137b33ddcafa5bcc5403464ead4da766d0f819348bf8d472c.error.log" warn;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header X-Content-Type-Options "nosniff" always;
    return 301 "https://example.test$request_uri";
}

server {
    listen 80;
    listen [::]:80;
    server_name example.test;
    access_log "/var/log/stackfort/accounts/019c1234-5678-7abc-8def-0123456789ad/domain-9b263fbcb589853137b33ddcafa5bcc5403464ead4da766d0f819348bf8d472c.access.log" stackfort_redacted;
    error_log "/var/log/stackfort/accounts/019c1234-5678-7abc-8def-0123456789ad/domain-9b263fbcb589853137b33ddcafa5bcc5403464ead4da766d0f819348bf8d472c.error.log" warn;
    root "/srv/hosting/accounts/019c1234-5678-7abc-8def-0123456789ad/public_html";
    disable_symlinks on from=$document_root;
    index index.html;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header X-Content-Type-Options "nosniff" always;

    location / {
        try_files $uri $uri/ =404;
    }
}

`
	if string(rendered.Content) != expected {
		t.Fatalf("rendered content differs\n--- got ---\n%s--- want ---\n%s", rendered.Content, expected)
	}
	if rendered.FileName != "account-"+rendererTestAccountID+".conf" || rendered.RenderedDomains != 1 {
		t.Fatalf("rendered metadata = %#v", rendered)
	}
	if rendered.Digest != sha256.Sum256(rendered.Content) {
		t.Fatal("rendered digest does not cover the returned bytes")
	}
}

func TestRenderAccountIsDeterministicAcrossInputOrder(t *testing.T) {
	identity := rendererTestIdentity(t)
	static := rendererStaticDomain(t, identity, "zeta.example", "sites/zeta/public")
	static.CanonicalMode = core.CanonicalServeBoth
	redirect := rendererRedirectDomain(t, identity, "alpha.example", "https://target.example/base?fixed=1")
	redirect.Target.Redirect.PreservePath = true
	redirect.Target.Redirect.PreserveQuery = true
	redirect.Target.Redirect.WildcardSubdomains = true

	first, err := RenderAccount(identity, []core.Domain{static, redirect}, Options{HeaderPolicies: []HeaderPolicy{
		HeaderNoSniff, HeaderPermissionsSensitiveOff, HeaderReferrerStrictOrigin,
	}})
	if err != nil {
		t.Fatalf("first RenderAccount() error = %v", err)
	}
	second, err := RenderAccount(identity, []core.Domain{redirect, static}, Options{HeaderPolicies: []HeaderPolicy{
		HeaderReferrerStrictOrigin, HeaderNoSniff, HeaderPermissionsSensitiveOff,
	}})
	if err != nil {
		t.Fatalf("second RenderAccount() error = %v", err)
	}
	if !bytes.Equal(first.Content, second.Content) || first.Digest != second.Digest {
		t.Fatal("equivalent desired states did not produce byte-identical output")
	}
	alpha := strings.Index(string(first.Content), "server_name alpha.example")
	zeta := strings.Index(string(first.Content), "server_name zeta.example")
	if alpha < 0 || zeta < 0 || alpha >= zeta {
		t.Fatalf("domains are not sorted by canonical ASCII name:\n%s", first.Content)
	}
	if !strings.Contains(string(first.Content), "map $args $sf_redirect_query_") {
		t.Fatalf("fixed and preserved queries do not use a deterministic map:\n%s", first.Content)
	}
}

func TestTypedWireSpecsRoundTripToIdenticalBytes(t *testing.T) {
	identity := rendererTestIdentity(t)
	static := rendererStaticDomain(t, identity, "static.example", "public_html")
	redirect := rendererRedirectDomain(t, identity, "redirect.example", "https://target.example/base")
	redirect.Target.Redirect.PreservePath = true
	options := DefaultOptions()

	direct, err := RenderAccount(identity, []core.Domain{static, redirect}, options)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{static, redirect}, options)
	if err != nil {
		t.Fatal(err)
	}
	fromWire, err := RenderSpecs(identity, specs, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(direct.Content, fromWire.Content) || direct.Digest != fromWire.Digest {
		t.Fatal("typed wire intent changed rendered output")
	}

	specs[0].Target.Redirect = &RedirectSpec{TargetURL: "https://attacker.example"}
	if _, err := RenderSpecs(identity, specs, options); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("mixed target union error = %v", err)
	}
}

func TestRenderAccountPHPUsesOnlyDerivedAccountSocket(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "php.example", "public_html")
	domain.Target.Type = core.DomainTargetPHP
	domain.Target.PHPVersion = "8.4"

	direct, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatalf("RenderAccount: %v", err)
	}
	content := string(direct.Content)
	for _, required := range []string{
		"index index.php index.html;",
		"try_files $uri $uri/ /index.php?$query_string;",
		"location ~ \\.php$",
		"try_files $uri =404;",
		"fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;",
		"fastcgi_param HTTP_PROXY \"\";",
		"fastcgi_pass \"unix:/run/stackfort-php/account-200000-php8.4.sock\";",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("rendered PHP configuration omitted %q:\n%s", required, content)
		}
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || len(specs) != 1 || specs[0].Target.PHPVersion != "8.4" {
		t.Fatalf("SpecsFromDomains = %#v / %v", specs, err)
	}
	fromWire, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(fromWire.Content, direct.Content) {
		t.Fatalf("PHP wire round trip changed bytes: %v", err)
	}
}

func TestRenderAccountUsesOnlyFixedWAFProfilesAndBypassesACME(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "waf.example", "public_html")
	domain.WAF.Mode = core.WAFModeDetectionOnly
	domain.TLS = core.DomainTLSState{
		Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
	}
	rendered, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	if strings.Count(content, "coraza on;") != 2 ||
		strings.Count(content, "coraza_transaction_id $request_id;") != 2 ||
		strings.Count(content, `coraza_rules_file "/etc/nginx/stackfort/coraza/profiles/detection-pl1.conf";`) != 2 ||
		strings.Count(content, "coraza off;") != 2 {
		t.Fatalf("WAF policy or ACME bypass is incomplete:\n%s", content)
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || len(specs) != 1 || specs[0].WAFMode != core.WAFModeDetectionOnly {
		t.Fatalf("typed WAF specs = %#v, %v", specs, err)
	}
	fromWire, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(fromWire.Content, rendered.Content) {
		t.Fatalf("WAF wire round trip changed bytes: %v", err)
	}

	domain.WAF.Mode = "../../attacker.conf"
	if _, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("unbounded WAF mode error = %v", err)
	}
}

func TestRenderAccountGeneratesExpiringExactWAFExceptionWithoutSecLangInput(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "exception.example", "public_html")
	domain.WAF.Mode = core.WAFModeBlockingPL1
	domain.WAF.Exceptions = []core.DomainWAFException{{
		ID: "019c1234-5678-7abc-8def-0123456789af", AccountID: core.ID(identity.AccountID),
		RuleID: 941100, RequestPath: "/search", Parameter: "q",
		ExpiresAt: time.Unix(1_800_000_000, 0).UTC(),
	}}
	rendered, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	for _, expected := range []string{
		`SecRule TIME_EPOCH "@lt 1800000000"`,
		`SecRule REQUEST_URI "@rx ^/search(?:\?.*)?$"`,
		`ctl:ruleRemoveTargetById=941100;ARGS:q`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("generated exception omitted %q:\n%s", expected, content)
		}
	}
	include := "Include /etc/nginx/stackfort/coraza/profiles/blocking-pl1.conf"
	if !strings.Contains(content, include) || strings.Index(content, "ctl:ruleRemoveTargetById=941100;ARGS:q") >
		strings.Index(content, include) {
		t.Fatal("runtime WAF exception is registered after the CRS profile")
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || len(specs[0].WAFExceptions) != 1 {
		t.Fatalf("typed exception specs = %#v / %v", specs, err)
	}
	fromWire, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(fromWire.Content, rendered.Content) {
		t.Fatalf("exception wire round trip changed bytes: %v", err)
	}
	unsafe := domain
	unsafe.WAF.Exceptions = append([]core.DomainWAFException(nil), domain.WAF.Exceptions...)
	unsafe.WAF.Exceptions[0].RequestPath = "/.*"
	if _, err := RenderAccount(identity, []core.Domain{unsafe}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("unbounded exception error = %v", err)
	}
}

func TestRenderAccountRunsWAFBeforeCustomerAndCanonicalRedirects(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	redirect := rendererRedirectDomain(t, identity, "redirect-waf.example", "https://target.example/base")
	redirect.WAF.Mode = core.WAFModeBlockingPL1
	static := rendererStaticDomain(t, identity, "static-waf.example", "public_html")
	static.WAF.Mode = core.WAFModeBlockingPL1

	rendered, err := RenderAccount(identity, []core.Domain{redirect, static}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	for _, required := range []string{
		"try_files /.__stackfort_waf_redirect_never_exists__ @stackfort_waf_redirect;",
		"location @stackfort_waf_redirect {\n        return 302 \"https://target.example/base\";",
		"location @stackfort_waf_redirect {\n        return 301 \"https://static-waf.example$request_uri\";",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("WAF redirect bypasses the access phase; missing %q:\n%s", required, content)
		}
	}
}

func TestRenderAccountScopesActivationRevisionHeaderToProbeURI(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "probe.example", "public_html")
	options := DefaultOptions()
	options.ActivationRevisionID = "019c1234-5678-7abc-8def-0123456789ab"

	rendered, err := RenderAccount(identity, []core.Domain{domain}, options)
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	for _, required := range []string{
		"map $request_uri $sf_activation_019c123456787abc8def0123456789ad {",
		`"/.__stackfort_activation_probe__" "019c1234-5678-7abc-8def-0123456789ab";`,
		"add_header X-Stackfort-Activation $sf_activation_019c123456787abc8def0123456789ad always;",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("activation probe rendering omits %q:\n%s", required, content)
		}
	}
	options.ActivationRevisionID = "../../attacker.conf"
	if _, err := RenderAccount(identity, []core.Domain{domain}, options); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("unbounded activation revision error = %v", err)
	}
}

func TestRenderAccountRejectsUnapprovedPHPVersion(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	for _, version := range []string{"", "latest", "8.4/../../run/evil.sock", "8.4;return 200"} {
		domain := rendererStaticDomain(t, identity, "php.example", "public_html")
		domain.Target.Type = core.DomainTargetPHP
		domain.Target.PHPVersion = version
		if _, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("PHP version %q error = %v", version, err)
		}
	}
}

func TestHTTP01RoutePrecedesCanonicalAndCustomerRedirects(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	static := rendererStaticDomain(t, identity, "static.example", "public_html")
	static.TLS = core.DomainTLSState{
		Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
	}
	redirect := rendererRedirectDomain(t, identity, "redirect.example", "https://target.example/base")
	redirect.TLS = static.TLS
	rendered, err := RenderAccount(identity, []core.Domain{static, redirect}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	if strings.Count(content, "location ^~ /.well-known/acme-challenge/") != 3 ||
		strings.Count(content, "alias /var/lib/stackfort-agent/acme-http01/;") != 3 ||
		strings.Count(content, "add_header Cache-Control \"no-store\" always;") != 3 {
		t.Fatalf("HTTP-01 routes are missing from canonical and content servers:\n%s", content)
	}
	canonicalServer := content[strings.Index(content, "server_name www.static.example;"):]
	if challenge := strings.Index(canonicalServer, "location ^~ /.well-known/acme-challenge/"); challenge < 0 ||
		strings.Index(canonicalServer, "location / {") < challenge {
		t.Fatalf("canonical redirect is not isolated below the challenge route:\n%s", canonicalServer)
	}
	redirectServer := content[strings.Index(content, "server_name redirect.example"):]
	if challenge := strings.Index(redirectServer, "location ^~ /.well-known/acme-challenge/"); challenge < 0 ||
		strings.Index(redirectServer, "location / {") < challenge {
		t.Fatalf("customer redirect is not isolated below the challenge route:\n%s", redirectServer)
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{static, redirect}, DefaultOptions())
	if err != nil || len(specs) != 2 || !specs[0].HTTP01Challenge || !specs[1].HTTP01Challenge {
		t.Fatalf("typed HTTP-01 specs = %#v, %v", specs, err)
	}
	fromWire, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(fromWire.Content, rendered.Content) {
		t.Fatalf("HTTP-01 wire round trip changed bytes: %v", err)
	}
}

func TestActiveCertificateRendersHTTPSAndKeepsHTTP01OnPort80Only(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "secure.example", "public_html")
	domain.TLS = core.DomainTLSState{
		Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
		IssuanceStatus:       core.TLSActive,
		Names:                []string{"secure.example", "www.secure.example"},
		ActiveCertificateRef: "019c1234-5678-7abc-8def-0123456789ae",
	}
	rendered, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	if strings.Count(content, "listen 443 ssl;") != 2 ||
		strings.Count(content, "ssl_certificate \"/etc/nginx/stackfort/certificates/019c1234-5678-7abc-8def-0123456789ae/fullchain.pem\";") != 2 ||
		strings.Count(content, "ssl_certificate_key \"/etc/nginx/stackfort/certificates/019c1234-5678-7abc-8def-0123456789ae/private-key.pem\";") != 2 {
		t.Fatalf("HTTPS certificate directives are incomplete:\n%s", content)
	}
	if strings.Count(content, "location ^~ /.well-known/acme-challenge/") != 2 {
		t.Fatalf("HTTP-01 route leaked into HTTPS server:\n%s", content)
	}
	for _, server := range strings.Split(content, "server {")[1:] {
		if strings.Contains(server, "listen 443 ssl;") && strings.Contains(server, "location ^~ /.well-known/acme-challenge/") {
			t.Fatalf("HTTP-01 route leaked into an HTTPS server:\n%s", server)
		}
	}
	if !strings.Contains(content, `return 301 "https://secure.example$request_uri";`) ||
		!strings.Contains(content, "ssl_protocols TLSv1.2 TLSv1.3;") ||
		!strings.Contains(content, "ssl_session_tickets off;") {
		t.Fatalf("secure redirect or TLS policy is missing:\n%s", content)
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || len(specs) != 1 || specs[0].TLSCertificateID != domain.TLS.ActiveCertificateRef {
		t.Fatalf("TLS wire spec = %#v, %v", specs, err)
	}
	fromWire, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(fromWire.Content, rendered.Content) {
		t.Fatalf("TLS wire round trip changed bytes: %v", err)
	}
}

func TestRenderAccountRejectsAdversarialHostnames(t *testing.T) {
	identity := rendererTestIdentity(t)
	for _, mutate := range []func(*core.Domain){
		func(domain *core.Domain) { domain.Name.ASCII = "example.test; return 200" },
		func(domain *core.Domain) { domain.Name.ASCII = "example.test\nreturn 200" },
		func(domain *core.Domain) { domain.Name.ASCII = "*.example.test" },
		func(domain *core.Domain) { domain.Name.ASCII = "EXAMPLE.TEST" },
		func(domain *core.Domain) {
			domain.Name.ASCII = "www.example.test"
			domain.Name.Display = "www.example.test"
		},
		func(domain *core.Domain) { domain.Name.Display = "different.example" },
	} {
		domain := rendererStaticDomain(t, identity, "example.test", "public_html")
		mutate(&domain)
		if _, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("RenderAccount(%#v) error = %v", domain.Name, err)
		}
	}
}

func TestRenderAccountRejectsAdversarialDocumentRoots(t *testing.T) {
	identity := rendererTestIdentity(t)
	for _, documentRoot := range []string{
		"../private", "/etc/nginx", "public_html/../private", "public_html\\escape",
		"public_html/$uri", "public_html/\"; return 200; #", "public_html\nreturn 200", "public_html/./assets",
	} {
		domain := rendererStaticDomain(t, identity, "example.test", "public_html")
		domain.Target.DocumentRoot.RelativePath = documentRoot
		if _, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("RenderAccount(documentRoot=%q) error = %v", documentRoot, err)
		}
	}
	domain := rendererStaticDomain(t, identity, "example.test", "public_html")
	domain.Target.DocumentRoot.AccountID = "019c1234-5678-7abc-8def-0123456789ac"
	if _, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("RenderAccount(cross-account root) error = %v", err)
	}
}

func TestRenderAccountEscapesRedirectLiteralsByContext(t *testing.T) {
	identity := rendererTestIdentity(t)
	domain := rendererRedirectDomain(
		t, identity, "redirect.example", `https://target.example/a$uri;return%20200?fixed=$host`,
	)
	domain.Target.Redirect.PreservePath = true
	domain.Target.Redirect.PreserveQuery = true

	rendered, err := RenderAccount(identity, []core.Domain{domain}, Options{})
	if err != nil {
		t.Fatalf("RenderAccount() error = %v", err)
	}
	content := string(rendered.Content)
	if !strings.Contains(content, `"https://target.example/a%24uri;return%20200$uri?fixed=%24host$sf_redirect_query_`) {
		t.Fatalf("redirect literals were not escaped separately from trusted variables:\n%s", content)
	}
	if strings.Contains(content, `"https://target.example/a$uri`) || strings.Contains(content, `fixed=$host`) ||
		strings.Contains(content, `\$uri`) {
		t.Fatalf("user-controlled dollar expression became an NGINX variable:\n%s", content)
	}
	if got := quoteDynamic(`a$b"c\d`, "$uri"); got != `"a%24b\"c\\d$uri"` {
		t.Fatalf("quoteDynamic() = %q", got)
	}
}

func TestRenderAccountRejectsInvalidRedirects(t *testing.T) {
	identity := rendererTestIdentity(t)
	tests := []struct {
		name   string
		mutate func(*core.DomainRedirect)
	}{
		{"HTTP target", func(redirect *core.DomainRedirect) { redirect.TargetURL = "http://target.example" }},
		{"fragment", func(redirect *core.DomainRedirect) { redirect.TargetURL = "https://target.example/#fragment" }},
		{"line break", func(redirect *core.DomainRedirect) { redirect.TargetURL = "https://target.example/%0a" }},
		{"host mismatch", func(redirect *core.DomainRedirect) { redirect.TargetASCIIHost = "other.example" }},
		{"status", func(redirect *core.DomainRedirect) { redirect.StatusCode = 307 }},
		{"host mode", func(redirect *core.DomainRedirect) { redirect.HostMode = "unknown" }},
		{"wildcard host mode", func(redirect *core.DomainRedirect) {
			redirect.HostMode = core.RedirectHostApexOnly
			redirect.WildcardSubdomains = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			domain := rendererRedirectDomain(t, identity, "redirect.example", "https://target.example/base")
			test.mutate(domain.Target.Redirect)
			if _, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("RenderAccount() error = %v", err)
			}
		})
	}
}

func TestRedirectHostScopeAndPreviewMatchRenderedBehavior(t *testing.T) {
	t.Parallel()
	identity := rendererTestIdentity(t)
	domain := rendererRedirectDomain(t, identity, "redirect.example", "https://target.example/base?fixed=1")
	domain.Target.Redirect.StatusCode = core.RedirectPermanent
	domain.Target.Redirect.HostMode = core.RedirectHostWWWOnly
	domain.Target.Redirect.PreservePath = true
	domain.Target.Redirect.PreserveQuery = true
	domain.TLS = core.DomainTLSState{
		Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
	}

	rendered, err := RenderAccount(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	content := string(rendered.Content)
	if !strings.Contains(content, "server_name www.redirect.example;") ||
		!strings.Contains(content, `return 301 "https://target.example/base$uri?fixed=1$sf_redirect_query_00001";`) ||
		!strings.Contains(content, "server_name redirect.example;") ||
		!strings.Contains(content, "return 404;") ||
		strings.Count(content, "location ^~ /.well-known/acme-challenge/") != 2 {
		t.Fatalf("host-scoped redirect output =\n%s", content)
	}
	preview, err := core.PreviewDomainRouting(core.DomainRoutingPreviewParams{
		Name: domain.Name.ASCII, CanonicalMode: domain.CanonicalMode,
		Target: core.DomainTargetSpec{Type: core.DomainTargetRedirect, Redirect: &core.RedirectSpec{
			StatusCode: domain.Target.Redirect.StatusCode, TargetURL: domain.Target.Redirect.TargetURL,
			HostMode: domain.Target.Redirect.HostMode, PreservePath: domain.Target.Redirect.PreservePath,
			PreserveQuery: domain.Target.Redirect.PreserveQuery,
		}},
	})
	if err != nil || len(preview.Routes) != 2 || preview.Routes[0].Action != core.DomainRouteInactive ||
		preview.Routes[1].DestinationURL != "https://target.example/base/example/path?fixed=1&source=preview" {
		t.Fatalf("routing preview = %#v, %v", preview, err)
	}
	specs, err := SpecsFromDomains(identity, []core.Domain{domain}, DefaultOptions())
	if err != nil || specs[0].Target.Redirect.HostMode != core.RedirectHostWWWOnly {
		t.Fatalf("typed host scope = %#v, %v", specs, err)
	}
	fromWire, err := RenderSpecs(identity, specs, DefaultOptions())
	if err != nil || !bytes.Equal(fromWire.Content, rendered.Content) {
		t.Fatalf("host scope changed across wire round trip: %v", err)
	}
}

func TestRenderAccountAcceptsOnlyEnumeratedHeaderPolicies(t *testing.T) {
	identity := rendererTestIdentity(t)
	domain := rendererStaticDomain(t, identity, "example.test", "public_html")
	for _, policies := range [][]HeaderPolicy{
		{HeaderPolicy("safe;\nreturn 200")},
		{HeaderNoSniff, HeaderNoSniff},
	} {
		if _, err := RenderAccount(identity, []core.Domain{domain}, Options{HeaderPolicies: policies}); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("RenderAccount(headers=%q) error = %v", policies, err)
		}
	}

	rendered, err := RenderAccount(identity, []core.Domain{domain}, Options{HeaderPolicies: []HeaderPolicy{
		HeaderPermissionsSensitiveOff, HeaderNoSniff,
	}})
	if err != nil {
		t.Fatalf("RenderAccount(valid headers) error = %v", err)
	}
	if strings.Count(string(rendered.Content), "add_header Permissions-Policy") != 2 ||
		strings.Count(string(rendered.Content), "add_header X-Content-Type-Options") != 2 {
		t.Fatalf("enumerated headers are missing from both generated servers:\n%s", rendered.Content)
	}
}

func TestRenderAccountRejectsRouteConflictsAndUnsupportedTargets(t *testing.T) {
	identity := rendererTestIdentity(t)
	duplicate := rendererStaticDomain(t, identity, "example.test", "other")
	if _, err := RenderAccount(identity, []core.Domain{
		rendererStaticDomain(t, identity, "example.test", "public_html"), duplicate,
	}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("duplicate route error = %v", err)
	}

	wildcard := rendererRedirectDomain(t, identity, "example.test", "https://target.example")
	wildcard.Target.Redirect.WildcardSubdomains = true
	child := rendererStaticDomain(t, identity, "shop.eu.example.test", "shop")
	if _, err := RenderAccount(identity, []core.Domain{child, wildcard}, DefaultOptions()); !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("wildcard overlap error = %v", err)
	}

	unsupported := rendererStaticDomain(t, identity, "oci.example", "public_html")
	unsupported.Target.Type = core.DomainTargetOCIApplication
	unsupported.Target.DocumentRoot = nil
	applicationID := core.ID("019c1234-5678-7abc-8def-0123456789aa")
	unsupported.Target.ApplicationID = &applicationID
	if _, err := RenderAccount(identity, []core.Domain{unsupported}, DefaultOptions()); !errors.Is(err, ErrUnsupportedTarget) {
		t.Errorf("unsupported target error = %v", err)
	}
}

func TestRenderAccountSkipsInactiveLifecycleStates(t *testing.T) {
	identity := rendererTestIdentity(t)
	active := rendererStaticDomain(t, identity, "active.example", "public_html")
	suspended := rendererStaticDomain(t, identity, "suspended.example", "suspended")
	suspended.Status = core.DomainSuspended
	removed := rendererStaticDomain(t, identity, "removed.example", "removed")
	removed.Status = core.DomainRemoved

	rendered, err := RenderAccount(identity, []core.Domain{suspended, active, removed}, DefaultOptions())
	if err != nil {
		t.Fatalf("RenderAccount() error = %v", err)
	}
	if rendered.RenderedDomains != 1 || strings.Contains(string(rendered.Content), "suspended.example") ||
		strings.Contains(string(rendered.Content), "removed.example") {
		t.Fatalf("inactive lifecycle states were rendered: %#v\n%s", rendered, rendered.Content)
	}
}

func rendererTestIdentity(t *testing.T) hostingidentity.Spec {
	t.Helper()
	username, err := hostingidentity.UsernameForAccount(rendererTestAccountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(rendererTestAccountID)
	if err != nil {
		t.Fatal(err)
	}
	return hostingidentity.Spec{
		AccountID: rendererTestAccountID, Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home,
	}
}

func rendererStaticDomain(
	t *testing.T,
	identity hostingidentity.Spec,
	name string,
	documentRoot string,
) core.Domain {
	t.Helper()
	normalized, err := core.NormalizeDomainName(name)
	if err != nil {
		t.Fatal(err)
	}
	return core.Domain{
		AccountID: core.ID(identity.AccountID), Name: normalized, Status: core.DomainActive,
		CanonicalMode: core.CanonicalPreferApex,
		Target: core.DomainTarget{
			Type: core.DomainTargetStatic,
			DocumentRoot: &core.DocumentRoot{
				AccountID: core.ID(identity.AccountID), RelativePath: documentRoot,
			},
		},
	}
}

func rendererRedirectDomain(
	t *testing.T,
	identity hostingidentity.Spec,
	name string,
	target string,
) core.Domain {
	t.Helper()
	domain := rendererStaticDomain(t, identity, name, "unused")
	normalized, host, _, err := core.NormalizeRedirectURL(target)
	if err != nil {
		t.Fatal(err)
	}
	domain.Target = core.DomainTarget{
		Type: core.DomainTargetRedirect,
		Redirect: &core.DomainRedirect{
			StatusCode: core.RedirectTemporary, TargetURL: normalized, TargetASCIIHost: host,
		},
	}
	return domain
}
