// SPDX-License-Identifier: AGPL-3.0-or-later

package agentrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostdatabase"
	"github.com/RTBGG/stackfort/internal/hostfiles"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/hostociimage"
	"github.com/RTBGG/stackfort/internal/hostociresources"
	"github.com/RTBGG/stackfort/internal/hostphp"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/phpruntime"
)

func TestHandshakeNegotiationAndIdempotentReplay(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil)
	first := rpcRequest(t, handshakeRequest("request-1", "stable-key", 1, 1))
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	firstResponse := decodeTestResponse(t, firstRecorder.Body, "request-1")
	if firstResponse.Handshake == nil || firstResponse.Handshake.SelectedVersion != 1 {
		t.Fatalf("first response = %#v", firstResponse)
	}

	replay := rpcRequest(t, handshakeRequest("request-2", "stable-key", 1, 1))
	replayRecorder := httptest.NewRecorder()
	handler.ServeHTTP(replayRecorder, replay)
	replayResponse := decodeTestResponse(t, replayRecorder.Body, "request-2")
	if replayRecorder.Code != http.StatusOK || replayResponse.Handshake == nil {
		t.Fatalf("replay status=%d response=%#v", replayRecorder.Code, replayResponse)
	}

	conflict := rpcRequest(t, handshakeRequest("request-3", "stable-key", 2, 2))
	conflictRecorder := httptest.NewRecorder()
	handler.ServeHTTP(conflictRecorder, conflict)
	conflictResponse := decodeTestResponse(t, conflictRecorder.Body, "request-3")
	if conflictRecorder.Code != http.StatusConflict || conflictResponse.Error == nil ||
		conflictResponse.Error.Code != agentprotocol.ErrorIdempotencyConflict {
		t.Fatalf("conflict status=%d response=%#v", conflictRecorder.Code, conflictResponse)
	}
}

func TestOCIImageDispatchAndTypedScanRejection(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	spec := ociimage.PrepareSpec{
		Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 1,
		Source: ociapps.Source{Kind: ociapps.SourceImageDigest, ImageReference: "registry.example/app@sha256:" + strings.Repeat("a", 64)},
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019d2eaa-62d0-7f52-8ac7-0aeb932455db", ActorKind: agentprotocol.ActorSystem,
		AccountID: identity.AccountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "oci-image-request", IdempotencyKey: "oci-image-key",
		Operation: agentprotocol.OperationPrepareOCIImage, Correlation: &correlation,
		PrepareOCIImage: &agentprotocol.OCIImagePrepareRequest{Spec: spec},
	}
	handler := NewHandler(nil)
	preparer := &fakeOCIImagePreparer{result: ociimage.Result{
		ImageDigest: "sha256:" + strings.Repeat("b", 64), SourceDigest: "sha256:" + strings.Repeat("a", 64),
		PolicyVersion: ociimage.PolicyVersion, ScannerProvider: ociimage.ScannerProvider, ScannerVersion: ociimage.ScannerVersion,
	}}
	handler.images = preparer
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, agentprotocol.OperationPrepareOCIImage)
	if recorder.Code != http.StatusOK || response.OCIImage == nil ||
		response.OCIImage.Result.ImageDigest != preparer.result.ImageDigest || preparer.calls.Load() != 1 {
		t.Fatalf("status=%d response=%#v calls=%d", recorder.Code, response, preparer.calls.Load())
	}

	rejected := NewHandler(nil)
	rejected.images = &fakeOCIImagePreparer{err: hostociimage.ErrScanRejected}
	request.RequestID, request.IdempotencyKey = "oci-image-rejected", "oci-image-rejected-key"
	recorder = httptest.NewRecorder()
	rejected.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, agentprotocol.OperationPrepareOCIImage)
	if recorder.Code != http.StatusUnprocessableEntity || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorOCIImageRejected {
		t.Fatalf("rejected status=%d response=%#v", recorder.Code, response)
	}
}

func TestOCIResourceDispatchAndTypedHostConflict(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	spec := ociresources.Spec{
		Identity: identity, ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 1,
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019d2eaa-62d0-7f52-8ac7-0aeb932455db", ActorKind: agentprotocol.ActorSystem,
		AccountID: identity.AccountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "oci-resource-request",
		IdempotencyKey: "oci-resource-key", Operation: agentprotocol.OperationReconcileOCIResources,
		Correlation:           &correlation,
		ReconcileOCIResources: &agentprotocol.OCIResourceReconcileRequest{Spec: spec},
	}
	result, err := ociresources.ResultFor(spec, true)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil)
	reconciler := &fakeOCIResourceReconciler{result: result}
	handler.ociResources = reconciler
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, agentprotocol.OperationReconcileOCIResources)
	if recorder.Code != http.StatusOK || response.OCIResources == nil ||
		response.OCIResources.Result.ResourceDigest != result.ResourceDigest || reconciler.calls.Load() != 1 {
		t.Fatalf("status=%d response=%#v calls=%d", recorder.Code, response, reconciler.calls.Load())
	}

	conflict := NewHandler(nil)
	conflict.ociResources = &fakeOCIResourceReconciler{err: hostociresources.ErrConflict}
	request.RequestID, request.IdempotencyKey = "oci-resource-conflict", "oci-resource-conflict-key"
	recorder = httptest.NewRecorder()
	conflict.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, agentprotocol.OperationReconcileOCIResources)
	if recorder.Code != http.StatusConflict || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorOCIResourceConflict {
		t.Fatalf("conflict status=%d response=%#v", recorder.Code, response)
	}
}

func TestResponseCacheDoesNotPinTransientServerFailures(t *testing.T) {
	t.Parallel()
	cache := &responseCache{entries: map[string]cachedResponse{}, limit: 4, ttl: time.Minute}
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("mutation"))
	calls := 0
	failed := func() (int, agentprotocol.Response) {
		calls++
		return http.StatusInternalServerError, agentprotocol.Response{Error: &agentprotocol.ResponseError{
			Code: agentprotocol.ErrorMutationFailed, Message: "failed",
		}}
	}
	if status, _, replayed, _ := cache.execute("mutation-key", digest, "request-1", now, failed); status != http.StatusInternalServerError || replayed {
		t.Fatalf("failed status=%d replayed=%t", status, replayed)
	}
	succeeded := func() (int, agentprotocol.Response) {
		calls++
		return http.StatusOK, agentprotocol.Response{HostingIdentity: &agentprotocol.HostingIdentityResponse{}}
	}
	if status, _, replayed, _ := cache.execute("mutation-key", digest, "request-2", now, succeeded); status != http.StatusOK || replayed || calls != 2 {
		t.Fatalf("success status=%d replayed=%t calls=%d", status, replayed, calls)
	}
	if _, _, replayed, _ := cache.execute("mutation-key", digest, "request-3", now, succeeded); !replayed || calls != 2 {
		t.Fatalf("cached replay=%t calls=%d", replayed, calls)
	}
}

func TestHandshakeRejectsIncompatibleVersions(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(recorder, rpcRequest(t, handshakeRequest("request-4", "key-4", 2, 2)))
	response := decodeTestResponse(t, recorder.Body, "request-4")
	if recorder.Code != http.StatusUpgradeRequired || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorIncompatibleProtocol {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestInspectCapabilitiesDispatchAndReplay(t *testing.T) {
	t.Parallel()

	inspector := &fakeCapabilityInspector{report: handlerCapabilityReport()}
	handler := NewHandlerWithCapabilityInspector(nil, inspector)
	request := capabilityRequest("cap-request-1", "cap-key-1")
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, rpcRequest(t, request))
	response := decodeTestResponseFor(t, first.Body, request.RequestID, request.Operation)
	if first.Code != http.StatusOK || response.Capabilities == nil ||
		response.Capabilities.Platform.DistributionID != "debian" {
		t.Fatalf("status=%d response=%#v", first.Code, response)
	}

	replay := request
	replay.RequestID = "cap-request-2"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, replay))
	decodeTestResponseFor(t, recorder.Body, replay.RequestID, replay.Operation)
	if inspector.calls.Load() != 1 {
		t.Fatalf("inspector calls = %d, want 1", inspector.calls.Load())
	}
}

func TestInspectCapabilitiesInternalFailureIsTyped(t *testing.T) {
	t.Parallel()

	inspector := &fakeCapabilityInspector{err: errors.New("fixture failure containing internals")}
	request := capabilityRequest("cap-request-3", "cap-key-3")
	recorder := httptest.NewRecorder()
	NewHandlerWithCapabilityInspector(nil, inspector).ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusInternalServerError || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorInternal || strings.Contains(response.Error.Message, "fixture") {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestHostingIdentityMutationDispatchIsCorrelatedAndIdempotent(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	mutator := &fakeIdentityReconciler{reconcile: hostidentity.ReconcileResult{
		GroupCreated: true, UserCreated: true, DirectoryCreated: true,
	}}
	handler := newHandlerWithServices(nil, &fakeCapabilityInspector{report: handlerCapabilityReport()}, mutator)
	request := identityRequest("identity-request-1", "identity-key-1", agentprotocol.OperationReconcileIdentity, identity)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, rpcRequest(t, request))
	response := decodeTestResponseFor(t, first.Body, request.RequestID, request.Operation)
	if first.Code != http.StatusOK || response.HostingIdentity == nil ||
		!response.HostingIdentity.Changed || !response.HostingIdentity.UserCreated {
		t.Fatalf("status=%d response=%#v", first.Code, response)
	}
	replay := request
	replay.RequestID = "identity-request-2"
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, rpcRequest(t, replay))
	decodeTestResponseFor(t, second.Body, replay.RequestID, replay.Operation)
	if mutator.baseCalls.Load() != 1 {
		t.Fatalf("base reconcile calls = %d", mutator.baseCalls.Load())
	}
}

func TestHostingIdentityStageDispatchesWithoutPreparingRuntimeEarly(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	mutator := &fakeIdentityReconciler{reconcile: hostidentity.ReconcileResult{DirectoryCreated: true}}
	handler := newHandlerWithServices(nil, &fakeCapabilityInspector{report: handlerCapabilityReport()}, mutator)
	request := identityRequest("identity-base-request", "identity-base-key", agentprotocol.OperationReconcileIdentity, identity)
	request.ReconcileIdentity.Stage = agentprotocol.HostingIdentityStageBase
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.HostingIdentity == nil || mutator.baseCalls.Load() != 1 ||
		mutator.reconcileCalls.Load() != 0 || mutator.runtimeCalls.Load() != 0 {
		t.Fatalf("base stage status=%d response=%#v calls=%d/%d/%d", recorder.Code, response,
			mutator.baseCalls.Load(), mutator.reconcileCalls.Load(), mutator.runtimeCalls.Load())
	}
}

func TestHostingIdentityMutationFailuresAreStableAndRedacted(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	tests := []struct {
		name       string
		operation  agentprotocol.Operation
		err        error
		wantStatus int
		wantCode   agentprotocol.ErrorCode
		wantReason string
	}{
		{"conflict", agentprotocol.OperationReconcileIdentity, hostidentity.ErrIdentityConflict,
			http.StatusConflict, agentprotocol.ErrorIdentityConflict, ""},
		{"archive", agentprotocol.OperationDeleteIdentity, hostidentity.ErrArchiveRequired,
			http.StatusConflict, agentprotocol.ErrorArchiveRequired, ""},
		{"OCI runtime", agentprotocol.OperationReconcileIdentity, &hostidentity.RuntimeCapabilityError{
			Capability: agentprotocol.Capability{
				Status: agentprotocol.CapabilityUnavailable, ReasonCode: "rootful-podman-socket-enabled",
			},
		}, http.StatusUnprocessableEntity, agentprotocol.ErrorOCIRuntimeUnavailable, "rootful-podman-socket-enabled"},
		{"internal", agentprotocol.OperationReconcileIdentity, errors.New("secret fixture detail"),
			http.StatusInternalServerError, agentprotocol.ErrorMutationFailed, ""},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutator := &fakeIdentityReconciler{err: test.err}
			handler := newHandlerWithServices(nil, &fakeCapabilityInspector{report: handlerCapabilityReport()}, mutator)
			request := identityRequest(
				fmt.Sprintf("identity-error-%d", index), fmt.Sprintf("identity-error-key-%d", index),
				test.operation, identity,
			)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, rpcRequest(t, request))
			response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
			if recorder.Code != test.wantStatus || response.Error == nil || response.Error.Code != test.wantCode ||
				strings.Contains(response.Error.Message, "fixture") {
				t.Fatalf("status=%d response=%#v", recorder.Code, response)
			}
			if test.wantReason != "" && (response.Error.Capability == nil ||
				response.Error.Capability.ReasonCode != test.wantReason) {
				t.Fatalf("capability=%#v, want reason %q", response.Error.Capability, test.wantReason)
			}
		})
	}
}

func TestHostingFilesystemDispatchAndQuotaCapabilityFailure(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	storage := hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID, ByteLimit: 10 << 30, InodeLimit: 100000,
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab",
		ActorKind:   agentprotocol.ActorSystem, AccountID: identity.AccountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "filesystem-request-1",
		IdempotencyKey: "filesystem-key-1", Operation: agentprotocol.OperationReconcileFilesystem,
		Correlation:         &correlation,
		ReconcileFilesystem: &agentprotocol.HostingFilesystemRequest{Storage: storage},
	}
	filesystem := &fakeFilesystemReconciler{reconcile: hostfilesystem.ReconcileResult{
		Layout: hostfilesystem.LayoutResult{
			ProjectAssigned: true, DirectoriesCreated: []string{"applications", "backups"},
		},
		QuotaApplied: true,
		Capability:   agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}}
	handler := newHandlerWithFilesystemServices(
		nil, &fakeCapabilityInspector{report: handlerCapabilityReport()}, &fakeIdentityReconciler{}, filesystem,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.HostingFilesystem == nil ||
		!response.HostingFilesystem.QuotaApplied || filesystem.reconcileCalls.Load() != 1 {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}

	filesystem.err = &hostfilesystem.CapabilityError{Capability: agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnavailable, ReasonCode: "project-quota-not-mounted",
	}}
	request.RequestID, request.IdempotencyKey = "filesystem-request-2", "filesystem-key-2"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusUnprocessableEntity || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorQuotaUnavailable || response.Error.Capability == nil ||
		response.Error.Capability.ReasonCode != "project-quota-not-mounted" {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestHostingResourceDispatchAndCapabilityFailure(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	resources := hostingresources.Spec{
		Identity:        identity,
		CPUQuotaPercent: hostingresources.OptionalUint64{Set: true, Value: 250},
		MemoryBytes:     hostingresources.OptionalUint64{Set: true, Value: 512 << 20},
		SwapBytes:       hostingresources.OptionalUint64{Set: true, Value: 0},
		ProcessLimit:    hostingresources.OptionalUint64{Set: true, Value: 64},
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab",
		ActorKind:   agentprotocol.ActorSystem, AccountID: identity.AccountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "resources-request-1",
		IdempotencyKey: "resources-key-1", Operation: agentprotocol.OperationReconcileResources,
		Correlation:        &correlation,
		ReconcileResources: &agentprotocol.HostingResourcesRequest{Resources: resources},
	}
	resourceReconciler := &fakeResourceReconciler{result: hostresources.Result{
		UnitName:     "stackfort-accounts-200000.slice",
		ControlGroup: "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-200000.slice",
		UnitsChanged: true, LimitsApplied: true,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}}
	handler := newHandlerWithResourceServices(
		nil, &fakeCapabilityInspector{report: handlerCapabilityReport()},
		&fakeIdentityReconciler{}, &fakeFilesystemReconciler{}, resourceReconciler,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.HostingResources == nil ||
		!response.HostingResources.LimitsApplied || resourceReconciler.calls.Load() != 1 {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}

	resourceReconciler.err = &hostresources.CapabilityError{Capability: agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnavailable, ReasonCode: "cgroup-controller-memory-missing",
	}}
	request.RequestID, request.IdempotencyKey = "resources-request-2", "resources-key-2"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusUnprocessableEntity || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorResourceControlUnavailable || response.Error.Capability == nil ||
		response.Error.Capability.ReasonCode != "cgroup-controller-memory-missing" {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestNGINXBaselineDispatchAndTypedFailure(t *testing.T) {
	t.Parallel()
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "nginx-request-1",
		IdempotencyKey: "nginx-key-1", Operation: agentprotocol.OperationReconcileNGINXBaseline,
		Correlation: &correlation, ReconcileNGINXBaseline: &agentprotocol.NGINXBaselineRequest{},
	}
	nginx := &fakeNGINXReconciler{result: hostnginx.Result{
		Changed: true, ConfigurationTested: true, ActivationPerformed: true,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}}
	handler := newHandlerWithNGINXServices(
		nil, &fakeCapabilityInspector{report: handlerCapabilityReport()}, &fakeIdentityReconciler{},
		&fakeFilesystemReconciler{}, &fakeResourceReconciler{}, nginx,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.NGINXBaseline == nil ||
		response.NGINXBaseline.MainConfiguration != nginxbaseline.MainConfiguration ||
		len(response.NGINXBaseline.TrustedProxyHops) != 2 || nginx.calls.Load() != 1 {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}

	nginx.err = &hostnginx.CapabilityError{Capability: agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnavailable, ReasonCode: "nginx-binary-unavailable",
	}}
	request.RequestID, request.IdempotencyKey = "nginx-request-2", "nginx-key-2"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusUnprocessableEntity || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorNGINXUnavailable || response.Error.Capability == nil {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestNGINXActivationDispatchAndTypedHealthFailure(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	name, err := core.NormalizeDomainName("activation.example")
	if err != nil {
		t.Fatal(err)
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
		AccountID: identity.AccountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "activation-request-1",
		IdempotencyKey: "activation-key-1", Operation: agentprotocol.OperationActivateNGINXSites,
		Correlation: &correlation,
		ActivateNGINXSites: &agentprotocol.NGINXActivationRequest{
			Identity: identity, DesiredStateRevisionID: "019c1234-5678-7abc-8def-0123456789aa",
			Domains: []nginxconfig.DomainSpec{{
				Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
				Target: nginxconfig.TargetSpec{Type: core.DomainTargetStatic, DocumentRoot: "public_html"},
			}},
			Options: nginxconfig.DefaultOptions(),
		},
	}
	digest := sha256.Sum256([]byte("rendered"))
	activator := &fakeNGINXSiteActivator{result: hostnginx.ActivationResult{
		Changed: true, ConfigurationTested: true, ReloadPerformed: true, HealthChecked: true,
		ActiveRevisionID:       correlation.OperationID,
		PreviousRevisionID:     "019c1234-5678-7abc-8def-0123456789ac",
		DesiredStateRevisionID: request.ActivateNGINXSites.DesiredStateRevisionID,
		ConfigDigest:           digest, RenderedDomains: 1,
	}}
	handler := newHandlerWithNGINXActivationServices(
		nil, &fakeCapabilityInspector{report: handlerCapabilityReport()}, &fakeIdentityReconciler{},
		&fakeFilesystemReconciler{}, &fakeResourceReconciler{}, &fakeNGINXReconciler{}, activator,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.NGINXActivation == nil ||
		response.NGINXActivation.ConfigDigest != fmt.Sprintf("%x", digest) || activator.calls.Load() != 1 {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}

	activator.err = hostnginx.ErrHealthCheckFailed
	request.RequestID, request.IdempotencyKey = "activation-request-2", "activation-key-2"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusBadGateway || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorNGINXHealthCheck {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestPHPPoolDispatchAndTypedCapabilityFailure(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
		AccountID: identity.AccountID,
	}
	pools := phpruntime.PoolSetSpec{
		Identity: identity, Versions: []string{"8.4"}, MaxChildren: 4, MemoryLimitMiB: 128,
		RetireAbsent: true,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "php-request-1",
		IdempotencyKey: "php-key-1", Operation: agentprotocol.OperationReconcilePHPPools,
		Correlation: &correlation, ReconcilePHPPools: &agentprotocol.PHPPoolSetRequest{Pools: pools},
	}
	phpPools := &fakePHPPoolReconciler{result: hostphp.Result{
		Versions: []string{"8.4"}, Changed: true, Active: true,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}}
	handler := NewHandler(nil)
	handler.phpPools = phpPools
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.PHPPools == nil || !response.PHPPools.Active ||
		phpPools.calls.Load() != 1 || !phpPools.spec.RetireAbsent {
		t.Fatalf("status=%d response=%#v spec=%#v", recorder.Code, response, phpPools.spec)
	}

	phpPools.err = &hostphp.CapabilityError{Capability: agentprotocol.Capability{
		Status: agentprotocol.CapabilityUnsupported, ReasonCode: "php-runtime-version-unsupported",
	}}
	request.RequestID, request.IdempotencyKey = "php-request-2", "php-key-2"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response = decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusUnprocessableEntity || response.Error == nil ||
		response.Error.Code != agentprotocol.ErrorPHPUnavailable || response.Error.Capability == nil ||
		response.Error.Capability.ReasonCode != "php-runtime-version-unsupported" {
		t.Fatalf("status=%d response=%#v", recorder.Code, response)
	}
}

func TestPHPPoolInspectionDispatchReturnsOnlyAggregateStatus(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	memory, cpu, processes := uint64(16<<20), uint64(8_000_000), uint64(2)
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "php-inspect-request-1",
		IdempotencyKey: "php-inspect-key-1", Operation: agentprotocol.OperationInspectPHPPools,
		InspectPHPPools: &agentprotocol.PHPPoolInspectRequest{
			Identity: identity, Versions: []string{"8.4"},
		},
	}
	phpPools := &fakePHPPoolReconciler{inspection: hostphp.Inspection{Pools: []hostphp.PoolInspection{{
		Version: "8.4", State: agentprotocol.PHPPoolActive, MemoryBytes: &memory,
		CPUTimeNanosec: &cpu, Processes: &processes,
	}}}}
	handler := NewHandler(nil)
	handler.phpPools = phpPools
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.PHPPoolInspection == nil ||
		len(response.PHPPoolInspection.Pools) != 1 || phpPools.inspectCalls.Load() != 1 ||
		phpPools.inspectRequest.Identity != identity {
		t.Fatalf("status=%d response=%#v request=%#v", recorder.Code, response, phpPools.inspectRequest)
	}
	encoded := recorder.Body.String()
	for _, forbidden := range []string{"unit", "socket", "controlGroup", "arguments"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("inspection response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestFileListingDispatchReturnsBoundedMetadataOnly(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "file-list-request-1",
		IdempotencyKey: "file-list-key-1", Operation: agentprotocol.OperationListFiles,
		ListFiles: &agentprotocol.FileListRequest{Identity: identity, Path: "public_html", Limit: 100},
	}
	files := &fakeFileBrowser{result: agentprotocol.FileListResponse{
		Path: "public_html", Entries: []agentprotocol.FileEntry{{
			Name: "index.html", Type: agentprotocol.FileEntryRegular, SizeBytes: 128, Mode: 0o640,
			ModifiedAt: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC),
		}},
	}}
	handler := NewHandler(nil)
	handler.files = files
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, request.Operation)
	if recorder.Code != http.StatusOK || response.FileListing == nil || len(response.FileListing.Entries) != 1 ||
		files.calls.Load() != 1 || files.request.Identity != identity {
		t.Fatalf("status=%d response=%#v request=%#v", recorder.Code, response, files.request)
	}
	for _, forbidden := range []string{"homeDirectory", "uid", "content"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("file response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestFileDownloadUsesSeparateBoundedStreamingEndpoint(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	start, end := uint64(6), uint64(9)
	protocolRequest := agentprotocol.FileDownloadRequest{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "file-download-request-1",
		Identity: identity, Path: "public_html/site.txt",
		Range: &agentprotocol.FileDownloadRange{Start: &start, EndInclusive: &end},
	}
	encoded, err := json.Marshal(protocolRequest)
	if err != nil {
		t.Fatal(err)
	}
	downloads := &fakeFileDownloader{result: hostfiles.Download{
		TotalSize: 10, Offset: 6, Length: 4, Partial: true,
		ModifiedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC).Unix(),
		Body:       io.NopCloser(strings.NewReader("fort")),
	}}
	handler := NewHandler(nil)
	handler.downloads = downloads
	handler.downloadSlots = make(chan struct{}, 1)
	request := httptest.NewRequest(http.MethodPost, agentprotocol.FileDownloadEndpoint, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", agentprotocol.MediaType)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "fort" ||
		recorder.Header().Get("Content-Range") != "bytes 6-9/10" || downloads.calls.Load() != 1 ||
		downloads.request.Path != protocolRequest.Path || downloads.request.Identity != identity {
		t.Fatalf("status=%d headers=%v calls=%d request=%#v body=%q",
			recorder.Code, recorder.Header(), downloads.calls.Load(), downloads.request, recorder.Body.String())
	}
}

func TestFileDownloadReturnsTypedUnsatisfiedRange(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	start := uint64(20)
	protocolRequest := agentprotocol.FileDownloadRequest{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "file-download-request-2",
		Identity: identity, Path: "index.html", Range: &agentprotocol.FileDownloadRange{Start: &start},
	}
	encoded, _ := json.Marshal(protocolRequest)
	handler := NewHandler(nil)
	handler.downloads = &fakeFileDownloader{result: hostfiles.Download{TotalSize: 12}, err: hostfiles.ErrRange}
	handler.downloadSlots = make(chan struct{}, 1)
	request := httptest.NewRequest(http.MethodPost, agentprotocol.FileDownloadEndpoint, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", agentprotocol.MediaType)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response, err := agentprotocol.DecodeFileDownloadErrorResponse(recorder.Body, protocolRequest.RequestID)
	if err != nil || recorder.Code != http.StatusRequestedRangeNotSatisfiable ||
		response.Error.Code != agentprotocol.ErrorFileRangeNotSatisfiable ||
		recorder.Header().Get("Content-Range") != "bytes */12" {
		t.Fatalf("status=%d response=%#v headers=%v err=%v", recorder.Code, response, recorder.Header(), err)
	}
}

func TestFileDownloadRejectsWhenStreamingCapacityIsFull(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	protocolRequest := agentprotocol.FileDownloadRequest{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "file-download-request-3",
		Identity: identity, Path: "index.html",
	}
	encoded, _ := json.Marshal(protocolRequest)
	downloads := &fakeFileDownloader{}
	handler := NewHandler(nil)
	handler.downloads = downloads
	handler.downloadSlots = make(chan struct{}, 1)
	handler.downloadSlots <- struct{}{}
	request := httptest.NewRequest(http.MethodPost, agentprotocol.FileDownloadEndpoint, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", agentprotocol.MediaType)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response, err := agentprotocol.DecodeFileDownloadErrorResponse(recorder.Body, protocolRequest.RequestID)
	if err != nil || recorder.Code != http.StatusTooManyRequests ||
		response.Error.Code != agentprotocol.ErrorFileDownloadBusy || downloads.calls.Load() != 0 {
		t.Fatalf("status=%d response=%#v calls=%d err=%v",
			recorder.Code, response, downloads.calls.Load(), err)
	}
}

func TestFileWriteUsesBoundedControlAndRawChunk(t *testing.T) {
	t.Parallel()
	identity := handlerIdentitySpec(t)
	protocolRequest := agentprotocol.FileWriteRequest{ProtocolVersion: agentprotocol.WireVersion,
		RequestID: "file-write-request-1", Action: agentprotocol.FileWriteChunk, Identity: identity,
		UploadID: "019c1234-5678-7abc-8def-0123456789ae", Offset: 4, ChunkLength: 4}
	control, _ := json.Marshal(protocolRequest)
	writes := &fakeFileWriter{result: agentprotocol.FileWriteResult{UploadID: protocolRequest.UploadID,
		Directory: "public_html", Name: "site.txt", SizeBytes: 8, ReceivedBytes: 8,
		CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)}}
	handler := NewHandler(nil)
	handler.writes, handler.writeSlots = writes, make(chan struct{}, 1)
	request := httptest.NewRequest(http.MethodPost, agentprotocol.FileWriteEndpoint,
		io.MultiReader(bytes.NewReader(control), strings.NewReader("fort")))
	request.Header.Set("Content-Type", agentprotocol.FileWriteMediaType)
	request.Header.Set(agentprotocol.FileWriteControlHeader, fmt.Sprintf("%d", len(control)))
	request.ContentLength = int64(len(control) + 4)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response, err := agentprotocol.DecodeFileWriteResponse(recorder.Body, protocolRequest)
	if err != nil || recorder.Code != http.StatusOK || response.Result == nil ||
		response.Result.ReceivedBytes != 8 || writes.calls.Load() != 1 || string(writes.body) != "fort" ||
		writes.request.Identity != identity {
		t.Fatalf("status=%d response=%#v calls=%d body=%q request=%#v err=%v",
			recorder.Code, response, writes.calls.Load(), writes.body, writes.request, err)
	}
}

func TestDatabaseProvisioningDispatchUsesClosedAgentBoundary(t *testing.T) {
	t.Parallel()
	accountID := "019d2ea9-e3f7-7f52-81c7-0aeb932455db"
	operationID := "019d2eaa-42d0-7f52-81c7-0aeb932455db"
	prefix := "sf_019d2ea9e3f77f5281c70aeb932455db_"
	correlation := agentprotocol.AuditCorrelation{
		OperationID: operationID, ActorKind: agentprotocol.ActorSystem, AccountID: accountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: "database-agent-request",
		IdempotencyKey: "database-agent-key", Operation: agentprotocol.OperationProvisionDatabase,
		Correlation: &correlation,
		ProvisionDatabase: &agentprotocol.DatabaseProvisionRequest{
			DatabaseAlias: "application", DatabaseName: prefix + "application",
			UserAlias: "application", Username: prefix + "application", Host: "localhost",
			Password: []byte("0123456789abcdefghijklmn"), CreateUser: true,
			Preset: agentprotocol.DatabaseGrantReadWrite,
		},
	}
	database := &fakeDatabaseReconciler{result: hostdatabase.Result{Changed: true, Active: true}}
	handler := NewHandler(nil)
	handler.databases = database
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rpcRequest(t, request))
	response := decodeTestResponseFor(t, recorder.Body, request.RequestID, agentprotocol.OperationProvisionDatabase)
	if recorder.Code != http.StatusOK || response.Database == nil || !response.Database.Active ||
		database.calls.Load() != 1 || database.operationID != operationID || database.accountID != accountID ||
		database.request.DatabaseName != request.ProvisionDatabase.DatabaseName {
		t.Fatalf("database response=%#v reconciler=%#v", response, database)
	}
}

func TestRPCRejectsMalformedOversizedAndNonAllowlistedRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantCode    agentprotocol.ErrorCode
	}{
		{"media type", "text/plain", `{}`, http.StatusUnsupportedMediaType, agentprotocol.ErrorUnsupportedMediaType},
		{"unknown field", agentprotocol.MediaType, `{"unknown":true}`, http.StatusBadRequest, agentprotocol.ErrorInvalidRequest},
		{"unsupported operation", agentprotocol.MediaType, `{"protocolVersion":1,"requestId":"req-unsupported","idempotencyKey":"key-unsupported","operation":"command.run","handshake":{"minimumVersion":1,"maximumVersion":1,"clientBuild":{"version":"dev","commit":"unknown","buildDate":"unknown"}}}`, http.StatusBadRequest, agentprotocol.ErrorUnsupportedOperation},
		{"oversized", agentprotocol.MediaType, strings.Repeat(" ", agentprotocol.MaxRequestBytes+1), http.StatusRequestEntityTooLarge, agentprotocol.ErrorRequestTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, agentprotocol.Endpoint, strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()
			NewHandler(nil).ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response agentprotocol.Response
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Error == nil || response.Error.Code != test.wantCode {
				t.Fatalf("response=%#v", response)
			}
		})
	}
}

func TestHealthAndUnknownPathsDoNotExposeRPCInternals(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil)
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body.String())
	}
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/run-command", nil))
	if unknown.Code != http.StatusNotFound || strings.Contains(unknown.Body.String(), "command") {
		t.Fatalf("unknown status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

func handshakeRequest(requestID, idempotencyKey string, minimum, maximum int) agentprotocol.Request {
	return agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationHandshake,
		Handshake: &agentprotocol.HandshakeRequest{
			MinimumVersion: minimum, MaximumVersion: maximum, ClientBuild: buildinfo.Current(),
		},
	}
}

func capabilityRequest(requestID, idempotencyKey string) agentprotocol.Request {
	return agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: agentprotocol.OperationInspectCapabilities,
		InspectCapabilities: &agentprotocol.InspectCapabilitiesRequest{},
	}
}

func identityRequest(
	requestID string,
	idempotencyKey string,
	operation agentprotocol.Operation,
	identity hostingidentity.Spec,
) agentprotocol.Request {
	correlation := agentprotocol.AuditCorrelation{
		OperationID: "019c1234-5678-7abc-8def-0123456789ab", ActorKind: agentprotocol.ActorSystem,
		AccountID: identity.AccountID,
	}
	request := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion, RequestID: requestID,
		IdempotencyKey: idempotencyKey, Operation: operation, Correlation: &correlation,
	}
	payload := &agentprotocol.HostingIdentityRequest{Identity: identity}
	if operation == agentprotocol.OperationReconcileIdentity {
		payload.Stage = agentprotocol.HostingIdentityStageBase
		request.ReconcileIdentity = payload
	} else {
		request.DeleteIdentity = payload
	}
	return request
}

func handlerIdentitySpec(t *testing.T) hostingidentity.Spec {
	t.Helper()
	accountID := "019c1234-5678-7abc-8def-0123456789ad"
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	return hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
		GID: hostingidentity.MinimumID, HomeDirectory: home,
	}
}

func rpcRequest(t *testing.T, request agentprotocol.Request) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpRequest := httptest.NewRequest(http.MethodPost, agentprotocol.Endpoint, bytes.NewReader(encoded))
	httpRequest.Header.Set("Content-Type", agentprotocol.MediaType)
	return httpRequest
}

func decodeTestResponse(t *testing.T, reader io.Reader, requestID string) agentprotocol.Response {
	return decodeTestResponseFor(t, reader, requestID, agentprotocol.OperationHandshake)
}

func decodeTestResponseFor(
	t *testing.T,
	reader io.Reader,
	requestID string,
	operation agentprotocol.Operation,
) agentprotocol.Response {
	t.Helper()
	response, err := agentprotocol.DecodeResponse(reader, requestID, operation)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

type fakeCapabilityInspector struct {
	report agentprotocol.CapabilityReport
	err    error
	calls  atomic.Int64
}

type fakeIdentityReconciler struct {
	reconcile      hostidentity.ReconcileResult
	deleted        hostidentity.DeleteResult
	err            error
	reconcileCalls atomic.Int64
	baseCalls      atomic.Int64
	runtimeCalls   atomic.Int64
	deleteCalls    atomic.Int64
}

type fakeFilesystemReconciler struct {
	reconcile      hostfilesystem.ReconcileResult
	document       hostfilesystem.DocumentRootResult
	err            error
	reconcileCalls atomic.Int64
	documentCalls  atomic.Int64
}

type fakeResourceReconciler struct {
	result hostresources.Result
	err    error
	calls  atomic.Int64
}

type fakeNGINXReconciler struct {
	result hostnginx.Result
	err    error
	calls  atomic.Int64
}

type fakeNGINXSiteActivator struct {
	result hostnginx.ActivationResult
	err    error
	calls  atomic.Int64
}

type fakePHPPoolReconciler struct {
	result         hostphp.Result
	inspection     hostphp.Inspection
	err            error
	spec           phpruntime.PoolSetSpec
	inspectRequest agentprotocol.PHPPoolInspectRequest
	calls          atomic.Int64
	inspectCalls   atomic.Int64
}

type fakeFileBrowser struct {
	result  agentprotocol.FileListResponse
	err     error
	request agentprotocol.FileListRequest
	calls   atomic.Int64
}

type fakeFileDownloader struct {
	result  hostfiles.Download
	err     error
	request agentprotocol.FileDownloadRequest
	calls   atomic.Int32
}

type fakeFileWriter struct {
	result  agentprotocol.FileWriteResult
	err     error
	request agentprotocol.FileWriteRequest
	body    []byte
	calls   atomic.Int32
}

type fakeOCIImagePreparer struct {
	result ociimage.Result
	err    error
	calls  atomic.Int64
}

type fakeOCIResourceReconciler struct {
	result ociresources.Result
	err    error
	calls  atomic.Int64
}

func (reconciler *fakeOCIResourceReconciler) Reconcile(
	_ context.Context, _ string, _ ociresources.Spec,
) (ociresources.Result, error) {
	reconciler.calls.Add(1)
	return reconciler.result, reconciler.err
}

func (preparer *fakeOCIImagePreparer) Prepare(
	_ context.Context, _ string, _ ociimage.PrepareSpec,
) (ociimage.Result, error) {
	preparer.calls.Add(1)
	return preparer.result, preparer.err
}

func (writer *fakeFileWriter) Execute(
	_ context.Context, request agentprotocol.FileWriteRequest, body io.Reader,
) (agentprotocol.FileWriteResult, error) {
	writer.calls.Add(1)
	writer.request = request
	writer.body, _ = io.ReadAll(body)
	return writer.result, writer.err
}

func (downloader *fakeFileDownloader) Open(
	_ context.Context, request agentprotocol.FileDownloadRequest,
) (hostfiles.Download, error) {
	downloader.calls.Add(1)
	downloader.request = request
	return downloader.result, downloader.err
}

func (browser *fakeFileBrowser) List(
	_ context.Context,
	request agentprotocol.FileListRequest,
) (agentprotocol.FileListResponse, error) {
	browser.calls.Add(1)
	browser.request = request
	return browser.result, browser.err
}

type fakeDatabaseReconciler struct {
	result        hostdatabase.Result
	err           error
	operationID   string
	accountID     string
	request       agentprotocol.DatabaseProvisionRequest
	rotateRequest agentprotocol.DatabasePasswordRotateRequest
	dropRequest   agentprotocol.DatabaseDropRequest
	calls         atomic.Int64
}

func (reconciler *fakeDatabaseReconciler) RotatePassword(
	_ context.Context,
	operationID, accountID string,
	request agentprotocol.DatabasePasswordRotateRequest,
) (hostdatabase.Result, error) {
	reconciler.calls.Add(1)
	reconciler.operationID, reconciler.accountID, reconciler.rotateRequest = operationID, accountID, request
	return reconciler.result, reconciler.err
}

func (reconciler *fakeDatabaseReconciler) Drop(
	_ context.Context,
	operationID, accountID string,
	request agentprotocol.DatabaseDropRequest,
) (hostdatabase.Result, error) {
	reconciler.calls.Add(1)
	reconciler.operationID, reconciler.accountID, reconciler.dropRequest = operationID, accountID, request
	return reconciler.result, reconciler.err
}

func (reconciler *fakeDatabaseReconciler) Reconcile(
	_ context.Context,
	operationID, accountID string,
	request agentprotocol.DatabaseProvisionRequest,
) (hostdatabase.Result, error) {
	reconciler.calls.Add(1)
	reconciler.operationID, reconciler.accountID, reconciler.request = operationID, accountID, request
	return reconciler.result, reconciler.err
}

func (reconciler *fakePHPPoolReconciler) Inspect(
	_ context.Context,
	request agentprotocol.PHPPoolInspectRequest,
) (hostphp.Inspection, error) {
	reconciler.inspectCalls.Add(1)
	reconciler.inspectRequest = request
	return reconciler.inspection, reconciler.err
}

func (reconciler *fakePHPPoolReconciler) Reconcile(
	_ context.Context,
	spec phpruntime.PoolSetSpec,
) (hostphp.Result, error) {
	reconciler.calls.Add(1)
	reconciler.spec = spec
	return reconciler.result, reconciler.err
}

func (activator *fakeNGINXSiteActivator) Activate(
	context.Context,
	hostnginx.ActivationSpec,
) (hostnginx.ActivationResult, error) {
	activator.calls.Add(1)
	return activator.result, activator.err
}

func (reconciler *fakeNGINXReconciler) Reconcile(context.Context) (hostnginx.Result, error) {
	reconciler.calls.Add(1)
	return reconciler.result, reconciler.err
}

func (reconciler *fakeResourceReconciler) Reconcile(
	context.Context,
	hostingresources.Spec,
) (hostresources.Result, error) {
	reconciler.calls.Add(1)
	return reconciler.result, reconciler.err
}

func (reconciler *fakeFilesystemReconciler) Reconcile(
	context.Context,
	hostingstorage.Spec,
) (hostfilesystem.ReconcileResult, error) {
	reconciler.reconcileCalls.Add(1)
	return reconciler.reconcile, reconciler.err
}

func (reconciler *fakeFilesystemReconciler) EnsureDocumentRoot(
	context.Context,
	hostingidentity.Spec,
	string,
	agentprotocol.DocumentRootAccess,
) (hostfilesystem.DocumentRootResult, error) {
	reconciler.documentCalls.Add(1)
	return reconciler.document, reconciler.err
}

func (reconciler *fakeIdentityReconciler) Reconcile(
	context.Context,
	hostingidentity.Spec,
) (hostidentity.ReconcileResult, error) {
	reconciler.reconcileCalls.Add(1)
	return reconciler.reconcile, reconciler.err
}

func (reconciler *fakeIdentityReconciler) ReconcileBase(
	context.Context,
	hostingidentity.Spec,
) (hostidentity.ReconcileResult, error) {
	reconciler.baseCalls.Add(1)
	return reconciler.reconcile, reconciler.err
}

func (reconciler *fakeIdentityReconciler) ReconcileRuntime(
	context.Context,
	hostingidentity.Spec,
) (hostidentity.ReconcileResult, error) {
	reconciler.runtimeCalls.Add(1)
	return reconciler.reconcile, reconciler.err
}

func (reconciler *fakeIdentityReconciler) Delete(
	context.Context,
	hostingidentity.Spec,
) (hostidentity.DeleteResult, error) {
	reconciler.deleteCalls.Add(1)
	return reconciler.deleted, reconciler.err
}

func (inspector *fakeCapabilityInspector) Inspect(context.Context) (agentprotocol.CapabilityReport, error) {
	inspector.calls.Add(1)
	return inspector.report, inspector.err
}

func handlerCapabilityReport() agentprotocol.CapabilityReport {
	available := agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable}
	report := agentprotocol.CapabilityReport{
		InspectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Platform: agentprotocol.PlatformCapabilities{
			DistributionID: "debian", VersionID: "13", Architecture: "amd64",
			KernelRelease: "6.12.0", Support: available,
		},
		Systemd: available,
		Cgroup: agentprotocol.CgroupCapabilities{
			Version: 2, Unified: available, CPU: available, Memory: available, IO: available, PIDs: available,
		},
		Filesystem: agentprotocol.FilesystemCapabilities{
			Target: agentprotocol.ManagedHostingRoot, MountPoint: "/srv/hosting", Type: "ext4",
			Inspection: available, ProjectQuota: available,
		},
		Security: agentprotocol.SecurityCapabilities{Provider: "apparmor", Mode: "enabled", Enforcement: available},
		OCI: agentprotocol.OCIRuntimeCapabilities{
			Provider: "podman", Version: "5.5.2", ScannerProvider: "trivy", ScannerVersion: "0.74.0",
			Rootless: available, Quadlet: available,
			Network: available, Storage: available, RootfulSocketIsolation: available,
			ImagePreparation: available, ImageScanning: available,
		},
		Ports: []agentprotocol.PortCapability{
			{Port: 80, Network: "tcp", Availability: available},
			{Port: 443, Network: "tcp", Availability: available},
			{Port: 8443, Network: "tcp", Availability: available},
		},
	}
	packageNames := []struct{ key, name string }{
		{"nginx", "nginx"}, {"php-fpm", "php-fpm"}, {"mariadb", "mariadb-server"},
		{"vinyl", "vinyl-cache"}, {"podman", "podman"}, {"netavark", "netavark"},
		{"aardvark-dns", "aardvark-dns"}, {"passt", "passt"}, {"slirp4netns", "slirp4netns"},
		{"fuse-overlayfs", "fuse-overlayfs"}, {"uidmap", "uidmap"}, {"coraza", "stackfort-waf"},
	}
	for _, item := range packageNames {
		report.Packages = append(report.Packages, agentprotocol.PackageCapability{
			Key: item.key, PackageName: item.name, Version: map[bool]string{true: "5.5.2", false: "1"}[item.key == "podman"], Availability: available,
		})
	}
	serviceNames := []struct{ key, unit string }{
		{"nginx", "nginx.service"}, {"php-fpm", "php-fpm.service"}, {"mariadb", "mariadb.service"},
		{"vinyl", "vinyl.service"}, {"podman", "podman.socket"}, {"firewall", "nftables.service"},
		{"stackfort-api", "stackfort-api.service"}, {"stackfort-agent", "stackfort-agent.service"},
	}
	for _, item := range serviceNames {
		report.Services = append(report.Services, agentprotocol.ServiceCapability{
			Key: item.key, Unit: item.unit, LoadState: "loaded", ActiveState: "active",
			SubState: "running", UnitFileState: "enabled", Availability: available,
		})
	}
	return report
}
