// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
)

type selfServiceStub struct {
	contextResult core.SelfServiceContext
	profile       core.UpdateOwnProfileParams
}

func (stub *selfServiceStub) GetSelfServiceContext(
	context.Context,
	core.GetSelfServiceContextParams,
) (core.SelfServiceContext, error) {
	return stub.contextResult, nil
}

func (stub *selfServiceStub) UpdateOwnProfile(
	_ context.Context,
	params core.UpdateOwnProfileParams,
) (core.Identity, error) {
	stub.profile = params
	return core.Identity{
		ID: params.Subject.IdentityID(), Email: params.Email,
		DisplayName: params.DisplayName, Locale: params.Locale,
	}, nil
}

func TestSelfServiceContextIsAuthenticatedAndOmitsInternalAccountState(t *testing.T) {
	t.Parallel()
	service := &selfServiceStub{contextResult: core.SelfServiceContext{
		PlatformAdministrator: true,
		Accounts: []core.SelfServiceAccount{{
			ID: "0198b935-b600-7000-8000-000000000301", Name: "Primary", Slug: "primary",
			Status: core.AccountActive, MembershipRole: core.MembershipOwner,
			PackageID: "0198b935-b600-7000-8000-000000000302", PackageName: "Starter",
			PackageRevision: 2, HostReady: true, EffectiveLimits: core.PackageLimits{
				MaxDomains: 10, AllowedPHPVersions: []string{},
			}, DomainCount: 3,
		}},
	}}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: &authenticationServiceStub{authenticated: authenticatedHostTestSession()},
		SelfService:    service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, `"platformAdministrator":true`) ||
		!strings.Contains(body, `"membershipRole":"owner"`) ||
		!strings.Contains(body, `"hostReady":true`) ||
		!strings.Contains(body, `"usage":{"domains":3}`) || strings.Contains(body, "UnixIdentity") {
		t.Fatalf("status/body = %d/%s", recorder.Code, body)
	}
}

func TestSelfServiceProfileUpdateRequiresCSRFAndUsesAuthenticatedSubject(t *testing.T) {
	t.Parallel()
	authenticated := authenticatedHostTestSession()
	authentication := &authenticationServiceStub{authenticated: authenticated}
	service := &selfServiceStub{}
	handler := NewWithServices(discardHostTestLogger(), nil, Services{
		Authentication: authentication, SelfService: service,
	})
	body := bytes.NewBufferString(`{"email":"owner@example.test","displayName":"Owner","locale":"de"}`)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile", body)
	request.RemoteAddr = "192.0.2.20:443"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, "csrf-bound")
	request.Header.Set("X-Request-ID", "profile-request")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-bound"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !authentication.authParams.RequireCSRF ||
		service.profile.Subject.IdentityID() != authenticated.Identity.ID ||
		service.profile.Email != "owner@example.test" || service.profile.Locale != core.LocaleGerman ||
		service.profile.SourceAddress != "192.0.2.20" || service.profile.RequestID != "profile-request" {
		t.Fatalf("status/auth/profile/body = %d/%#v/%#v/%s", recorder.Code, authentication.authParams, service.profile, recorder.Body.String())
	}
}
