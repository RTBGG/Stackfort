// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
)

func TestNGINXActivationIsAccountScopedTypedAndCorrelated(t *testing.T) {
	t.Parallel()
	request := validNGINXActivationRequest(t)
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("ValidateRequest() error = %v", err)
	}

	invalid := request
	invalid.ActivateNGINXSites = cloneNGINXActivation(t, request.ActivateNGINXSites)
	invalid.ActivateNGINXSites.Domains[0].Target.DocumentRoot = "public_html/../private"
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("traversal error = %v", err)
	}

	invalid = request
	invalid.ActivateNGINXSites = cloneNGINXActivation(t, request.ActivateNGINXSites)
	invalid.ActivateNGINXSites.DesiredStateRevisionID = "550e8400-e29b-41d4-a716-446655440000"
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("non-v7 desired revision error = %v", err)
	}

	invalid = request
	correlation := *request.Correlation
	correlation.AccountID = "019c1234-5678-7abc-8def-0123456789ac"
	invalid.Correlation = &correlation
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-account error = %v", err)
	}
}

func TestNGINXActivationDecodeRejectsRawConfigurationField(t *testing.T) {
	t.Parallel()
	request := validNGINXActivationRequest(t)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	encoded = bytes.Replace(encoded, []byte(`"options":`), []byte(`"configurationText":"return 200;","options":`), 1)
	if _, err := DecodeRequest(bytes.NewReader(encoded)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("raw configuration field error = %v", err)
	}
}

func TestNGINXActivationResponseBindsRevisionAndDigest(t *testing.T) {
	t.Parallel()
	response := Response{
		ProtocolVersion: WireVersion, RequestID: "activation-response-1",
		NGINXActivation: &NGINXActivationResponse{
			Changed: true, ConfigurationTested: true, ReloadPerformed: true, HealthChecked: true,
			ActiveRevisionID:       "019c1234-5678-7abc-8def-0123456789ab",
			PreviousRevisionID:     "019c1234-5678-7abc-8def-0123456789ac",
			DesiredStateRevisionID: "019c1234-5678-7abc-8def-0123456789aa",
			ConfigDigest:           "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			RenderedDomains:        1,
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationActivateNGINXSites); err != nil {
		t.Fatalf("ValidateResponse() error = %v", err)
	}
	response.NGINXActivation.ConfigDigest = "ABCDEF"
	if err := ValidateResponse(response, response.RequestID, OperationActivateNGINXSites); err == nil {
		t.Fatal("malformed digest was accepted")
	}
}

func validNGINXActivationRequest(t *testing.T) Request {
	t.Helper()
	const accountID = "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	name, err := core.NormalizeDomainName("example.test")
	if err != nil {
		t.Fatal(err)
	}
	correlation := AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: ActorSystem,
		AccountID: accountID,
	}
	return Request{
		ProtocolVersion: WireVersion, RequestID: "activation-request-1", IdempotencyKey: "activation-key-1",
		Operation: OperationActivateNGINXSites, Correlation: &correlation,
		ActivateNGINXSites: &NGINXActivationRequest{
			Identity: hostingidentity.Spec{
				AccountID: accountID, Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home,
			},
			DesiredStateRevisionID: "019c1234-5678-7abc-8def-0123456789aa",
			Domains: []nginxconfig.DomainSpec{{
				Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
				Target: nginxconfig.TargetSpec{Type: core.DomainTargetStatic, DocumentRoot: "public_html"},
			}},
			Options: nginxconfig.DefaultOptions(),
		},
	}
}

func cloneNGINXActivation(t *testing.T, input *NGINXActivationRequest) *NGINXActivationRequest {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output NGINXActivationRequest
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	return &output
}
