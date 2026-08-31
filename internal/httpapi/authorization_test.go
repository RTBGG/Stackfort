// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/store"
)

type authorizationServiceStub struct {
	result core.AuthorizedHostingAccount
	err    error
	params core.GetAuthorizedHostingAccountParams
	calls  int
}

func (stub *authorizationServiceStub) GetAuthorizedHostingAccount(
	_ context.Context,
	params core.GetAuthorizedHostingAccountParams,
) (core.AuthorizedHostingAccount, error) {
	stub.calls++
	stub.params = params
	return stub.result, stub.err
}

func TestAuthorizedAccountEndpointUsesServerDerivedSubject(t *testing.T) {
	t.Parallel()

	identityID := core.ID("0198b935-b600-7000-8000-000000000010")
	sessionID := core.ID("0198b935-b600-7000-8000-000000000011")
	accountID := core.ID("0198b935-b600-7000-8000-000000000012")
	assignmentID := core.ID("0198b935-b600-7000-8000-000000000013")
	role := core.MembershipOwner
	now := time.Date(2026, time.August, 16, 16, 0, 0, 0, time.UTC)
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: identityID, Status: core.IdentityActive},
		Session:  core.Session{ID: sessionID, IdentityID: identityID},
	}}
	authorization := &authorizationServiceStub{result: core.AuthorizedHostingAccount{
		Account: core.HostingAccount{
			ID: accountID, Name: "Example", Slug: "example", Status: core.AccountActive,
			CurrentPackageAssignmentID: assignmentID, CreatedAt: now, UpdatedAt: now,
		},
		Authorization: core.AuthorizationDecision{
			Action: core.AuthorizationAccountView, AccountID: &accountID, MembershipRole: &role,
		},
	}}
	handler := NewWithServices(nil, nil, Services{Authentication: authentication, Authorization: authorization})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+string(accountID), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_server-derived"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if authentication.authCalls != 1 || authentication.authParams.SessionToken != "sfs_server-derived" {
		t.Fatalf("authentication calls=%d params=%#v", authentication.authCalls, authentication.authParams)
	}
	if authorization.calls != 1 || authorization.params.AccountID != accountID ||
		authorization.params.Subject.IdentityID() != identityID || authorization.params.Subject.SessionID() != sessionID {
		t.Fatalf("authorization calls=%d params=%#v", authorization.calls, authorization.params)
	}
	var response hostingAccountResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != accountID || response.Authorization.MembershipRole == nil ||
		*response.Authorization.MembershipRole != core.MembershipOwner {
		t.Fatalf("response = %#v", response)
	}
}

func TestAuthorizedAccountEndpointHidesDeniedAndMissingResources(t *testing.T) {
	t.Parallel()

	identityID := core.ID("0198b935-b600-7000-8000-000000000020")
	sessionID := core.ID("0198b935-b600-7000-8000-000000000021")
	accountID := core.ID("0198b935-b600-7000-8000-000000000022")
	authentication := &authenticationServiceStub{authenticated: core.AuthenticatedSession{
		Identity: core.Identity{ID: identityID}, Session: core.Session{ID: sessionID, IdentityID: identityID},
	}}
	authorization := &authorizationServiceStub{err: core.ErrAuthorizationDenied}
	handler := NewWithServices(nil, nil, Services{Authentication: authentication, Authorization: authorization})

	denied := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+string(accountID), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_denied"})
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("denied status=%d body=%s", denied.Code, denied.Body.String())
	}

	invalid := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/not-an-id", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sfs_denied"})
	handler.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusNotFound || invalid.Body.String() != denied.Body.String() {
		t.Fatalf("invalid response=%d %q, denied=%d %q", invalid.Code, invalid.Body.String(), denied.Code, denied.Body.String())
	}
	if authorization.calls != 1 {
		t.Fatalf("authorization calls = %d, invalid identifiers must not reach the service", authorization.calls)
	}
}

func TestAuthorizedAccountEndpointRequiresAuthentication(t *testing.T) {
	t.Parallel()

	authentication := &authenticationServiceStub{}
	authorization := &authorizationServiceStub{}
	handler := NewWithServices(nil, nil, Services{Authentication: authentication, Authorization: authorization})
	accountID := "0198b935-b600-7000-8000-000000000030"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+accountID, nil))
	if recorder.Code != http.StatusUnauthorized || authentication.authCalls != 0 || authorization.calls != 0 {
		t.Fatalf("status=%d authentication=%d authorization=%d", recorder.Code, authentication.authCalls, authorization.calls)
	}
}

func TestAuthorizedAccountEndpointEnforcesTenantBoundaryEndToEnd(t *testing.T) {
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
	ownerOne := createHTTPAuthorizationIdentity(t, repository, "http-owner-one@example.test")
	ownerTwo := createHTTPAuthorizationIdentity(t, repository, "http-owner-two@example.test")
	administrator := createHTTPAuthorizationIdentity(t, repository, "http-admin@example.test")
	if err := repository.GrantPlatformRole(ctx, core.GrantPlatformRoleParams{
		IdentityID: administrator.ID, Role: core.PlatformAdministrator,
	}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	packageRecord, err := repository.CreatePackage(ctx, core.CreatePackageParams{
		Name: "HTTP authorization", Slug: "http-authorization", Limits: core.PackageLimits{MaxDomains: 10},
		ActorID: &administrator.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	accountOne, err := repository.CreateHostingAccount(ctx, core.CreateHostingAccountParams{
		Name: "HTTP One", Slug: "http-one", OwnerIdentityID: ownerOne.ID,
		PackageID: packageRecord.ID, ActorID: &administrator.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount one: %v", err)
	}
	accountTwo, err := repository.CreateHostingAccount(ctx, core.CreateHostingAccountParams{
		Name: "HTTP Two", Slug: "http-two", OwnerIdentityID: ownerTwo.ID,
		PackageID: packageRecord.ID, ActorID: &administrator.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount two: %v", err)
	}
	ownerToken := createHTTPAuthorizationSession(t, repository, ownerOne)
	adminToken := createHTTPAuthorizationSession(t, repository, administrator)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWithServices(logger, state, Services{Authentication: repository, Authorization: repository})

	own := performAccountRequest(handler, accountOne.ID, ownerToken)
	if own.Code != http.StatusOK {
		t.Fatalf("own account status=%d body=%s", own.Code, own.Body.String())
	}
	foreign := performAccountRequest(handler, accountTwo.ID, ownerToken)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign account status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	missingID, err := core.NewID()
	if err != nil {
		t.Fatalf("core.NewID: %v", err)
	}
	missing := performAccountRequest(handler, missingID, ownerToken)
	if missing.Code != http.StatusNotFound || missing.Body.String() != foreign.Body.String() {
		t.Fatalf("missing=%d %q foreign=%d %q", missing.Code, missing.Body.String(), foreign.Code, foreign.Body.String())
	}
	admin := performAccountRequest(handler, accountTwo.ID, adminToken)
	if admin.Code != http.StatusOK {
		t.Fatalf("administrator account status=%d body=%s", admin.Code, admin.Body.String())
	}
}

func TestResourceAuthorizationErrorRequiresRecentAuthentication(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeResourceAuthorizationError(recorder, slog.Default(), core.ErrRecentAuthenticationRequired)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Code != "recent_authentication_required" {
		t.Fatalf("body=%s error=%v", recorder.Body.String(), err)
	}
}

func createHTTPAuthorizationIdentity(t *testing.T, repository *core.Repository, email string) core.Identity {
	t.Helper()
	identity, err := repository.CreateIdentity(context.Background(), core.CreateIdentityParams{
		Email: email, DisplayName: "HTTP Authorization", Locale: core.LocaleEnglish,
	})
	if err != nil {
		t.Fatalf("CreateIdentity(%q): %v", email, err)
	}
	return identity
}

func createHTTPAuthorizationSession(t *testing.T, repository *core.Repository, identity core.Identity) string {
	t.Helper()
	token := "sfs_http_authorization_" + string(identity.ID)
	tokenHash := sha256.Sum256([]byte(token))
	csrfHash := sha256.Sum256([]byte("csrf:" + token))
	if _, err := repository.CreateSession(context.Background(), core.CreateSessionParams{
		IdentityID: identity.ID, TokenHash: tokenHash[:], CSRFSecretHash: csrfHash[:],
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession(%s): %v", identity.ID, err)
	}
	return token
}

func performAccountRequest(handler http.Handler, accountID core.ID, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+string(accountID), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	handler.ServeHTTP(recorder, request)
	return recorder
}
