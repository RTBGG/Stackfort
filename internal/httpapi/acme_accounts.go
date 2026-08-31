// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/RTBGG/stackfort/internal/acmeaccounts"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/google/uuid"
)

const maxACMEAccountRequestBytes = 4 << 10

type ACMEAccountService interface {
	QueueRegistration(context.Context, acmeaccounts.RegisterCommand) (core.Operation, error)
	List(context.Context, core.AuthorizationSubject) ([]core.ACMEAccount, error)
}

type acmeAccountRegistrationRequest struct {
	Environment   core.ACMEEnvironment `json:"environment"`
	ContactEmail  string               `json:"contactEmail"`
	TermsAccepted bool                 `json:"termsAccepted"`
}

type acmeAccountOperationResponse struct {
	OperationID core.ID              `json:"operationId"`
	Status      core.OperationStatus `json:"status"`
}

type acmeAccountListResponse struct {
	Accounts []acmeAccountResponse `json:"accounts"`
}

// acmeAccountResponse intentionally excludes the account private key, its
// envelope, the public thumbprint, and authority-internal account/order URLs.
type acmeAccountResponse struct {
	ID            core.ID                `json:"id"`
	Environment   core.ACMEEnvironment   `json:"environment"`
	DirectoryURL  string                 `json:"directoryUrl"`
	ContactEmail  string                 `json:"contactEmail"`
	Status        core.ACMEAccountStatus `json:"status"`
	TermsAgreedAt time.Time              `json:"termsAgreedAt"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
	RegisteredAt  *time.Time             `json:"registeredAt,omitempty"`
}

func registerACMEAccountRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service ACMEAccountService,
) {
	mux.HandleFunc("GET /api/v1/admin/acme/accounts", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accounts, err := service.List(request.Context(), authenticated.AuthorizationSubject())
		if err != nil {
			writeACMEAccountError(w, logger, err)
			return
		}
		response := acmeAccountListResponse{Accounts: make([]acmeAccountResponse, 0, len(accounts))}
		for _, account := range accounts {
			response.Accounts = append(response.Accounts, acmeAccountResponse{
				ID: account.ID, Environment: account.Environment, DirectoryURL: account.DirectoryURL,
				ContactEmail: account.ContactEmail, Status: account.Status,
				TermsAgreedAt: account.TermsAgreedAt, CreatedAt: account.CreatedAt,
				UpdatedAt: account.UpdatedAt, RegisteredAt: account.RegisteredAt,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /api/v1/admin/acme/accounts", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		if !requestHasJSONContentType(request) {
			writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
			return
		}
		var input acmeAccountRegistrationRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxACMEAccountRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || ensureJSONEnd(decoder) != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeACMEAccountError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		operation, err := service.QueueRegistration(request.Context(), acmeaccounts.RegisterCommand{
			Subject: authenticated.AuthorizationSubject(), Environment: input.Environment,
			ContactEmail: input.ContactEmail, TermsAccepted: input.TermsAccepted,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeACMEAccountError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, acmeAccountOperationResponse{
			OperationID: operation.ID, Status: operation.Status,
		})
	})
}

func writeACMEAccountError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied):
		writeAPIError(w, http.StatusForbidden, "permission_denied", "Platform administrator access is required.")
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Recent authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The ACME account request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The ACME environment already has conflicting account data.")
	default:
		logger.Error("process ACME account request", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
