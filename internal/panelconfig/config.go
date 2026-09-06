// SPDX-License-Identifier: AGPL-3.0-or-later

// Package panelconfig describes the root-managed, optional panel HTTPS origin.
// It is deliberately separate from tenant domains and their ACME state.
package panelconfig

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

const (
	ConfigurationPath = nginxbaseline.PanelDirectory + "/20-hostname.conf"
	CandidatePath     = nginxbaseline.PanelDirectory + "/hostname.pending"
	CandidateMainPath = nginxbaseline.ManagedRoot + "/panel-candidate.pending"
	JournalPath       = nginxbaseline.ManagedRoot + "/.panel-host-transaction.json"
	TLSDirectory      = "/etc/stackfort/panel-tls"
	MaximumFile       = 128 << 10
	marker            = "# Stackfort panel hostname v1: "
)

var ErrInvalid = errors.New("invalid panel hostname, certificate, or managed configuration")

type Config struct {
	Hostname      string `json:"hostname"`
	BundleSHA256  string `json:"bundleSha256"`
	AutoRenew     bool   `json:"autoRenew,omitempty"`
	ChallengeOnly bool   `json:"challengeOnly,omitempty"`
}

func Hostname(value string) error {
	name, err := core.NormalizeDomainName(value)
	if err != nil || name.ASCII != value || !strings.Contains(value, ".") || strings.Contains(value, "*") {
		return ErrInvalid
	}
	return nil
}

func (config Config) Validate() error {
	if config.ChallengeOnly {
		if !config.AutoRenew || config.BundleSHA256 != "" {
			return ErrInvalid
		}
		return Hostname(config.Hostname)
	}
	decoded, err := hex.DecodeString(config.BundleSHA256)
	if Hostname(config.Hostname) != nil || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != config.BundleSHA256 {
		return ErrInvalid
	}
	return nil
}

func (config Config) BundlePath() string {
	return TLSDirectory + "/host-" + config.BundleSHA256 + ".pem"
}

// Import requires an unencrypted, matching server key and a chain trusted by
// the host. roots=nil means the system trust store, never verification bypass.
func Import(hostname string, certificatePEM, keyPEM []byte, now time.Time, roots *x509.CertPool) (Config, []byte, error) {
	if Hostname(hostname) != nil || len(certificatePEM) > MaximumFile/2 || len(keyPEM) > MaximumFile/2 {
		return Config{}, nil, ErrInvalid
	}
	var certificates []*x509.Certificate
	var normalized []byte
	for rest := bytes.TrimSpace(certificatePEM); len(rest) > 0; {
		if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) {
			return Config{}, nil, ErrInvalid
		}
		block, trailing := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(certificates) >= 8 {
			return Config{}, nil, ErrInvalid
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return Config{}, nil, ErrInvalid
		}
		certificates = append(certificates, certificate)
		normalized = append(normalized, pem.EncodeToMemory(block)...)
		rest = bytes.TrimSpace(trailing)
	}
	keyBlock, trailing := pem.Decode(bytes.TrimSpace(keyPEM))
	if len(certificates) == 0 || keyBlock == nil || len(keyBlock.Headers) != 0 || len(bytes.TrimSpace(trailing)) != 0 ||
		!bytes.HasPrefix(bytes.TrimSpace(keyPEM), []byte("-----BEGIN "+keyBlock.Type+"-----")) {
		return Config{}, nil, ErrInvalid
	}
	pair, err := tls.X509KeyPair(normalized, keyPEM)
	if err != nil {
		return Config{}, nil, ErrInvalid
	}
	switch key := pair.PrivateKey.(type) {
	case *rsa.PrivateKey:
		if key.N.BitLen() < 2048 {
			return Config{}, nil, ErrInvalid
		}
	case *ecdsa.PrivateKey:
		if key.Curve.Params().BitSize < 256 {
			return Config{}, nil, ErrInvalid
		}
	case ed25519.PrivateKey:
	default:
		return Config{}, nil, ErrInvalid
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range certificates[1:] {
		intermediates.AddCert(certificate)
	}
	leaf := certificates[0]
	if leaf.IsCA || !leaf.NotAfter.After(now.Add(24*time.Hour)) {
		return Config{}, nil, ErrInvalid
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: hostname, Roots: roots, Intermediates: intermediates, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return Config{}, nil, ErrInvalid
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		return Config{}, nil, ErrInvalid
	}
	defer clear(keyDER)
	encodedKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	defer clear(encodedKey)
	bundle := append(normalized, encodedKey...)
	digest := sha256.Sum256(bundle)
	return Config{Hostname: hostname, BundleSHA256: hex.EncodeToString(digest[:])}, bundle, nil
}

func Render(spec nginxbaseline.Spec, config Config) (string, error) {
	if config.Validate() != nil {
		return "", ErrInvalid
	}
	metadata, _ := json.Marshal(config)
	if config.ChallengeOnly {
		return marker + string(metadata) + "\n" + panelHTTP(config.Hostname, true), nil
	}
	content := nginxbaseline.Panel(spec)
	content = strings.ReplaceAll(content, "8443 ssl default_server", "443 ssl")
	content = strings.Replace(content, "server_name _;", "server_name "+config.Hostname+";\n    if ($host != "+config.Hostname+") { return 421; }\n    if ($ssl_server_name != "+config.Hostname+") { return 421; }", 1)
	content = strings.ReplaceAll(content, nginxbaseline.PanelTLSBundlePath, config.BundlePath())
	return marker + string(metadata) + "\n" + content + "\n" + panelHTTP(config.Hostname, false), nil
}

func panelHTTP(hostname string, challengeOnly bool) string {
	fallback := "return 308 https://" + hostname + "$request_uri;"
	if challengeOnly {
		fallback = "return 503;"
	}
	return "server {\n    listen 80;\n    listen [::]:80;\n    server_name " + hostname + ";\n" +
		"    location ~ \"^/\\.well-known/acme-challenge/([A-Za-z0-9_-]{22,256})$\" {\n" +
		"        alias " + acmehttp01.ChallengeDirectory + "/$1;\n        default_type text/plain;\n        disable_symlinks on;\n        access_log off;\n        limit_except GET { deny all; }\n    }\n" +
		"    location / { " + fallback + " }\n}\n"
}

// Parse accepts only an exact generated configuration, never arbitrary NGINX.
func Parse(spec nginxbaseline.Spec, content []byte) (Config, error) {
	line, _, ok := strings.Cut(string(content), "\n")
	if !ok || len(content) > MaximumFile || !strings.HasPrefix(line, marker) {
		return Config{}, ErrInvalid
	}
	var config Config
	if json.Unmarshal([]byte(strings.TrimPrefix(line, marker)), &config) != nil {
		return Config{}, ErrInvalid
	}
	expected, err := Render(spec, config)
	if err != nil || expected != string(content) {
		return Config{}, ErrInvalid
	}
	return config, nil
}

func CandidateMain(spec nginxbaseline.Spec) string {
	return strings.Replace(nginxbaseline.Main(spec), "include /etc/nginx/stackfort/panel-enabled/*.conf;",
		"include "+nginxbaseline.PanelConfigurationPath+";\n    include "+CandidatePath+";", 1)
}

// Conflicts examines only renderer-owned server_name lines; wildcard hosts
// are conservatively reserved as well as exact names and generated www aliases.
func Conflicts(content []byte, hostname string) bool {
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "server_name" {
			continue
		}
		for _, field := range fields[1:] {
			name := strings.TrimSuffix(field, ";")
			if name == hostname || (strings.HasPrefix(name, "*.") && strings.HasSuffix(hostname, name[1:])) {
				return true
			}
		}
	}
	return false
}
