// SPDX-License-Identifier: AGPL-3.0-or-later
package agentrpc

import (
	"context"
	"errors"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPanelRPCFixedActionReplayAndRedaction(t *testing.T) {
	handler := NewHandler(nil)
	calls := 0
	handler.panel = func(ctx context.Context, input hostnginx.PanelRequest) (hostnginx.PanelStatus, error) {
		calls++
		if input.Action != "issue" || input.CertificatePath != "" || input.PrivateKeyPath != "" || !input.AcceptTerms {
			t.Fatalf("unsafe root request: %#v", input)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 4*time.Minute {
			t.Fatal("unbounded panel call")
		}
		return hostnginx.PanelStatus{Enabled: true, Hostname: input.Hostname, URL: "https://" + input.Hostname + "/", CertificateExpiresAt: time.Now().Add(time.Hour), AutoRenew: true}, nil
	}
	request := agentprotocol.Request{ProtocolVersion: agentprotocol.WireVersion, RequestID: "panel-rpc", IdempotencyKey: "panel-key", Operation: agentprotocol.OperationIssuePanel,
		Correlation: &agentprotocol.AuditCorrelation{OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorIdentity, ActorID: "019c1234-5678-7abc-8def-0123456789ac"},
		IssuePanel:  &agentprotocol.PanelIssueRequest{Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true, ConfirmOriginChange: true}}
	for range 2 {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, rpcRequest(t, request))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "panelStatus") {
			t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("replayed issuance %d times", calls)
	}
	handler.panel = func(context.Context, hostnginx.PanelRequest) (hostnginx.PanelStatus, error) {
		return hostnginx.PanelStatus{}, errors.New("PRIVATE KEY /root/secret.pem")
	}
	request.IdempotencyKey = "failure-key"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	if recorder.Code != 503 || strings.Contains(recorder.Body.String(), "PRIVATE") || strings.Contains(recorder.Body.String(), "/root") {
		t.Fatalf("unsafe error: %s", recorder.Body.String())
	}
}
