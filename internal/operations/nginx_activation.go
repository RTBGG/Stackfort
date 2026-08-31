// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
)

const NGINXActivationKind = string(agentprotocol.OperationActivateNGINXSites)

type NGINXActivationPayload struct {
	DesiredStateRevisionID string                   `json:"desiredStateRevisionId"`
	Domains                []nginxconfig.DomainSpec `json:"domains"`
	Options                nginxconfig.Options      `json:"options"`
}

type NGINXActivationRepository interface {
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	RecordAppliedStateRevision(
		context.Context,
		core.RecordAppliedStateRevisionParams,
	) (core.AppliedStateRevision, error)
}

type NGINXActivationClient interface {
	ActivateNGINXSiteSpecs(
		context.Context,
		string,
		agentprotocol.AuditCorrelation,
		hostingidentity.Spec,
		string,
		[]nginxconfig.DomainSpec,
		nginxconfig.Options,
	) (agentprotocol.NGINXActivationResponse, error)
}

// NGINXActivationHandler joins the durable operation lease with the agent's
// crash-safe host transaction. Its immutable payload makes replay independent
// of later domain-row changes.
type NGINXActivationHandler struct {
	repository NGINXActivationRepository
	client     NGINXActivationClient
}

type nginxActivationProgress struct {
	validating int64
	activating int64
	recording  int64
}

var defaultNGINXActivationProgress = nginxActivationProgress{
	validating: 10,
	activating: 35,
	recording:  90,
}

func NewNGINXActivationHandler(
	repository NGINXActivationRepository,
	client NGINXActivationClient,
) (*NGINXActivationHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("NGINX activation handler requires a repository and agent client")
	}
	return &NGINXActivationHandler{repository: repository, client: client}, nil
}

// NewNGINXActivationPayload returns a detached JSON-compatible operation
// object. The operation repository will apply its own size and secret checks.
func NewNGINXActivationPayload(
	desiredStateRevisionID core.ID,
	domains []nginxconfig.DomainSpec,
	options nginxconfig.Options,
) (map[string]any, error) {
	if _, err := core.ParseID(string(desiredStateRevisionID)); err != nil {
		return nil, fmt.Errorf("desired-state revision ID: %w", err)
	}
	encoded, err := json.Marshal(NGINXActivationPayload{
		DesiredStateRevisionID: string(desiredStateRevisionID), Domains: domains, Options: options,
	})
	if err != nil {
		return nil, fmt.Errorf("encode NGINX activation payload: %w", err)
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode NGINX activation payload: %w", err)
	}
	return result, nil
}

func (handler *NGINXActivationHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != NGINXActivationKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "nginx.activation_operation_invalid"}
	}
	payload, err := decodeNGINXActivationPayload(operation.Payload)
	if err != nil {
		return nil, &Failure{Code: "nginx.activation_payload_invalid"}
	}
	return handler.runPayload(ctx, operation, reporter, payload)
}

// runPayload is shared with compound lifecycle operations after they have
// durably captured an immutable desired-state document. The caller remains
// responsible for validating its own operation kind.
func (handler *NGINXActivationHandler) runPayload(
	ctx context.Context,
	operation core.Operation,
	reporter ProgressReporter,
	payload NGINXActivationPayload,
) (map[string]any, error) {
	return handler.runPayloadWithProgress(ctx, operation, reporter, payload, defaultNGINXActivationProgress)
}

func (handler *NGINXActivationHandler) runPayloadWithProgress(
	ctx context.Context,
	operation core.Operation,
	reporter ProgressReporter,
	payload NGINXActivationPayload,
	progress nginxActivationProgress,
) (map[string]any, error) {
	if operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "nginx.activation_operation_invalid"}
	}
	if payload.DesiredStateRevisionID == "" || progress.validating < 0 ||
		progress.validating > progress.activating || progress.activating > progress.recording ||
		progress.recording > 100 {
		return nil, &Failure{Code: "nginx.activation_payload_invalid"}
	}
	if err := reporter.Checkpoint(
		ctx, "validating", progress.validating, "nginx.activation.validating", map[string]any{"domains": len(payload.Domains)},
	); err != nil {
		return nil, err
	}

	account, err := handler.repository.GetHostingAccount(ctx, *operation.AccountID)
	if err != nil {
		return nil, classifyNGINXRepositoryFailure(err)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil || identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "nginx.activation_identity_invalid"}
	}
	if _, err := core.ParseID(payload.DesiredStateRevisionID); err != nil {
		return nil, &Failure{Code: "nginx.activation_payload_invalid"}
	}
	rendered, err := nginxconfig.RenderSpecs(identity, payload.Domains, payload.Options)
	if err != nil {
		return nil, &Failure{Code: "nginx.activation_payload_invalid"}
	}
	correlation := agentprotocol.AuditCorrelation{
		OperationID: string(operation.ID), ActorKind: agentprotocol.ActorSystem,
		AccountID: string(*operation.AccountID),
	}
	if operation.ActorID != nil {
		correlation.ActorKind = agentprotocol.ActorIdentity
		correlation.ActorID = string(*operation.ActorID)
	}
	idempotencyKey := "nginx-activation-" + string(operation.ID)
	protocolRequest := agentprotocol.Request{
		ProtocolVersion: agentprotocol.WireVersion,
		RequestID:       string(operation.ID),
		IdempotencyKey:  idempotencyKey,
		Operation:       agentprotocol.OperationActivateNGINXSites,
		Correlation:     &correlation,
		ActivateNGINXSites: &agentprotocol.NGINXActivationRequest{
			Identity: identity, DesiredStateRevisionID: payload.DesiredStateRevisionID,
			Domains: payload.Domains, Options: payload.Options,
		},
	}
	encodedRequest, encodeErr := json.Marshal(protocolRequest)
	if encodeErr != nil || len(encodedRequest) > agentprotocol.MaxRequestBytes ||
		agentprotocol.ValidateRequest(protocolRequest) != nil {
		return nil, &Failure{Code: "nginx.activation_payload_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "activating", progress.activating, "nginx.activation.applying", nil); err != nil {
		return nil, err
	}

	response, err := handler.client.ActivateNGINXSiteSpecs(
		ctx,
		idempotencyKey,
		correlation,
		identity,
		payload.DesiredStateRevisionID,
		payload.Domains,
		payload.Options,
	)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, classifyNGINXAgentFailure(err)
	}
	digest, err := validateNGINXActivationResult(
		operation, payload, rendered.Digest, rendered.RenderedDomains, response,
	)
	if err != nil {
		return nil, &Failure{Code: "nginx.activation_response_invalid", Retryable: true}
	}
	if err := reporter.Checkpoint(ctx, "recording", progress.recording, "nginx.activation.recording", nil); err != nil {
		return nil, err
	}

	desiredRevisionID, _ := core.ParseID(payload.DesiredStateRevisionID)
	applied, err := handler.repository.RecordAppliedStateRevision(ctx, core.RecordAppliedStateRevisionParams{
		AccountID:              *operation.AccountID,
		DesiredStateRevisionID: desiredRevisionID,
		OperationID:            &operation.ID,
		ConfigDigest:           digest,
		ActorID:                operation.ActorID,
		RequestID:              operation.RequestID,
	})
	if err != nil {
		return nil, classifyNGINXRepositoryFailure(err)
	}
	return map[string]any{
		"activeRevisionId":       response.ActiveRevisionID,
		"appliedStateRevisionId": string(applied.ID),
		"changed":                response.Changed,
		"configDigest":           response.ConfigDigest,
		"desiredStateRevisionId": response.DesiredStateRevisionID,
		"recoveryPerformed":      response.RecoveryPerformed,
		"renderedDomains":        response.RenderedDomains,
	}, nil
}

func decodeNGINXActivationPayload(value map[string]any) (NGINXActivationPayload, error) {
	if value == nil {
		return NGINXActivationPayload{}, errors.New("missing payload")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return NGINXActivationPayload{}, err
	}
	var payload NGINXActivationPayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return NGINXActivationPayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return NGINXActivationPayload{}, errors.New("payload contains trailing JSON")
	}
	return payload, nil
}

func validateNGINXActivationResult(
	operation core.Operation,
	payload NGINXActivationPayload,
	expectedDigest [32]byte,
	expectedDomains int,
	response agentprotocol.NGINXActivationResponse,
) ([]byte, error) {
	if response.ActiveRevisionID != string(operation.ID) ||
		response.DesiredStateRevisionID != payload.DesiredStateRevisionID ||
		!response.ConfigurationTested || !response.HealthChecked ||
		response.RenderedDomains != expectedDomains || response.RenderedDomains < 0 ||
		response.RenderedDomains > nginxconfig.MaximumDomains {
		return nil, errors.New("NGINX activation response correlation is invalid")
	}
	digest, err := hex.DecodeString(response.ConfigDigest)
	if err != nil || len(digest) != 32 || response.ConfigDigest != hex.EncodeToString(digest) {
		return nil, errors.New("NGINX activation response digest is invalid")
	}
	if !bytes.Equal(digest, expectedDigest[:]) {
		return nil, errors.New("NGINX activation response digest differs from typed control-plane render")
	}
	return digest, nil
}

func classifyNGINXAgentFailure(err error) error {
	if errors.Is(err, agentprotocol.ErrInvalidRequest) {
		return &Failure{Code: "nginx.activation_rejected"}
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "nginx.agent_unreachable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorNGINXConflict, agentprotocol.ErrorNGINXValidation,
		agentprotocol.ErrorIdempotencyConflict, agentprotocol.ErrorInvalidRequest:
		return &Failure{Code: "nginx.activation_rejected"}
	case agentprotocol.ErrorNGINXUnavailable:
		return &Failure{Code: "nginx.capability_unavailable", Retryable: true}
	case agentprotocol.ErrorNGINXHealthCheck:
		return &Failure{Code: "nginx.activation_health_failed", Retryable: true}
	default:
		return &Failure{Code: "nginx.activation_failed", Retryable: true}
	}
}

func classifyNGINXRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "nginx.activation_state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "nginx.activation_state_conflict"}
	default:
		return &Failure{Code: "nginx.activation_state_unavailable", Retryable: true}
	}
}
