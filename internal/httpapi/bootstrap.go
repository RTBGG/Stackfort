// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

const maxBootstrapRequestBytes = 4 << 10

type bootstrapRequest struct {
	Token       string      `json:"token"`
	Email       string      `json:"email"`
	DisplayName string      `json:"displayName"`
	Password    string      `json:"password"`
	Locale      core.Locale `json:"locale"`
}

type bootstrapStatusResponse struct {
	Required         bool       `json:"required"`
	CapabilityActive bool       `json:"capabilityActive"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
}

type bootstrapAdministratorResponse struct {
	ID          core.ID     `json:"id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"displayName"`
	Locale      core.Locale `json:"locale"`
	CreatedAt   time.Time   `json:"createdAt"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerBootstrapRoutes(mux *http.ServeMux, logger *slog.Logger, service BootstrapService) {
	mux.HandleFunc("GET /api/v1/bootstrap", func(w http.ResponseWriter, request *http.Request) {
		status, err := service.AdministratorBootstrapStatus(request.Context())
		if err != nil {
			logger.Error("read administrator bootstrap status", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}
		writeJSON(w, http.StatusOK, bootstrapStatusResponse{
			Required:         status.Required,
			CapabilityActive: status.CapabilityActive,
			ExpiresAt:        status.ExpiresAt,
		})
	})

	mux.HandleFunc("POST /api/v1/bootstrap", func(w http.ResponseWriter, request *http.Request) {
		var input bootstrapRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, maxBootstrapRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		if err := ensureJSONEnd(decoder); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "The request body is invalid.")
			return
		}
		sourceAddress, err := remoteSourceAddress(request.RemoteAddr)
		if err != nil {
			logger.Error("resolve bootstrap request source", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			return
		}

		identity, err := service.BootstrapAdministrator(request.Context(), core.BootstrapAdministratorParams{
			Token:         input.Token,
			Email:         input.Email,
			DisplayName:   input.DisplayName,
			Password:      input.Password,
			Locale:        input.Locale,
			SourceAddress: sourceAddress,
			RequestID:     request.Header.Get("X-Request-ID"),
		})
		if err != nil {
			writeBootstrapError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusCreated, bootstrapAdministratorResponse{
			ID:          identity.ID,
			Email:       identity.Email,
			DisplayName: identity.DisplayName,
			Locale:      identity.Locale,
			CreatedAt:   identity.CreatedAt,
		})
	})
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body contains more than one JSON value")
		}
		return err
	}
	return nil
}

func remoteSourceAddress(remoteAddress string) (string, error) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return "", fmt.Errorf("parse remote address: %w", err)
	}
	return address.Unmap().String(), nil
}

func writeBootstrapError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var rateLimit *core.BootstrapRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		retrySeconds := max(int64(1), int64((rateLimit.RetryAfter+time.Second-1)/time.Second))
		w.Header().Set("Retry-After", strconv.FormatInt(retrySeconds, 10))
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
	case errors.Is(err, core.ErrBootstrapDenied):
		writeAPIError(w, http.StatusForbidden, "bootstrap_denied", "The bootstrap capability is invalid or unavailable.")
	case errors.Is(err, core.ErrBootstrapDisabled):
		writeAPIError(w, http.StatusConflict, "bootstrap_disabled", "Administrator bootstrap is disabled.")
	case errors.Is(err, core.ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The supplied administrator details are invalid.")
	case errors.Is(err, core.ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The administrator could not be created because a record conflicts.")
	default:
		logger.Error("bootstrap administrator", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}
