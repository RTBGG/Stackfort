// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RTBGG/stackfort/internal/cacheworkspace"
	"github.com/RTBGG/stackfort/internal/core"
)

const maxCacheRequestBytes = 4 << 10

type CacheWorkspaceService interface {
	Inspect(context.Context, cacheworkspace.InspectParams) (cacheworkspace.Status, error)
	QueuePurge(context.Context, cacheworkspace.PurgeCommand) (core.Operation, error)
}

type cachePurgeRequest struct {
	PathPrefix string `json:"pathPrefix"`
}

func registerCacheRoutes(
	mux *http.ServeMux, logger *slog.Logger, authentication AuthenticationService,
	service CacheWorkspaceService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/domains/{domainID}/cache", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		domainID, ok := parseDomainRouteID(w, request.PathValue("domainID"))
		if !ok {
			return
		}
		status, err := service.Inspect(request.Context(), cacheworkspace.InspectParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
		})
		if err != nil {
			writeCacheError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/domains/{domainID}/cache/purge", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		domainID, ok := parseDomainRouteID(w, request.PathValue("domainID"))
		if !ok {
			return
		}
		var input cachePurgeRequest
		if !decodeBoundedJSON(w, request, &input, maxCacheRequestBytes) {
			return
		}
		requestID, ok := scheduledJobRequestID(w, request)
		if !ok {
			return
		}
		operation, err := service.QueuePurge(request.Context(), cacheworkspace.PurgeCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
			PathPrefix: input.PathPrefix, RequestID: requestID,
			IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeCacheError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, domainOperationResponse{
			OperationID: operation.ID, DomainID: domainID, Status: operation.Status,
		})
	})
}

func writeCacheError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The cache request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "cache_conflict", "The cache is disabled or the host state is not ready.")
	default:
		logger.Error("process cache request", "error", err)
		writeAPIError(w, http.StatusServiceUnavailable, "cache_unavailable", "Cache metrics are temporarily unavailable.")
	}
}
