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
	"github.com/RTBGG/stackfort/internal/ociresources"
)

const (
	OCIResourceReconcileKind          = "oci.resources.reconcile"
	ociResourceReconcileSchemaVersion = 1
)

// OCIResourceReconcilePayload captures only immutable metadata references.
// It cannot carry secret plaintext, host paths, ports, capabilities, devices,
// namespaces, or arbitrary Podman arguments.
type OCIResourceReconcilePayload struct {
	SchemaVersion int               `json:"schemaVersion"`
	Spec          ociresources.Spec `json:"spec"`
}

type OCIResourceReconcileRepository interface {
	OCIResourcePrepareSpec(context.Context, core.ID, core.ID) (ociresources.Spec, error)
	RecordOCIResourceArtifact(context.Context, core.RecordOCIResourceArtifactParams) (core.OCIApplication, core.OCIResourceArtifact, error)
}

type OCIResourceReconcileClient interface {
	ReconcileOCIResources(context.Context, string, agentprotocol.AuditCorrelation, ociresources.Spec) (agentprotocol.OCIResourceReconcileResponse, error)
}

type OCIResourceReconcileHandler struct {
	repository OCIResourceReconcileRepository
	client     OCIResourceReconcileClient
}

func NewOCIResourceReconcilePayload(payload OCIResourceReconcilePayload) (map[string]any, error) {
	if payload.SchemaVersion == 0 {
		payload.SchemaVersion = ociResourceReconcileSchemaVersion
	}
	if err := validateOCIResourceReconcilePayload(payload); err != nil {
		return nil, err
	}
	return structToObject(payload)
}

func NewOCIResourceReconcileHandler(
	repository OCIResourceReconcileRepository,
	client OCIResourceReconcileClient,
) (*OCIResourceReconcileHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("OCI private-resource handler requires repository and agent client")
	}
	return &OCIResourceReconcileHandler{repository: repository, client: client}, nil
}

func (handler *OCIResourceReconcileHandler) Run(
	ctx context.Context, claimed core.ClaimedOperation, reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != OCIResourceReconcileKind || operation.AccountID == nil || operation.ActorID == nil || reporter == nil {
		return nil, &Failure{Code: "oci_resources.operation_invalid"}
	}
	payload, err := decodeOCIResourceReconcilePayload(operation.Payload)
	if err != nil || validateOCIResourceReconcilePayload(payload) != nil ||
		payload.Spec.Identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "oci_resources.payload_invalid"}
	}
	applicationID, err := core.ParseID(payload.Spec.ApplicationID)
	if err != nil {
		return nil, &Failure{Code: "oci_resources.payload_invalid"}
	}
	current, err := handler.repository.OCIResourcePrepareSpec(ctx, *operation.AccountID, applicationID)
	if err != nil {
		return nil, classifyOCIResourceRepositoryFailure(err)
	}
	currentDigest, currentErr := ociresources.SemanticDigest(current)
	payloadDigest, payloadErr := ociresources.SemanticDigest(payload.Spec)
	if currentErr != nil || payloadErr != nil || currentDigest != payloadDigest {
		return nil, &Failure{Code: "oci_resources.revision_superseded"}
	}
	if err := reporter.Checkpoint(ctx, "reconciling", 20, "oci_resources.reconcile.running", map[string]any{
		"applicationId": payload.Spec.ApplicationID, "revision": payload.Spec.Revision,
		"environmentReferenceCount": len(payload.Spec.EnvironmentReferences), "volumeCount": len(payload.Spec.Volumes),
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.ReconcileOCIResources(
		ctx, string(operation.ID)+"-resources", lifecycleCorrelation(operation), payload.Spec,
	)
	if err != nil {
		return nil, classifyOCIResourceAgentFailure(err)
	}
	if err := reporter.Checkpoint(ctx, "recording", 90, "oci_resources.reconcile.recording", nil); err != nil {
		return nil, err
	}
	application, artifact, err := handler.repository.RecordOCIResourceArtifact(ctx, core.RecordOCIResourceArtifactParams{
		AccountID: *operation.AccountID, ApplicationID: applicationID,
		ExpectedRevision: payload.Spec.Revision, Result: response.Result,
		ActorID: *operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyOCIResourceRepositoryFailure(err)
	}
	return map[string]any{
		"accountId": string(*operation.AccountID), "applicationId": string(application.ID),
		"revision": artifact.ApplicationRevision, "resourceDigest": artifact.Result.ResourceDigest,
		"policyVersion": artifact.Result.PolicyVersion, "networkName": artifact.Result.NetworkName,
		"environmentReferenceCount": artifact.Result.EnvironmentReferenceCount,
		"volumeCount":               artifact.Result.VolumeCount,
	}, nil
}

func decodeOCIResourceReconcilePayload(value map[string]any) (OCIResourceReconcilePayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return OCIResourceReconcilePayload{}, err
	}
	var payload OCIResourceReconcilePayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return OCIResourceReconcilePayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return OCIResourceReconcilePayload{}, errors.New("OCI private-resource payload contains trailing JSON")
	}
	return payload, nil
}

func validateOCIResourceReconcilePayload(payload OCIResourceReconcilePayload) error {
	if payload.SchemaVersion != ociResourceReconcileSchemaVersion {
		return errors.New("unsupported OCI private-resource payload schema")
	}
	return ociresources.Validate(payload.Spec)
}

func classifyOCIResourceRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "oci_resources.state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "oci_resources.revision_superseded"}
	default:
		return &Failure{Code: "oci_resources.state_unavailable", Retryable: true}
	}
}

func classifyOCIResourceAgentFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "oci_resources.agent_unavailable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorOCIResourceInvalid:
		return &Failure{Code: "oci_resources.intent_rejected"}
	case agentprotocol.ErrorOCIResourceConflict:
		return &Failure{Code: "oci_resources.host_conflict"}
	case agentprotocol.ErrorOCIResourceUnavailable:
		return &Failure{Code: "oci_resources.host_unavailable", Retryable: true}
	default:
		return &Failure{Code: "oci_resources.agent_unavailable", Retryable: remote.StatusCode >= 500}
	}
}
