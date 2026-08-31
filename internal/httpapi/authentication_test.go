// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- test helper implements the RFC 6238 interoperable SHA-1 profile.
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/store"
)

type authenticationServiceStub struct {
	loginResult core.PasswordLoginResult
	loginErr    error
	loginParams core.PasswordLoginParams
	loginCalls  int
	mfaResult   core.PasswordLoginResult
	mfaErr      error
	mfaParams   core.CompleteMFALoginParams
	mfaCalls    int

	authenticated core.AuthenticatedSession
	authErr       error
	authParams    core.AuthenticateSessionParams
	authCalls     int

	revokeErr    error
	revokeParams core.RevokeSessionParams
	revokeCalls  int
}

func (stub *authenticationServiceStub) CompleteMFALogin(_ context.Context, params core.CompleteMFALoginParams) (core.PasswordLoginResult, error) {
	stub.mfaCalls++
	stub.mfaParams = params
	return stub.mfaResult, stub.mfaErr
}

func (stub *authenticationServiceStub) PasswordLogin(_ context.Context, params core.PasswordLoginParams) (core.PasswordLoginResult, error) {
	stub.loginCalls++
	stub.loginParams = params
	return stub.loginResult, stub.loginErr
}

func (stub *authenticationServiceStub) AuthenticateSession(_ context.Context, params core.AuthenticateSessionParams) (core.AuthenticatedSession, error) {
	stub.authCalls++
	stub.authParams = params
	return stub.authenticated, stub.authErr
}

func (stub *authenticationServiceStub) RevokeSession(_ context.Context, params core.RevokeSessionParams) error {
	stub.revokeCalls++
	stub.revokeParams = params
	return stub.revokeErr
}

func TestLoginSetsHostOnlyStrictCookiesWithoutExposingSecrets(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	service := &authenticationServiceStub{loginResult: core.PasswordLoginResult{
		Identity: core.Identity{
			ID: "0198b935-b600-7000-8000-000000000010", Email: "admin@example.com",
			DisplayName: "Administrator", Locale: core.LocaleEnglish,
		},
		Session: core.Session{
			ID:              "0198b935-b600-7000-8000-000000000011",
			AuthenticatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(12 * time.Hour),
		},
		SessionToken: "sfs_session-secret", CSRFToken: "sfc_csrf-secret",
	}}
	const password = "correct horse battery staple"
	var logs bytes.Buffer
	recorder := httptest.NewRecorder()
	request := newJSONRequest(http.MethodPost, "/api/v1/login", `{"email":"admin@example.com","password":"`+password+`"}`)
	request.RemoteAddr = "[::ffff:192.0.2.20]:443"
	request.Header.Set("User-Agent", "Stackfort Browser")
	request.Header.Set("X-Request-ID", "login-1")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_previous"})
	NewWithServices(slog.New(slog.NewJSONHandler(&logs, nil)), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.loginCalls != 1 || service.loginParams.SourceAddress != "192.0.2.20" ||
		service.loginParams.PreviousSessionToken != "sfs_previous" || service.loginParams.UserAgent != "Stackfort Browser" {
		t.Fatalf("login params = %#v", service.loginParams)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("cookies=%#v", cookies)
	}
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	sessionCookie := byName[sessionCookieName]
	csrfCookie := byName[csrfCookieName]
	mfaCookie := byName[mfaCookieName]
	if sessionCookie == nil || sessionCookie.Value != service.loginResult.SessionToken ||
		!sessionCookie.Secure || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode ||
		sessionCookie.Path != "/" || sessionCookie.Domain != "" || sessionCookie.MaxAge != 0 || !sessionCookie.Expires.IsZero() {
		t.Fatalf("unsafe session cookie: %#v", sessionCookie)
	}
	if csrfCookie == nil || csrfCookie.Value != service.loginResult.CSRFToken ||
		!csrfCookie.Secure || csrfCookie.HttpOnly || csrfCookie.SameSite != http.SameSiteStrictMode ||
		csrfCookie.Path != "/" || csrfCookie.Domain != "" || csrfCookie.MaxAge != 0 || !csrfCookie.Expires.IsZero() {
		t.Fatalf("unsafe CSRF cookie: %#v", csrfCookie)
	}
	if mfaCookie == nil || mfaCookie.MaxAge != -1 || !mfaCookie.Secure || !mfaCookie.HttpOnly ||
		mfaCookie.SameSite != http.SameSiteStrictMode || mfaCookie.Path != "/" {
		t.Fatalf("unsafe stale MFA cookie deletion: %#v", mfaCookie)
	}
	visible := logs.String() + recorder.Body.String()
	for _, secret := range []string{password, service.loginResult.SessionToken, service.loginResult.CSRFToken} {
		if strings.Contains(visible, secret) {
			t.Fatalf("login log/response exposed %q: %s", secret, visible)
		}
	}
}

func TestLoginFailuresUseIdenticalGenericResponse(t *testing.T) {
	t.Parallel()

	responses := make([]string, 0, 2)
	for _, email := range []string{"known@example.com", "unknown@example.com"} {
		service := &authenticationServiceStub{loginErr: core.ErrAuthenticationDenied}
		recorder := httptest.NewRecorder()
		request := newJSONRequest(http.MethodPost, "/api/v1/login", `{"email":"`+email+`","password":"incorrect password value"}`)
		NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d", email, recorder.Code)
		}
		responses = append(responses, recorder.Body.String())
	}
	if responses[0] != responses[1] {
		t.Fatalf("login failure responses differ: %q != %q", responses[0], responses[1])
	}
}

func TestLoginRateLimitIncludesRetryAfter(t *testing.T) {
	t.Parallel()

	service := &authenticationServiceStub{loginErr: &core.AuthenticationRateLimitError{RetryAfter: 2500 * time.Millisecond}}
	recorder := httptest.NewRecorder()
	request := newJSONRequest(http.MethodPost, "/api/v1/login", validLoginJSON())
	NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "3" {
		t.Fatalf("status=%d Retry-After=%q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
}

func TestLoginRejectsSimpleMalformedAndOversizedRequestsBeforeService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{name: "simple form", body: "email=a", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType},
		{name: "unknown field", body: `{"email":"a@example.com","password":"value","extra":true}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "multiple values", body: validLoginJSON() + `{}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "oversized", body: `{"email":"` + strings.Repeat("x", maxLoginRequestBytes) + `"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &authenticationServiceStub{}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || service.loginCalls != 0 {
				t.Fatalf("status=%d calls=%d", recorder.Code, service.loginCalls)
			}
		})
	}
}

func TestSessionEndpointUsesCookieOnlyAndReturnsNoSecret(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	service := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{
			ID: "0198b935-b600-7000-8000-000000000020", Email: "admin@example.com",
			DisplayName: "Administrator", Locale: core.LocaleGerman,
		},
		Session: core.Session{
			ID:              "0198b935-b600-7000-8000-000000000021",
			AuthenticatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
		},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session?session=sfs_query", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_cookie"})
	NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.authParams.SessionToken != "sfs_cookie" || service.authParams.RequireCSRF {
		t.Fatalf("status=%d auth params=%#v", recorder.Code, service.authParams)
	}
	if strings.Contains(recorder.Body.String(), "sfs_cookie") || strings.Contains(recorder.Body.String(), "sfs_query") {
		t.Fatalf("session response exposed a bearer value: %s", recorder.Body.String())
	}
}

func TestLogoutRequiresBoundCSRFAndRevokesServerSession(t *testing.T) {
	t.Parallel()

	service := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: "0198b935-b600-7000-8000-000000000030"},
		Session:  core.Session{ID: "0198b935-b600-7000-8000-000000000031"},
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	request.RemoteAddr = "192.0.2.30:443"
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "sfc_bound"})
	request.Header.Set(csrfHeaderName, "sfc_bound")
	request.Header.Set("X-Request-ID", "logout-1")
	NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent || service.authCalls != 1 || service.revokeCalls != 1 {
		t.Fatalf("status=%d auth=%d revoke=%d", recorder.Code, service.authCalls, service.revokeCalls)
	}
	if !service.authParams.RequireCSRF || service.authParams.CSRFHeaderToken != "sfc_bound" ||
		service.authParams.CSRFCookieToken != "sfc_bound" || service.revokeParams.Reason != "logout" ||
		service.revokeParams.SourceAddress != "192.0.2.30" {
		t.Fatalf("auth=%#v revoke=%#v", service.authParams, service.revokeParams)
	}
	if recorder.Header().Get("Clear-Site-Data") == "" {
		t.Fatal("logout omitted Clear-Site-Data")
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			t.Fatalf("logout did not expire cookie: %#v", cookie)
		}
	}
}

func TestLogoutRejectsInvalidCSRFWithoutRevocation(t *testing.T) {
	t.Parallel()

	service := &authenticationServiceStub{authErr: core.ErrCSRFInvalid}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_session"})
	NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || service.revokeCalls != 0 {
		t.Fatalf("status=%d revoke=%d", recorder.Code, service.revokeCalls)
	}
}

func TestInvalidSessionLogoutIsIdempotentAndClearsCookies(t *testing.T) {
	t.Parallel()

	service := &authenticationServiceStub{authErr: core.ErrSessionInvalid}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_invalid"})
	NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || service.revokeCalls != 0 || len(recorder.Result().Cookies()) != 2 {
		t.Fatalf("status=%d revoke=%d cookies=%#v", recorder.Code, service.revokeCalls, recorder.Result().Cookies())
	}
}

func TestCrossSiteMutationIsRejectedBeforeAuthenticationService(t *testing.T) {
	t.Parallel()

	service := &authenticationServiceStub{}
	recorder := httptest.NewRecorder()
	request := newJSONRequest(http.MethodPost, "/api/v1/login", validLoginJSON())
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	NewWithServices(discardLogger(), healthChecker{}, Services{Authentication: service}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || service.loginCalls != 0 {
		t.Fatalf("status=%d login calls=%d", recorder.Code, service.loginCalls)
	}
}

func TestAuthenticationHTTPIntegrationLoginLogoutAndReplay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := core.NewRepository(state)
	if err != nil {
		t.Fatalf("core.NewRepository: %v", err)
	}
	capability, err := repository.CreateBootstrapCapability(ctx, core.CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	const password = "correct horse battery staple"
	if _, err := repository.BootstrapAdministrator(ctx, core.BootstrapAdministratorParams{
		Token: capability.Token, Email: "admin@example.com", DisplayName: "Administrator",
		Password: password, Locale: core.LocaleEnglish, SourceAddress: "192.0.2.40",
	}); err != nil {
		t.Fatalf("BootstrapAdministrator: %v", err)
	}
	handler := NewWithServices(discardLogger(), state, Services{Bootstrap: repository, Authentication: repository})

	loginRecorder := httptest.NewRecorder()
	loginRequest := newJSONRequest(http.MethodPost, "/api/v1/login", validLoginJSON())
	loginRequest.RemoteAddr = "192.0.2.40:443"
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range loginRecorder.Result().Cookies() {
		switch cookie.Name {
		case sessionCookieName:
			sessionCookie = cookie
		case csrfCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("login cookies=%#v", loginRecorder.Result().Cookies())
	}

	sessionRecorder := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("session status=%d body=%s", sessionRecorder.Code, sessionRecorder.Body.String())
	}

	logoutRecorder := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/logout", nil)
	logoutRequest.RemoteAddr = "192.0.2.40:443"
	logoutRequest.AddCookie(sessionCookie)
	logoutRequest.AddCookie(csrfCookie)
	logoutRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	handler.ServeHTTP(logoutRecorder, logoutRequest)
	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logoutRecorder.Code, logoutRecorder.Body.String())
	}

	replayRecorder := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	replayRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(replayRecorder, replayRequest)
	if replayRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d body=%s", replayRecorder.Code, replayRecorder.Body.String())
	}
}

func TestMFAHTTPIntegrationWithRecoveryLoginAndSessionRevocation(t *testing.T) {
	ctx := context.Background()
	state, err := store.Open(ctx, filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := core.NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x61}, 32))
	if err != nil {
		t.Fatalf("core.NewRepositoryWithMasterKey: %v", err)
	}
	capability, err := repository.CreateBootstrapCapability(ctx, core.CreateBootstrapCapabilityParams{})
	if err != nil {
		t.Fatalf("CreateBootstrapCapability: %v", err)
	}
	if _, err := repository.BootstrapAdministrator(ctx, core.BootstrapAdministratorParams{
		Token: capability.Token, Email: "admin@example.com", DisplayName: "Administrator",
		Password: "correct horse battery staple", Locale: core.LocaleEnglish,
		SourceAddress: "192.0.2.50",
	}); err != nil {
		t.Fatalf("BootstrapAdministrator: %v", err)
	}
	handler := NewWithServices(discardLogger(), state, Services{
		Authentication: repository, MultiFactor: repository, Sessions: repository,
	})

	loginRecorder := httptest.NewRecorder()
	loginRequest := newJSONRequest(http.MethodPost, "/api/v1/login", validLoginJSON())
	loginRequest.RemoteAddr = "192.0.2.50:443"
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("initial login status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	sessionCookie, csrfCookie := authenticationCookies(loginRecorder.Result().Cookies())
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("initial login cookies=%#v", loginRecorder.Result().Cookies())
	}

	setupRecorder := httptest.NewRecorder()
	setupRequest := newJSONRequest(http.MethodPost, "/api/v1/mfa/totp/setup", `{}`)
	setupRequest.RemoteAddr = "192.0.2.50:443"
	setupRequest.AddCookie(sessionCookie)
	setupRequest.AddCookie(csrfCookie)
	setupRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	handler.ServeHTTP(setupRecorder, setupRequest)
	if setupRecorder.Code != http.StatusCreated {
		t.Fatalf("setup status=%d body=%s", setupRecorder.Code, setupRecorder.Body.String())
	}
	var enrollment totpEnrollmentResponse
	if err := json.Unmarshal(setupRecorder.Body.Bytes(), &enrollment); err != nil {
		t.Fatalf("decode enrollment: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatalf("decode TOTP secret: %v", err)
	}
	code := testHOTP(secret, uint64(time.Now().UTC().Unix()/30))
	confirmRecorder := httptest.NewRecorder()
	confirmRequest := newJSONRequest(http.MethodPost,
		"/api/v1/mfa/totp/setup/"+string(enrollment.ChallengeID)+"/confirm",
		`{"code":"`+code+`"}`)
	confirmRequest.RemoteAddr = "192.0.2.50:443"
	confirmRequest.AddCookie(sessionCookie)
	confirmRequest.AddCookie(csrfCookie)
	confirmRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	handler.ServeHTTP(confirmRecorder, confirmRequest)
	if confirmRecorder.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmRecorder.Code, confirmRecorder.Body.String())
	}
	var activation totpActivationResponse
	if err := json.Unmarshal(confirmRecorder.Body.Bytes(), &activation); err != nil {
		t.Fatalf("decode activation: %v", err)
	}
	if len(activation.RecoveryCodes) == 0 {
		t.Fatal("activation did not return one-time recovery codes")
	}

	mfaPasswordRecorder := httptest.NewRecorder()
	mfaPasswordRequest := newJSONRequest(http.MethodPost, "/api/v1/login", validLoginJSON())
	mfaPasswordRequest.RemoteAddr = "192.0.2.50:443"
	handler.ServeHTTP(mfaPasswordRecorder, mfaPasswordRequest)
	if mfaPasswordRecorder.Code != http.StatusAccepted ||
		strings.Contains(mfaPasswordRecorder.Body.String(), "sfm_") {
		t.Fatalf("MFA password phase status=%d body=%s", mfaPasswordRecorder.Code, mfaPasswordRecorder.Body.String())
	}
	var mfaCookie *http.Cookie
	for _, cookie := range mfaPasswordRecorder.Result().Cookies() {
		if cookie.Name == mfaCookieName {
			mfaCookie = cookie
		}
		if cookie.Name == sessionCookieName || cookie.Name == csrfCookieName {
			t.Fatalf("password phase issued authentication cookie: %#v", cookie)
		}
	}
	if mfaCookie == nil || !mfaCookie.Secure || !mfaCookie.HttpOnly ||
		mfaCookie.SameSite != http.SameSiteStrictMode || mfaCookie.Path != "/" {
		t.Fatalf("unsafe MFA challenge cookie: %#v", mfaCookie)
	}

	mfaRecorder := httptest.NewRecorder()
	mfaRequest := newJSONRequest(http.MethodPost, "/api/v1/login/mfa",
		`{"code":"`+activation.RecoveryCodes[0]+`"}`)
	mfaRequest.AddCookie(mfaCookie)
	handler.ServeHTTP(mfaRecorder, mfaRequest)
	if mfaRecorder.Code != http.StatusOK || strings.Contains(mfaRecorder.Body.String(), "sfs_") ||
		strings.Contains(mfaRecorder.Body.String(), "sfc_") || strings.Contains(mfaRecorder.Body.String(), "sfm_") {
		t.Fatalf("MFA completion status=%d body=%s", mfaRecorder.Code, mfaRecorder.Body.String())
	}
	sessionCookie, csrfCookie = authenticationCookies(mfaRecorder.Result().Cookies())
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("MFA completion cookies=%#v", mfaRecorder.Result().Cookies())
	}

	sessionsRecorder := httptest.NewRecorder()
	sessionsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	sessionsRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(sessionsRecorder, sessionsRequest)
	if sessionsRecorder.Code != http.StatusOK || !strings.Contains(sessionsRecorder.Body.String(), `"current":true`) ||
		!strings.Contains(sessionsRecorder.Body.String(), `"authenticationLevel":"recovery"`) {
		t.Fatalf("sessions status=%d body=%s", sessionsRecorder.Code, sessionsRecorder.Body.String())
	}

	revokeRecorder := httptest.NewRecorder()
	revokeRequest := newJSONRequest(http.MethodPost, "/api/v1/sessions/revoke-all", `{"keepCurrent":false}`)
	revokeRequest.RemoteAddr = "192.0.2.50:443"
	revokeRequest.AddCookie(sessionCookie)
	revokeRequest.AddCookie(csrfCookie)
	revokeRequest.Header.Set(csrfHeaderName, csrfCookie.Value)
	handler.ServeHTTP(revokeRecorder, revokeRequest)
	if revokeRecorder.Code != http.StatusOK || !strings.Contains(revokeRecorder.Body.String(), `"currentRevoked":true`) {
		t.Fatalf("revoke all status=%d body=%s", revokeRecorder.Code, revokeRecorder.Body.String())
	}
	replayRecorder := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	replayRequest.AddCookie(sessionCookie)
	handler.ServeHTTP(replayRecorder, replayRequest)
	if replayRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("revoked replay status=%d body=%s", replayRecorder.Code, replayRecorder.Body.String())
	}
}

func authenticationCookies(cookies []*http.Cookie) (*http.Cookie, *http.Cookie) {
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case sessionCookieName:
			if cookie.MaxAge >= 0 {
				sessionCookie = cookie
			}
		case csrfCookieName:
			if cookie.MaxAge >= 0 {
				csrfCookie = cookie
			}
		}
	}
	return sessionCookie, csrfCookie
}

func testHOTP(secret []byte, counter uint64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	digest := hmac.New(sha1.New, secret)
	_, _ = digest.Write(message[:])
	sum := digest.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}

func newJSONRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func validLoginJSON() string {
	encoded, _ := json.Marshal(loginRequest{Email: "admin@example.com", Password: "correct horse battery staple"})
	return string(encoded)
}
