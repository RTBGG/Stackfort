// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/acme"
)

func TestRFC8555IssuerCompletesAndResumesPrivateStagingOrder(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	names := []string{"example.test", "www.example.test"}
	accountKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(10), Subject: pkix.Name{CommonName: "Stackfort Private Staging CA"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	tokens := []string{"abcdefghijklmnopqrstuv", "bcdefghijklmnopqrstuvw"}
	accepted := []bool{false, false}
	finalized := false
	var leafDER []byte
	var server *httptest.Server
	mux := http.NewServeMux()
	withNonce := func(w http.ResponseWriter) { w.Header().Set("Replay-Nonce", "bm9uY2U") }
	orderObject := func() map[string]any {
		status := "pending"
		if accepted[0] && accepted[1] {
			status = "ready"
		}
		if finalized {
			status = "valid"
		}
		result := map[string]any{
			"status": status, "expires": now.Add(time.Hour),
			"identifiers":    []map[string]string{{"type": "dns", "value": names[0]}, {"type": "dns", "value": names[1]}},
			"authorizations": []string{server.URL + "/authz/0", server.URL + "/authz/1"},
			"finalize":       server.URL + "/order/1/finalize",
		}
		if finalized {
			result["certificate"] = server.URL + "/cert/1"
		}
		return result
	}
	mux.HandleFunc("GET /directory", func(w http.ResponseWriter, _ *http.Request) {
		withNonce(w)
		writeTestCAJSON(t, w, http.StatusOK, map[string]any{
			"newNonce": server.URL + "/nonce", "newAccount": server.URL + "/new-account",
			"newOrder": server.URL + "/new-order",
		})
	})
	mux.HandleFunc("HEAD /nonce", func(w http.ResponseWriter, _ *http.Request) {
		withNonce(w)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /new-order", func(w http.ResponseWriter, request *http.Request) {
		payload := decodeTestJWSPayload(t, request)
		var input struct {
			Identifiers []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"identifiers"`
		}
		if err := json.Unmarshal(payload, &input); err != nil {
			t.Error(err)
		}
		requested := []string{}
		for _, identifier := range input.Identifiers {
			if identifier.Type != "dns" {
				t.Errorf("identifier type = %q", identifier.Type)
			}
			requested = append(requested, identifier.Value)
		}
		slices.Sort(requested)
		if !slices.Equal(requested, names) {
			t.Errorf("order names = %#v", requested)
		}
		withNonce(w)
		w.Header().Set("Location", server.URL+"/order/1")
		writeTestCAJSON(t, w, http.StatusCreated, orderObject())
	})
	mux.HandleFunc("POST /order/1", func(w http.ResponseWriter, request *http.Request) {
		_ = decodeTestJWSPayload(t, request)
		withNonce(w)
		w.Header().Set("Location", server.URL+"/order/1")
		writeTestCAJSON(t, w, http.StatusOK, orderObject())
	})
	for index := range tokens {
		index := index
		mux.HandleFunc(fmt.Sprintf("POST /authz/%d", index), func(w http.ResponseWriter, request *http.Request) {
			_ = decodeTestJWSPayload(t, request)
			status := "pending"
			challengeStatus := "pending"
			if accepted[index] {
				status, challengeStatus = "valid", "valid"
			}
			withNonce(w)
			writeTestCAJSON(t, w, http.StatusOK, map[string]any{
				"status": status, "identifier": map[string]string{"type": "dns", "value": names[index]},
				"challenges": []map[string]any{{
					"type": "http-01", "url": fmt.Sprintf("%s/challenge/%d", server.URL, index),
					"status": challengeStatus, "token": tokens[index],
				}},
			})
		})
		mux.HandleFunc(fmt.Sprintf("POST /challenge/%d", index), func(w http.ResponseWriter, request *http.Request) {
			_ = decodeTestJWSPayload(t, request)
			accepted[index] = true
			withNonce(w)
			writeTestCAJSON(t, w, http.StatusOK, map[string]any{
				"type": "http-01", "url": fmt.Sprintf("%s/challenge/%d", server.URL, index),
				"status": "pending", "token": tokens[index],
			})
		})
	}
	mux.HandleFunc("POST /order/1/finalize", func(w http.ResponseWriter, request *http.Request) {
		payload := decodeTestJWSPayload(t, request)
		var input struct {
			CSR string `json:"csr"`
		}
		if err := json.Unmarshal(payload, &input); err != nil {
			t.Error(err)
		}
		csrDER, err := base64.RawURLEncoding.DecodeString(input.CSR)
		if err != nil {
			t.Error(err)
		}
		csr, err := x509.ParseCertificateRequest(csrDER)
		if err != nil || csr.CheckSignature() != nil {
			t.Errorf("CSR = %#v, %v", csr, err)
		}
		csrNames := slices.Clone(csr.DNSNames)
		slices.Sort(csrNames)
		if !slices.Equal(csrNames, names) {
			t.Errorf("CSR names = %#v", csrNames)
		}
		leafTemplate := &x509.Certificate{
			SerialNumber: big.NewInt(11), Subject: pkix.Name{CommonName: names[0]}, DNSNames: csrNames,
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour),
			BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		leafDER, err = x509.CreateCertificate(rand.Reader, leafTemplate, caCertificate, csr.PublicKey, caKey)
		if err != nil {
			t.Error(err)
		}
		finalized = true
		withNonce(w)
		w.Header().Set("Location", server.URL+"/order/1")
		writeTestCAJSON(t, w, http.StatusOK, orderObject())
	})
	mux.HandleFunc("POST /cert/1", func(w http.ResponseWriter, request *http.Request) {
		_ = decodeTestJWSPayload(t, request)
		withNonce(w)
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		w.WriteHeader(http.StatusOK)
		_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
		_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	})
	server = httptest.NewTLSServer(mux)
	defer server.Close()

	orderURL := ""
	presented, cleaned := 0, 0
	issuer := RFC8555Issuer{HTTPClient: server.Client()}
	result, err := issuer.Issue(context.Background(), ACMEIssueRequest{
		DirectoryURL: server.URL + "/directory", AccountURI: server.URL + "/account/1",
		AccountSigner: accountKey, CertificateKey: certificateKey, Names: names,
	}, ACMEIssueCallbacks{
		RecordOrderURL: func(_ context.Context, value string) error { orderURL = value; return nil },
		PresentHTTP01: func(_ context.Context, token, response string) error {
			if !slices.Contains(tokens, token) || !strings.HasPrefix(response, token+".") {
				t.Errorf("challenge response = %q / %q", token, response)
			}
			presented++
			return nil
		},
		CleanupHTTP01: func(_ context.Context, token string) error {
			if !slices.Contains(tokens, token) {
				t.Errorf("cleanup token = %q", token)
			}
			cleaned++
			return nil
		},
	})
	if err != nil || orderURL != server.URL+"/order/1" || result.CertificateURL != server.URL+"/cert/1" ||
		len(result.DERChain) != 2 || presented != 2 || cleaned != 2 {
		t.Fatalf("issue result=%#v orderURL=%q present/clean=%d/%d err=%v",
			result, orderURL, presented, cleaned, err)
	}
	if _, err := validateIssuedCertificate(result, names, certificateKey, now, bytes.NewReader(make([]byte, 8))); err != nil {
		t.Fatalf("validate private staging certificate: %v", err)
	}
	replayed, err := issuer.Issue(context.Background(), ACMEIssueRequest{
		DirectoryURL: server.URL + "/directory", AccountURI: server.URL + "/account/1",
		AccountSigner: accountKey, CertificateKey: certificateKey, Names: names, OrderURL: orderURL,
	}, ACMEIssueCallbacks{
		RecordOrderURL: func(context.Context, string) error { t.Fatal("replay created another order"); return nil },
		PresentHTTP01:  func(context.Context, string, string) error { t.Fatal("replay presented a challenge"); return nil },
		CleanupHTTP01:  func(context.Context, string) error { t.Fatal("replay cleaned a challenge"); return nil },
	})
	if err != nil || replayed.CertificateURL != result.CertificateURL || len(replayed.DERChain) != 2 {
		t.Fatalf("replayed issue result = %#v, %v", replayed, err)
	}
}

func decodeTestJWSPayload(t *testing.T, request *http.Request) []byte {
	t.Helper()
	encoded, err := io.ReadAll(request.Body)
	if err != nil {
		t.Error(err)
		return nil
	}
	var jws struct {
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(encoded, &jws); err != nil {
		t.Error(err)
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(jws.Payload)
	if err != nil {
		t.Error(err)
		return nil
	}
	return payload
}

func TestValidateIssuedCertificatePinsNamesKeyChainAndJitteredRenewal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	names := []string{"example.test", "www.example.test"}
	leafKey, chain := issueCertificateFixture(t, now, names)
	metadata, err := validateIssuedCertificate(
		ACMEIssueResult{DERChain: chain, CertificateURL: "https://acme.example/cert/1"},
		names, leafKey, now, bytes.NewReader(make([]byte, 8)),
	)
	if err != nil {
		t.Fatal(err)
	}
	lifetime := metadata.expiresAt.Sub(metadata.notBefore)
	if metadata.issuer != "Stackfort Test CA" || len(metadata.fingerprint) != 64 ||
		metadata.serialHex != "2" || metadata.fullChainPEM == "" ||
		metadata.nextRenewalAt != metadata.notBefore.Add(time.Duration(float64(lifetime)*0.60)) {
		t.Fatalf("validated metadata = %#v", metadata)
	}
	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateIssuedCertificate(
		ACMEIssueResult{DERChain: chain}, names, wrongKey, now, bytes.NewReader(make([]byte, 8)),
	); err == nil {
		t.Fatal("certificate with a different private key was accepted")
	}
	if _, err := validateIssuedCertificate(
		ACMEIssueResult{DERChain: chain}, []string{"example.test"}, leafKey, now,
		bytes.NewReader(make([]byte, 8)),
	); err == nil {
		t.Fatal("certificate with a different exact SAN set was accepted")
	}
}

func TestACMEOrderNamesMatchIsOrderIndependentAndExact(t *testing.T) {
	t.Parallel()
	order := &acme.Order{Identifiers: []acme.AuthzID{
		{Type: "dns", Value: "www.example.test"}, {Type: "dns", Value: "example.test"},
	}}
	if !acmeOrderNamesMatch(order, []string{"example.test", "www.example.test"}) {
		t.Fatal("exact order identifiers were rejected")
	}
	order.Identifiers = append(order.Identifiers, acme.AuthzID{Type: "dns", Value: "extra.example.test"})
	if acmeOrderNamesMatch(order, []string{"example.test", "www.example.test"}) {
		t.Fatal("order with an extra identifier was accepted")
	}
}

func issueCertificateFixture(
	t *testing.T,
	now time.Time,
	names []string,
) (*ecdsa.PrivateKey, [][]byte) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Stackfort Test CA"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafNames := slices.Clone(names)
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: names[0]}, DNSNames: leafNames,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(89*24*time.Hour + 23*time.Hour),
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	return leafKey, [][]byte{leafDER, rootDER}
}
