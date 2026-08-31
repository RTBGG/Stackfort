// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

// AuthorizationService exposes only resource lookups whose policy check and
// account-scoped query are coupled inside the application layer.
type AuthorizationService interface {
	GetAuthorizedHostingAccount(context.Context, core.GetAuthorizedHostingAccountParams) (core.AuthorizedHostingAccount, error)
}

type accountAuthorizationResponse struct {
	PlatformAdministrator bool                 `json:"platformAdministrator"`
	MembershipRole        *core.MembershipRole `json:"membershipRole,omitempty"`
}

type hostingAccountResponse struct {
	ID                         core.ID                      `json:"id"`
	Name                       string                       `json:"name"`
	Slug                       string                       `json:"slug"`
	Status                     core.AccountStatus           `json:"status"`
	CurrentPackageAssignmentID core.ID                      `json:"currentPackageAssignmentId"`
	CreatedAt                  time.Time                    `json:"createdAt"`
	UpdatedAt                  time.Time                    `json:"updatedAt"`
	Authorization              accountAuthorizationResponse `json:"authorization"`
}

func registerAuthorizationRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	authorization AuthorizationService,
) {
	mux.HandleFunc("GET /api/v1/accounts/{accountID}", func(w http.ResponseWriter, request *http.Request) {
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
		result, err := authorization.GetAuthorizedHostingAccount(request.Context(), core.GetAuthorizedHostingAccountParams{
			Subject:   authenticated.AuthorizationSubject(),
			AccountID: accountID,
		})
		if err != nil {
			writeResourceAuthorizationError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newHostingAccountResponse(result))
	})
}

func authenticateRequestSession(request *http.Request, authentication AuthenticationService) (core.AuthenticatedSession, error) {
	sessionCookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return core.AuthenticatedSession{}, core.ErrSessionInvalid
	}
	return authentication.AuthenticateSession(request.Context(), core.AuthenticateSessionParams{
		SessionToken: sessionCookie.Value,
	})
}

func newHostingAccountResponse(result core.AuthorizedHostingAccount) hostingAccountResponse {
	return hostingAccountResponse{
		ID:                         result.Account.ID,
		Name:                       result.Account.Name,
		Slug:                       result.Account.Slug,
		Status:                     result.Account.Status,
		CurrentPackageAssignmentID: result.Account.CurrentPackageAssignmentID,
		CreatedAt:                  result.Account.CreatedAt,
		UpdatedAt:                  result.Account.UpdatedAt,
		Authorization: accountAuthorizationResponse{
			PlatformAdministrator: result.Authorization.PlatformAdministrator,
			MembershipRole:        result.Authorization.MembershipRole,
		},
	}
}

func writeResourceAuthorizationError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Please authenticate again before continuing.")
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	default:
		logger.Error("authorize resource request", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeResourceNotFound(w http.ResponseWriter) {
	writeAPIError(w, http.StatusNotFound, "resource_not_found", "The requested resource was not found.")
}
