// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/ociresources"
)

type OCIResourceReconcileRequest struct {
	Spec ociresources.Spec `json:"spec"`
}

type OCIResourceReconcileResponse struct {
	Result ociresources.Result `json:"result"`
}

func validateOCIResourceReconcileRequest(
	correlation *AuditCorrelation, request OCIResourceReconcileRequest,
) error {
	if correlation == nil || correlation.AccountID == "" || correlation.AccountID != request.Spec.Identity.AccountID {
		return errors.New("OCI private-resource account does not match audit correlation")
	}
	return ociresources.Validate(request.Spec)
}

func validateOCIResourceReconcileResponse(response OCIResourceReconcileResponse, operation Operation) error {
	if operation != OperationReconcileOCIResources {
		return errors.New("agent OCI private-resource response operation is unknown")
	}
	return ociresources.ValidateResult(response.Result)
}
