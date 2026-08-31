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
)

type PHPWorkspaceService interface {
	Status(context.Context, phpworkspace.Params) (phpworkspace.Status, error)
}

type accountPHPResponse struct {
	RuntimeCapability      agentprotocol.Capability `json:"runtimeCapability"`
	HostApprovedVersions   []string                 `json:"hostApprovedVersions"`
	PackageAllowedVersions []string                 `json:"packageAllowedVersions"`
	AvailableVersions      []string                 `json:"availableVersions"`
	Pools                  []accountPHPPoolResponse `json:"pools"`
}

type accountPHPPoolResponse struct {
	Version           string                     `json:"version"`
	State             agentprotocol.PHPPoolState `json:"state"`
	ConfiguredDomains uint64                     `json:"configuredDomains"`
	MemoryBytes       *uint64                    `json:"memoryBytes,omitempty"`
	CPUTimeNanosec    *uint64                    `json:"cpuTimeNanoseconds,omitempty"`
	Processes         *uint64                    `json:"processes,omitempty"`
}

func registerPHPWorkspaceRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service PHPWorkspaceService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/php", func(w http.ResponseWriter, request *http.Request) {
		authenticated, err := authenticateRequestSession(request, authentication)
		if err != nil {
			writeSessionError(w, logger, err)
			return
		}
		accountID, err := core.ParseID(request.PathValue("accountID"))
		if err != nil {
			writeResourceNotFound(w)
			return
		}
		status, err := service.Status(request.Context(), phpworkspace.Params{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
		})
		if err != nil {
			writePHPWorkspaceError(w, logger, err)
			return
		}
		response := accountPHPResponse{
			RuntimeCapability:      status.RuntimeCapability,
			HostApprovedVersions:   append([]string(nil), status.HostApprovedVersions...),
			PackageAllowedVersions: append([]string(nil), status.PackageAllowedVersions...),
			AvailableVersions:      append([]string(nil), status.AvailableVersions...),
			Pools:                  make([]accountPHPPoolResponse, 0, len(status.Pools)),
		}
		for _, pool := range status.Pools {
			response.Pools = append(response.Pools, accountPHPPoolResponse{
				Version: pool.Version, State: pool.State, ConfiguredDomains: pool.ConfiguredDomains,
				MemoryBytes: pool.MemoryBytes, CPUTimeNanosec: pool.CPUTimeNanosec,
				Processes: pool.Processes,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func writePHPWorkspaceError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeResourceNotFound(w)
	case errors.Is(err, phpworkspace.ErrUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "php_status_unavailable", "PHP status is temporarily unavailable.")
	default:
		logger.Error("load account PHP status", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
