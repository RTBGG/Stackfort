// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
)

const PanelIssueKind = "panel.hostname.issue"

type PanelIssuer interface {
	IssuePanel(context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.PanelIssueRequest) (agentprotocol.PanelStatusResponse, error)
}

type PanelIssueHandler struct{ Agent PanelIssuer }

func PanelIssuePayload(input agentprotocol.PanelIssueRequest) map[string]any {
	return map[string]any{"hostname": input.Hostname, "email": input.Email, "acceptTerms": input.AcceptTerms, "confirmOriginChange": input.ConfirmOriginChange}
}

func (handler *PanelIssueHandler) Run(ctx context.Context, claimed core.ClaimedOperation, reporter ProgressReporter) (map[string]any, error) {
	op := claimed.Operation
	if handler.Agent == nil || reporter == nil || op.Kind != PanelIssueKind || op.ActorID == nil || op.AccountID != nil {
		return nil, &Failure{Code: "panel.invalid_operation"}
	}
	encoded, err := json.Marshal(op.Payload)
	if err != nil {
		return nil, &Failure{Code: "panel.invalid_payload"}
	}
	var input agentprotocol.PanelIssueRequest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || agentprotocol.ValidatePanelIssue(input) != nil {
		return nil, &Failure{Code: "panel.invalid_payload"}
	}
	if err := reporter.Checkpoint(ctx, "issuing", 10, "panel.issuing", nil); err != nil {
		return nil, err
	}
	result, err := handler.Agent.IssuePanel(ctx, string(op.ID), agentprotocol.AuditCorrelation{OperationID: string(op.ID), ActorKind: agentprotocol.ActorIdentity, ActorID: string(*op.ActorID)}, input)
	if err != nil {
		return nil, &Failure{Code: "panel.issuance_failed"}
	}
	if !result.Enabled || !result.AutoRenew || result.RecoveryRequired || result.Hostname != input.Hostname {
		return nil, &Failure{Code: "panel.activation_unconfirmed"}
	}
	return map[string]any{"hostname": result.Hostname, "url": result.URL, "autoRenew": result.AutoRenew}, nil
}
