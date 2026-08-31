// SPDX-License-Identifier: AGPL-3.0-or-later

package tlsartifact

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestBundleValidationPinsFixedIDNamesAndKey(t *testing.T) {
	t.Parallel()
	bundle := certificateBundleFixture(t)
	if err := Validate(bundle); err != nil {
		t.Fatal(err)
	}
	certificatePath, err := CertificatePath(bundle.CertificateID)
	if err != nil || certificatePath != "/etc/nginx/stackfort/certificates/019c1234-5678-7abc-8def-0123456789ae/fullchain.pem" {
		t.Fatalf("certificate path = %q, %v", certificatePath, err)
	}
	privateKeyPath, err := PrivateKeyPath(bundle.CertificateID)
	if err != nil || privateKeyPath != "/etc/nginx/stackfort/certificates/019c1234-5678-7abc-8def-0123456789ae/private-key.pem" {
		t.Fatalf("private key path = %q, %v", privateKeyPath, err)
	}

	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongDER, err := x509.MarshalPKCS8PrivateKey(wrongKey)
	if err != nil {
		t.Fatal(err)
	}
	invalid := bundle
	invalid.PrivateKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: wrongDER}))
	if Validate(invalid) == nil {
		t.Fatal("bundle with mismatched private key was accepted")
	}
	invalid = bundle
	invalid.CertificateID = "../../escape"
	if Validate(invalid) == nil {
		t.Fatal("bundle with path-like certificate ID was accepted")
	}
	invalid = bundle
	invalid.Names = []string{"www.example.test", "example.test"}
	if Validate(invalid) == nil {
		t.Fatal("bundle with non-canonical name order was accepted")
	}
	invalid = bundle
	invalid.PrivateKeyPEM += "trailing"
	if Validate(invalid) == nil {
		t.Fatal("bundle with trailing key data was accepted")
	}
}

func certificateBundleFixture(t *testing.T) Bundle {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.test"},
		DNSNames:  []string{"example.test", "www.example.test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return Bundle{
		CertificateID: "019c1234-5678-7abc-8def-0123456789ae",
		Names:         []string{"example.test", "www.example.test"},
		FullChainPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}
}
