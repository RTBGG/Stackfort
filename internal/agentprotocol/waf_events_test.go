// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

func TestWAFEventProtocolHasNoRawDiagnosticSurface(t *testing.T) {
	t.Parallel()
	domain, _ := core.NormalizeDomainName("example.test")
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "waf-event-request-1", IdempotencyKey: "waf-event-key-1",
		Operation: OperationReadWAFEvents,
		ReadWAFEvents: &WAFEventReadRequest{
			Identity: validHostingIdentitySpec(), Domain: domain, Limit: MaximumWAFEventEntries,
		},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("ValidateRequest() error = %v", err)
	}
	if RequiresAuditCorrelation(OperationReadWAFEvents) {
		t.Fatal("read-only WAF events unexpectedly require mutation correlation")
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		WAFEvents: &WAFEventReadResponse{
			Domain: domain, Events: []WAFEvent{{
				ID: "0123456789abcdef0123456789abcdef", Timestamp: "2026-08-31T10:00:00Z",
				RuleID: 942100, Category: WAFEventSQLInjection, Severity: WAFSeverityCritical,
				Outcome: WAFEventBlocked, Method: "GET", Path: "/search", CorrelationID: "abcdef0123456789",
			}}, Next: "42:128", RetentionDays: hostinglogs.RetentionDays,
			MaximumActiveBytes: hostinglogs.MaximumActiveBytes, NativeDataWithheld: true,
		},
	}
	if err := ValidateResponse(response, request.RequestID, request.Operation); err != nil {
		t.Fatalf("ValidateResponse() error = %v", err)
	}
	response.WAFEvents.Events[0].Path = "/search?password=secret"
	if err := ValidateResponse(response, request.RequestID, request.Operation); err == nil {
		t.Fatal("WAF event query string was accepted")
	}
	response.WAFEvents.Events[0].Path = "/search"
	response.WAFEvents.NativeDataWithheld = false
	if err := ValidateResponse(response, request.RequestID, request.Operation); err == nil {
		t.Fatal("WAF event response without withholding guarantee was accepted")
	}
}

func TestWAFEventRequestRejectsMixedPayload(t *testing.T) {
	t.Parallel()
	domain, _ := core.NormalizeDomainName("example.test")
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "waf-event-request-2", IdempotencyKey: "waf-event-key-2",
		Operation:       OperationReadWAFEvents,
		ReadWAFEvents:   &WAFEventReadRequest{Identity: validHostingIdentitySpec(), Domain: domain, Limit: 1},
		ReadHostingLogs: &HostingLogReadRequest{},
	}
	if err := ValidateRequest(request); err == nil {
		t.Fatal("mixed WAF-event and log payload was accepted")
	}
}
