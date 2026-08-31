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
	"github.com/RTBGG/stackfort/internal/ociimage"
)

const (
	OCIImagePrepareKind          = "oci.image.prepare"
	ociImagePrepareSchemaVersion = 1
)

// OCIImagePreparePayload is the immutable application revision captured when
// an image-preparation operation is queued. It contains no command arguments,
// host paths, scanner switches, or engine endpoint.
type OCIImagePreparePayload struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Spec          ociimage.PrepareSpec `json:"spec"`
}

type OCIImagePrepareRepository interface {
	OCIImagePrepareSpec(context.Context, core.ID, core.ID) (ociimage.PrepareSpec, error)
	RecordOCIImageArtifact(context.Context, core.RecordOCIImageArtifactParams) (core.OCIApplication, core.OCIImageArtifact, error)
}

type OCIImagePrepareClient interface {
	PrepareOCIImage(context.Context, string, agentprotocol.AuditCorrelation, ociimage.PrepareSpec) (agentprotocol.OCIImagePrepareResponse, error)
}

type OCIImagePrepareHandler struct {
	repository OCIImagePrepareRepository
	client     OCIImagePrepareClient
}

func NewOCIImagePreparePayload(payload OCIImagePreparePayload) (map[string]any, error) {
	if payload.SchemaVersion == 0 {
		payload.SchemaVersion = ociImagePrepareSchemaVersion
	}
	if err := validateOCIImagePreparePayload(payload); err != nil {
		return nil, err
	}
	return structToObject(payload)
}

func NewOCIImagePrepareHandler(
	repository OCIImagePrepareRepository,
	client OCIImagePrepareClient,
) (*OCIImagePrepareHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("OCI image prepare handler requires repository and agent client")
	}
	return &OCIImagePrepareHandler{repository: repository, client: client}, nil
}

func (handler *OCIImagePrepareHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != OCIImagePrepareKind || operation.AccountID == nil || operation.ActorID == nil || reporter == nil {
		return nil, &Failure{Code: "oci_image.operation_invalid"}
	}
	payload, err := decodeOCIImagePreparePayload(operation.Payload)
	if err != nil || validateOCIImagePreparePayload(payload) != nil ||
		payload.Spec.Identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "oci_image.payload_invalid"}
	}
	applicationID, err := core.ParseID(payload.Spec.ApplicationID)
	if err != nil {
		return nil, &Failure{Code: "oci_image.payload_invalid"}
	}
	current, err := handler.repository.OCIImagePrepareSpec(ctx, *operation.AccountID, applicationID)
	if err != nil {
		return nil, classifyOCIImageRepositoryFailure(err)
	}
	currentDigest, currentErr := ociimage.SemanticDigest(current)
	payloadDigest, payloadErr := ociimage.SemanticDigest(payload.Spec)
	if currentErr != nil || payloadErr != nil || currentDigest != payloadDigest {
		return nil, &Failure{Code: "oci_image.revision_superseded"}
	}
	if err := reporter.Checkpoint(ctx, "preparing", 20, "oci_image.prepare.running", map[string]any{
		"applicationId": payload.Spec.ApplicationID, "revision": payload.Spec.Revision,
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.PrepareOCIImage(
		ctx, string(operation.ID)+"-image", lifecycleCorrelation(operation), payload.Spec,
	)
	if err != nil {
		return nil, classifyOCIImageAgentFailure(err)
	}
	if err := reporter.Checkpoint(ctx, "recording", 90, "oci_image.prepare.recording", nil); err != nil {
		return nil, err
	}
	application, artifact, err := handler.repository.RecordOCIImageArtifact(ctx, core.RecordOCIImageArtifactParams{
		AccountID: *operation.AccountID, ApplicationID: applicationID,
		ExpectedRevision: payload.Spec.Revision, Result: response.Result,
		ActorID: *operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyOCIImageRepositoryFailure(err)
	}
	return map[string]any{
		"accountId": string(*operation.AccountID), "applicationId": string(application.ID),
		"revision": artifact.ApplicationRevision, "imageDigest": artifact.Result.ImageDigest,
		"policyVersion": artifact.Result.PolicyVersion, "scannerProvider": artifact.Result.ScannerProvider,
		"scannerVersion": artifact.Result.ScannerVersion,
	}, nil
}

func decodeOCIImagePreparePayload(value map[string]any) (OCIImagePreparePayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return OCIImagePreparePayload{}, err
	}
	var payload OCIImagePreparePayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return OCIImagePreparePayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return OCIImagePreparePayload{}, errors.New("OCI image payload contains trailing JSON")
	}
	return payload, nil
}

func validateOCIImagePreparePayload(payload OCIImagePreparePayload) error {
	if payload.SchemaVersion != ociImagePrepareSchemaVersion {
		return errors.New("unsupported OCI image prepare payload schema")
	}
	return ociimage.ValidateSpec(payload.Spec)
}

func classifyOCIImageRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "oci_image.state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "oci_image.revision_superseded"}
	default:
		return &Failure{Code: "oci_image.state_unavailable", Retryable: true}
	}
}

func classifyOCIImageAgentFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "oci_image.agent_unavailable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorOCIImageInvalid:
		return &Failure{Code: "oci_image.source_rejected"}
	case agentprotocol.ErrorOCIImageRejected:
		return &Failure{Code: "oci_image.scan_rejected"}
	case agentprotocol.ErrorOCIImageUnavailable:
		return &Failure{Code: "oci_image.host_unavailable", Retryable: true}
	default:
		return &Failure{Code: "oci_image.agent_unavailable", Retryable: remote.StatusCode >= 500}
	}
}
