// SPDX-License-Identifier: AGPL-3.0-or-later
package agentprotocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPanelContractIsClosedAndAdministratorScoped(t *testing.T) {
	input := PanelIssueRequest{Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true, ConfirmOriginChange: true}
	correlation := AuditCorrelation{OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: ActorIdentity, ActorID: "019c1234-5678-7abc-8def-0123456789ac"}
	base := Request{ProtocolVersion: WireVersion, RequestID: "panel-test", IdempotencyKey: "panel-test", Operation: OperationIssuePanel, Correlation: &correlation, IssuePanel: &input}
	if err := ValidateRequest(base); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Request, *PanelIssueRequest, *AuditCorrelation){
		func(r *Request, _ *PanelIssueRequest, _ *AuditCorrelation) { r.Correlation = nil },
		func(_ *Request, _ *PanelIssueRequest, c *AuditCorrelation) { c.ActorKind = ActorSystem; c.ActorID = "" },
		func(_ *Request, _ *PanelIssueRequest, c *AuditCorrelation) { c.AccountID = c.ActorID },
		func(_ *Request, i *PanelIssueRequest, _ *AuditCorrelation) { i.AcceptTerms = false },
		func(_ *Request, i *PanelIssueRequest, _ *AuditCorrelation) { i.ConfirmOriginChange = false },
		func(_ *Request, i *PanelIssueRequest, _ *AuditCorrelation) { i.Hostname = "https://panel.example.com" },
		func(_ *Request, i *PanelIssueRequest, _ *AuditCorrelation) { i.Hostname = "127.0.0.1" },
		func(_ *Request, i *PanelIssueRequest, _ *AuditCorrelation) { i.Hostname = "panel.example.com; reboot" },
		func(_ *Request, i *PanelIssueRequest, _ *AuditCorrelation) { i.Email = "invalid" },
		func(r *Request, _ *PanelIssueRequest, _ *AuditCorrelation) { r.InspectPanel = &PanelInspectRequest{} },
	} {
		request, intent, actor := base, input, correlation
		request.IssuePanel, request.Correlation = &intent, &actor
		mutate(&request, &intent, &actor)
		if ValidateRequest(request) == nil {
			t.Fatalf("unsafe request accepted: %#v", request)
		}
	}
	encoded, _ := json.Marshal(base)
	for _, field := range []string{"privateKeyPath", "directoryUrl", "action", "certificatePath"} {
		raw := strings.Replace(string(encoded), `"acceptTerms":true`, `"acceptTerms":true,"`+field+`":"attacker"`, 1)
		if _, err := DecodeRequest(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted %s", field)
		}
	}
	response := Response{ProtocolVersion: WireVersion, RequestID: "panel-test", PanelStatus: &PanelStatusResponse{Enabled: true, Hostname: input.Hostname, URL: "https://" + input.Hostname + "/", CertificateExpiresAt: time.Now().Add(time.Hour), AutoRenew: true}}
	if err := ValidateResponse(response, "panel-test", OperationIssuePanel); err != nil {
		t.Fatal(err)
	}
	response.PanelStatus.URL = "https://attacker.example/"
	if ValidateResponse(response, "panel-test", OperationIssuePanel) == nil {
		t.Fatal("accepted foreign URL")
	}
	response.PanelStatus.URL = "https://" + input.Hostname + "/"
	if ValidateResponse(response, "panel-test", OperationInspectCapabilities) == nil {
		t.Fatal("accepted mixed response")
	}
}
