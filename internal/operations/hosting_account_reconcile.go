// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
)

const (
	HostingAccountReconcileKind = "hosting.account.reconcile"
	hostingAccountSchemaVersion = 1
)

type HostingAccountReconcilePayload struct {
	SchemaVersion      int                   `json:"schemaVersion"`
	Identity           hostingidentity.Spec  `json:"identity"`
	FilesystemRevision int64                 `json:"filesystemRevision"`
	Storage            hostingstorage.Spec   `json:"storage"`
	ResourceRevision   int64                 `json:"resourceRevision"`
	Resources          hostingresources.Spec `json:"resources"`
}

type HostingAccountReconcileRepository interface {
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	ConfirmHostingFilesystemApplied(context.Context, core.ConfirmHostingFilesystemAppliedParams) (core.HostingFilesystemState, error)
	ConfirmHostingResourcesApplied(context.Context, core.ConfirmHostingResourcesAppliedParams) (core.HostingResourceState, error)
	MarkHostingUnixIdentityReconciled(context.Context, core.HostingAccountLifecycleParams) (core.HostingAccount, error)
}

type HostingAccountReconcileClient interface {
	ReconcileHostingIdentity(context.Context, string, agentprotocol.AuditCorrelation, hostingidentity.Spec) (agentprotocol.HostingIdentityResponse, error)
	ReconcileHostingFilesystem(context.Context, string, agentprotocol.AuditCorrelation, hostingstorage.Spec) (agentprotocol.HostingFilesystemResponse, error)
	ReconcileHostingResources(context.Context, string, agentprotocol.AuditCorrelation, hostingresources.Spec) (agentprotocol.HostingResourcesResponse, error)
}

type HostingAccountReconcileHandler struct {
	repository HostingAccountReconcileRepository
	client     HostingAccountReconcileClient
}

func NewHostingAccountReconcilePayload(payload HostingAccountReconcilePayload) (map[string]any, error) {
	if payload.SchemaVersion == 0 {
		payload.SchemaVersion = hostingAccountSchemaVersion
	}
	if err := validateHostingAccountReconcilePayload(payload); err != nil {
		return nil, err
	}
	return structToObject(payload)
}

func NewHostingAccountReconcileHandler(
	repository HostingAccountReconcileRepository,
	client HostingAccountReconcileClient,
) (*HostingAccountReconcileHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("hosting account reconcile handler requires repository and agent client")
	}
	return &HostingAccountReconcileHandler{repository: repository, client: client}, nil
}

func (handler *HostingAccountReconcileHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != HostingAccountReconcileKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "hosting_account.operation_invalid"}
	}
	payload, err := decodeHostingAccountReconcilePayload(operation.Payload)
	if err != nil || validateHostingAccountReconcilePayload(payload) != nil ||
		payload.Identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "hosting_account.payload_invalid"}
	}
	account, err := handler.repository.GetHostingAccount(ctx, *operation.AccountID)
	if err != nil || account.ID != *operation.AccountID ||
		account.UnixIdentity.AccountID != *operation.AccountID ||
		account.UnixIdentity.Username != payload.Identity.Username ||
		account.UnixIdentity.UID != payload.Identity.UID || account.UnixIdentity.GID != payload.Identity.GID {
		return nil, classifyHostingAccountRepositoryFailure(err)
	}
	if account.UnixIdentity.HomeDirectory != payload.Identity.HomeDirectory {
		return nil, &Failure{Code: "hosting_account.state_invalid"}
	}
	correlation := lifecycleCorrelation(operation)
	if err := reporter.Checkpoint(ctx, "identity", 10, "hosting_account.reconcile.identity", nil); err != nil {
		return nil, err
	}
	if _, err := handler.client.ReconcileHostingIdentity(
		ctx, string(operation.ID)+"-identity", correlation, payload.Identity,
	); err != nil {
		return nil, classifyHostingAccountAgentFailure(err)
	}
	if err := reporter.Checkpoint(ctx, "filesystem", 35, "hosting_account.reconcile.filesystem", nil); err != nil {
		return nil, err
	}
	filesystem, err := handler.client.ReconcileHostingFilesystem(
		ctx, string(operation.ID)+"-filesystem", correlation, payload.Storage,
	)
	if err != nil || filesystem.ProjectID != payload.Storage.ProjectID {
		return nil, classifyHostingAccountAgentFailure(err)
	}
	if err := reporter.Checkpoint(ctx, "resources", 65, "hosting_account.reconcile.resources", nil); err != nil {
		return nil, err
	}
	resources, err := handler.client.ReconcileHostingResources(
		ctx, string(operation.ID)+"-resources", correlation, payload.Resources,
	)
	if err != nil || resources.UID != payload.Identity.UID {
		return nil, classifyHostingAccountAgentFailure(err)
	}
	if err := reporter.Checkpoint(ctx, "confirming", 90, "hosting_account.reconcile.confirming", nil); err != nil {
		return nil, err
	}
	if _, err := handler.repository.ConfirmHostingFilesystemApplied(ctx, core.ConfirmHostingFilesystemAppliedParams{
		AccountID: *operation.AccountID, ExpectedRevision: payload.FilesystemRevision,
		OperationID: operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
	}); err != nil {
		return nil, classifyHostingAccountRepositoryFailure(err)
	}
	if _, err := handler.repository.ConfirmHostingResourcesApplied(ctx, core.ConfirmHostingResourcesAppliedParams{
		AccountID: *operation.AccountID, ExpectedRevision: payload.ResourceRevision,
		OperationID: operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
	}); err != nil {
		return nil, classifyHostingAccountRepositoryFailure(err)
	}
	if _, err := handler.repository.MarkHostingUnixIdentityReconciled(ctx, core.HostingAccountLifecycleParams{
		AccountID: *operation.AccountID, ActorID: operation.ActorID,
		OperationID: &operation.ID, RequestID: operation.RequestID,
	}); err != nil {
		return nil, classifyHostingAccountRepositoryFailure(err)
	}
	return map[string]any{
		"accountId": string(*operation.AccountID), "filesystemRevision": payload.FilesystemRevision,
		"resourceRevision": payload.ResourceRevision, "unixIdentityReconciled": true,
	}, nil
}

func decodeHostingAccountReconcilePayload(value map[string]any) (HostingAccountReconcilePayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return HostingAccountReconcilePayload{}, err
	}
	var payload HostingAccountReconcilePayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return HostingAccountReconcilePayload{}, err
	}
	return payload, nil
}

func validateHostingAccountReconcilePayload(payload HostingAccountReconcilePayload) error {
	if payload.SchemaVersion != hostingAccountSchemaVersion || payload.FilesystemRevision < 1 ||
		payload.ResourceRevision < 1 || hostingidentity.Validate(payload.Identity) != nil ||
		hostingstorage.Validate(payload.Storage) != nil || hostingresources.Validate(payload.Resources) != nil ||
		payload.Storage.Identity != payload.Identity || payload.Resources.Identity != payload.Identity {
		return errors.New("invalid hosting account reconcile payload")
	}
	return nil
}

func classifyHostingAccountAgentFailure(err error) error {
	if err == nil {
		return &Failure{Code: "hosting_account.agent_response_invalid", Retryable: true}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var remote *agentclient.RemoteError
	if errors.As(err, &remote) && remote.StatusCode >= 400 && remote.StatusCode < 500 {
		return &Failure{Code: "hosting_account.host_conflict"}
	}
	return &Failure{Code: "hosting_account.agent_unavailable", Retryable: true}
}

func classifyHostingAccountRepositoryFailure(err error) error {
	switch {
	case err == nil:
		return &Failure{Code: "hosting_account.state_invalid"}
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "hosting_account.state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "hosting_account.reconcile_superseded"}
	default:
		return &Failure{Code: "hosting_account.state_unavailable", Retryable: true}
	}
}
