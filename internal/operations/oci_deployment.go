// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/ocideployment"
)

const (
	OCIDeploymentLifecycleKind          = "oci.deployment.lifecycle"
	ociDeploymentLifecycleSchemaVersion = 1
)

// OCIDeploymentLifecyclePayload is safe for durable persistence. Environment
// plaintext is deliberately loaded only after this payload is revision-fenced
// immediately before the local agent call.
type OCIDeploymentLifecyclePayload struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Action        ocideployment.Action `json:"action"`
	Spec          ocideployment.Spec   `json:"spec"`
}

type OCIDeploymentLifecycleRepository interface {
	CurrentOCIDeploymentSpec(context.Context, core.ID, core.ID) (ocideployment.Spec, error)
	LoadOCIDeploymentValues(context.Context, core.ID, core.ID, ocideployment.Spec) ([]ocideployment.EnvironmentValue, error)
	RecordOCIDeploymentArtifact(context.Context, core.RecordOCIDeploymentArtifactParams) (core.OCIApplication, core.OCIDeploymentArtifact, error)
	ChangeOCIApplicationDeploymentStatus(context.Context, core.ChangeOCIApplicationDeploymentStatusParams) (core.OCIApplication, error)
	EnsureOCIApplicationRemovable(context.Context, core.ID, core.ID) error
}

type OCIDeploymentLifecycleClient interface {
	ReconcileOCIDeployment(context.Context, string, agentprotocol.AuditCorrelation,
		ocideployment.Request) (agentprotocol.OCIDeploymentResponse, error)
}

type OCIDeploymentLifecycleHandler struct {
	repository OCIDeploymentLifecycleRepository
	client     OCIDeploymentLifecycleClient
}

func NewOCIDeploymentLifecyclePayload(payload OCIDeploymentLifecyclePayload) (map[string]any, error) {
	if payload.SchemaVersion == 0 {
		payload.SchemaVersion = ociDeploymentLifecycleSchemaVersion
	}
	if err := validateOCIDeploymentLifecyclePayload(payload); err != nil {
		return nil, err
	}
	return structToObject(payload)
}

func NewOCIDeploymentLifecycleHandler(repository OCIDeploymentLifecycleRepository,
	client OCIDeploymentLifecycleClient) (*OCIDeploymentLifecycleHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("OCI deployment lifecycle requires repository and agent client")
	}
	return &OCIDeploymentLifecycleHandler{repository: repository, client: client}, nil
}

func (handler *OCIDeploymentLifecycleHandler) Run(ctx context.Context, claimed core.ClaimedOperation,
	reporter ProgressReporter) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != OCIDeploymentLifecycleKind || operation.AccountID == nil || operation.ActorID == nil || reporter == nil {
		return nil, &Failure{Code: "oci_deployment.operation_invalid"}
	}
	payload, err := decodeOCIDeploymentLifecyclePayload(operation.Payload)
	if err != nil || validateOCIDeploymentLifecyclePayload(payload) != nil ||
		payload.Spec.Identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "oci_deployment.payload_invalid"}
	}
	applicationID, err := core.ParseID(payload.Spec.ApplicationID)
	if err != nil {
		return nil, &Failure{Code: "oci_deployment.payload_invalid"}
	}
	current, err := handler.repository.CurrentOCIDeploymentSpec(ctx, *operation.AccountID, applicationID)
	if err != nil {
		return nil, classifyOCIDeploymentRepositoryFailure(err)
	}
	currentDigest, currentErr := ocideployment.SemanticDigest(current)
	payloadDigest, payloadErr := ocideployment.SemanticDigest(payload.Spec)
	if currentErr != nil || payloadErr != nil || currentDigest != payloadDigest {
		return nil, &Failure{Code: "oci_deployment.revision_superseded"}
	}
	if payload.Action == ocideployment.ActionRemove || payload.Action == ocideployment.ActionSuspend {
		if err := handler.repository.EnsureOCIApplicationRemovable(ctx, *operation.AccountID, applicationID); err != nil {
			return nil, classifyOCIDeploymentRepositoryFailure(err)
		}
	}
	if err := reporter.Checkpoint(ctx, "reconciling", 20, "oci_deployment.reconcile.running", map[string]any{
		"applicationId": payload.Spec.ApplicationID, "revision": payload.Spec.Revision, "action": payload.Action,
	}); err != nil {
		return nil, err
	}
	request := ocideployment.Request{Action: payload.Action, Spec: payload.Spec}
	if payload.Action == ocideployment.ActionDeploy || payload.Action == ocideployment.ActionRollback {
		request.Values, err = handler.repository.LoadOCIDeploymentValues(ctx, *operation.AccountID, applicationID, payload.Spec)
		if err != nil {
			return nil, classifyOCIDeploymentRepositoryFailure(err)
		}
		defer core.ClearOCIDeploymentValues(request.Values)
	}
	response, err := handler.client.ReconcileOCIDeployment(ctx, string(operation.ID)+"-deployment",
		lifecycleCorrelation(operation), request)
	if err != nil {
		return nil, classifyOCIDeploymentAgentFailure(err)
	}
	if ocideployment.ValidateLifecycleResult(response.Result) != nil || response.Result.Action != payload.Action {
		return nil, &Failure{Code: "oci_deployment.response_invalid", Retryable: true}
	}
	if err := reporter.Checkpoint(ctx, "recording", 90, "oci_deployment.reconcile.recording", nil); err != nil {
		return nil, err
	}
	switch payload.Action {
	case ocideployment.ActionDeploy:
		if response.Result.Deployment == nil {
			return nil, &Failure{Code: "oci_deployment.response_invalid", Retryable: true}
		}
		_, _, err = handler.repository.RecordOCIDeploymentArtifact(ctx, core.RecordOCIDeploymentArtifactParams{
			AccountID: *operation.AccountID, ApplicationID: applicationID, ExpectedRevision: payload.Spec.Revision,
			Result: *response.Result.Deployment, ActorID: *operation.ActorID, RequestID: operation.RequestID})
	case ocideployment.ActionSuspend:
		_, err = handler.repository.ChangeOCIApplicationDeploymentStatus(ctx, core.ChangeOCIApplicationDeploymentStatusParams{
			AccountID: *operation.AccountID, ApplicationID: applicationID, Expected: core.OCIApplicationActive,
			Status: core.OCIApplicationSuspended, ActorID: *operation.ActorID, OperationID: operation.ID, RequestID: operation.RequestID})
	case ocideployment.ActionResume:
		_, err = handler.repository.ChangeOCIApplicationDeploymentStatus(ctx, core.ChangeOCIApplicationDeploymentStatusParams{
			AccountID: *operation.AccountID, ApplicationID: applicationID, Expected: core.OCIApplicationSuspended,
			Status: core.OCIApplicationActive, ActorID: *operation.ActorID, OperationID: operation.ID, RequestID: operation.RequestID})
	case ocideployment.ActionRemove:
		_, err = handler.repository.ChangeOCIApplicationDeploymentStatus(ctx, core.ChangeOCIApplicationDeploymentStatusParams{
			AccountID: *operation.AccountID, ApplicationID: applicationID, Expected: core.OCIApplicationActive,
			Status: core.OCIApplicationDeleted, ActorID: *operation.ActorID, OperationID: operation.ID, RequestID: operation.RequestID})
		if errors.Is(err, core.ErrConflict) {
			_, err = handler.repository.ChangeOCIApplicationDeploymentStatus(ctx, core.ChangeOCIApplicationDeploymentStatusParams{
				AccountID: *operation.AccountID, ApplicationID: applicationID, Expected: core.OCIApplicationSuspended,
				Status: core.OCIApplicationDeleted, ActorID: *operation.ActorID, OperationID: operation.ID, RequestID: operation.RequestID})
		}
	case ocideployment.ActionRollback:
		// Rollback re-converges the last control-plane-approved deployment and
		// intentionally leaves application state/revision unchanged.
	default:
		err = core.ErrInvalidInput
	}
	if err != nil {
		return nil, classifyOCIDeploymentRepositoryFailure(err)
	}
	result := map[string]any{"accountId": string(*operation.AccountID), "applicationId": payload.Spec.ApplicationID,
		"revision": payload.Spec.Revision, "action": payload.Action, "state": response.Result.State,
		"healthy": response.Result.Healthy, "changed": response.Result.Changed, "reused": response.Result.Reused}
	if response.Result.Deployment != nil {
		result["deploymentDigest"] = response.Result.Deployment.DeploymentDigest
		result["quadletDigest"] = response.Result.Deployment.QuadletDigest
		result["loopbackPort"] = response.Result.Deployment.LoopbackPort
	}
	return result, nil
}

func decodeOCIDeploymentLifecyclePayload(value map[string]any) (OCIDeploymentLifecyclePayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return OCIDeploymentLifecyclePayload{}, err
	}
	var payload OCIDeploymentLifecyclePayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return OCIDeploymentLifecyclePayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return OCIDeploymentLifecyclePayload{}, errors.New("OCI deployment payload contains trailing JSON")
	}
	return payload, nil
}

func validateOCIDeploymentLifecyclePayload(payload OCIDeploymentLifecyclePayload) error {
	if payload.SchemaVersion != ociDeploymentLifecycleSchemaVersion || ocideployment.Validate(payload.Spec) != nil {
		return errors.New("unsupported OCI deployment lifecycle payload")
	}
	return ocideployment.ValidateRequest(ocideployment.Request{Action: payload.Action, Spec: payload.Spec,
		Values: deploymentPlaceholderValues(payload)})
}

func deploymentPlaceholderValues(payload OCIDeploymentLifecyclePayload) []ocideployment.EnvironmentValue {
	if payload.Action != ocideployment.ActionDeploy && payload.Action != ocideployment.ActionRollback {
		return nil
	}
	values := make([]ocideployment.EnvironmentValue, len(payload.Spec.EnvironmentReferences))
	for index, reference := range payload.Spec.EnvironmentReferences {
		values[index] = ocideployment.EnvironmentValue{ValueID: reference.ValueID, Environment: reference.Environment,
			Generation: reference.Generation, Value: "x"}
	}
	return values
}

func classifyOCIDeploymentRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "oci_deployment.state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "oci_deployment.revision_superseded"}
	default:
		return &Failure{Code: "oci_deployment.state_unavailable", Retryable: true}
	}
}

func classifyOCIDeploymentAgentFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "oci_deployment.agent_unavailable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorOCIDeploymentInvalid:
		return &Failure{Code: "oci_deployment.intent_rejected"}
	case agentprotocol.ErrorOCIDeploymentConflict:
		return &Failure{Code: "oci_deployment.host_conflict"}
	case agentprotocol.ErrorOCIDeploymentUnhealthy:
		return &Failure{Code: "oci_deployment.health_failed"}
	case agentprotocol.ErrorOCIDeploymentUnavailable:
		return &Failure{Code: "oci_deployment.agent_unavailable", Retryable: true}
	default:
		return &Failure{Code: "oci_deployment.agent_unavailable", Retryable: remote.StatusCode >= 500}
	}
}
