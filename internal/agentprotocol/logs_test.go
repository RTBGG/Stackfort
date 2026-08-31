// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

func TestHostingLogProtocolIsBoundedAndRejectsRawQueries(t *testing.T) {
	t.Parallel()
	domain, err := core.NormalizeDomainName("example.test")
	if err != nil {
		t.Fatal(err)
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "log-request-1", IdempotencyKey: "log-key-1",
		Operation: OperationReadHostingLogs,
		ReadHostingLogs: &HostingLogReadRequest{
			Identity: validHostingIdentitySpec(), Domain: domain, Kind: HostingLogAccess, Limit: MaximumHostingLogEntries,
		},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("ValidateRequest() error = %v", err)
	}
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		HostingLogs: &HostingLogReadResponse{
			Domain: domain, Kind: HostingLogAccess,
			Records: []HostingLogRecord{{
				Timestamp: "2026-08-29T10:00:00Z", Level: "info", ClientAddress: "192.0.2.10",
				Host: "example.test", Method: "GET", Path: "/index.html", Status: 200, Bytes: 42,
			}},
			Next: "42:128", RetentionDays: hostinglogs.RetentionDays,
			MaximumActiveBytes: hostinglogs.MaximumActiveBytes, SensitiveRedaction: true,
		},
	}
	if err := ValidateResponse(response, request.RequestID, request.Operation); err != nil {
		t.Fatalf("ValidateResponse() error = %v", err)
	}
	response.HostingLogs.Records[0].Path = "/index.html?token=secret"
	if err := ValidateResponse(response, request.RequestID, request.Operation); err == nil {
		t.Fatal("access record containing a query string was accepted")
	}
	response.HostingLogs.Records[0].Path = "/index.html"
	response.HostingLogs.QueryStringsStored = true
	if err := ValidateResponse(response, request.RequestID, request.Operation); err == nil {
		t.Fatal("response claiming stored query strings was accepted")
	}
}

func TestHostingLogRequestRejectsMixedPayloadAndMalformedCursor(t *testing.T) {
	t.Parallel()
	domain, _ := core.NormalizeDomainName("example.test")
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "log-request-2", IdempotencyKey: "log-key-2",
		Operation: OperationReadHostingLogs,
		ReadHostingLogs: &HostingLogReadRequest{
			Identity: validHostingIdentitySpec(), Domain: domain, Kind: HostingLogError, Cursor: "01:2", Limit: 1,
		},
	}
	if err := ValidateRequest(request); err == nil {
		t.Fatal("non-canonical cursor was accepted")
	}
	request.ReadHostingLogs.Cursor = ""
	request.ListFiles = &FileListRequest{}
	if err := ValidateRequest(request); err == nil {
		t.Fatal("mixed log and file payload was accepted")
	}
}
