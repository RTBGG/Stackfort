// SPDX-License-Identifier: AGPL-3.0-or-later
package operations

import (
	"context"
	"errors"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"testing"
)

type panelIssuerStub struct {
	calls       int
	correlation agentprotocol.AuditCorrelation
	err         error
}

func (s *panelIssuerStub) IssuePanel(_ context.Context, _ string, c agentprotocol.AuditCorrelation, i agentprotocol.PanelIssueRequest) (agentprotocol.PanelStatusResponse, error) {
	s.calls++
	s.correlation = c
	return agentprotocol.PanelStatusResponse{Enabled: true, AutoRenew: true, Hostname: i.Hostname, URL: "https://" + i.Hostname + "/"}, s.err
}

type panelReporter struct{}

func (panelReporter) Checkpoint(context.Context, string, int64, string, map[string]any) error {
	return nil
}

func TestPanelWorkerUsesBoundedIntentAndDoesNotRetryIssuance(t *testing.T) {
	actor := core.ID("019c1234-5678-7abc-8def-0123456789ab")
	op := core.Operation{ID: "019c1234-5678-7abc-8def-0123456789ac", ActorID: &actor, Kind: PanelIssueKind, Payload: PanelIssuePayload(agentprotocol.PanelIssueRequest{Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true, ConfirmOriginChange: true})}
	agent := &panelIssuerStub{}
	handler := &PanelIssueHandler{Agent: agent}
	result, err := handler.Run(t.Context(), core.ClaimedOperation{Operation: op}, panelReporter{})
	if err != nil || result["hostname"] != "panel.example.com" || agent.correlation.ActorID != string(actor) || agent.correlation.OperationID != string(op.ID) {
		t.Fatalf("%#v %v", result, err)
	}
	agent.err = errors.New("raw secret CA detail")
	_, err = handler.Run(t.Context(), core.ClaimedOperation{Operation: op}, panelReporter{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Retryable || failure.Code != "panel.issuance_failed" {
		t.Fatalf("unsafe failure: %v", err)
	}
	agent.calls = 0
	op.Payload["privateKeyPath"] = "/etc/shadow"
	if _, err := handler.Run(t.Context(), core.ClaimedOperation{Operation: op}, panelReporter{}); err == nil || agent.calls != 0 {
		t.Fatal("unsafe payload crossed agent boundary")
	}
}

func TestMissingACMEAccountHasActionableFailure(t *testing.T) {
	failure := classifyTLSRepositoryFailure(core.ErrACMEAccountRequired)
	var typed *Failure
	if !errors.As(failure, &typed) || typed.Code != "tls.acme_account_required" || typed.Retryable {
		t.Fatalf("%v", failure)
	}
}
