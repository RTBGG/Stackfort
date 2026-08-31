// SPDX-License-Identifier: AGPL-3.0-or-later

// Package paneltls creates and validates the local certificate used by the
// initial Stackfort management endpoint. It is deliberately separate from
// customer and ACME certificate state.
package paneltls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"slices"
	"strings"
	"time"
)

const (
	Organization = "Stackfort"
	CommonName   = "Stackfort bootstrap panel"
	Lifetime     = 397 * 24 * time.Hour
)

var ErrInvalidBundle = errors.New("invalid Stackfort panel TLS bundle")

// New creates one atomic PEM payload containing the certificate followed by
// its PKCS#8 private key. NGINX can safely use the same root-only file for both
// ssl_certificate and ssl_certificate_key.
func New(now time.Time, hostname string, addresses []net.IP, random io.Reader) ([]byte, error) {
	if random == nil {
		random = rand.Reader
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(random, serialLimit)
	if err != nil {
		return nil, err
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	now = now.UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{Organization},
			CommonName:   CommonName,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(Lifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           normalizedAddresses(addresses),
	}
	if normalized := normalizedHostname(hostname); normalized != "" && normalized != "localhost" {
		template.DNSNames = append(template.DNSNames, normalized)
	}
	der, err := x509.CreateCertificate(random, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})
	clear(encodedKey)
	bundle := append(certificatePEM, privateKeyPEM...)
	if _, err := Validate(bundle, now); err != nil {
		clear(bundle)
		return nil, err
	}
	return bundle, nil
}

// Validate verifies the managed marker, self-signature, key match, server-auth
// purpose, and current validity of a combined panel certificate/key bundle.
func Validate(bundle []byte, now time.Time) (*x509.Certificate, error) {
	certificateBlock, rest := pem.Decode(bundle)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" {
		return nil, ErrInvalidBundle
	}
	keyBlock, trailing := pem.Decode(rest)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(bytes.TrimSpace(trailing)) != 0 {
		return nil, ErrInvalidBundle
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil || certificate.Subject.CommonName != CommonName ||
		!slices.Contains(certificate.Subject.Organization, Organization) || certificate.IsCA ||
		!slices.Equal(certificate.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) ||
		certificate.KeyUsage&x509.KeyUsageDigitalSignature == 0 ||
		certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature) != nil {
		return nil, ErrInvalidBundle
	}
	privateKeyValue, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, ErrInvalidBundle
	}
	privateKey, ok := privateKeyValue.(*ecdsa.PrivateKey)
	publicKey, publicOK := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || !publicOK || privateKey.Curve != elliptic.P256() || publicKey.Curve != elliptic.P256() ||
		privateKey.PublicKey.X.Cmp(publicKey.X) != 0 || privateKey.PublicKey.Y.Cmp(publicKey.Y) != 0 {
		return nil, ErrInvalidBundle
	}
	now = now.UTC()
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) ||
		!slices.Contains(certificate.DNSNames, "localhost") {
		return nil, ErrInvalidBundle
	}
	return certificate, nil
}

func normalizedAddresses(addresses []net.IP) []net.IP {
	values := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	seen := map[string]struct{}{"127.0.0.1": {}, "::1": {}}
	for _, address := range addresses {
		if address == nil || address.IsUnspecified() {
			continue
		}
		normalized := address.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		values = append(values, append(net.IP(nil), address...))
	}
	slices.SortFunc(values, func(left, right net.IP) int { return strings.Compare(left.String(), right.String()) })
	return values
}

func normalizedHostname(hostname string) string {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if hostname == "" || len(hostname) > 253 {
		return ""
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return ""
			}
		}
	}
	return hostname
}
