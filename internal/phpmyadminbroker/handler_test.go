// SPDX-License-Identifier: AGPL-3.0-or-later

package phpmyadminbroker

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

type redeemerStub struct {
	credential core.PHPMyAdminCredential
	err        error
	params     core.RedeemPHPMyAdminHandoffParams
	calls      int
}

func (stub *redeemerStub) RedeemPHPMyAdminHandoff(_ context.Context, params core.RedeemPHPMyAdminHandoffParams) (core.PHPMyAdminCredential, error) {
	stub.calls++
	stub.params = params
	if stub.err != nil {
		return core.PHPMyAdminCredential{}, stub.err
	}
	return core.PHPMyAdminCredential{
		Username: stub.credential.Username, Host: stub.credential.Host,
		Password: append([]byte(nil), stub.credential.Password...),
	}, nil
}

func TestRedeemRequiresLoopbackAndValidAuthentication(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x42}, SharedKeyBytes)
	token := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE"
	stub := &redeemerStub{credential: core.PHPMyAdminCredential{
		Username: "sf_account_user", Host: "localhost", Password: []byte("secret password"),
	}}
	handler, err := New(stub, key)
	if err != nil {
		t.Fatal(err)
	}

	request := newRedeemRequest(token, "invalid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || stub.calls != 0 {
		t.Fatalf("invalid authentication status/calls = %d/%d", recorder.Code, stub.calls)
	}

	request = newRedeemRequest(token, AuthenticationValue(key, token))
	request.RemoteAddr = "192.0.2.10:12345"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || stub.calls != 0 {
		t.Fatalf("non-loopback status/calls = %d/%d", recorder.Code, stub.calls)
	}

	request = newRedeemRequest(token, AuthenticationValue(key, token))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || stub.calls != 1 ||
		stub.params.Token != token || stub.params.Audience != core.PHPMyAdminHandoffAudience {
		t.Fatalf("status/calls/params = %d/%d/%#v", recorder.Code, stub.calls, stub.params)
	}
	if !strings.Contains(body, `"username":"sf_account_user"`) ||
		!strings.Contains(body, `"passwordBase64":"`+base64.StdEncoding.EncodeToString([]byte("secret password"))+`"`) ||
		strings.Contains(body, token) {
		t.Fatalf("unexpected credential response: %s", body)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response may be cached")
	}
}

func TestRedeemDoesNotExposeRepositoryErrorsAndReplayFails(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x24}, SharedKeyBytes)
	token := "YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI"
	stub := &redeemerStub{err: core.ErrNotFound}
	handler, err := New(stub, key)
	if err != nil {
		t.Fatal(err)
	}
	request := newRedeemRequest(token, AuthenticationValue(key, token))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), token) ||
		strings.Contains(strings.ToLower(recorder.Body.String()), "database") {
		t.Fatalf("replay response = %d/%s", recorder.Code, recorder.Body.String())
	}

	stub.err = errors.New("private database failure")
	request = newRedeemRequest(token, AuthenticationValue(key, token))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "private") {
		t.Fatalf("internal response = %d/%s", recorder.Code, recorder.Body.String())
	}
}

func newRedeemRequest(token, authentication string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, RedeemPath,
		strings.NewReader(`{"handoffToken":"`+token+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(AuthenticationHeader, authentication)
	request.RemoteAddr = "127.0.0.1:12345"
	return request
}
