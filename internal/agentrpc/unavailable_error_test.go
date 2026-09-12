// SPDX-License-Identifier: AGPL-3.0-or-later

package agentrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostjobs"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/hostocideployment"
	"github.com/RTBGG/stackfort/internal/hostociimage"
	"github.com/RTBGG/stackfort/internal/hostociresources"
	"github.com/RTBGG/stackfort/internal/hostphp"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

// Exercise the same strict decoder as the API and local qualification client:
// an HTTP failure is still a correlated, typed protocol response, not arbitrary
// JSON. Keep the stronger decoder requirement when a backend fails unexpectedly.
func TestCapabilityErrorHandlersPreserveWireContract(t *testing.T) {
	t.Parallel()
	type errorHandler func(*Handler, agentprotocol.Response, agentprotocol.Request, error) (int, agentprotocol.Response)
	capability := agentprotocol.Capability{Status: agentprotocol.CapabilityUnavailable, ReasonCode: "fixture-host-unavailable"}
	tests := []struct {
		name        string
		operation   agentprotocol.Operation
		handle      errorHandler
		capError    error
		genericCode agentprotocol.ErrorCode
	}{
		{"identity", agentprotocol.OperationReconcileIdentity, (*Handler).identityError,
			&hostidentity.RuntimeCapabilityError{Capability: capability}, agentprotocol.ErrorMutationFailed},
		{"filesystem", agentprotocol.OperationReconcileFilesystem, (*Handler).filesystemError,
			&hostfilesystem.CapabilityError{Capability: capability}, agentprotocol.ErrorMutationFailed},
		{"resources", agentprotocol.OperationReconcileResources, (*Handler).resourceError,
			&hostresources.CapabilityError{Capability: capability}, agentprotocol.ErrorMutationFailed},
		{"nginx baseline", agentprotocol.OperationReconcileNGINXBaseline, (*Handler).nginxError,
			&hostnginx.CapabilityError{Capability: capability}, agentprotocol.ErrorMutationFailed},
		{"nginx activation", agentprotocol.OperationActivateNGINXSites, (*Handler).nginxActivationError,
			&hostnginx.CapabilityError{Capability: capability}, agentprotocol.ErrorNGINXActivation},
		{"php inspection", agentprotocol.OperationInspectPHPPools, (*Handler).phpPoolInspectionError,
			&hostphp.CapabilityError{Capability: capability}, agentprotocol.ErrorInternal},
		{"php reconcile", agentprotocol.OperationReconcilePHPPools, (*Handler).phpPoolError,
			&hostphp.CapabilityError{Capability: capability}, agentprotocol.ErrorPHPActivation},
		{"scheduled job", agentprotocol.OperationReconcileScheduledJob, (*Handler).scheduledJobError,
			&hostjobs.CapabilityError{Capability: capability}, agentprotocol.ErrorMutationFailed},
		{"oci image", agentprotocol.OperationPrepareOCIImage, (*Handler).ociImageError,
			&hostociimage.CapabilityError{Capability: capability}, agentprotocol.ErrorOCIImageUnavailable},
		{"oci resources", agentprotocol.OperationReconcileOCIResources, (*Handler).ociResourceError,
			&hostociresources.CapabilityError{Capability: capability}, agentprotocol.ErrorOCIResourceUnavailable},
		{"oci deployment", agentprotocol.OperationReconcileOCIDeployment, (*Handler).ociDeploymentError,
			&hostocideployment.CapabilityError{Capability: capability}, agentprotocol.ErrorOCIDeploymentUnavailable},
		{"oci logs", agentprotocol.OperationReadOCIApplicationLogs, (*Handler).ociDeploymentError,
			&hostocideployment.CapabilityError{Capability: capability}, agentprotocol.ErrorOCIDeploymentUnavailable},
		{"cache", agentprotocol.OperationInspectCacheMetrics, (*Handler).cacheError,
			nil, agentprotocol.ErrorCacheUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, detailed := range []bool{false, true} {
				if detailed && test.capError == nil {
					continue
				}
				var logs bytes.Buffer
				handler := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))
				request := unavailableErrorRequest(test.operation)
				backendErr := errors.New("private-runtime-output-marker")
				if detailed {
					backendErr = fmt.Errorf("private-runtime-output-marker: %w", test.capError)
				}
				status, response := test.handle(handler, agentprotocol.Response{
					ProtocolVersion: agentprotocol.WireVersion, RequestID: request.RequestID,
				}, request, backendErr)
				encoded, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := agentprotocol.DecodeResponse(bytes.NewReader(encoded), request.RequestID, request.Operation)
				if err != nil {
					t.Fatalf("detailed=%t: invalid error wire response: %v", detailed, err)
				}
				if detailed {
					if status != http.StatusUnprocessableEntity || decoded.Error.Capability == nil ||
						*decoded.Error.Capability != capability {
						t.Fatal("genuine capability detail or 422 status was lost")
					}
				} else if status < 500 || decoded.Error.Code != test.genericCode {
					t.Fatalf("generic status=%d code=%s", status, decoded.Error.Code)
				}
				if bytes.Contains(encoded, []byte("private-runtime-output-marker")) ||
					strings.Contains(logs.String(), "private-runtime-output-marker") {
					t.Fatal("backend detail escaped into response or audit log")
				}
			}
		})
	}
}

func TestOCIUnavailableHTTPResponseContract(t *testing.T) {
	t.Parallel()
	capability := agentprotocol.Capability{Status: agentprotocol.CapabilityUnsupported, ReasonCode: "fixture-host-unsupported"}
	tests := []struct {
		name      string
		resources bool
		err       error
		status    int
		want      agentprotocol.Capability
	}{
		{"image generic", false, errors.New("private-runtime-output-marker"), 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-operation-failed"}},
		{"image build", false, ociimage.ErrBuildFailed, 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-build-failed"}},
		{"image pull", false, ociimage.ErrPullFailed, 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-pull-failed"}},
		{"image inspect", false, ociimage.ErrInspectFailed, 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-inspect-failed"}},
		{"image scan wrapped", false, fmt.Errorf("private-runtime-output-marker: %w", ociimage.ErrScanFailed), 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-scan-failed"}},
		{"image same message not sentinel", false, errors.New(ociimage.ErrBuildFailed.Error()), 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-image-operation-failed"}},
		{"image capability", false, &hostociimage.CapabilityError{Capability: capability}, 422, capability},
		{"resources generic", true, errors.New("private-runtime-output-marker"), 503,
			agentprotocol.Capability{Status: agentprotocol.CapabilityUnknown, ReasonCode: "oci-resource-operation-failed"}},
		{"resources capability", true, &hostociresources.CapabilityError{Capability: capability}, 422, capability},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity := handlerIdentitySpec(t)
			var logs bytes.Buffer
			handler := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))
			request := unavailableErrorRequest(agentprotocol.OperationPrepareOCIImage)
			request.Correlation.AccountID = identity.AccountID
			wantCode := agentprotocol.ErrorOCIImageUnavailable
			if test.resources {
				request.Operation = agentprotocol.OperationReconcileOCIResources
				request.ReconcileOCIResources = &agentprotocol.OCIResourceReconcileRequest{Spec: ociresources.Spec{
					Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 1,
				}}
				handler.ociResources = &fakeOCIResourceReconciler{err: test.err}
				wantCode = agentprotocol.ErrorOCIResourceUnavailable
			} else {
				request.PrepareOCIImage = &agentprotocol.OCIImagePrepareRequest{Spec: ociimage.PrepareSpec{
					Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 1,
					Source: ociapps.Source{Kind: ociapps.SourceImageDigest,
						ImageReference: "registry.example/app@sha256:" + strings.Repeat("a", 64)},
				}}
				handler.images = &fakeOCIImagePreparer{err: test.err}
			}
			if err := agentprotocol.ValidateRequest(request); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, rpcRequest(t, request))
			if recorder.Code != test.status {
				t.Fatalf("HTTP status=%d, want %d", recorder.Code, test.status)
			}
			if strings.Contains(recorder.Body.String(), "private-runtime-output-marker") ||
				strings.Contains(logs.String(), "private-runtime-output-marker") {
				t.Fatal("backend detail escaped into response or audit log")
			}
			response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
			if response.Error == nil || response.Error.Code != wantCode || response.Error.Capability == nil ||
				*response.Error.Capability != test.want {
				t.Fatal("closed OCI error classification was lost")
			}
			// The fix is on the producer. Keep rejecting missing, successful, or
			// unbounded capability details instead of weakening the consumer.
			for _, invalid := range []*agentprotocol.Capability{nil,
				{Status: agentprotocol.CapabilityAvailable},
				{Status: agentprotocol.CapabilityUnknown, ReasonCode: strings.Repeat("x", 257)}} {
				response.Error.Capability = invalid
				if agentprotocol.ValidateResponse(response, request.RequestID, request.Operation) == nil {
					t.Fatal("malformed capability detail was accepted")
				}
			}
		})
	}
}

func unavailableErrorRequest(operation agentprotocol.Operation) agentprotocol.Request {
	return agentprotocol.Request{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "unavailable-response-request", IdempotencyKey: "unavailable-response-key", Operation: operation,
		Correlation: &agentprotocol.AuditCorrelation{
			OperationID: "019d2eaa-62d0-7f52-8ac7-0aeb932455db", ActorKind: agentprotocol.ActorSystem,
			AccountID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db",
		}}
}
