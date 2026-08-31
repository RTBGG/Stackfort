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
	"github.com/RTBGG/stackfort/internal/domainlifecycle"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/google/uuid"
)

const maxDomainRequestBytes = 8 << 10

type DomainLifecycleService interface {
	Queue(context.Context, domainlifecycle.Command) (core.Operation, error)
	List(context.Context, domainlifecycle.ListParams) ([]core.Domain, error)
	OperationStatus(context.Context, domainlifecycle.OperationStatusParams) (core.Operation, error)
	Preview(context.Context, domainlifecycle.PreviewParams) (core.DomainRoutingPreview, error)
	QueueWAFException(context.Context, domainlifecycle.WAFExceptionCommand) (core.Operation, error)
	QueueWAFExceptionRemoval(context.Context, domainlifecycle.RemoveWAFExceptionCommand) (core.Operation, error)
	ListWAFExceptions(context.Context, domainlifecycle.ListWAFExceptionsParams) ([]core.DomainWAFException, error)
}

type domainCreateRequest struct {
	Name          string                `json:"name"`
	CanonicalMode *core.CanonicalMode   `json:"canonicalMode,omitempty"`
	Target        core.DomainTargetSpec `json:"target"`
	DisableTLS    bool                  `json:"disableTls,omitempty"`
	TLSMode       core.TLSMode          `json:"tlsMode,omitempty"`
	WAFMode       *core.WAFMode         `json:"wafMode,omitempty"`
	CachePreset   *core.CachePreset     `json:"cachePreset,omitempty"`
}

type domainEditRequest struct {
	CanonicalMode *core.CanonicalMode    `json:"canonicalMode,omitempty"`
	Target        *core.DomainTargetSpec `json:"target,omitempty"`
	WAFMode       *core.WAFMode          `json:"wafMode,omitempty"`
	CachePreset   *core.CachePreset      `json:"cachePreset,omitempty"`
}

type domainRoutingPreviewRequest struct {
	Name          string                `json:"name"`
	CanonicalMode core.CanonicalMode    `json:"canonicalMode,omitempty"`
	Target        core.DomainTargetSpec `json:"target"`
}

type domainRoutingPreviewResponse struct {
	Name          domainNameResponse           `json:"name"`
	CanonicalMode core.CanonicalMode           `json:"canonicalMode"`
	TargetType    core.DomainTargetType        `json:"targetType"`
	SamplePath    string                       `json:"samplePath"`
	SampleQuery   string                       `json:"sampleQuery"`
	Routes        []domainRoutePreviewResponse `json:"routes"`
}

type domainRoutePreviewResponse struct {
	SourcePattern  string                  `json:"sourcePattern"`
	SourceURL      string                  `json:"sourceUrl"`
	Action         core.DomainRouteAction  `json:"action"`
	StatusCode     core.RedirectStatusCode `json:"statusCode,omitempty"`
	DestinationURL string                  `json:"destinationUrl,omitempty"`
	PreservePath   bool                    `json:"preservePath"`
	PreserveQuery  bool                    `json:"preserveQuery"`
}

type domainOperationResponse struct {
	OperationID core.ID              `json:"operationId"`
	DomainID    core.ID              `json:"domainId"`
	Status      core.OperationStatus `json:"status"`
}

type accountOperationResponse struct {
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

type domainListResponse struct {
	Domains []domainResponse `json:"domains"`
}

type domainResponse struct {
	ID            core.ID              `json:"id"`
	Name          domainNameResponse   `json:"name"`
	Status        core.DomainStatus    `json:"status"`
	CanonicalMode core.CanonicalMode   `json:"canonicalMode"`
	Target        domainTargetResponse `json:"target"`
	TLS           domainTLSResponse    `json:"tls"`
	WAF           domainWAFResponse    `json:"waf"`
	Cache         domainCacheResponse  `json:"cache"`
	CreatedAt     time.Time            `json:"createdAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
}

type domainNameResponse struct {
	Display string `json:"display"`
	ASCII   string `json:"ascii"`
}

type domainTargetResponse struct {
	ID            core.ID                 `json:"id"`
	Type          core.DomainTargetType   `json:"type"`
	DocumentRoot  *documentRootResponse   `json:"documentRoot,omitempty"`
	PHPVersion    string                  `json:"phpVersion,omitempty"`
	ApplicationID *core.ID                `json:"applicationId,omitempty"`
	Redirect      *domainRedirectResponse `json:"redirect,omitempty"`
	CreatedAt     time.Time               `json:"createdAt"`
}

type domainRedirectResponse struct {
	StatusCode         core.RedirectStatusCode `json:"statusCode"`
	TargetURL          string                  `json:"targetUrl"`
	HostMode           core.RedirectHostMode   `json:"hostMode"`
	PreservePath       bool                    `json:"preservePath"`
	PreserveQuery      bool                    `json:"preserveQuery"`
	WildcardSubdomains bool                    `json:"wildcardSubdomains"`
}

type documentRootResponse struct {
	ID             core.ID `json:"id"`
	RelativePath   string  `json:"relativePath"`
	ReferenceCount int64   `json:"referenceCount"`
}

type domainTLSResponse struct {
	Enabled        bool                   `json:"enabled"`
	Mode           core.TLSMode           `json:"mode"`
	ChallengeType  core.TLSChallengeType  `json:"challengeType"`
	IssuanceStatus core.TLSIssuanceStatus `json:"issuanceStatus"`
	Names          []string               `json:"names"`
	ExpiresAt      *time.Time             `json:"expiresAt,omitempty"`
	NextRenewalAt  *time.Time             `json:"nextRenewalAt,omitempty"`
	LastErrorCode  string                 `json:"lastErrorCode,omitempty"`
}

type domainWAFResponse struct {
	Mode core.WAFMode `json:"mode"`
}

type domainCacheResponse struct {
	Preset core.CachePreset `json:"preset"`
}

type wafExceptionRequest struct {
	RuleID      uint32    `json:"ruleId"`
	RequestPath string    `json:"requestPath,omitempty"`
	Parameter   string    `json:"parameter,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type wafExceptionListResponse struct {
	Exceptions []core.DomainWAFException `json:"exceptions"`
}

func registerDomainRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service DomainLifecycleService,
) {
	registerAdministratorWAFExceptionRoutes(mux, logger, authentication, service)
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/domains/preview", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input domainRoutingPreviewRequest
		if !decodeDomainJSON(w, request, &input) {
			return
		}
		preview, err := service.Preview(request.Context(), domainlifecycle.PreviewParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
			Name: input.Name, CanonicalMode: input.CanonicalMode, Target: input.Target,
		})
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newDomainRoutingPreviewResponse(preview))
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/domains", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		domains, err := service.List(request.Context(), domainlifecycle.ListParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID,
		})
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		response := domainListResponse{Domains: make([]domainResponse, 0, len(domains))}
		for _, domain := range domains {
			response.Domains = append(response.Domains, newDomainResponse(domain))
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/operations/{operationID}", func(w http.ResponseWriter, request *http.Request) {
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
		operation, err := service.OperationStatus(request.Context(), domainlifecycle.OperationStatusParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, OperationID: operationID,
		})
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, newAccountOperationResponse(operation))
	})

	mux.HandleFunc("POST /api/v1/accounts/{accountID}/domains", func(w http.ResponseWriter, request *http.Request) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		var input domainCreateRequest
		if !decodeDomainJSON(w, request, &input) {
			return
		}
		operation, err := queueDomainRequest(request, service, authenticated, accountID, operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleCreate, Name: input.Name,
			CanonicalMode: input.CanonicalMode, Target: &input.Target,
			DisableTLS: input.DisableTLS, TLSMode: input.TLSMode, WAFMode: input.WAFMode,
			CachePreset: input.CachePreset,
		})
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, domainOperationResponse{
			OperationID: operation.ID, DomainID: operation.ID, Status: operation.Status,
		})
	})

	mux.HandleFunc("PATCH /api/v1/accounts/{accountID}/domains/{domainID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, domainID, ok := authenticateDomainMutation(w, request, logger, authentication)
		if !ok {
			return
		}
		var input domainEditRequest
		if !decodeDomainJSON(w, request, &input) {
			return
		}
		operation, err := queueDomainRequest(request, service, authenticated, accountID, operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleEdit, DomainID: string(domainID),
			CanonicalMode: input.CanonicalMode, Target: input.Target, WAFMode: input.WAFMode,
			CachePreset: input.CachePreset,
		})
		writeQueuedDomainMutation(w, logger, domainID, operation, err)
	})

	for _, route := range []struct {
		pattern string
		action  operations.DomainLifecycleAction
	}{
		{"POST /api/v1/accounts/{accountID}/domains/{domainID}/suspend", operations.DomainLifecycleSuspend},
		{"POST /api/v1/accounts/{accountID}/domains/{domainID}/resume", operations.DomainLifecycleResume},
		{"DELETE /api/v1/accounts/{accountID}/domains/{domainID}", operations.DomainLifecycleRemove},
	} {
		route := route
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, request *http.Request) {
			authenticated, accountID, domainID, ok := authenticateDomainMutation(w, request, logger, authentication)
			if !ok {
				return
			}
			operation, err := queueDomainRequest(request, service, authenticated, accountID, operations.DomainLifecyclePayload{
				Action: route.action, DomainID: string(domainID),
			})
			writeQueuedDomainMutation(w, logger, domainID, operation, err)
		})
	}
}

func registerAdministratorWAFExceptionRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service DomainLifecycleService,
) {
	const collection = "/api/v1/admin/accounts/{accountID}/domains/{domainID}/waf-exceptions"
	mux.HandleFunc("GET "+collection, func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, domainID, ok := authenticateAdministratorDomain(w, request, logger, authentication, false)
		if !ok {
			return
		}
		exceptions, err := service.ListWAFExceptions(request.Context(), domainlifecycle.ListWAFExceptionsParams{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
		})
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, wafExceptionListResponse{Exceptions: exceptions})
	})
	mux.HandleFunc("POST "+collection, func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, domainID, ok := authenticateAdministratorDomain(w, request, logger, authentication, true)
		if !ok {
			return
		}
		var input wafExceptionRequest
		if !decodeDomainJSON(w, request, &input) {
			return
		}
		requestID, err := domainRequestID(request)
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		operation, err := service.QueueWAFException(request.Context(), domainlifecycle.WAFExceptionCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
			RuleID: input.RuleID, RequestPath: input.RequestPath, Parameter: input.Parameter,
			ExpiresAt: input.ExpiresAt, RequestID: requestID,
			IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		writeQueuedDomainMutation(w, logger, domainID, operation, err)
	})
	mux.HandleFunc("DELETE "+collection+"/{exceptionID}", func(w http.ResponseWriter, request *http.Request) {
		authenticated, accountID, domainID, ok := authenticateAdministratorDomain(w, request, logger, authentication, true)
		if !ok {
			return
		}
		exceptionID, ok := parseDomainRouteID(w, request.PathValue("exceptionID"))
		if !ok {
			return
		}
		requestID, err := domainRequestID(request)
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		operation, err := service.QueueWAFExceptionRemoval(request.Context(), domainlifecycle.RemoveWAFExceptionCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
			ExceptionID: exceptionID, RequestID: requestID,
			IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		writeQueuedDomainMutation(w, logger, domainID, operation, err)
	})
}

func authenticateAdministratorDomain(
	w http.ResponseWriter,
	request *http.Request,
	logger *slog.Logger,
	authentication AuthenticationService,
	requireCSRF bool,
) (core.AuthenticatedSession, core.ID, core.ID, bool) {
	authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, requireCSRF)
	if !ok {
		return core.AuthenticatedSession{}, "", "", false
	}
	accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
	if !ok {
		return core.AuthenticatedSession{}, "", "", false
	}
	domainID, ok := parseDomainRouteID(w, request.PathValue("domainID"))
	return authenticated, accountID, domainID, ok
}

func domainRequestID(request *http.Request) (string, error) {
	if value := request.Header.Get("X-Request-ID"); value != "" {
		return value, nil
	}
	generated, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return generated.String(), nil
}

func authenticateDomainMutation(
	w http.ResponseWriter,
	request *http.Request,
	logger *slog.Logger,
	authentication AuthenticationService,
) (core.AuthenticatedSession, core.ID, core.ID, bool) {
	authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, true)
	if !ok {
		return core.AuthenticatedSession{}, "", "", false
	}
	accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
	if !ok {
		return core.AuthenticatedSession{}, "", "", false
	}
	domainID, ok := parseDomainRouteID(w, request.PathValue("domainID"))
	return authenticated, accountID, domainID, ok
}

func queueDomainRequest(
	request *http.Request,
	service DomainLifecycleService,
	authenticated core.AuthenticatedSession,
	accountID core.ID,
	payload operations.DomainLifecyclePayload,
) (core.Operation, error) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		var err error
		requestID, err = domainRequestID(request)
		if err != nil {
			return core.Operation{}, err
		}
	}
	return service.Queue(request.Context(), domainlifecycle.Command{
		Subject: authenticated.AuthorizationSubject(), AccountID: accountID, Payload: payload,
		RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
	})
}

func writeQueuedDomainMutation(
	w http.ResponseWriter,
	logger *slog.Logger,
	domainID core.ID,
	operation core.Operation,
	err error,
) {
	if err != nil {
		writeDomainError(w, logger, err)
		return
	}
	writeJSON(w, http.StatusAccepted, domainOperationResponse{
		OperationID: operation.ID, DomainID: domainID, Status: operation.Status,
	})
}

func decodeDomainJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	if !requestHasJSONContentType(request) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxDomainRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil || ensureJSONEnd(decoder) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
		return false
	}
	return true
}

func parseDomainRouteID(w http.ResponseWriter, value string) (core.ID, bool) {
	id, err := core.ParseID(value)
	if err != nil {
		writeResourceNotFound(w)
		return "", false
	}
	return id, true
}

func writeDomainError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, core.ErrAuthorizationDenied), errors.Is(err, core.ErrNotFound):
		writeResourceNotFound(w)
	case errors.Is(err, core.ErrSessionInvalid):
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, core.ErrRecentAuthenticationRequired):
		writeAPIError(w, http.StatusForbidden, "recent_authentication_required", "Recent authentication is required.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The domain request is invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The domain request conflicts with current state.")
	default:
		logger.Error("process domain request", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func newDomainResponse(domain core.Domain) domainResponse {
	target := domainTargetResponse{
		ID: domain.Target.ID, Type: domain.Target.Type, PHPVersion: domain.Target.PHPVersion,
		ApplicationID: domain.Target.ApplicationID, CreatedAt: domain.Target.CreatedAt,
	}
	if domain.Target.Redirect != nil {
		target.Redirect = &domainRedirectResponse{
			StatusCode: domain.Target.Redirect.StatusCode, TargetURL: domain.Target.Redirect.TargetURL,
			HostMode:     domain.Target.Redirect.HostMode,
			PreservePath: domain.Target.Redirect.PreservePath, PreserveQuery: domain.Target.Redirect.PreserveQuery,
			WildcardSubdomains: domain.Target.Redirect.WildcardSubdomains,
		}
	}
	if domain.Target.DocumentRoot != nil {
		target.DocumentRoot = &documentRootResponse{
			ID: domain.Target.DocumentRoot.ID, RelativePath: domain.Target.DocumentRoot.RelativePath,
			ReferenceCount: domain.Target.DocumentRoot.ReferenceCount,
		}
	}
	return domainResponse{
		ID: domain.ID, Name: domainNameResponse{Display: domain.Name.Display, ASCII: domain.Name.ASCII}, Status: domain.Status,
		CanonicalMode: domain.CanonicalMode, Target: target,
		TLS: domainTLSResponse{
			Enabled: domain.TLS.Enabled, Mode: domain.TLS.Mode,
			ChallengeType: domain.TLS.ChallengeType, IssuanceStatus: domain.TLS.IssuanceStatus,
			Names: append([]string(nil), domain.TLS.Names...), ExpiresAt: domain.TLS.ExpiresAt,
			NextRenewalAt: domain.TLS.NextRenewalAt, LastErrorCode: domain.TLS.LastErrorCode,
		},
		WAF:       domainWAFResponse{Mode: domain.WAF.Mode},
		Cache:     domainCacheResponse{Preset: domain.Cache.Preset},
		CreatedAt: domain.CreatedAt, UpdatedAt: domain.UpdatedAt,
	}
}

func newAccountOperationResponse(operation core.Operation) accountOperationResponse {
	return accountOperationResponse{
		ID: operation.ID, AccountID: operation.AccountID, Kind: operation.Kind,
		Status: operation.Status, Stage: operation.Stage, ProgressPercent: operation.ProgressPercent,
		ErrorCode: operation.ErrorCode, AttemptCount: operation.AttemptCount, MaxAttempts: operation.MaxAttempts,
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt,
	}
}

func newDomainRoutingPreviewResponse(preview core.DomainRoutingPreview) domainRoutingPreviewResponse {
	response := domainRoutingPreviewResponse{
		Name:          domainNameResponse{Display: preview.Name.Display, ASCII: preview.Name.ASCII},
		CanonicalMode: preview.CanonicalMode, TargetType: preview.TargetType,
		SamplePath: preview.SamplePath, SampleQuery: preview.SampleQuery,
		Routes: make([]domainRoutePreviewResponse, 0, len(preview.Routes)),
	}
	for _, route := range preview.Routes {
		response.Routes = append(response.Routes, domainRoutePreviewResponse{
			SourcePattern: route.SourcePattern, SourceURL: route.SourceURL, Action: route.Action,
			StatusCode: route.StatusCode, DestinationURL: route.DestinationURL,
			PreservePath: route.PreservePath, PreserveQuery: route.PreserveQuery,
		})
	}
	return response
}
