// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import "testing"

func TestPlatformUpdateProtocolIsStrictAndGlobal(t *testing.T) {
	correlation := validIdentityAuditCorrelation()
	correlation.AccountID = ""
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "update-start-request", IdempotencyKey: "update-start-key",
		Operation: OperationStartPlatformUpdate, Correlation: &correlation,
		StartPlatformUpdate: &PlatformUpdateStartRequest{Version: "1.2.3-beta.4"},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatal(err)
	}
	request.StartPlatformUpdate.Version = "1.2.3;reboot"
	if err := ValidateRequest(request); err == nil {
		t.Fatal("non-canonical update version was accepted")
	}
	request.StartPlatformUpdate.Version = "1.2.3"
	request.Correlation.AccountID = validIdentityAuditCorrelation().AccountID
	if err := ValidateRequest(request); err == nil {
		t.Fatal("account-scoped platform update was accepted")
	}
}

func TestPlatformUpdateResponsesAreBounded(t *testing.T) {
	start := Response{ProtocolVersion: WireVersion, RequestID: "request-1",
		PlatformUpdateStart: &PlatformUpdateStartResponse{Version: "1.2.3", Accepted: true}}
	if err := ValidateResponse(start, "request-1", OperationStartPlatformUpdate); err != nil {
		t.Fatal(err)
	}
	status := Response{ProtocolVersion: WireVersion, RequestID: "request-2",
		PlatformUpdateStatus: &PlatformUpdateStatusResponse{State: "complete", CurrentVersion: "1.2.2",
			TargetVersion: "1.2.3", StartedAt: "2026-09-02T10:00:00Z", UpdatedAt: "2026-09-02T10:01:00Z",
			CompletedAt: "2026-09-02T10:01:00Z"}}
	if err := ValidateResponse(status, "request-2", OperationInspectPlatformUpdate); err != nil {
		t.Fatal(err)
	}
	status.PlatformUpdateStatus.ErrorCode = "contains spaces but bounded"
	if err := ValidateResponse(status, "request-2", OperationInspectPlatformUpdate); err == nil {
		t.Fatal("malformed update error code was accepted")
	}
}
