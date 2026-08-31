// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

const (
	sessionCookieName    = "__Host-sf-id"
	csrfCookieName       = "__Host-sf-csrf"
	mfaCookieName        = "__Host-sf-mfa"
	csrfHeaderName       = "X-CSRF-Token"
	maxLoginRequestBytes = 4 << 10
)

// AuthenticationService is the password/session surface used by the browser API.
type AuthenticationService interface {
	PasswordLogin(context.Context, core.PasswordLoginParams) (core.PasswordLoginResult, error)
	CompleteMFALogin(context.Context, core.CompleteMFALoginParams) (core.PasswordLoginResult, error)
	AuthenticateSession(context.Context, core.AuthenticateSessionParams) (core.AuthenticatedSession, error)
	RevokeSession(context.Context, core.RevokeSessionParams) error
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type mfaLoginRequest struct {
	Code string `json:"code"`
}

type mfaRequiredResponse struct {
	MFARequired bool      `json:"mfaRequired"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type identityResponse struct {
	ID          core.ID     `json:"id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"displayName"`
	Locale      core.Locale `json:"locale"`
}

type sessionResponse struct {
	Identity            identityResponse                `json:"identity"`
	SessionID           core.ID                         `json:"sessionId"`
	AuthenticatedAt     time.Time                       `json:"authenticatedAt"`
	LastSeenAt          time.Time                       `json:"lastSeenAt"`
	ExpiresAt           time.Time                       `json:"expiresAt"`
	AuthenticationLevel core.SessionAuthenticationLevel `json:"authenticationLevel"`
	MFAAuthenticatedAt  *time.Time                      `json:"mfaAuthenticatedAt,omitempty"`
}

func registerAuthenticationRoutes(mux *http.ServeMux, logger *slog.Logger, service AuthenticationService) {
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, request *http.Request) {
		if !requestHasJSONContentType(request) {
			writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return
		}
		var input loginRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxLoginRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		if err := ensureJSONEnd(decoder); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		sourceAddress, err := remoteSourceAddress(request.RemoteAddr)
		if err != nil {
			logger.Error("resolve login request source", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		previousSessionToken := ""
		if cookie, err := request.Cookie(sessionCookieName); err == nil {
			previousSessionToken = cookie.Value
		}
		result, err := service.PasswordLogin(request.Context(), core.PasswordLoginParams{
			Email: input.Email, Password: input.Password,
			SourceAddress: sourceAddress, UserAgent: request.UserAgent(),
			RequestID:            request.Header.Get("X-Request-ID"),
			PreviousSessionToken: previousSessionToken,
		})
		if err != nil {
			writeLoginError(w, logger, err)
			return
		}
		if result.MFARequired {
			setMFAChallengeCookie(w, result.MFAChallengeToken, result.MFAChallengeExpiresAt)
			writeJSON(w, http.StatusAccepted, mfaRequiredResponse{
				MFARequired: true, ExpiresAt: result.MFAChallengeExpiresAt,
			})
			return
		}
		clearMFAChallengeCookie(w)
		setAuthenticationCookies(w, result)
		writeJSON(w, http.StatusOK, newSessionResponse(result.Identity, result.Session))
	})

	mux.HandleFunc("POST /api/v1/login/mfa", func(w http.ResponseWriter, request *http.Request) {
		if !requestHasJSONContentType(request) {
			writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return
		}
		challengeCookie, err := request.Cookie(mfaCookieName)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "mfa_challenge_required", "A valid MFA login challenge is required.")
			return
		}
		var input mfaLoginRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxLoginRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		if err := ensureJSONEnd(decoder); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		result, err := service.CompleteMFALogin(request.Context(), core.CompleteMFALoginParams{
			ChallengeToken: challengeCookie.Value, Code: input.Code,
			RequestID: request.Header.Get("X-Request-ID"),
		})
		if err != nil {
			writeMFAError(w, logger, err)
			return
		}
		clearMFAChallengeCookie(w)
		setAuthenticationCookies(w, result)
		writeJSON(w, http.StatusOK, newSessionResponse(result.Identity, result.Session))
	})

	mux.HandleFunc("GET /api/v1/session", func(w http.ResponseWriter, request *http.Request) {
		sessionCookie, err := request.Cookie(sessionCookieName)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			return
		}
		authenticated, err := service.AuthenticateSession(request.Context(), core.AuthenticateSessionParams{
			SessionToken: sessionCookie.Value,
		})
		if err != nil {
			writeSessionError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newSessionResponse(authenticated.Identity, authenticated.Session))
	})

	mux.HandleFunc("POST /api/v1/logout", func(w http.ResponseWriter, request *http.Request) {
		sessionCookie, err := request.Cookie(sessionCookieName)
		if err != nil {
			clearAuthenticationCookies(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		csrfCookie, csrfCookieErr := request.Cookie(csrfCookieName)
		csrfCookieValue := ""
		if csrfCookieErr == nil {
			csrfCookieValue = csrfCookie.Value
		}
		authenticated, err := service.AuthenticateSession(request.Context(), core.AuthenticateSessionParams{
			SessionToken: sessionCookie.Value, RequireCSRF: true,
			CSRFHeaderToken: request.Header.Get(csrfHeaderName),
			CSRFCookieToken: csrfCookieValue,
		})
		if errors.Is(err, core.ErrSessionInvalid) {
			clearAuthenticationCookies(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			writeSessionError(w, logger, err)
			return
		}
		sourceAddress, err := remoteSourceAddress(request.RemoteAddr)
		if err != nil {
			logger.Error("resolve logout request source", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		err = service.RevokeSession(request.Context(), core.RevokeSessionParams{
			IdentityID: authenticated.Identity.ID, SessionID: authenticated.Session.ID,
			Reason: "logout", RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil && !errors.Is(err, core.ErrSessionInvalid) {
			logger.Error("revoke logout session", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		clearAuthenticationCookies(w)
		w.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
		w.WriteHeader(http.StatusNoContent)
	})
}

func requestHasJSONContentType(request *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func setAuthenticationCookies(w http.ResponseWriter, result core.PasswordLoginResult) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: result.SessionToken,
		Path:   "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	// #nosec G124 -- the CSRF synchronizer cookie must be readable by same-origin JavaScript; the session bearer remains HttpOnly.
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: result.CSRFToken,
		Path:   "/",
		Secure: true, HttpOnly: false, SameSite: http.SameSiteStrictMode,
	})
}

func clearAuthenticationCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	// #nosec G124 -- deletion must repeat the readable CSRF cookie's exact security attributes.
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		Secure: true, HttpOnly: false, SameSite: http.SameSiteStrictMode,
	})
}

func setMFAChallengeCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: mfaCookieName, Value: value, Path: "/", Expires: expiresAt,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func clearMFAChallengeCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: mfaCookieName, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

func newSessionResponse(identity core.Identity, session core.Session) sessionResponse {
	return sessionResponse{
		Identity: identityResponse{
			ID: identity.ID, Email: identity.Email,
			DisplayName: identity.DisplayName, Locale: identity.Locale,
		},
		SessionID: session.ID, AuthenticatedAt: session.AuthenticatedAt,
		LastSeenAt: session.LastSeenAt, ExpiresAt: session.ExpiresAt,
		AuthenticationLevel: session.AuthenticationLevel,
		MFAAuthenticatedAt:  session.MFAAuthenticatedAt,
	}
}

func writeMFAError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var rateLimit *core.AuthenticationRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		retrySeconds := max(int64(1), int64((rateLimit.RetryAfter+time.Second-1)/time.Second))
		w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds, 10))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
	case errors.Is(err, core.ErrMFAChallengeInvalid):
		writeAPIError(w, http.StatusUnauthorized, "mfa_failed", "The verification code or challenge is invalid.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The MFA request is invalid.")
	default:
		logger.Error("complete MFA login", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeLoginError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var rateLimit *core.AuthenticationRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		retrySeconds := max(int64(1), int64((rateLimit.RetryAfter+time.Second-1)/time.Second))
		w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds, 10))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
	case errors.Is(err, core.ErrAuthenticationDenied):
		writeAPIError(w, http.StatusUnauthorized, "authentication_failed", "The email address or password is incorrect.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The login request is invalid.")
	default:
		logger.Error("password login", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeSessionError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrCSRFInvalid):
		writeAPIError(w, http.StatusForbidden, "csrf_failed", "The request could not be verified.")
	default:
		logger.Error("authenticate session", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
