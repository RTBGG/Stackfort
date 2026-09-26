// SPDX-License-Identifier: AGPL-3.0-or-later

// Package panelworkspace is the administrator-only panel hostname boundary.
package panelworkspace

import (
	"context"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/google/uuid"
	"strings"
)

type Repository interface {
	Authorize(context.Context, core.AuthorizeParams) (core.AuthorizationDecision, error)
	CreateOperation(context.Context, core.CreateOperationParams) (core.Operation, error)
}

type Inspector interface {
	InspectPanel(context.Context, string) (agentprotocol.PanelStatusResponse, error)
}

type Service struct {
	Repository Repository
	Agent      Inspector
}

func (service *Service) Status(ctx context.Context, subject core.AuthorizationSubject) (agentprotocol.PanelStatusResponse, error) {
	if _, err := service.Repository.Authorize(ctx, core.AuthorizeParams{Subject: subject, Action: core.AuthorizationPlatformView}); err != nil {
		return agentprotocol.PanelStatusResponse{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return agentprotocol.PanelStatusResponse{}, err
	}
	return service.Agent.InspectPanel(ctx, id.String())
}

func (service *Service) Queue(ctx context.Context, subject core.AuthorizationSubject, input agentprotocol.PanelIssueRequest, requestID, key string) (core.Operation, error) {
	if _, err := service.Repository.Authorize(ctx, core.AuthorizeParams{Subject: subject, Action: core.AuthorizationPlatformManage}); err != nil {
		return core.Operation{}, err
	}
	if agentprotocol.ValidatePanelIssue(input) != nil || key == "" || strings.TrimSpace(key) != key || len(key) > 128 || requestID == "" || strings.TrimSpace(requestID) != requestID || len(requestID) > 128 {
		return core.Operation{}, core.ErrInvalidInput
	}
	actor := subject.IdentityID()
	// An interrupted issuance requires inspection, never an automatic CA retry.
	return service.Repository.CreateOperation(ctx, core.CreateOperationParams{ActorID: &actor, Kind: operations.PanelIssueKind, RetryClass: core.RetryNone, MaxAttempts: 1, RequestID: requestID, IdempotencyKey: key, Payload: operations.PanelIssuePayload(input)})
}
