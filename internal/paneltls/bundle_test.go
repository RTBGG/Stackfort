// SPDX-License-Identifier: AGPL-3.0-or-later

package paneltls

import (
	"bytes"
	"crypto/rand"
	"net"
	"testing"
	"time"
)

func TestBundlePinsManagedIdentityNamesKeyAndLifetime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	bundle, err := New(now, "Panel-Node.EXAMPLE.", []net.IP{
		net.ParseIP("192.0.2.10"), net.ParseIP("192.0.2.10"), net.IPv4zero,
	}, rand.Reader)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	certificate, err := Validate(bundle, now)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if certificate.Subject.CommonName != CommonName || certificate.NotAfter.Sub(now) != Lifetime ||
		certificate.VerifyHostname("localhost") != nil || certificate.VerifyHostname("panel-node.example") != nil ||
		certificate.VerifyHostname("192.0.2.10") != nil {
		t.Fatalf("certificate = %#v", certificate)
	}
	if bytes.Count(bundle, []byte("-----BEGIN")) != 2 {
		t.Fatalf("unexpected PEM block count: %q", bundle)
	}
}

func TestBundleRejectsTamperingAndUnsafeHostname(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	bundle, err := New(now, "bad_host.example", nil, rand.Reader)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	certificate, err := Validate(bundle, now)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if certificate.VerifyHostname("bad_host.example") == nil {
		t.Fatal("unsafe hostname entered the certificate")
	}
	tampered := append([]byte(nil), bundle...)
	tampered[len(tampered)/2] ^= 1
	if _, err := Validate(tampered, now); err == nil {
		t.Fatal("tampered bundle was accepted")
	}
	if _, err := Validate(bundle, now.Add(Lifetime+time.Hour)); err == nil {
		t.Fatal("expired bundle was accepted")
	}
}
