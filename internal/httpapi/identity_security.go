// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

// MultiFactorService is the authenticated factor-management surface.
type MultiFactorService interface {
	GetTOTPStatus(context.Context, core.AuthorizationSubject) (core.TOTPStatus, error)
	BeginTOTPEnrollment(context.Context, core.BeginTOTPEnrollmentParams) (core.TOTPEnrollment, error)
	ConfirmTOTPEnrollment(context.Context, core.ConfirmTOTPEnrollmentParams) (core.TOTPActivation, error)
	DisableTOTP(context.Context, core.DisableTOTPParams) error
}

// SessionManagementService exposes only self-service session review and
// revocation. It cannot address another identity.
type SessionManagementService interface {
	ListManagedSessions(context.Context, core.ListManagedSessionsParams) ([]core.ManagedSession, error)
	RevokeManagedSession(context.Context, core.RevokeManagedSessionParams) error
	RevokeAllManagedSessions(context.Context, core.RevokeAllManagedSessionsParams) (core.RevokeAllManagedSessionsResult, error)
}

type factorProofRequest struct {
	CurrentFactor string `json:"currentFactor"`
}

type confirmTOTPRequest struct {
	Code string `json:"code"`
}

type totpStatusResponse struct {
	Enabled                bool       `json:"enabled"`
	FactorID               *core.ID   `json:"factorId,omitempty"`
	ActivatedAt            *time.Time `json:"activatedAt,omitempty"`
	RecoveryCodesRemaining int64      `json:"recoveryCodesRemaining"`
}

type totpEnrollmentResponse struct {
	ChallengeID     core.ID   `json:"challengeId"`
	Secret          string    `json:"secret"`
	ProvisioningURI string    `json:"provisioningUri"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type totpActivationResponse struct {
	FactorID      core.ID   `json:"factorId"`
	ActivatedAt   time.Time `json:"activatedAt"`
	RecoveryCodes []string  `json:"recoveryCodes"`
}

type managedSessionResponse struct {
	ID                  core.ID                         `json:"id"`
	Current             bool                            `json:"current"`
	CreatedAt           time.Time                       `json:"createdAt"`
	AuthenticatedAt     time.Time                       `json:"authenticatedAt"`
	LastSeenAt          time.Time                       `json:"lastSeenAt"`
	ExpiresAt           time.Time                       `json:"expiresAt"`
	SourceAddress       string                          `json:"sourceAddress"`
	UserAgent           string                          `json:"userAgent"`
	AuthenticationLevel core.SessionAuthenticationLevel `json:"authenticationLevel"`
	MFAAuthenticatedAt  *time.Time                      `json:"mfaAuthenticatedAt,omitempty"`
}

type revokeAllSessionsRequest struct {
	KeepCurrent bool `json:"keepCurrent"`
}

type revokeAllSessionsResponse struct {
	Revoked        int64 `json:"revoked"`
	CurrentRevoked bool  `json:"currentRevoked"`
}

func registerMultiFactorRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service MultiFactorService,
) {
	mux.HandleFunc("GET /api/v1/mfa/totp", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		status, err := service.GetTOTPStatus(request.Context(), authenticated.AuthorizationSubject())
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, totpStatusResponse{
			Enabled: status.Enabled, FactorID: status.FactorID, ActivatedAt: status.ActivatedAt,
			RecoveryCodesRemaining: status.RecoveryCodesRemaining,
		})
	})

	mux.HandleFunc("POST /api/v1/mfa/totp/setup", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input factorProofRequest
		if !decodeIdentitySecurityJSON(w, request, &input) {
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		enrollment, err := service.BeginTOTPEnrollment(request.Context(), core.BeginTOTPEnrollmentParams{
			Subject: authenticated.AuthorizationSubject(), CurrentFactor: input.CurrentFactor,
			RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, totpEnrollmentResponse{
			ChallengeID: enrollment.ChallengeID, Secret: enrollment.Secret,
			ProvisioningURI: enrollment.ProvisioningURI, ExpiresAt: enrollment.ExpiresAt,
		})
	})

	mux.HandleFunc("POST /api/v1/mfa/totp/setup/{challengeID}/confirm", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		challengeID, err := core.ParseID(request.PathValue("challengeID"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The setup challenge is invalid.")
			return
		}
		var input confirmTOTPRequest
		if !decodeIdentitySecurityJSON(w, request, &input) {
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		activation, err := service.ConfirmTOTPEnrollment(request.Context(), core.ConfirmTOTPEnrollmentParams{
			Subject: authenticated.AuthorizationSubject(), ChallengeID: challengeID, Code: input.Code,
			RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		clearAuthenticationCookies(w)
		clearMFAChallengeCookie(w)
		writeJSON(w, http.StatusOK, totpActivationResponse{
			FactorID: activation.FactorID, ActivatedAt: activation.ActivatedAt,
			RecoveryCodes: activation.RecoveryCodes,
		})
	})

	mux.HandleFunc("DELETE /api/v1/mfa/totp", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input factorProofRequest
		if !decodeIdentitySecurityJSON(w, request, &input) {
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		err := service.DisableTOTP(request.Context(), core.DisableTOTPParams{
			Subject: authenticated.AuthorizationSubject(), CurrentFactor: input.CurrentFactor,
			RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		clearAuthenticationCookies(w)
		clearMFAChallengeCookie(w)
		w.WriteHeader(http.StatusNoContent)
	})
}

func registerSessionManagementRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service SessionManagementService,
) {
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		sessions, err := service.ListManagedSessions(request.Context(), core.ListManagedSessionsParams{
			Subject: authenticated.AuthorizationSubject(),
		})
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		response := make([]managedSessionResponse, len(sessions))
		for index, session := range sessions {
			response[index] = newManagedSessionResponse(session)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": response})
	})

	mux.HandleFunc("DELETE /api/v1/sessions/{sessionID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		targetSessionID, err := core.ParseID(request.PathValue("sessionID"))
		if err != nil {
			writeAPIError(w, http.StatusForbidden, "forbidden", "The session cannot be changed.")
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		err = service.RevokeManagedSession(request.Context(), core.RevokeManagedSessionParams{
			Subject: authenticated.AuthorizationSubject(), TargetSessionID: targetSessionID,
			RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		if targetSessionID == authenticated.Session.ID {
			clearAuthenticationCookies(w)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /api/v1/sessions/revoke-all", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input revokeAllSessionsRequest
		if !decodeIdentitySecurityJSON(w, request, &input) {
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		result, err := service.RevokeAllManagedSessions(request.Context(), core.RevokeAllManagedSessionsParams{
			Subject: authenticated.AuthorizationSubject(), KeepCurrent: input.KeepCurrent,
			RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeIdentitySecurityError(w, logger, err)
			return
		}
		if result.CurrentRevoked {
			clearAuthenticationCookies(w)
		}
		writeJSON(w, http.StatusOK, revokeAllSessionsResponse{
			Revoked: result.Revoked, CurrentRevoked: result.CurrentRevoked,
		})
	})
}

func authenticateBrowserSession(
	w http.ResponseWriter,
	request *http.Request,
	logger *slog.Logger,
	service AuthenticationService,
	requireCSRF bool,
) (core.AuthenticatedSession, bool) {
	sessionCookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return core.AuthenticatedSession{}, false
	}
	csrfCookieValue := ""
	if csrfCookie, cookieErr := request.Cookie(csrfCookieName); cookieErr == nil {
		csrfCookieValue = csrfCookie.Value
	}
	authenticated, err := service.AuthenticateSession(request.Context(), core.AuthenticateSessionParams{
		SessionToken: sessionCookie.Value, RequireCSRF: requireCSRF,
		CSRFHeaderToken: request.Header.Get(csrfHeaderName), CSRFCookieToken: csrfCookieValue,
	})
	if err != nil {
		if errors.Is(err, core.ErrSessionInvalid) {
			clearAuthenticationCookies(w)
		}
		writeSessionError(w, logger, err)
		return core.AuthenticatedSession{}, false
	}
	return authenticated, true
}

func decodeIdentitySecurityJSON(w http.ResponseWriter, request *http.Request, target any) bool {
	if !requestHasJSONContentType(request) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxLoginRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return false
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return false
	}
	return true
}

func requestSourceAddress(w http.ResponseWriter, request *http.Request, logger *slog.Logger) (string, bool) {
	sourceAddress, err := remoteSourceAddress(request.RemoteAddr)
	if err != nil {
		logger.Error("resolve identity security request source", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return "", false
	}
	return sourceAddress, true
}

func newManagedSessionResponse(session core.ManagedSession) managedSessionResponse {
	return managedSessionResponse{
		ID: session.ID, Current: session.Current, CreatedAt: session.CreatedAt,
		AuthenticatedAt: session.AuthenticatedAt, LastSeenAt: session.LastSeenAt,
		ExpiresAt: session.ExpiresAt, SourceAddress: session.SourceAddress,
		UserAgent: session.UserAgent, AuthenticationLevel: session.AuthenticationLevel,
		MFAAuthenticatedAt: session.MFAAuthenticatedAt,
	}
}

func writeIdentitySecurityError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var rateLimit *core.AuthenticationRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		retrySeconds := max(int64(1), int64((rateLimit.RetryAfter+time.Second-1)/time.Second))
		w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds, 10))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
	case errors.Is(err, core.ErrSessionInvalid):
		clearAuthenticationCookies(w)
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrCSRFInvalid):
		writeAPIError(w, http.StatusForbidden, "csrf_failed", "The request could not be verified.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Recent authentication is required.")
	case errors.Is(err, core.ErrAuthorizationDenied):
		writeAPIError(w, http.StatusForbidden, "forbidden", "The requested change is not allowed.")
	case errors.Is(err, core.ErrMFAChallengeInvalid):
		writeAPIError(w, http.StatusUnauthorized, "verification_failed", "The factor or verification code is invalid.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request is invalid.")
	case errors.Is(err, core.ErrSecretStorageUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "secret_storage_unavailable", "Secure secret storage is unavailable.")
	default:
		logger.Error("identity security operation", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
