// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
)

type PanelHostnameService interface {
	Status(context.Context, core.AuthorizationSubject) (agentprotocol.PanelStatusResponse, error)
	Queue(context.Context, core.AuthorizationSubject, agentprotocol.PanelIssueRequest, string, string) (core.Operation, error)
}

func registerPanelRoutes(mux *http.ServeMux, logger *slog.Logger, authentication AuthenticationService, service PanelHostnameService) {
	mux.HandleFunc("GET /api/v1/admin/panel", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticateBrowserSession(w, r, logger, authentication, false)
		if !ok {
			return
		}
		status, err := service.Status(r.Context(), session.AuthorizationSubject())
		if err != nil {
			writeACMEAccountError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /api/v1/admin/panel/issue", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticateBrowserSession(w, r, logger, authentication, true)
		if !ok {
			return
		}
		if !requestHasJSONContentType(r) {
			writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return
		}
		var input agentprotocol.PanelIssueRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || ensureJSONEnd(decoder) != nil || agentprotocol.ValidatePanelIssue(input) != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The panel hostname request is invalid.")
			return
		}
		id, err := uuid.NewV7()
		if err != nil {
			writeACMEAccountError(w, logger, err)
			return
		}
		operation, err := service.Queue(r.Context(), session.AuthorizationSubject(), input, id.String(), r.Header.Get("Idempotency-Key"))
		if err != nil {
			writeACMEAccountError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, acmeAccountOperationResponse{OperationID: operation.ID, Status: operation.Status})
	})
}
