// SPDX-License-Identifier: AGPL-3.0-or-later

package agentclient

import (
	"context"
	"errors"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"net/http"
)

func (client *Client) InspectPanel(ctx context.Context, key string) (agentprotocol.PanelStatusResponse, error) {
	return client.panel(ctx, key, nil, nil)
}

func (client *Client) IssuePanel(ctx context.Context, key string, correlation agentprotocol.AuditCorrelation, input agentprotocol.PanelIssueRequest) (agentprotocol.PanelStatusResponse, error) {
	return client.panel(ctx, key, &correlation, &input)
}

func (client *Client) panel(ctx context.Context, key string, correlation *agentprotocol.AuditCorrelation, input *agentprotocol.PanelIssueRequest) (agentprotocol.PanelStatusResponse, error) {
	id, err := newRequestID()
	if err != nil {
		return agentprotocol.PanelStatusResponse{}, err
	}
	request := agentprotocol.Request{ProtocolVersion: agentprotocol.WireVersion, RequestID: id, IdempotencyKey: key, Operation: agentprotocol.OperationInspectPanel, InspectPanel: &agentprotocol.PanelInspectRequest{}}
	if input != nil {
		request.Operation, request.InspectPanel, request.IssuePanel, request.Correlation = agentprotocol.OperationIssuePanel, nil, input, correlation
	}
	response, status, err := client.callValidated(ctx, request)
	if err != nil {
		return agentprotocol.PanelStatusResponse{}, err
	}
	if response.Error != nil {
		return agentprotocol.PanelStatusResponse{}, remoteError(status, response.Error)
	}
	if status != http.StatusOK || response.PanelStatus == nil {
		return agentprotocol.PanelStatusResponse{}, errors.New("invalid panel response")
	}
	return *response.PanelStatus, nil
}
