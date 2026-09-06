// SPDX-License-Identifier: AGPL-3.0-or-later

package panelconfig

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/paneltls"
)

func fixture(t *testing.T) ([]byte, []byte, *x509.CertPool) {
	t.Helper()
	bundle, err := paneltls.New(time.Now(), "panel.example.com", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	block, key := pem.Decode(bundle)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return pem.EncodeToMemory(block), key, roots
}

func TestImportAndCanonicalRender(t *testing.T) {
	cert, key, roots := fixture(t)
	config, bundle, err := Import("panel.example.com", cert, key, time.Now(), roots)
	if err != nil || len(bundle) == 0 {
		t.Fatalf("import: %v", err)
	}
	for _, distribution := range []string{"debian", "ubuntu", "rocky"} {
		spec, _ := nginxbaseline.ForDistribution(distribution)
		content, err := Render(spec, config)
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"listen 443 ssl;", "listen [::]:443 ssl;", "server_name panel.example.com;", "if ($host != panel.example.com)", "if ($ssl_server_name != panel.example.com)", "return 308 https://panel.example.com$request_uri;", "proxy_pass http://127.0.0.1:8080;", "ssl_certificate " + config.BundlePath() + ";"} {
			if !strings.Contains(content, required) {
				t.Errorf("missing %q", required)
			}
		}
		if strings.Contains(content, "default_server") || strings.Contains(content, "8443") || strings.Contains(content, "PRIVATE KEY") {
			t.Fatal("named origin escaped its boundary")
		}
		parsed, err := Parse(spec, []byte(content))
		if err != nil || parsed != config {
			t.Fatal("roundtrip", err)
		}
		if _, err := Parse(spec, []byte(content+"# drift\n")); err == nil {
			t.Fatal("accepted drift")
		}
		if strings.Contains(CandidateMain(spec), "panel-enabled/*.conf") || !strings.Contains(CandidateMain(spec), nginxbaseline.PanelConfigurationPath) {
			t.Fatal("candidate includes live named panel")
		}
	}
}

func TestRejectUnsafeHostnamesAndCertificates(t *testing.T) {
	cert, key, roots := fixture(t)
	_, otherKey, _ := fixture(t)
	for _, hostname := range []string{"", "localhost", "127.0.0.1", "*.example.com", "Panel.example.com", "panel.example.com.", "https://panel.example.com", "panel.example.com:443", "panel.example.com;", "panel.example.com\ninclude /tmp/x;", " panel.example.com", "a_b.example.com"} {
		if Hostname(hostname) == nil {
			t.Errorf("accepted hostname %q", hostname)
		}
	}
	for _, test := range []struct {
		name      string
		cert, key []byte
		hostname  string
		now       time.Time
		roots     *x509.CertPool
	}{
		{"mismatched key", cert, otherKey, "panel.example.com", time.Now(), roots},
		{"wrong hostname", cert, key, "other.example.com", time.Now(), roots},
		{"untrusted", cert, key, "panel.example.com", time.Now(), x509.NewCertPool()},
		{"expired", cert, key, "panel.example.com", time.Now().Add(400 * 24 * time.Hour), roots},
		{"not yet valid", cert, key, "panel.example.com", time.Now().Add(-24 * time.Hour), roots},
		{"trailing certificate data", append(append([]byte{}, cert...), 'x'), key, "panel.example.com", time.Now(), roots},
		{"trailing key data", cert, append(append([]byte{}, key...), 'x'), "panel.example.com", time.Now(), roots},
		{"prefixed key data", cert, append([]byte("garbage\n"), key...), "panel.example.com", time.Now(), roots},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := Import(test.hostname, test.cert, test.key, test.now, test.roots); err == nil {
				t.Fatal("accepted invalid TLS input")
			}
		})
	}
}

func TestHostnameReservation(t *testing.T) {
	for _, names := range []string{"panel.example.com", "example.com panel.example.com", "*.example.com", "example.org *.example.com"} {
		if !Conflicts([]byte("    server_name "+names+";\n"), "panel.example.com") {
			t.Fatal("missed reservation", names)
		}
	}
	if Conflicts([]byte("server_name other.example.com;\n# panel.example.com\n"), "panel.example.com") {
		t.Fatal("false conflict")
	}
}
