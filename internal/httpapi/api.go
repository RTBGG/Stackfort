// SPDX-License-Identifier: AGPL-3.0-or-later

// Package httpapi implements Stackfort's initial versioned HTTP surface.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/core"
)

// HealthChecker is the minimal control-plane dependency required by the
// readiness endpoint.
type HealthChecker interface {
	Ping(context.Context) error
}

// BootstrapService is the narrow unauthenticated surface available before the
// first platform administrator exists.
type BootstrapService interface {
	AdministratorBootstrapStatus(context.Context) (core.BootstrapStatus, error)
	BootstrapAdministrator(context.Context, core.BootstrapAdministratorParams) (core.Identity, error)
}

// Services contains optional versioned API capabilities.
type Services struct {
	Bootstrap             BootstrapService
	Authentication        AuthenticationService
	Authorization         AuthorizationService
	PlatformAuthorization PlatformAuthorizationService
	HostCapabilities      HostCapabilityService
	MultiFactor           MultiFactorService
	Sessions              SessionManagementService
	Domains               DomainLifecycleService
	ACMEAccounts          ACMEAccountService
	TLSCertificates       TLSCertificateService
	AdminConsole          AdminConsoleService
	AccountProvisioning   AccountProvisioningService
	SelfService           SelfServiceService
	PHPWorkspace          PHPWorkspaceService
	DatabaseWorkspace     DatabaseWorkspaceService
	FileWorkspace         FileWorkspaceService
	BackupWorkspace       BackupWorkspaceService
	LogWorkspace          LogWorkspaceService
	CacheWorkspace        CacheWorkspaceService
	ScheduledJobs         ScheduledJobWorkspaceService
	UpdateChecks          UpdateCheckService
}

// New returns the API handler with safe defaults and explicit routes.
func New(logger *slog.Logger, state HealthChecker, bootstrapServices ...BootstrapService) http.Handler {
	services := Services{}
	if len(bootstrapServices) > 0 {
		services.Bootstrap = bootstrapServices[0]
	}
	return NewWithServices(logger, state, services)
}

// NewWithServices returns the API handler with explicitly selected services.
func NewWithServices(logger *slog.Logger, state HealthChecker, services Services) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, request *http.Request) {
		if state == nil || state.Ping(request.Context()) != nil {
			logger.Error("panel state health check failed")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"service": "stackfort-api",
				"status":  "unavailable",
				"storage": "unavailable",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "stackfort-api",
			"status":  "ok",
			"storage": "ok",
		})
	})
	mux.HandleFunc("GET /api/v1/build", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, buildinfo.Current())
	})
	if services.Bootstrap != nil {
		registerBootstrapRoutes(mux, logger, services.Bootstrap)
	}
	if services.Authentication != nil {
		registerAuthenticationRoutes(mux, logger, services.Authentication)
	}
	if services.Authentication != nil && services.Authorization != nil {
		registerAuthorizationRoutes(mux, logger, services.Authentication, services.Authorization)
	}
	if services.Authentication != nil && services.PlatformAuthorization != nil && services.HostCapabilities != nil {
		registerHostCapabilityRoutes(
			mux, logger, services.Authentication, services.PlatformAuthorization, services.HostCapabilities,
		)
	}
	if services.Authentication != nil && services.MultiFactor != nil {
		registerMultiFactorRoutes(mux, logger, services.Authentication, services.MultiFactor)
	}
	if services.Authentication != nil && services.Sessions != nil {
		registerSessionManagementRoutes(mux, logger, services.Authentication, services.Sessions)
	}
	if services.Authentication != nil && services.Domains != nil {
		registerDomainRoutes(mux, logger, services.Authentication, services.Domains)
	}
	if services.Authentication != nil && services.ACMEAccounts != nil {
		registerACMEAccountRoutes(mux, logger, services.Authentication, services.ACMEAccounts)
	}
	if services.Authentication != nil && services.TLSCertificates != nil {
		registerTLSCertificateRoutes(mux, logger, services.Authentication, services.TLSCertificates)
	}
	if services.Authentication != nil && services.PlatformAuthorization != nil && services.AdminConsole != nil {
		registerAdminConsoleRoutes(
			mux, logger, services.Authentication, services.PlatformAuthorization,
			services.AdminConsole, services.AccountProvisioning,
		)
	}
	if services.Authentication != nil && services.SelfService != nil {
		registerSelfServiceRoutes(mux, logger, services.Authentication, services.SelfService)
	}
	if services.Authentication != nil && services.PHPWorkspace != nil {
		registerPHPWorkspaceRoutes(mux, logger, services.Authentication, services.PHPWorkspace)
	}
	if services.Authentication != nil && services.DatabaseWorkspace != nil {
		registerDatabaseRoutes(mux, logger, services.Authentication, services.DatabaseWorkspace)
	}
	if services.Authentication != nil && services.FileWorkspace != nil {
		registerFileRoutes(mux, logger, services.Authentication, services.FileWorkspace)
	}
	if services.Authentication != nil && services.BackupWorkspace != nil {
		registerBackupRoutes(mux, logger, services.Authentication, services.BackupWorkspace)
	}
	if services.Authentication != nil && services.LogWorkspace != nil {
		registerLogRoutes(mux, logger, services.Authentication, services.LogWorkspace)
	}
	if services.Authentication != nil && services.CacheWorkspace != nil {
		registerCacheRoutes(mux, logger, services.Authentication, services.CacheWorkspace)
	}
	if services.Authentication != nil && services.ScheduledJobs != nil {
		registerScheduledJobRoutes(mux, logger, services.Authentication, services.ScheduledJobs)
	}
	if services.Authentication != nil && services.PlatformAuthorization != nil && services.UpdateChecks != nil {
		registerUpdateRoutes(
			mux, logger, services.Authentication, services.PlatformAuthorization, services.UpdateChecks,
		)
	}

	return securityHeaders(requestLog(logger, rejectCrossSiteMutations(mux)))
}

func rejectCrossSiteMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeAPIError(w, http.StatusForbidden, "cross_site_request", "Cross-site state changes are not allowed.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
