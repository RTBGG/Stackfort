// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/RTBGG/stackfort/internal/accountprovisioning"
	"github.com/RTBGG/stackfort/internal/core"
)

const maxAdminConsoleRequestBytes = 16 << 10

// AdminConsoleService is the narrow platform-administrator inventory and
// creation surface required by the Phase 1 browser flows.
type AdminConsoleService interface {
	ListPackages(context.Context) ([]core.Package, error)
	CreatePackage(context.Context, core.CreatePackageParams) (core.Package, error)
	ListHostingAccountSummaries(context.Context) ([]core.HostingAccountSummary, error)
	ListRecentOperations(context.Context, int) ([]core.Operation, error)
	ListRecentAuditEvents(context.Context, int) ([]core.AuditEvent, error)
}

type AccountProvisioningService interface {
	Create(context.Context, accountprovisioning.CreateCommand) (accountprovisioning.CreateResult, error)
}

type adminPackageRequest struct {
	Name   string             `json:"name"`
	Slug   string             `json:"slug"`
	Limits core.PackageLimits `json:"limits"`
}

type adminPackageResponse struct {
	ID              core.ID            `json:"id"`
	Name            string             `json:"name"`
	Slug            string             `json:"slug"`
	Status          core.PackageStatus `json:"status"`
	CurrentRevision int64              `json:"currentRevision"`
	Limits          core.PackageLimits `json:"limits"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

type adminPackageListResponse struct {
	Packages []adminPackageResponse `json:"packages"`
}

type adminAccountRequest struct {
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	PackageID       core.ID  `json:"packageId"`
	OwnerIdentityID *core.ID `json:"ownerIdentityId,omitempty"`
}

type adminAccountResponse struct {
	ID                         core.ID              `json:"id"`
	Name                       string               `json:"name"`
	Slug                       string               `json:"slug"`
	Status                     core.AccountStatus   `json:"status"`
	CurrentPackageAssignmentID core.ID              `json:"currentPackageAssignmentId"`
	PackageID                  core.ID              `json:"packageId,omitempty"`
	PackageName                string               `json:"packageName,omitempty"`
	PackageRevision            int64                `json:"packageRevision,omitempty"`
	HostReady                  bool                 `json:"hostReady"`
	CreatedAt                  time.Time            `json:"createdAt"`
	UpdatedAt                  time.Time            `json:"updatedAt"`
	ProvisioningOperationID    *core.ID             `json:"provisioningOperationId,omitempty"`
	ProvisioningStatus         core.OperationStatus `json:"provisioningStatus,omitempty"`
}

type adminAccountListResponse struct {
	Accounts []adminAccountResponse `json:"accounts"`
}

type adminOperationResponse struct {
	ID              core.ID              `json:"id"`
	AccountID       *core.ID             `json:"accountId,omitempty"`
	Kind            string               `json:"kind"`
	Status          core.OperationStatus `json:"status"`
	Stage           string               `json:"stage"`
	ProgressPercent int64                `json:"progressPercent"`
	ErrorCode       string               `json:"errorCode,omitempty"`
	AttemptCount    int64                `json:"attemptCount"`
	MaxAttempts     int64                `json:"maxAttempts"`
	CreatedAt       time.Time            `json:"createdAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
}

type adminOperationListResponse struct {
	Operations []adminOperationResponse `json:"operations"`
}

type adminAuditEventResponse struct {
	Sequence      int64            `json:"sequence"`
	ID            core.ID          `json:"id"`
	OccurredAt    time.Time        `json:"occurredAt"`
	ActorID       *core.ID         `json:"actorId,omitempty"`
	SourceAddress string           `json:"sourceAddress,omitempty"`
	Action        string           `json:"action"`
	TargetType    string           `json:"targetType"`
	TargetID      string           `json:"targetId,omitempty"`
	AccountID     *core.ID         `json:"accountId,omitempty"`
	OperationID   *core.ID         `json:"operationId,omitempty"`
	Result        core.AuditResult `json:"result"`
	Details       map[string]any   `json:"details"`
}

type adminAuditEventListResponse struct {
	Events []adminAuditEventResponse `json:"events"`
}

func registerAdminConsoleRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	authorization PlatformAuthorizationService,
	service AdminConsoleService,
	provisioning AccountProvisioningService,
) {
	mux.HandleFunc("GET /api/v1/admin/packages", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := authorizeAdminConsoleRequest(
			w, request, logger, authentication, authorization, core.AuthorizationPackagesView, false,
		); !ok {
			return
		}
		packages, err := service.ListPackages(request.Context())
		if err != nil {
			writeAdminConsoleError(w, logger, "list packages", err)
			return
		}
		response := adminPackageListResponse{Packages: make([]adminPackageResponse, 0, len(packages))}
		for _, item := range packages {
			response.Packages = append(response.Packages, newAdminPackageResponse(item))
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /api/v1/admin/packages", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authorizeAdminConsoleRequest(
			w, request, logger, authentication, authorization, core.AuthorizationPackagesManage, true,
		)
		if !ok {
			return
		}
		var input adminPackageRequest
		if !decodeAdminConsoleJSON(w, request, &input) {
			return
		}
		created, err := service.CreatePackage(request.Context(), core.CreatePackageParams{
			Name: input.Name, Slug: input.Slug, Limits: input.Limits,
			ActorID: &authenticated.Identity.ID, RequestID: request.Header.Get("X-Request-ID"),
		})
		if err != nil {
			writeAdminConsoleError(w, logger, "create package", err)
			return
		}
		writeJSON(w, http.StatusCreated, newAdminPackageResponse(created))
	})

	mux.HandleFunc("GET /api/v1/admin/accounts", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := authorizeAdminConsoleRequest(
			w, request, logger, authentication, authorization, core.AuthorizationPlatformView, false,
		); !ok {
			return
		}
		accounts, err := service.ListHostingAccountSummaries(request.Context())
		if err != nil {
			writeAdminConsoleError(w, logger, "list accounts", err)
			return
		}
		response := adminAccountListResponse{Accounts: make([]adminAccountResponse, 0, len(accounts))}
		for _, item := range accounts {
			response.Accounts = append(response.Accounts, adminAccountResponse{
				ID: item.ID, Name: item.Name, Slug: item.Slug, Status: item.Status,
				CurrentPackageAssignmentID: item.CurrentPackageAssignmentID,
				PackageID:                  item.PackageID, PackageName: item.PackageName, PackageRevision: item.PackageRevision,
				HostReady: item.HostReady, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /api/v1/admin/accounts", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authorizeAdminConsoleRequest(
			w, request, logger, authentication, authorization, core.AuthorizationAccountsCreate, true,
		)
		if !ok {
			return
		}
		if provisioning == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "account_provisioning_unavailable", "Hosting account provisioning is unavailable.")
			return
		}
		var input adminAccountRequest
		if !decodeAdminConsoleJSON(w, request, &input) {
			return
		}
		ownerID := authenticated.Identity.ID
		if input.OwnerIdentityID != nil {
			ownerID = *input.OwnerIdentityID
		}
		created, err := provisioning.Create(request.Context(), accountprovisioning.CreateCommand{
			Subject: authenticated.AuthorizationSubject(),
			Name:    input.Name, Slug: input.Slug, OwnerIdentityID: ownerID, PackageID: input.PackageID,
			RequestID: request.Header.Get("X-Request-ID"),
		})
		if err != nil {
			writeAdminConsoleError(w, logger, "create account", err)
			return
		}
		writeJSON(w, http.StatusCreated, adminAccountResponse{
			ID: created.Account.ID, Name: created.Account.Name, Slug: created.Account.Slug, Status: created.Account.Status,
			CurrentPackageAssignmentID: created.Account.CurrentPackageAssignmentID,
			PackageID:                  input.PackageID, HostReady: false,
			CreatedAt: created.Account.CreatedAt, UpdatedAt: created.Account.UpdatedAt,
			ProvisioningOperationID: &created.Operation.ID, ProvisioningStatus: created.Operation.Status,
		})
	})

	mux.HandleFunc("GET /api/v1/admin/operations", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := authorizeAdminConsoleRequest(
			w, request, logger, authentication, authorization, core.AuthorizationPlatformView, false,
		); !ok {
			return
		}
		limit, ok := adminListLimit(w, request)
		if !ok {
			return
		}
		operations, err := service.ListRecentOperations(request.Context(), limit)
		if err != nil {
			writeAdminConsoleError(w, logger, "list operations", err)
			return
		}
		response := adminOperationListResponse{Operations: make([]adminOperationResponse, 0, len(operations))}
		for _, item := range operations {
			response.Operations = append(response.Operations, adminOperationResponse{
				ID: item.ID, AccountID: item.AccountID, Kind: item.Kind, Status: item.Status,
				Stage: item.Stage, ProgressPercent: item.ProgressPercent, ErrorCode: item.ErrorCode,
				AttemptCount: item.AttemptCount, MaxAttempts: item.MaxAttempts,
				CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("GET /api/v1/admin/audit-events", func(w http.ResponseWriter, request *http.Request) {
		if _, ok := authorizeAdminConsoleRequest(
			w, request, logger, authentication, authorization, core.AuthorizationPlatformView, false,
		); !ok {
			return
		}
		limit, ok := adminListLimit(w, request)
		if !ok {
			return
		}
		events, err := service.ListRecentAuditEvents(request.Context(), limit)
		if err != nil {
			writeAdminConsoleError(w, logger, "list audit events", err)
			return
		}
		response := adminAuditEventListResponse{Events: make([]adminAuditEventResponse, 0, len(events))}
		for _, item := range events {
			response.Events = append(response.Events, adminAuditEventResponse{
				Sequence: item.Sequence, ID: item.ID, OccurredAt: item.OccurredAt,
				ActorID: item.ActorID, SourceAddress: item.SourceAddress, Action: item.Action,
				TargetType: item.TargetType, TargetID: item.TargetID, AccountID: item.AccountID,
				OperationID: item.OperationID, Result: item.Result, Details: item.Details,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func authorizeAdminConsoleRequest(
	w http.ResponseWriter,
	request *http.Request,
	logger *slog.Logger,
	authentication AuthenticationService,
	authorization PlatformAuthorizationService,
	action core.AuthorizationAction,
	requireCSRF bool,
) (core.AuthenticatedSession, bool) {
	authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, requireCSRF)
	if !ok {
		return core.AuthenticatedSession{}, false
	}
	if _, err := authorization.Authorize(request.Context(), core.AuthorizeParams{
		Subject: authenticated.AuthorizationSubject(), Action: action,
	}); err != nil {
		writePlatformAuthorizationError(w, logger, err)
		return core.AuthenticatedSession{}, false
	}
	return authenticated, true
}

func decodeAdminConsoleJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	if !requestHasJSONContentType(request) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxAdminConsoleRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil || ensureJSONEnd(decoder) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return false
	}
	return true
}

func adminListLimit(w http.ResponseWriter, request *http.Request) (int, bool) {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return 50, true
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > 200 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The list limit must be between 1 and 200.")
		return 0, false
	}
	return limit, true
}

func newAdminPackageResponse(item core.Package) adminPackageResponse {
	return adminPackageResponse{
		ID: item.ID, Name: item.Name, Slug: item.Slug, Status: item.Status,
		CurrentRevision: item.CurrentRevision, Limits: item.Limits,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func writeAdminConsoleError(w http.ResponseWriter, logger *slog.Logger, action string, err error) {
	switch {
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The administrator request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The administrator request conflicts with current state.")
	case errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	default:
		logger.Error(action, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}
