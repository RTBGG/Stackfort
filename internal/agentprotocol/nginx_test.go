// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

func TestNGINXBaselineMutationIsGlobalTypedAndCorrelated(t *testing.T) {
	t.Parallel()
	correlation := AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: ActorSystem,
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "nginx-request-1", IdempotencyKey: "nginx-key-1",
		Operation: OperationReconcileNGINXBaseline, Correlation: &correlation,
		ReconcileNGINXBaseline: &NGINXBaselineRequest{},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid NGINX baseline request: %v", err)
	}
	invalid := request
	invalidCorrelation := correlation
	invalidCorrelation.AccountID = "019c1234-5678-7abc-8def-0123456789ad"
	invalid.Correlation = &invalidCorrelation
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("account-scoped global mutation error = %v", err)
	}
	invalid = request
	invalid.InspectCapabilities = &InspectCapabilitiesRequest{}
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("mixed payload error = %v", err)
	}
}

func TestNGINXBaselineResponseIsExactAndCapabilityLabelled(t *testing.T) {
	t.Parallel()
	response := Response{
		ProtocolVersion: WireVersion, RequestID: "nginx-response-1",
		NGINXBaseline: &NGINXBaselineResponse{
			Changed: true, ConfigurationTested: true, ServiceActive: true, ServiceEnabled: true,
			ActivationPerformed: true, ConfigurationRoot: nginxbaseline.ManagedRoot,
			MainConfiguration:         nginxbaseline.MainConfiguration,
			PanelIncludeDirectory:     nginxbaseline.PanelDirectory,
			SitesIncludeDirectory:     nginxbaseline.SitesDirectory,
			HTTPDefaultRejectsUnknown: true, HTTPSDefaultRejectsUnknown: true,
			TrustedProxyHops: []string{nginxbaseline.LoopbackIPv4, nginxbaseline.LoopbackIPv6},
			Capability:       Capability{Status: CapabilityAvailable},
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationReconcileNGINXBaseline); err != nil {
		t.Fatalf("valid NGINX response: %v", err)
	}
	response.NGINXBaseline.TrustedProxyHops = []string{"0.0.0.0/0", "::/0"}
	if err := ValidateResponse(response, response.RequestID, OperationReconcileNGINXBaseline); err == nil {
		t.Fatal("public trusted proxy range was accepted")
	}
	unavailable := Response{
		ProtocolVersion: WireVersion, RequestID: "nginx-response-2",
		Error: &ResponseError{
			Code: ErrorNGINXUnavailable, Message: "NGINX is unavailable.",
			Capability: &Capability{Status: CapabilityUnavailable, ReasonCode: "nginx-binary-unavailable"},
		},
	}
	if err := ValidateResponse(unavailable, unavailable.RequestID, OperationReconcileNGINXBaseline); err != nil {
		t.Fatalf("valid unavailable response: %v", err)
	}
}
