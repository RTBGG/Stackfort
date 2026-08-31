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
)

const maxSelfServiceRequestBytes = 8 << 10

// SelfServiceService is identity-scoped. Its implementation derives every
// account and mutation target from the authenticated authorization subject.
type SelfServiceService interface {
	GetSelfServiceContext(context.Context, core.GetSelfServiceContextParams) (core.SelfServiceContext, error)
	UpdateOwnProfile(context.Context, core.UpdateOwnProfileParams) (core.Identity, error)
}

type selfServiceContextResponse struct {
	PlatformAdministrator bool                         `json:"platformAdministrator"`
	Accounts              []selfServiceAccountResponse `json:"accounts"`
}

type selfServiceAccountResponse struct {
	ID              core.ID             `json:"id"`
	Name            string              `json:"name"`
	Slug            string              `json:"slug"`
	Status          core.AccountStatus  `json:"status"`
	MembershipRole  core.MembershipRole `json:"membershipRole"`
	PackageID       core.ID             `json:"packageId"`
	PackageName     string              `json:"packageName"`
	PackageRevision int64               `json:"packageRevision"`
	HostReady       bool                `json:"hostReady"`
	EffectiveLimits core.PackageLimits  `json:"effectiveLimits"`
	Usage           selfServiceUsage    `json:"usage"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type selfServiceUsage struct {
	Domains int64 `json:"domains"`
}

type updateOwnProfileRequest struct {
	Email       string      `json:"email"`
	DisplayName string      `json:"displayName"`
	Locale      core.Locale `json:"locale"`
}

func registerSelfServiceRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service SelfServiceService,
) {
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		result, err := service.GetSelfServiceContext(request.Context(), core.GetSelfServiceContextParams{
			Subject: authenticated.AuthorizationSubject(),
		})
		if err != nil {
			writeSelfServiceError(w, logger, "load self-service context", err)
			return
		}
		response := selfServiceContextResponse{
			PlatformAdministrator: result.PlatformAdministrator,
			Accounts:              make([]selfServiceAccountResponse, 0, len(result.Accounts)),
		}
		for _, account := range result.Accounts {
			response.Accounts = append(response.Accounts, selfServiceAccountResponse{
				ID: account.ID, Name: account.Name, Slug: account.Slug, Status: account.Status,
				MembershipRole: account.MembershipRole, PackageID: account.PackageID,
				PackageName: account.PackageName, PackageRevision: account.PackageRevision,
				HostReady:       account.HostReady,
				EffectiveLimits: account.EffectiveLimits, Usage: selfServiceUsage{Domains: account.DomainCount},
				CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("PATCH /api/v1/me/profile", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input updateOwnProfileRequest
		if !decodeSelfServiceJSON(w, request, &input) {
			return
		}
		sourceAddress, ok := requestSourceAddress(w, request, logger)
		if !ok {
			return
		}
		identity, err := service.UpdateOwnProfile(request.Context(), core.UpdateOwnProfileParams{
			Subject: authenticated.AuthorizationSubject(), Email: input.Email,
			DisplayName: input.DisplayName, Locale: input.Locale,
			RequestID: request.Header.Get("X-Request-ID"), SourceAddress: sourceAddress,
		})
		if err != nil {
			writeSelfServiceError(w, logger, "update own profile", err)
			return
		}
		writeJSON(w, http.StatusOK, identityResponse{
			ID: identity.ID, Email: identity.Email,
			DisplayName: identity.DisplayName, Locale: identity.Locale,
		})
	})
}

func decodeSelfServiceJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	if !requestHasJSONContentType(request) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxSelfServiceRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil || ensureJSONEnd(decoder) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return false
	}
	return true
}

func writeSelfServiceError(w http.ResponseWriter, logger *slog.Logger, action string, err error) {
	switch {
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The profile request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The profile conflicts with current state.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Please authenticate again before continuing.")
	case errors.Is(err, core.ErrAuthorizationDenied):
		writeAPIError(w, http.StatusForbidden, "forbidden", "The request is not permitted.")
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	default:
		logger.Error(action, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
