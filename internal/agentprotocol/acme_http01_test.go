// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
)

func TestACMEHTTP01ProtocolRequiresAccountCorrelationAndClosedIntent(t *testing.T) {
	t.Parallel()
	correlation := validIdentityAuditCorrelation()
	token := "0123456789abcdefghijkl"
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "acme-http01-request",
		IdempotencyKey: "acme-http01-key", Operation: OperationReconcileACMEHTTP01,
		Correlation: &correlation, ReconcileACMEHTTP01: &ACMEHTTP01Request{Intent: acmehttp01.Intent{
			Action: acmehttp01.ActionPresent, Token: token,
			KeyAuthorization: token + "." + strings.Repeat("a", 43),
		}},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid ACME HTTP-01 request: %v", err)
	}
	withoutAccount := request
	withoutAccountCorrelation := correlation
	withoutAccountCorrelation.AccountID = ""
	withoutAccount.Correlation = &withoutAccountCorrelation
	if err := ValidateRequest(withoutAccount); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing account correlation error = %v", err)
	}
	traversal := request
	traversalPayload := *request.ReconcileACMEHTTP01
	traversalPayload.Intent.Token = "../escape"
	traversal.ReconcileACMEHTTP01 = &traversalPayload
	if err := ValidateRequest(traversal); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("traversal token error = %v", err)
	}
}

func TestACMEHTTP01ResponseIsOperationSpecific(t *testing.T) {
	t.Parallel()
	response := Response{
		ProtocolVersion: WireVersion, RequestID: "acme-http01-response",
		ACMEHTTP01: &ACMEHTTP01Response{
			Action: acmehttp01.ActionPresent, Changed: true, Presented: true,
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationReconcileACMEHTTP01); err != nil {
		t.Fatalf("valid response: %v", err)
	}
	if err := ValidateResponse(response, response.RequestID, OperationActivateNGINXSites); err == nil {
		t.Fatal("ACME HTTP-01 response was accepted for NGINX activation")
	}
	response.ACMEHTTP01.Presented = false
	if err := ValidateResponse(response, response.RequestID, OperationReconcileACMEHTTP01); err == nil {
		t.Fatal("present response without presented state was accepted")
	}
}
