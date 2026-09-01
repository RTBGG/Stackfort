// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/ociresources"
)

func TestOCIResourceHandlerFencesIntentAndPersistsMetadata(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	actorID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dd")
	operationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455de")
	imageSpec := testOCIImagePrepareSpec(string(accountID), string(applicationID))
	spec := ociresources.Spec{
		Identity: imageSpec.Identity, ApplicationID: string(applicationID), Revision: 1,
		EnvironmentReferences: []ociresources.EnvironmentReference{{
			SecretID: "019d2eaa-52d0-7f52-8ac7-0aeb932455df", Environment: "TOKEN", Generation: 2,
		}},
	}
	payload, err := NewOCIResourceReconcilePayload(OCIResourceReconcilePayload{Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || strings.Contains(strings.ToLower(string(encoded)), "secret") {
		t.Fatalf("durable OCI resource payload contains secret-named material: %s / %v", encoded, err)
	}
	result, err := ociresources.ResultFor(spec, true)
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeOCIResourceRepository{spec: spec}
	client := &fakeOCIResourceClient{response: agentprotocol.OCIResourceReconcileResponse{Result: result}}
	handler, err := NewOCIResourceReconcileHandler(repository, client)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeNGINXReporter{}
	operation := core.Operation{
		ID: operationID, AccountID: &accountID, ActorID: &actorID, Kind: OCIResourceReconcileKind,
		RequestID: "oci-resource-request", Payload: payload,
	}
	response, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || client.key != string(operationID)+"-resources" ||
		client.correlation.AccountID != string(accountID) || client.correlation.ActorID != string(actorID) {
		t.Fatalf("client call = %#v / %q / %d", client.correlation, client.key, client.calls)
	}
	if repository.recorded == nil || repository.recorded.ExpectedRevision != 1 ||
		repository.recorded.Result != result || repository.recorded.ActorID != actorID {
		t.Fatalf("recorded = %#v", repository.recorded)
	}
	if !reflect.DeepEqual(reporter.stages, []string{"reconciling", "recording"}) ||
		response["resourceDigest"] != result.ResourceDigest ||
		response["environmentReferenceCount"] != result.EnvironmentReferenceCount {
		t.Fatalf("stages/result = %#v / %#v", reporter.stages, response)
	}
}

func TestOCIResourceHandlerRejectsStaleAndForeignHostState(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	actorID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dd")
	imageSpec := testOCIImagePrepareSpec(string(accountID), string(applicationID))
	spec := ociresources.Spec{Identity: imageSpec.Identity, ApplicationID: string(applicationID), Revision: 1}
	payload, _ := NewOCIResourceReconcilePayload(OCIResourceReconcilePayload{Spec: spec})
	operation := core.Operation{
		ID: "019d2eaa-62d0-7f52-8ac7-0aeb932455de", AccountID: &accountID,
		ActorID: &actorID, Kind: OCIResourceReconcileKind, Payload: payload,
	}
	stale := spec
	stale.Revision++
	handler, _ := NewOCIResourceReconcileHandler(&fakeOCIResourceRepository{spec: stale}, &fakeOCIResourceClient{})
	if _, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{}); failureCode(err) != "oci_resources.revision_superseded" {
		t.Fatalf("stale error = %v", err)
	}
	handler, _ = NewOCIResourceReconcileHandler(&fakeOCIResourceRepository{spec: spec}, &fakeOCIResourceClient{err: &agentclient.RemoteError{
		StatusCode: 409, Code: agentprotocol.ErrorOCIResourceConflict,
	}})
	if _, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{}); failureCode(err) != "oci_resources.host_conflict" {
		t.Fatalf("host conflict error = %v", err)
	}
	payload["hostPath"] = "/etc"
	if _, err := decodeOCIResourceReconcilePayload(payload); err == nil {
		t.Fatal("caller-controlled host path was accepted")
	}
}

type fakeOCIResourceRepository struct {
	spec     ociresources.Spec
	recorded *core.RecordOCIResourceArtifactParams
}

func (repository *fakeOCIResourceRepository) OCIResourcePrepareSpec(
	context.Context, core.ID, core.ID,
) (ociresources.Spec, error) {
	return repository.spec, nil
}

func (repository *fakeOCIResourceRepository) RecordOCIResourceArtifact(
	_ context.Context, params core.RecordOCIResourceArtifactParams,
) (core.OCIApplication, core.OCIResourceArtifact, error) {
	repository.recorded = &params
	return core.OCIApplication{ID: params.ApplicationID}, core.OCIResourceArtifact{
		ApplicationID: params.ApplicationID, AccountID: params.AccountID,
		ApplicationRevision: params.ExpectedRevision, Result: params.Result,
	}, nil
}

type fakeOCIResourceClient struct {
	response    agentprotocol.OCIResourceReconcileResponse
	err         error
	calls       int
	key         string
	correlation agentprotocol.AuditCorrelation
}

func (client *fakeOCIResourceClient) ReconcileOCIResources(
	_ context.Context, key string, correlation agentprotocol.AuditCorrelation, _ ociresources.Spec,
) (agentprotocol.OCIResourceReconcileResponse, error) {
	client.calls++
	client.key = key
	client.correlation = correlation
	return client.response, client.err
}
