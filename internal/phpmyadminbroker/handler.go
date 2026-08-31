// SPDX-License-Identifier: AGPL-3.0-or-later

// Package phpmyadminbroker exposes a loopback-only, authenticated credential
// handoff for Stackfort's dedicated phpMyAdmin runtime.
package phpmyadminbroker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/RTBGG/stackfort/internal/core"
)

const (
	DefaultAddress       = "127.0.0.1:8081"
	DefaultKeyPath       = "/var/lib/stackfort-phpmyadmin-broker/broker.key"
	RedeemPath           = "/v1/redeem"
	AuthenticationHeader = "X-Stackfort-PMA-Authentication"
	SharedKeyBytes       = 32
	maxRequestBytes      = 1024
	macContext           = "stackfort-phpmyadmin-broker-v1\n"
)

type Redeemer interface {
	RedeemPHPMyAdminHandoff(context.Context, core.RedeemPHPMyAdminHandoffParams) (core.PHPMyAdminCredential, error)
}

type redeemRequest struct {
	HandoffToken string `json:"handoffToken"`
}

type redeemResponse struct {
	Username       string `json:"username"`
	Host           string `json:"host"`
	PasswordBase64 string `json:"passwordBase64"`
}

func New(redeemer Redeemer, sharedKey []byte) (http.Handler, error) {
	if redeemer == nil {
		return nil, errors.New("phpMyAdmin broker requires a redeemer")
	}
	if len(sharedKey) != SharedKeyBytes {
		return nil, fmt.Errorf("phpMyAdmin broker key must contain exactly %d bytes", SharedKeyBytes)
	}
	key := append([]byte(nil), sharedKey...)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+RedeemPath, func(w http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(w)
		if !isLoopbackRequest(request.RemoteAddr) {
			writeError(w, http.StatusForbidden)
			return
		}
		if !strings.EqualFold(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType)
			return
		}
		var input redeemRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || ensureJSONEnd(decoder) != nil {
			writeError(w, http.StatusBadRequest)
			return
		}
		if !validAuthentication(key, input.HandoffToken, request.Header.Get(AuthenticationHeader)) {
			writeError(w, http.StatusForbidden)
			return
		}
		credential, err := redeemer.RedeemPHPMyAdminHandoff(request.Context(), core.RedeemPHPMyAdminHandoffParams{
			Token: input.HandoffToken, Audience: core.PHPMyAdminHandoffAudience,
		})
		if err != nil {
			if errors.Is(err, core.ErrNotFound) || errors.Is(err, core.ErrAuthorizationDenied) ||
				errors.Is(err, core.ErrSessionInvalid) || errors.Is(err, core.ErrRecentAuthenticationRequired) {
				writeError(w, http.StatusNotFound)
				return
			}
			writeError(w, http.StatusInternalServerError)
			return
		}
		defer clear(credential.Password)
		writeJSON(w, http.StatusOK, redeemResponse{
			Username: credential.Username, Host: credential.Host,
			PasswordBase64: base64.StdEncoding.EncodeToString(credential.Password),
		})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(w)
		mux.ServeHTTP(w, request)
	}), nil
}

// AuthenticationValue returns the request authenticator used by the dedicated
// local phpMyAdmin process. It intentionally signs only the unguessable,
// single-use handoff bearer; replay is rejected by the repository transaction.
func AuthenticationValue(sharedKey []byte, handoffToken string) string {
	mac := hmac.New(sha256.New, sharedKey)
	_, _ = mac.Write([]byte(macContext))
	_, _ = mac.Write([]byte(handoffToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validAuthentication(sharedKey []byte, handoffToken, supplied string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(supplied)
	if err != nil || len(decoded) != sha256.Size || base64.RawURLEncoding.EncodeToString(decoded) != supplied {
		clear(decoded)
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(AuthenticationValue(sharedKey, handoffToken))
	if err != nil {
		clear(decoded)
		return false
	}
	valid := hmac.Equal(decoded, expected)
	clear(decoded)
	clear(expected)
	return valid
}

func isLoopbackRequest(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return false
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("unexpected trailing JSON")
	} else if errors.Is(err, io.EOF) {
		return nil
	} else {
		return err
	}
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int) {
	writeJSON(w, status, map[string]string{"error": http.StatusText(status)})
}
