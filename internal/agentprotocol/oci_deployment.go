// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/ocideployment"
)

type OCIDeploymentRequest struct {
	Request ocideployment.Request `json:"request"`
}

type OCIDeploymentResponse struct {
	Result ocideployment.LifecycleResult `json:"result"`
}

type OCIApplicationLogReadRequest struct {
	Spec ocideployment.LogSpec `json:"spec"`
}

type OCIApplicationLogReadResponse struct {
	Result ocideployment.LogResult `json:"result"`
}

func validateOCIDeploymentRequest(correlation *AuditCorrelation, request OCIDeploymentRequest) error {
	if correlation == nil || correlation.AccountID == "" ||
		correlation.AccountID != request.Request.Spec.Identity.AccountID {
		return errors.New("OCI deployment account does not match audit correlation")
	}
	return ocideployment.ValidateRequest(request.Request)
}

func validateOCIDeploymentResponse(response OCIDeploymentResponse, operation Operation) error {
	if operation != OperationReconcileOCIDeployment {
		return errors.New("agent OCI deployment response operation is unknown")
	}
	return ocideployment.ValidateLifecycleResult(response.Result)
}

func validateOCIApplicationLogReadRequest(request OCIApplicationLogReadRequest) error {
	return ocideployment.ValidateLogSpec(request.Spec)
}

func validateOCIApplicationLogReadResponse(response OCIApplicationLogReadResponse, operation Operation) error {
	if operation != OperationReadOCIApplicationLogs || len(response.Result.Entries) > ocideployment.MaximumLogEntries {
		return errors.New("agent OCI application log response is invalid")
	}
	for _, entry := range response.Result.Entries {
		if entry.Timestamp == "" || len(entry.Timestamp) > 64 || len(entry.Message) > 8<<10 {
			return errors.New("agent OCI application log entry is invalid")
		}
	}
	return nil
}
