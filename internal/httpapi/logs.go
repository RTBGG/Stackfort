// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/logworkspace"
)

type LogWorkspaceService interface {
	Read(context.Context, logworkspace.ReadParams) (agentprotocol.HostingLogReadResponse, error)
	ReadWAFEvents(context.Context, logworkspace.WAFReadParams) (agentprotocol.WAFEventReadResponse, error)
}

func registerLogRoutes(
	mux *http.ServeMux, logger *slog.Logger, authentication AuthenticationService, service LogWorkspaceService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/logs", func(w http.ResponseWriter, request *http.Request) {
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
		query := request.URL.Query()
		domainValues, kindValues, cursorValues := query["domainId"], query["kind"], query["cursor"]
		if len(domainValues) != 1 || len(kindValues) != 1 || len(cursorValues) > 1 ||
			len(query) != 2 && len(query) != 3 || len(query) == 3 && len(cursorValues) != 1 {
			writeAPIError(w, http.StatusBadRequest, "invalid_log_request", "The domain log request is invalid.")
			return
		}
		domainID, err := core.ParseID(domainValues[0])
		if err != nil {
			writeResourceNotFound(w)
			return
		}
		cursor := ""
		if len(cursorValues) == 1 {
			cursor = cursorValues[0]
		}
		response, err := service.Read(request.Context(), logworkspace.ReadParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
			Kind: agentprotocol.HostingLogKind(kindValues[0]), Cursor: cursor,
		})
		if err != nil {
			writeLogWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/waf-events", func(w http.ResponseWriter, request *http.Request) {
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
		query := request.URL.Query()
		domainValues, cursorValues := query["domainId"], query["cursor"]
		if len(domainValues) != 1 || len(cursorValues) > 1 ||
			(len(query) != 1 && len(query) != 2) || len(query) == 2 && len(cursorValues) != 1 {
			writeAPIError(w, http.StatusBadRequest, "invalid_waf_event_request", "The WAF event request is invalid.")
			return
		}
		domainID, err := core.ParseID(domainValues[0])
		if err != nil {
			writeResourceNotFound(w)
			return
		}
		cursor := ""
		if len(cursorValues) == 1 {
			cursor = cursorValues[0]
		}
		response, err := service.ReadWAFEvents(request.Context(), logworkspace.WAFReadParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID, Cursor: cursor,
		})
		if err != nil {
			writeLogWorkspaceError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func writeLogWorkspaceError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound), errors.Is(err, core.ErrInvalidInput):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, logworkspace.ErrNotReady):
		writeAPIError(w, http.StatusConflict, "log_workspace_not_ready", "The account log workspace is not ready.")
	case errors.Is(err, logworkspace.ErrConflict):
		writeAPIError(w, http.StatusConflict, "log_workspace_conflict", "The domain log conflicts with managed host state.")
	case errors.Is(err, logworkspace.ErrUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "log_workspace_unavailable", "Domain logs are temporarily unavailable.")
	default:
		logger.Error("read domain logs", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
