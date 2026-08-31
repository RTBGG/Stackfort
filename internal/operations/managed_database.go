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
)

const DatabaseLifecycleKind = "database.lifecycle"

type DatabaseLifecyclePayload struct {
	Action         string                   `json:"action"`
	DatabaseAlias  string                   `json:"databaseAlias"`
	DatabaseID     string                   `json:"databaseId,omitempty"`
	DatabaseUserID string                   `json:"databaseUserId,omitempty"`
	ExistingUserID string                   `json:"existingUserId,omitempty"`
	NewUserAlias   string                   `json:"newUserAlias,omitempty"`
	Preset         core.DatabaseGrantPreset `json:"preset"`
}

type DatabaseLifecycleRepository interface {
	LoadDatabaseProvisioning(
		context.Context, core.ID, core.ID,
	) (core.ManagedDatabaseProvisioning, core.DatabaseCredential, error)
	CompleteDatabaseProvisioning(
		context.Context, core.CompleteDatabaseProvisioningParams,
	) (core.ManagedDatabaseProvisioning, error)
	LoadDatabaseDeletion(context.Context, core.ID, core.ID) (core.ManagedDatabaseDeletion, error)
	CompleteDatabaseDeletion(
		context.Context, core.CompleteDatabaseDeletionParams,
	) (core.ManagedDatabaseDeletion, error)
	LoadDatabaseCredentialRotation(
		context.Context, core.ID, core.ID,
	) (core.ManagedDatabaseCredentialRotation, core.DatabaseCredential, error)
	CompleteDatabaseCredentialRotation(
		context.Context, core.CompleteDatabaseCredentialRotationParams,
	) (core.ManagedDatabaseCredentialRotation, error)
}

type DatabaseLifecycleClient interface {
	ProvisionDatabase(
		context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.DatabaseProvisionRequest,
	) (agentprotocol.DatabaseProvisionResponse, error)
	DropDatabase(
		context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.DatabaseDropRequest,
	) (agentprotocol.DatabaseDropResponse, error)
	RotateDatabasePassword(
		context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.DatabasePasswordRotateRequest,
	) (agentprotocol.DatabasePasswordRotateResponse, error)
}

type DatabaseLifecycleHandler struct {
	repository DatabaseLifecycleRepository
	client     DatabaseLifecycleClient
}

func NewDatabaseLifecycleHandler(
	repository DatabaseLifecycleRepository,
	client DatabaseLifecycleClient,
) (*DatabaseLifecycleHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("database lifecycle handler requires a repository and agent client")
	}
	return &DatabaseLifecycleHandler{repository: repository, client: client}, nil
}

func (handler *DatabaseLifecycleHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != DatabaseLifecycleKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "database.lifecycle_operation_invalid"}
	}
	payload, err := decodeDatabaseLifecyclePayload(operation.Payload)
	if err != nil {
		return nil, &Failure{Code: "database.lifecycle_payload_invalid"}
	}
	if payload.Action == "rotate_user" {
		return handler.runPasswordRotation(ctx, operation, reporter, payload)
	}
	if payload.Action != "provision" {
		return handler.runDeletion(ctx, operation, reporter, payload)
	}
	if err := reporter.Checkpoint(ctx, "loading", 10, "database.lifecycle.loading", nil); err != nil {
		return nil, err
	}
	provisioning, credential, err := handler.repository.LoadDatabaseProvisioning(
		ctx, *operation.AccountID, operation.ID,
	)
	if err != nil {
		return nil, classifyDatabaseRepositoryFailure(err)
	}
	defer clear(credential.Password)
	createUser := provisioning.DatabaseUser.Status == core.ManagedDatabasePending
	if provisioning.Database.Alias != payload.DatabaseAlias || provisioning.Grant.Preset != payload.Preset ||
		(createUser && (payload.NewUserAlias != provisioning.DatabaseUser.Alias || payload.ExistingUserID != "")) ||
		(!createUser && (payload.ExistingUserID != string(provisioning.DatabaseUser.ID) || payload.NewUserAlias != "")) {
		return nil, &Failure{Code: "database.lifecycle_state_conflict"}
	}
	password := credential.Password
	if !createUser {
		password = nil
	}
	if err := reporter.Checkpoint(ctx, "provisioning", 35, "database.lifecycle.provisioning", map[string]any{
		"databaseId": string(provisioning.Database.ID),
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.ProvisionDatabase(
		ctx, "database-provision-"+string(operation.ID), databaseLifecycleCorrelation(operation),
		agentprotocol.DatabaseProvisionRequest{
			DatabaseAlias: provisioning.Database.Alias, DatabaseName: provisioning.Database.PhysicalName,
			UserAlias: provisioning.DatabaseUser.Alias, Username: provisioning.DatabaseUser.PhysicalName,
			Host: provisioning.DatabaseUser.Host, Password: password, CreateUser: createUser,
			Preset: agentprotocol.DatabaseGrantPreset(provisioning.Grant.Preset),
		},
	)
	if err != nil {
		return nil, classifyDatabaseAgentFailure(err)
	}
	if !response.Active || response.DatabaseName != provisioning.Database.PhysicalName ||
		response.Username != provisioning.DatabaseUser.PhysicalName || response.Host != provisioning.DatabaseUser.Host ||
		response.Preset != agentprotocol.DatabaseGrantPreset(provisioning.Grant.Preset) {
		return nil, &Failure{Code: "database.agent_response_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "recording", 85, "database.lifecycle.recording", nil); err != nil {
		return nil, err
	}
	provisioning, err = handler.repository.CompleteDatabaseProvisioning(ctx, core.CompleteDatabaseProvisioningParams{
		OperationID: operation.ID, AccountID: *operation.AccountID,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyDatabaseRepositoryFailure(err)
	}
	return map[string]any{
		"databaseId":     string(provisioning.Database.ID),
		"databaseUserId": string(provisioning.DatabaseUser.ID),
		"grantId":        string(provisioning.Grant.ID),
	}, nil
}

func (handler *DatabaseLifecycleHandler) runPasswordRotation(
	ctx context.Context,
	operation core.Operation,
	reporter ProgressReporter,
	payload DatabaseLifecyclePayload,
) (map[string]any, error) {
	if err := reporter.Checkpoint(ctx, "loading", 10, "database.password_rotation.loading", nil); err != nil {
		return nil, err
	}
	rotation, credential, err := handler.repository.LoadDatabaseCredentialRotation(
		ctx, *operation.AccountID, operation.ID,
	)
	if err != nil {
		return nil, classifyDatabaseRepositoryFailure(err)
	}
	defer clear(credential.Password)
	if payload.DatabaseUserID != string(rotation.DatabaseUser.ID) ||
		rotation.DatabaseUser.Status != core.ManagedDatabaseActive {
		return nil, &Failure{Code: "database.lifecycle_state_conflict"}
	}
	if rotation.AppliedAt != nil {
		return map[string]any{"databaseUserId": string(rotation.DatabaseUser.ID)}, nil
	}
	if err := reporter.Checkpoint(ctx, "rotating", 35, "database.password_rotation.rotating", map[string]any{
		"databaseUserId": string(rotation.DatabaseUser.ID),
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.RotateDatabasePassword(
		ctx, "database-password-rotate-"+string(operation.ID), databaseLifecycleCorrelation(operation),
		agentprotocol.DatabasePasswordRotateRequest{
			UserAlias: rotation.DatabaseUser.Alias, Username: credential.Username,
			Host: credential.Host, Password: credential.Password,
		},
	)
	if err != nil {
		return nil, classifyDatabaseAgentFailure(err)
	}
	if !response.Active || response.Username != credential.Username || response.Host != credential.Host {
		return nil, &Failure{Code: "database.agent_response_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "recording", 85, "database.password_rotation.recording", nil); err != nil {
		return nil, err
	}
	rotation, err = handler.repository.CompleteDatabaseCredentialRotation(
		ctx, core.CompleteDatabaseCredentialRotationParams{
			OperationID: operation.ID, AccountID: *operation.AccountID,
			ActorID: operation.ActorID, RequestID: operation.RequestID,
		},
	)
	if err != nil {
		return nil, classifyDatabaseRepositoryFailure(err)
	}
	return map[string]any{"databaseUserId": string(rotation.DatabaseUser.ID)}, nil
}

func (handler *DatabaseLifecycleHandler) runDeletion(
	ctx context.Context,
	operation core.Operation,
	reporter ProgressReporter,
	payload DatabaseLifecyclePayload,
) (map[string]any, error) {
	if err := reporter.Checkpoint(ctx, "loading", 10, "database.lifecycle.loading", nil); err != nil {
		return nil, err
	}
	deletion, err := handler.repository.LoadDatabaseDeletion(ctx, *operation.AccountID, operation.ID)
	if err != nil {
		return nil, classifyDatabaseRepositoryFailure(err)
	}
	request := agentprotocol.DatabaseDropRequest{Grants: []agentprotocol.DatabaseDropGrant{}}
	targetID := ""
	if deletion.Kind == core.DatabaseDeletionDatabase && deletion.Database != nil {
		targetID = string(deletion.Database.ID)
		if payload.Action != "drop_database" || payload.DatabaseID != targetID || payload.DatabaseUserID != "" {
			return nil, &Failure{Code: "database.lifecycle_state_conflict"}
		}
		request.Kind = agentprotocol.DatabaseDropDatabase
		request.Alias, request.Name = deletion.Database.Alias, deletion.Database.PhysicalName
		for index, grant := range deletion.Grants {
			if grant.RevokedAt != nil {
				continue
			}
			if index >= len(deletion.GrantUsers) {
				return nil, &Failure{Code: "database.lifecycle_state_conflict"}
			}
			user := deletion.GrantUsers[index]
			request.Grants = append(request.Grants, agentprotocol.DatabaseDropGrant{
				UserAlias: user.Alias, Username: user.PhysicalName, Host: user.Host,
				Preset: agentprotocol.DatabaseGrantPreset(grant.Preset),
			})
		}
	} else if deletion.Kind == core.DatabaseDeletionUser && deletion.User != nil {
		targetID = string(deletion.User.ID)
		if payload.Action != "drop_user" || payload.DatabaseUserID != targetID || payload.DatabaseID != "" {
			return nil, &Failure{Code: "database.lifecycle_state_conflict"}
		}
		request.Kind = agentprotocol.DatabaseDropUser
		request.Alias, request.Name, request.Host = deletion.User.Alias, deletion.User.PhysicalName, deletion.User.Host
	} else {
		return nil, &Failure{Code: "database.lifecycle_state_conflict"}
	}
	if err := reporter.Checkpoint(ctx, "deleting", 35, "database.lifecycle.deleting", map[string]any{
		"targetId": targetID,
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.DropDatabase(
		ctx, "database-drop-"+string(operation.ID), databaseLifecycleCorrelation(operation), request,
	)
	if err != nil {
		return nil, classifyDatabaseAgentFailure(err)
	}
	if !response.Deleted || response.Kind != request.Kind || response.Name != request.Name {
		return nil, &Failure{Code: "database.agent_response_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "recording", 85, "database.lifecycle.recording", nil); err != nil {
		return nil, err
	}
	deletion, err = handler.repository.CompleteDatabaseDeletion(ctx, core.CompleteDatabaseDeletionParams{
		OperationID: operation.ID, AccountID: *operation.AccountID,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyDatabaseRepositoryFailure(err)
	}
	return map[string]any{"targetId": targetID, "targetKind": string(deletion.Kind)}, nil
}

func decodeDatabaseLifecyclePayload(value map[string]any) (DatabaseLifecyclePayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return DatabaseLifecyclePayload{}, err
	}
	var payload DatabaseLifecyclePayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return DatabaseLifecyclePayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return DatabaseLifecyclePayload{}, errors.New("database lifecycle payload contains trailing JSON")
	}
	if payload.Action == "drop_database" {
		if payload.DatabaseID == "" || payload.DatabaseUserID != "" || payload.DatabaseAlias != "" ||
			payload.ExistingUserID != "" || payload.NewUserAlias != "" || payload.Preset != "" {
			return DatabaseLifecyclePayload{}, errors.New("database deletion payload is invalid")
		}
		if _, err := core.ParseID(payload.DatabaseID); err != nil {
			return DatabaseLifecyclePayload{}, errors.New("database deletion target ID is invalid")
		}
		return payload, nil
	}
	if payload.Action == "drop_user" {
		if payload.DatabaseUserID == "" || payload.DatabaseID != "" || payload.DatabaseAlias != "" ||
			payload.ExistingUserID != "" || payload.NewUserAlias != "" || payload.Preset != "" {
			return DatabaseLifecyclePayload{}, errors.New("database user deletion payload is invalid")
		}
		if _, err := core.ParseID(payload.DatabaseUserID); err != nil {
			return DatabaseLifecyclePayload{}, errors.New("database user deletion target ID is invalid")
		}
		return payload, nil
	}
	if payload.Action == "rotate_user" {
		if payload.DatabaseUserID == "" || payload.DatabaseID != "" || payload.DatabaseAlias != "" ||
			payload.ExistingUserID != "" || payload.NewUserAlias != "" || payload.Preset != "" {
			return DatabaseLifecyclePayload{}, errors.New("database password rotation payload is invalid")
		}
		if _, err := core.ParseID(payload.DatabaseUserID); err != nil {
			return DatabaseLifecyclePayload{}, errors.New("database password rotation target ID is invalid")
		}
		return payload, nil
	}
	if payload.Action != "provision" || payload.DatabaseAlias == "" || payload.DatabaseID != "" ||
		payload.DatabaseUserID != "" ||
		(payload.Preset != core.DatabaseGrantReadOnly && payload.Preset != core.DatabaseGrantReadWrite) ||
		(payload.ExistingUserID == "") == (payload.NewUserAlias == "") {
		return DatabaseLifecyclePayload{}, errors.New("database lifecycle payload is invalid")
	}
	if payload.ExistingUserID != "" {
		if _, err := core.ParseID(payload.ExistingUserID); err != nil {
			return DatabaseLifecyclePayload{}, errors.New("database lifecycle existing user ID is invalid")
		}
	}
	return payload, nil
}

func databaseLifecycleCorrelation(operation core.Operation) agentprotocol.AuditCorrelation {
	correlation := agentprotocol.AuditCorrelation{
		OperationID: string(operation.ID), ActorKind: agentprotocol.ActorSystem,
		AccountID: string(*operation.AccountID),
	}
	if operation.ActorID != nil {
		correlation.ActorKind = agentprotocol.ActorIdentity
		correlation.ActorID = string(*operation.ActorID)
	}
	return correlation
}

func classifyDatabaseRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "database.lifecycle_state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "database.lifecycle_state_conflict"}
	case errors.Is(err, core.ErrSecretStorageUnavailable):
		return &Failure{Code: "database.secret_storage_unavailable"}
	default:
		return &Failure{Code: "database.lifecycle_state_unavailable", Retryable: true}
	}
}

func classifyDatabaseAgentFailure(err error) error {
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "database.agent_unavailable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorDatabaseConflict:
		return &Failure{Code: "database.host_state_conflict"}
	case agentprotocol.ErrorDatabaseValidation:
		return &Failure{Code: "database.host_intent_invalid"}
	case agentprotocol.ErrorDatabaseUnavailable:
		return &Failure{Code: "database.service_unavailable", Retryable: true}
	default:
		return &Failure{Code: "database.host_mutation_failed", Retryable: true}
	}
}
