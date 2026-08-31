// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/phpworkspace"
	"github.com/google/uuid"
)

type PlatformAuthorizationService interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
}

type HostCapabilityService interface {
	InspectCapabilities(context.Context, string) (agentprotocol.CapabilityReport, error)
}

type hostCapabilityResponse struct {
	agentprotocol.CapabilityReport
	ManagedPHPVersions []string `json:"managedPhpVersions"`
}

func registerHostCapabilityRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	authorization PlatformAuthorizationService,
	capabilities HostCapabilityService,
) {
	mux.HandleFunc("GET /api/v1/admin/host/capabilities", func(w http.ResponseWriter, request *http.Request) {
		authenticated, err := authenticateRequestSession(request, authentication)
		if err != nil {
			writeSessionError(w, logger, err)
			return
		}
		_, err = authorization.Authorize(request.Context(), core.AuthorizeParams{
			Subject: authenticated.AuthorizationSubject(), Action: core.AuthorizationPlatformView,
		})
		if err != nil {
			writePlatformAuthorizationError(w, logger, err)
			return
		}
		requestID, err := uuid.NewV7()
		if err != nil {
			logger.Error("create host capability request ID")
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		report, err := capabilities.InspectCapabilities(
			request.Context(), "host-capabilities-"+requestID.String(),
		)
		if err != nil {
			logger.Warn("host capability inspection unavailable")
			writeAPIError(w, http.StatusServiceUnavailable, "host_agent_unavailable", "Host capability inspection is unavailable.")
			return
		}
		versions, _ := phpworkspace.HostRuntime(report)
		writeJSON(w, http.StatusOK, hostCapabilityResponse{
			CapabilityReport: report, ManagedPHPVersions: versions,
		})
	})
}

func writePlatformAuthorizationError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied):
		writeAPIError(w, http.StatusForbidden, "permission_denied", "Platform administrator access is required.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Please authenticate again before continuing.")
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	default:
		logger.Error("authorize platform request", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
