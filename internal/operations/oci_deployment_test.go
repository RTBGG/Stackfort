// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
)

func TestOCIDeploymentHandlerLoadsPlaintextOnlyForAgentCall(t *testing.T) {
	t.Parallel()
	accountID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455db")
	applicationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dc")
	actorID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455dd")
	operationID := core.ID("019d2eaa-62d0-7f52-8ac7-0aeb932455de")
	imageSpec := testOCIImagePrepareSpec(string(accountID), string(applicationID))
	spec, err := ocideployment.Normalize(ocideployment.Spec{
		Identity: imageSpec.Identity, ApplicationID: string(applicationID), Revision: 1,
		ImageDigest: "sha256:" + strings.Repeat("b", 64), ResourceDigest: "sha256:" + strings.Repeat("c", 64),
		InternalPort: 8080, LoopbackPort: 23456,
		Health: ociapps.HealthCheck{Kind: ociapps.HealthHTTP, Path: "/health", IntervalSeconds: 10, TimeoutSeconds: 3, Retries: 3},
		EnvironmentReferences: []ocideployment.EnvironmentReference{{
			ValueID: "019d2eaa-62d0-7f52-8ac7-0aeb932455df", Environment: "TOKEN", Generation: 2,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := NewOCIDeploymentLifecyclePayload(OCIDeploymentLifecyclePayload{Action: ocideployment.ActionDeploy, Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil || strings.Contains(string(encoded), "deployment-plaintext") {
		t.Fatalf("durable deployment payload contains plaintext: %s / %v", encoded, err)
	}
	deployment, err := ocideployment.ResultFor(spec, true)
	if err != nil {
		t.Fatal(err)
	}
	repository := &fakeOCIDeploymentRepository{spec: spec, values: []ocideployment.EnvironmentValue{{
		ValueID: spec.EnvironmentReferences[0].ValueID, Environment: "TOKEN", Generation: 2, Value: "deployment-plaintext",
	}}}
	client := &fakeOCIDeploymentClient{response: agentprotocol.OCIDeploymentResponse{Result: ocideployment.LifecycleResult{
		Action: ocideployment.ActionDeploy, State: ocideployment.StateActive, Deployment: &deployment,
		Healthy: true, Changed: true,
	}}}
	handler, err := NewOCIDeploymentLifecycleHandler(repository, client)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &fakeNGINXReporter{}
	operation := core.Operation{ID: operationID, AccountID: &accountID, ActorID: &actorID,
		Kind: OCIDeploymentLifecycleKind, RequestID: "oci-deployment-request", Payload: payload}
	response, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if repository.loads != 1 || !client.plaintextSeen || len(client.request.Values) != 1 || client.request.Values[0].Value != "" {
		t.Fatalf("plaintext boundary = loads %d, seen %t, retained request %#v", repository.loads, client.plaintextSeen, client.request)
	}
	if repository.recorded == nil || repository.recorded.Result != deployment ||
		repository.recorded.ExpectedRevision != spec.Revision {
		t.Fatalf("recorded deployment = %#v", repository.recorded)
	}
	if !reflect.DeepEqual(reporter.stages, []string{"reconciling", "recording"}) ||
		response["deploymentDigest"] != deployment.DeploymentDigest || response["healthy"] != true {
		t.Fatalf("stages/result = %#v / %#v", reporter.stages, response)
	}
}

type fakeOCIDeploymentRepository struct {
	spec     ocideployment.Spec
	values   []ocideployment.EnvironmentValue
	loads    int
	recorded *core.RecordOCIDeploymentArtifactParams
}

func (repository *fakeOCIDeploymentRepository) CurrentOCIDeploymentSpec(
	context.Context, core.ID, core.ID,
) (ocideployment.Spec, error) {
	return repository.spec, nil
}

func (repository *fakeOCIDeploymentRepository) LoadOCIDeploymentValues(
	context.Context, core.ID, core.ID, ocideployment.Spec,
) ([]ocideployment.EnvironmentValue, error) {
	repository.loads++
	return append([]ocideployment.EnvironmentValue(nil), repository.values...), nil
}

func (repository *fakeOCIDeploymentRepository) RecordOCIDeploymentArtifact(
	_ context.Context, params core.RecordOCIDeploymentArtifactParams,
) (core.OCIApplication, core.OCIDeploymentArtifact, error) {
	repository.recorded = &params
	return core.OCIApplication{ID: params.ApplicationID}, core.OCIDeploymentArtifact{Result: params.Result}, nil
}

func (repository *fakeOCIDeploymentRepository) ChangeOCIApplicationDeploymentStatus(
	context.Context, core.ChangeOCIApplicationDeploymentStatusParams,
) (core.OCIApplication, error) {
	return core.OCIApplication{}, nil
}

func (repository *fakeOCIDeploymentRepository) EnsureOCIApplicationRemovable(context.Context, core.ID, core.ID) error {
	return nil
}

type fakeOCIDeploymentClient struct {
	response      agentprotocol.OCIDeploymentResponse
	request       ocideployment.Request
	plaintextSeen bool
}

func (client *fakeOCIDeploymentClient) ReconcileOCIDeployment(
	_ context.Context, _ string, _ agentprotocol.AuditCorrelation, request ocideployment.Request,
) (agentprotocol.OCIDeploymentResponse, error) {
	client.request = request
	client.plaintextSeen = len(request.Values) == 1 && request.Values[0].Value == "deployment-plaintext"
	return client.response, nil
}
