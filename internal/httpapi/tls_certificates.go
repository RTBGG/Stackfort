// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	certificateapp "github.com/RTBGG/stackfort/internal/certificates"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/google/uuid"
)

type TLSCertificateService interface {
	QueueIssue(context.Context, certificateapp.IssueCommand) (core.Operation, error)
	List(context.Context, core.AuthorizationSubject, core.ID, core.ID) ([]core.TLSCertificate, error)
}

type tlsCertificateOperationResponse struct {
	OperationID core.ID              `json:"operationId"`
	DomainID    core.ID              `json:"domainId"`
	Status      core.OperationStatus `json:"status"`
}

type tlsCertificateListResponse struct {
	Certificates []tlsCertificateResponse `json:"certificates"`
}

type tlsCertificateResponse struct {
	ID                core.ID                   `json:"id"`
	Status            core.TLSCertificateStatus `json:"status"`
	Names             []string                  `json:"names"`
	Issuer            string                    `json:"issuer,omitempty"`
	SerialHex         string                    `json:"serialHex,omitempty"`
	FingerprintSHA256 string                    `json:"fingerprintSha256,omitempty"`
	NotBefore         *time.Time                `json:"notBefore,omitempty"`
	ExpiresAt         *time.Time                `json:"expiresAt,omitempty"`
	NextRenewalAt     *time.Time                `json:"nextRenewalAt,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	ActivatedAt       *time.Time                `json:"activatedAt,omitempty"`
	RetiredAt         *time.Time                `json:"retiredAt,omitempty"`
}

func registerTLSCertificateRoutes(
	mux *http.ServeMux,
	logger *slog.Logger,
	authentication AuthenticationService,
	service TLSCertificateService,
) {
	mux.HandleFunc("POST /api/v1/accounts/{accountID}/domains/{domainID}/tls/issue", func(
		w http.ResponseWriter,
		request *http.Request,
	) {
		authenticated, accountID, domainID, ok := authenticateDomainMutation(
			w, request, logger, authentication,
		)
		if !ok {
			return
		}
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			generated, err := uuid.NewV7()
			if err != nil {
				writeDomainError(w, logger, err)
				return
			}
			requestID = generated.String()
		}
		operation, err := service.QueueIssue(request.Context(), certificateapp.IssueCommand{
			Subject: authenticated.AuthorizationSubject(), AccountID: accountID, DomainID: domainID,
			RequestID: requestID, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, tlsCertificateOperationResponse{
			OperationID: operation.ID, DomainID: domainID, Status: operation.Status,
		})
	})

	mux.HandleFunc("GET /api/v1/accounts/{accountID}/domains/{domainID}/tls/certificates", func(
		w http.ResponseWriter,
		request *http.Request,
	) {
		authenticated, ok := authenticateBrowserSession(w, request, logger, authentication, false)
		if !ok {
			return
		}
		accountID, ok := parseDomainRouteID(w, request.PathValue("accountID"))
		if !ok {
			return
		}
		domainID, ok := parseDomainRouteID(w, request.PathValue("domainID"))
		if !ok {
			return
		}
		certificates, err := service.List(
			request.Context(), authenticated.AuthorizationSubject(), accountID, domainID,
		)
		if err != nil {
			writeDomainError(w, logger, err)
			return
		}
		response := tlsCertificateListResponse{Certificates: make([]tlsCertificateResponse, 0, len(certificates))}
		for _, certificate := range certificates {
			response.Certificates = append(response.Certificates, tlsCertificateResponse{
				ID: certificate.ID, Status: certificate.Status, Names: append([]string(nil), certificate.Names...),
				Issuer: certificate.Issuer, SerialHex: certificate.SerialHex,
				FingerprintSHA256: certificate.FingerprintSHA256,
				NotBefore:         certificate.NotBefore, ExpiresAt: certificate.ExpiresAt,
				NextRenewalAt: certificate.NextRenewalAt, CreatedAt: certificate.CreatedAt,
				ActivatedAt: certificate.ActivatedAt, RetiredAt: certificate.RetiredAt,
			})
		}
		writeJSON(w, http.StatusOK, response)
	})
}
