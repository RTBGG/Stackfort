// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/jobworkspace"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/google/uuid"
)

const maxScheduledJobRequestBytes = 8 << 10

type ScheduledJobWorkspaceService interface {
	List(context.Context, jobworkspace.ListParams) ([]core.ScheduledJob, error)
	Create(context.Context, jobworkspace.CreateCommand) (core.ScheduledJobMutation, error)
	Update(context.Context, jobworkspace.UpdateCommand) (core.ScheduledJobMutation, error)
	Delete(context.Context, jobworkspace.DeleteCommand) (core.ScheduledJobMutation, error)
	OperationStatus(context.Context, jobworkspace.OperationStatusParams) (core.Operation, error)
}

type scheduledJobRequest struct {
	ExpectedRevision int64                  `json:"expectedRevision,omitempty"`
	Name             string                 `json:"name"`
	Runtime          scheduledjobs.Runtime  `json:"runtime"`
	ScriptPath       string                 `json:"scriptPath"`
	PHPVersion       string                 `json:"phpVersion,omitempty"`
	Schedule         scheduledjobs.Schedule `json:"schedule"`
	Enabled          bool                   `json:"enabled"`
}

type scheduledJobDeleteRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

type scheduledJobMutationResponse struct {
	OperationID core.ID              `json:"operationId"`
	Status      core.OperationStatus `json:"status"`
	Job         core.ScheduledJob    `json:"job"`
}

func registerScheduledJobRoutes(
	mux *http.ServeMux, logger *slog.Logger, authentication AuthenticationService,
	service ScheduledJobWorkspaceService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/jobs", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		jobs, err := service.List(request.Context(), jobworkspace.ListParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
		})
		if err != nil {
			writeScheduledJobError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/jobs", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input scheduledJobRequest
		if !decodeBoundedJSON(w, request, &input, maxScheduledJobRequestBytes) {
			return
		}
		if input.ExpectedRevision != 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The scheduled job request is invalid.")
			return
		}
		requestID, ok := scheduledJobRequestID(w, request)
		if !ok {
			return
		}
		mutation, err := service.Create(request.Context(), jobworkspace.CreateCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			Name: input.Name, Runtime: input.Runtime, ScriptPath: input.ScriptPath,
			PHPVersion: input.PHPVersion, Schedule: input.Schedule, Enabled: input.Enabled,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		writeScheduledJobMutation(w, logger, mutation, err)
	})

	mux.HandleFunc("PUT /api/v1/accounts/{accountID}/jobs/{jobID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		jobID, ok := parseDomainRouteID(w, request.PathValue("jobID"))
		if !ok {
			return
		}
		var input scheduledJobRequest
		if !decodeBoundedJSON(w, request, &input, maxScheduledJobRequestBytes) {
			return
		}
		requestID, ok := scheduledJobRequestID(w, request)
		if !ok {
			return
		}
		mutation, err := service.Update(request.Context(), jobworkspace.UpdateCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, JobID: jobID,
			ExpectedRevision: input.ExpectedRevision, Name: input.Name, Runtime: input.Runtime,
			ScriptPath: input.ScriptPath, PHPVersion: input.PHPVersion,
			Schedule: input.Schedule, Enabled: input.Enabled,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		writeScheduledJobMutation(w, logger, mutation, err)
	})

	mux.HandleFunc("DELETE /api/v1/accounts/{accountID}/jobs/{jobID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		jobID, ok := parseDomainRouteID(w, request.PathValue("jobID"))
		if !ok {
			return
		}
		var input scheduledJobDeleteRequest
		if !decodeBoundedJSON(w, request, &input, maxScheduledJobRequestBytes) {
			return
		}
		requestID, ok := scheduledJobRequestID(w, request)
		if !ok {
			return
		}
		mutation, err := service.Delete(request.Context(), jobworkspace.DeleteCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, JobID: jobID,
			ExpectedRevision: input.ExpectedRevision, RequestID: requestID,
			IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		writeScheduledJobMutation(w, logger, mutation, err)
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/job-operations/{operationID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		operationID, ok := parseDomainRouteID(w, request.PathValue("operationID"))
		if !ok {
			return
		}
		operation, err := service.OperationStatus(request.Context(), jobworkspace.OperationStatusParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, OperationID: operationID,
		})
		if err != nil {
			writeScheduledJobError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newAccountOperationResponse(operation))
	})
}

func scheduledJobRequestID(w http.ResponseWriter, request *http.Request) (string, bool) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID != "" {
		return requestID, true
	}
	generated, err := uuid.NewV7()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
		return "", false
	}
	return generated.String(), true
}

func writeScheduledJobMutation(
	w http.ResponseWriter, logger *slog.Logger, mutation core.ScheduledJobMutation, err error,
) {
	if err != nil {
		writeScheduledJobError(w, logger, err)
		return
	}
	writeJSON(w, http.StatusAccepted, scheduledJobMutationResponse{
		OperationID: mutation.Operation.ID, Status: mutation.Operation.Status, Job: mutation.Job,
	})
}

func writeScheduledJobError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The scheduled job request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The scheduled job conflicts with current state or package limits.")
	default:
		logger.Error("process scheduled job request", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
