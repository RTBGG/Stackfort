// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/tlsartifact"
)

type TLSCertificateStageRequest struct {
	Bundle tlsartifact.Bundle `json:"bundle"`
}

type TLSCertificateStageResponse struct {
	CertificateID string `json:"certificateId"`
	Changed       bool   `json:"changed"`
}

func validateTLSCertificateStageResponse(response TLSCertificateStageResponse, operation Operation) error {
	if operation != OperationStageTLSCertificate || response.CertificateID == "" {
		return errors.New("agent TLS certificate response has the wrong operation")
	}
	if _, err := tlsartifact.CertificatePath(response.CertificateID); err != nil {
		return errors.New("agent TLS certificate response is malformed")
	}
	return nil
}
