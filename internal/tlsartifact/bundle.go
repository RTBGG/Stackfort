// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tlsartifact defines the closed certificate bundle format shared by
// the control-plane worker, NGINX renderer, and privileged host agent.
package tlsartifact

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"path"
	"slices"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
)

const (
	Directory              = "/etc/nginx/stackfort/certificates"
	FullChainFileName      = "fullchain.pem"
	PrivateKeyFileName     = "private-key.pem"
	MaximumFullChainBytes  = 40 << 10
	MaximumPrivateKeyBytes = 4 << 10
	MaximumNamesBytes      = 8 << 10
)

var ErrInvalidBundle = errors.New("invalid managed TLS certificate bundle")

type Bundle struct {
	CertificateID string   `json:"certificateId"`
	Names         []string `json:"names"`
	FullChainPEM  string   `json:"fullChainPem"`
	PrivateKeyPEM string   `json:"privateKeyPem"`
}

func CertificatePath(certificateID string) (string, error) {
	if _, err := core.ParseID(certificateID); err != nil {
		return "", ErrInvalidBundle
	}
	return path.Join(Directory, certificateID, FullChainFileName), nil
}

func PrivateKeyPath(certificateID string) (string, error) {
	if _, err := core.ParseID(certificateID); err != nil {
		return "", ErrInvalidBundle
	}
	return path.Join(Directory, certificateID, PrivateKeyFileName), nil
}

// Validate rejects arbitrary paths, non-canonical names, trailing PEM data,
// unsupported keys, and a key that does not match the leaf certificate.
func Validate(bundle Bundle) error {
	if _, err := core.ParseID(bundle.CertificateID); err != nil || len(bundle.Names) == 0 ||
		len(bundle.Names) > 100 || !slices.IsSorted(bundle.Names) ||
		len(bundle.FullChainPEM) == 0 || len(bundle.FullChainPEM) > MaximumFullChainBytes ||
		len(bundle.PrivateKeyPEM) == 0 || len(bundle.PrivateKeyPEM) > MaximumPrivateKeyBytes {
		return ErrInvalidBundle
	}
	totalNamesBytes := 0
	for index, name := range bundle.Names {
		totalNamesBytes += len(name)
		normalized, err := core.NormalizeDomainName(name)
		if err != nil || normalized.ASCII != name || strings.HasPrefix(name, "*.") ||
			(index > 0 && bundle.Names[index-1] == name) {
			return ErrInvalidBundle
		}
	}
	if totalNamesBytes > MaximumNamesBytes {
		return ErrInvalidBundle
	}
	certificates, err := ParseFullChain(bundle.FullChainPEM)
	if err != nil || len(certificates) == 0 {
		return ErrInvalidBundle
	}
	keyBlock, rest := pem.Decode([]byte(bundle.PrivateKeyPEM))
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return ErrInvalidBundle
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	key, ok := parsedKey.(*ecdsa.PrivateKey)
	if err != nil || !ok || key.Curve != elliptic.P256() {
		return ErrInvalidBundle
	}
	publicKey, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return ErrInvalidBundle
	}
	leafPublicKey, err := x509.MarshalPKIXPublicKey(certificates[0].PublicKey)
	if err != nil || !bytes.Equal(publicKey, leafPublicKey) {
		return ErrInvalidBundle
	}
	leafNames := slices.Clone(certificates[0].DNSNames)
	slices.Sort(leafNames)
	if !slices.Equal(leafNames, bundle.Names) {
		return ErrInvalidBundle
	}
	for index := 0; index+1 < len(certificates); index++ {
		if certificates[index].CheckSignatureFrom(certificates[index+1]) != nil {
			return ErrInvalidBundle
		}
	}
	return nil
}

func ParseFullChain(value string) ([]*x509.Certificate, error) {
	rest := []byte(value)
	certificates := []*x509.Certificate{}
	for len(bytes.TrimSpace(rest)) > 0 {
		block, remaining := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, ErrInvalidBundle
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, ErrInvalidBundle
		}
		certificates = append(certificates, certificate)
		rest = remaining
	}
	if len(certificates) == 0 {
		return nil, ErrInvalidBundle
	}
	return certificates, nil
}
