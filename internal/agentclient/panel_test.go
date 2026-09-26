// SPDX-License-Identifier: AGPL-3.0-or-later
package agentclient

import (
	"bytes"
	"encoding/json"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestPanelIssueUsesLongTransportAndFixedDeadline(t *testing.T) {
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("short transport used"); return nil, nil })}}
	calls := 0
	client.writeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > agentprotocol.MaximumPanelIssueDuration || time.Until(deadline) < 4*time.Minute {
			t.Fatal("wrong issuance deadline")
		}
		decoded, err := agentprotocol.DecodeRequest(r.Body)
		if err != nil || decoded.Operation != agentprotocol.OperationIssuePanel {
			t.Fatalf("%#v %v", decoded, err)
		}
		body, _ := json.Marshal(agentprotocol.Response{ProtocolVersion: agentprotocol.WireVersion, RequestID: decoded.RequestID, PanelStatus: &agentprotocol.PanelStatusResponse{}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{agentprotocol.MediaType}, "X-Stackfort-Protocol": []string{"1"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	actor := agentprotocol.AuditCorrelation{ActorKind: agentprotocol.ActorIdentity, OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorID: "019c1234-5678-7abc-8def-0123456789ac"}
	input := agentprotocol.PanelIssueRequest{Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true, ConfirmOriginChange: true}
	if _, err := client.IssuePanel(t.Context(), "panel-test", actor, input); err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	input.AcceptTerms = false
	if _, err := client.IssuePanel(t.Context(), "panel-invalid", actor, input); err == nil || calls != 1 {
		t.Fatal("invalid input sent")
	}
}
