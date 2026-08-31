// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/databaseworkspace"
	"github.com/google/uuid"
)

const maxDatabaseRequestBytes = 8 << 10

type DatabaseWorkspaceService interface {
	PrepareWizard(context.Context, databaseworkspace.WizardCommand) (core.ManagedDatabaseProvisioning, error)
	List(context.Context, databaseworkspace.ListParams) (core.DatabaseWorkspace, error)
	OperationStatus(context.Context, databaseworkspace.OperationStatusParams) (core.Operation, error)
	RevealCredential(context.Context, databaseworkspace.RevealCredentialParams) (core.RevealedDatabaseCredential, error)
	IssuePHPMyAdminHandoff(context.Context, databaseworkspace.IssuePHPMyAdminHandoffParams) (core.PHPMyAdminHandoff, error)
	RotateCredential(context.Context, databaseworkspace.RotateCredentialCommand) (core.ManagedDatabaseCredentialRotation, error)
	Delete(context.Context, databaseworkspace.DeleteCommand) (core.ManagedDatabaseDeletion, error)
}

type databaseWizardRequest struct {
	DatabaseAlias  string                   `json:"databaseAlias"`
	ExistingUserID *core.ID                 `json:"existingUserId,omitempty"`
	NewUserAlias   string                   `json:"newUserAlias,omitempty"`
	Preset         core.DatabaseGrantPreset `json:"preset"`
}

type databaseWizardResponse struct {
	OperationID    core.ID              `json:"operationId"`
	DatabaseID     core.ID              `json:"databaseId"`
	DatabaseUserID core.ID              `json:"databaseUserId"`
	GrantID        core.ID              `json:"grantId"`
	Status         core.OperationStatus `json:"status"`
}

type databaseWorkspaceResponse struct {
	Databases []managedDatabaseResponse      `json:"databases"`
	Users     []managedDatabaseUserResponse  `json:"users"`
	Grants    []managedDatabaseGrantResponse `json:"grants"`
}

type managedDatabaseResponse struct {
	ID        core.ID                    `json:"id"`
	Alias     string                     `json:"alias"`
	Status    core.ManagedDatabaseStatus `json:"status"`
	CreatedAt time.Time                  `json:"createdAt"`
	UpdatedAt time.Time                  `json:"updatedAt"`
}

type managedDatabaseUserResponse struct {
	ID        core.ID                    `json:"id"`
	Alias     string                     `json:"alias"`
	Host      string                     `json:"host"`
	Status    core.ManagedDatabaseStatus `json:"status"`
	Revealed  bool                       `json:"revealed"`
	CreatedAt time.Time                  `json:"createdAt"`
	UpdatedAt time.Time                  `json:"updatedAt"`
}

type managedDatabaseGrantResponse struct {
	ID             core.ID                  `json:"id"`
	DatabaseID     core.ID                  `json:"databaseId"`
	DatabaseUserID core.ID                  `json:"databaseUserId"`
	Preset         core.DatabaseGrantPreset `json:"preset"`
	Status         string                   `json:"status"`
}

type databaseCredentialResponse struct {
	Username string `json:"username"`
	Host     string `json:"host"`
	Password string `json:"password"`
}

type phpMyAdminHandoffResponse struct {
	HandoffToken string    `json:"handoffToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	LaunchPath   string    `json:"launchPath"`
}

type databaseDeletionRequest struct {
	Confirmation string `json:"confirmation"`
}

func registerDatabaseRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service DatabaseWorkspaceService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}/databases", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		workspace, err := service.List(request.Context(), databaseworkspace.ListParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newDatabaseWorkspaceResponse(workspace))
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/databases/wizard", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input databaseWizardRequest
		if !decodeBoundedJSON(w, request, &input, maxDatabaseRequestBytes) {
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeDatabaseError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		prepared, err := service.PrepareWizard(request.Context(), databaseworkspace.WizardCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			DatabaseAlias: input.DatabaseAlias, ExistingUserID: input.ExistingUserID,
			NewUserAlias: input.NewUserAlias, Preset: input.Preset,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, databaseWizardResponse{
			OperationID: prepared.Operation.ID, DatabaseID: prepared.Database.ID,
			DatabaseUserID: prepared.DatabaseUser.ID, GrantID: prepared.Grant.ID,
			Status: prepared.Operation.Status,
		})
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/database-operations/{operationID}", func(w http.ResponseWriter, request *http.Request) {
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
		operation, err := service.OperationStatus(request.Context(), databaseworkspace.OperationStatusParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, OperationID: operationID,
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newAccountOperationResponse(operation))
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/database-users/{userID}/credential/reveal", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		userID, ok := parseDomainRouteID(w, request.PathValue("userID"))
		if !ok {
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeDatabaseError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		credential, err := service.RevealCredential(request.Context(), databaseworkspace.RevealCredentialParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			UserID: userID, RequestID: requestID,
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		defer clear(credential.Password)
		writeJSON(w, http.StatusOK, databaseCredentialResponse{
			Username: credential.Username, Host: credential.Host, Password: string(credential.Password),
		})
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/database-users/{userID}/phpmyadmin-handoffs", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		userID, ok := parseDomainRouteID(w, request.PathValue("userID"))
		if !ok {
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeDatabaseError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		handoff, err := service.IssuePHPMyAdminHandoff(request.Context(), databaseworkspace.IssuePHPMyAdminHandoffParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			UserID: userID, RequestID: requestID,
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, phpMyAdminHandoffResponse{
			HandoffToken: handoff.Token, ExpiresAt: handoff.ExpiresAt,
			LaunchPath: "/phpmyadmin/stackfort-launch.php",
		})
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/database-users/{userID}/credential/rotate", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		userID, ok := parseDomainRouteID(w, request.PathValue("userID"))
		if !ok {
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeDatabaseError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		rotation, err := service.RotateCredential(request.Context(), databaseworkspace.RotateCredentialCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, UserID: userID,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"operationId": rotation.Operation.ID, "status": rotation.Operation.Status,
		})
	})

	registerDatabaseDeletionRoute(mux, logger, authentication, service,
		"DELETE /api/v1/accounts/{accountID}/databases/{targetID}", core.DatabaseDeletionDatabase)
	registerDatabaseDeletionRoute(mux, logger, authentication, service,
		"DELETE /api/v1/accounts/{accountID}/database-users/{targetID}", core.DatabaseDeletionUser)
}

func registerDatabaseDeletionRoute(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service DatabaseWorkspaceService,
	pattern string,
	targetKind core.DatabaseDeletionKind,
) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		targetID, ok := parseDomainRouteID(w, request.PathValue("targetID"))
		if !ok {
			return
		}
		var input databaseDeletionRequest
		if !decodeBoundedJSON(w, request, &input, maxDatabaseRequestBytes) {
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeDatabaseError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		deletion, err := service.Delete(request.Context(), databaseworkspace.DeleteCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			TargetKind: targetKind, TargetID: targetID, Confirmation: input.Confirmation,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeDatabaseError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"operationId": deletion.Operation.ID, "status": deletion.Operation.Status,
		})
	})
}

func decodeBoundedJSON(w http.ResponseWriter, request *http.Request, destination any, maximum int64) bool {
	if !requestHasJSONContentType(request) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maximum))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil || ensureJSONEnd(decoder) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return false
	}
	return true
}

func writeDatabaseError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Recent authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The database request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The database request conflicts with current state or package limits.")
	default:
		logger.Error("process database request", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func newDatabaseWorkspaceResponse(workspace core.DatabaseWorkspace) databaseWorkspaceResponse {
	response := databaseWorkspaceResponse{
		Databases: make([]managedDatabaseResponse, 0, len(workspace.Databases)),
		Users:     make([]managedDatabaseUserResponse, 0, len(workspace.Users)),
		Grants:    make([]managedDatabaseGrantResponse, 0, len(workspace.Grants)),
	}
	for _, database := range workspace.Databases {
		response.Databases = append(response.Databases, managedDatabaseResponse{
			ID: database.ID, Alias: database.Alias, Status: database.Status,
			CreatedAt: database.CreatedAt, UpdatedAt: database.UpdatedAt,
		})
	}
	for _, user := range workspace.Users {
		response.Users = append(response.Users, managedDatabaseUserResponse{
			ID: user.ID, Alias: user.Alias, Host: user.Host, Status: user.Status,
			Revealed: user.Revealed, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
		})
	}
	for _, grant := range workspace.Grants {
		response.Grants = append(response.Grants, managedDatabaseGrantResponse{
			ID: grant.ID, DatabaseID: grant.DatabaseID, DatabaseUserID: grant.DatabaseUserID,
			Preset: grant.Preset, Status: grant.Status,
		})
	}
	return response
}
