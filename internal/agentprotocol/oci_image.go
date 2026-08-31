// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/ociimage"
)

type OCIImagePrepareRequest struct {
	Spec ociimage.PrepareSpec `json:"spec"`
}

type OCIImagePrepareResponse struct {
	Result ociimage.Result `json:"result"`
}

func validateOCIImagePrepareRequest(correlation *AuditCorrelation, request OCIImagePrepareRequest) error {
	if correlation == nil || correlation.AccountID == "" || correlation.AccountID != request.Spec.Identity.AccountID {
		return errors.New("OCI image preparation account does not match audit correlation")
	}
	return ociimage.ValidateSpec(request.Spec)
}

func validateOCIImagePrepareResponse(response OCIImagePrepareResponse, operation Operation) error {
	if operation != OperationPrepareOCIImage {
		return errors.New("agent OCI image response operation is unknown")
	}
	return ociimage.ValidateResult(response.Result)
}
