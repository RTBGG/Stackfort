// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/updatecheck"
)

const maxUpdatePolicyRequestBytes = 2 << 10

type UpdateCheckService interface {
	Status(context.Context) (updatecheck.Status, error)
	UpdatePolicy(context.Context, core.UpdatePolicyParams) (updatecheck.Status, error)
	CheckNow(context.Context) (updatecheck.Status, error)
}

type updatePolicyRequest struct {
	Channel         core.UpdateChannel `json:"channel"`
	AutomaticChecks bool               `json:"automaticChecks"`
}

func registerUpdateRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	authorization PlatformAuthorizationService,
	service UpdateCheckService,
) {
	mux.HandleFunc("GET /api/v1/admin/updates", func(w http.ResponseWriter, request *http.Request) {
		_, ok := authorizeUpdateRequest(w, request, logger, authentication, authorization, false, core.AuthorizationPlatformView)
		if !ok {
			return
		}
		status, err := service.Status(request.Context())
		if err != nil {
			writeUpdateError(w, logger, "read update status", err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("PATCH /api/v1/admin/updates/policy", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authorizeUpdateRequest(
			w, request, logger, authentication, authorization, true, core.AuthorizationPlatformManage,
		)
		if !ok {
			return
		}
		var input updatePolicyRequest
		if !decodeBoundedJSON(w, request, &input, maxUpdatePolicyRequestBytes) {
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		status, err := service.UpdatePolicy(request.Context(), core.UpdatePolicyParams{
			Subject: authenticated.AuthorizationSubject(), Channel: input.Channel,
			AutomaticChecks: input.AutomaticChecks,
			RequestID:       request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeUpdateError(w, logger, "update release discovery policy", err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("POST /api/v1/admin/updates/check", func(w http.ResponseWriter, request *http.Request) {
		_, ok := authorizeUpdateRequest(w, request, logger, authentication, authorization, true, core.AuthorizationPlatformView)
		if !ok {
			return
		}
		status, err := service.CheckNow(request.Context())
		if err != nil {
			writeUpdateError(w, logger, "check GitHub releases", err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func authorizeUpdateRequest(
	w http.ResponseWriter,
	request *http.Request,
	logger *slog.Logger,
	authentication AuthenticationService,
	authorization PlatformAuthorizationService,
	requireCSRF bool,
	action core.AuthorizationAction,
) (core.AuthenticatedSession, bool) {
	authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, requireCSRF)
	if !ok {
		return core.AuthenticatedSession{}, false
	}
	if _, err := authorization.Authorize(request.Context(), core.AuthorizeParams{
		Subject: authenticated.AuthorizationSubject(), Action: action,
	}); err != nil {
		writePlatformAuthorizationError(w, logger, err)
		return core.AuthenticatedSession{}, false
	}
	return authenticated, true
}

func writeUpdateError(w http.ResponseWriter, logger *slog.Logger, action string, err error) {
	switch {
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The update policy is invalid.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Please authenticate again before continuing.")
	case errors.Is(err, core.ErrAuthorizationDenied):
		writeAPIError(w, http.StatusForbidden, "permission_denied", "Platform administrator access is required.")
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, updatecheck.ErrRateLimited):
		writeAPIError(w, http.StatusTooManyRequests, "update_check_rate_limited", "Release discovery is temporarily rate limited.")
	case errors.Is(err, updatecheck.ErrInvalidResponse):
		logger.Warn(action, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, "invalid_release_response", "Release discovery returned an invalid response.")
	case errors.Is(err, updatecheck.ErrDiscoveryUnavailable):
		logger.Warn(action, "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, "release_discovery_unavailable", "Release discovery is temporarily unavailable.")
	default:
		logger.Error(action, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
