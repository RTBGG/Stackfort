// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestRFC8555RegistrarRegistersAgainstPrivateStagingCA(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /directory", func(w http.ResponseWriter, _ *http.Request) {
		writeTestCAJSON(t, w, http.StatusOK, map[string]any{
			"newNonce": server.URL + "/nonce", "newAccount": server.URL + "/new-account",
			"newOrder": server.URL + "/new-order",
			"meta":     map[string]any{"termsOfService": server.URL + "/terms"},
		})
	})
	mux.HandleFunc("HEAD /nonce", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Replay-Nonce", "bm9uY2U")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /new-account", func(w http.ResponseWriter, request *http.Request) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		var jws struct {
			Payload string `json:"payload"`
		}
		if err := json.Unmarshal(encoded, &jws); err != nil {
			t.Errorf("decode JWS: %v", err)
		}
		payload, err := base64.RawURLEncoding.DecodeString(jws.Payload)
		if err != nil {
			t.Errorf("decode JWS payload: %v", err)
		}
		var registration struct {
			Contact              []string `json:"contact"`
			TermsOfServiceAgreed bool     `json:"termsOfServiceAgreed"`
		}
		if err := json.Unmarshal(payload, &registration); err != nil {
			t.Errorf("decode registration: %v", err)
		}
		if len(registration.Contact) != 1 || registration.Contact[0] != "mailto:admin@example.test" ||
			!registration.TermsOfServiceAgreed {
			t.Errorf("registration payload = %#v", registration)
		}
		w.Header().Set("Location", server.URL+"/account/1")
		w.Header().Set("Replay-Nonce", "bm9uY2Uy")
		writeTestCAJSON(t, w, http.StatusCreated, map[string]any{
			"status": "valid", "contact": registration.Contact,
			"orders": server.URL + "/account/1/orders",
		})
	})
	server = httptest.NewTLSServer(mux)
	defer server.Close()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (RFC8555Registrar{HTTPClient: server.Client()}).Register(context.Background(), ACMERegistrationRequest{
		DirectoryURL: server.URL + "/directory", ContactEmail: "admin@example.test",
		TermsAccepted: true, Signer: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != core.ACMEAccountValid || result.AccountURI != server.URL+"/account/1" ||
		result.OrdersURL != server.URL+"/account/1/orders" || result.TermsURL != server.URL+"/terms" {
		t.Fatalf("registration result = %#v", result)
	}
}

func writeTestCAJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
