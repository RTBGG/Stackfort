// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
)

func TestDatabaseLifecycleRunsPasswordRotationBeforePromotingEnvelope(t *testing.T) {
	t.Parallel()
	accountID := core.ID("0198b935-b600-7000-8000-000000000601")
	operationID := core.ID("0198b935-b600-7000-8000-000000000602")
	userID := core.ID("0198b935-b600-7000-8000-000000000603")
	actorID := core.ID("0198b935-b600-7000-8000-000000000604")
	password := []byte("0123456789abcdefghijklmnopqrstuv")
	repository := &databaseLifecycleTestRepository{
		rotation: core.ManagedDatabaseCredentialRotation{
			Operation: core.Operation{ID: operationID},
			DatabaseUser: core.ManagedDatabaseUser{
				ID: userID, AccountID: accountID, Alias: "application",
				PhysicalName: "sf_0198b935b60070008000000000000601_application",
				Host:         "localhost", Status: core.ManagedDatabaseActive,
			},
		},
		credential: core.DatabaseCredential{
			AccountID: accountID, UserID: userID,
			Username: "sf_0198b935b60070008000000000000601_application",
			Host:     "localhost", Password: append([]byte(nil), password...),
		},
	}
	client := &databaseLifecycleTestClient{}
	handler, err := NewDatabaseLifecycleHandler(repository, client)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &databaseLifecycleTestReporter{}
	result, err := handler.Run(t.Context(), core.ClaimedOperation{Operation: core.Operation{
		ID: operationID, AccountID: &accountID, ActorID: &actorID,
		Kind: DatabaseLifecycleKind, RequestID: "database-rotation-worker",
		Payload: map[string]any{"action": "rotate_user", "databaseUserId": string(userID)},
	}}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if result["databaseUserId"] != string(userID) || repository.completeCalls != 1 || client.rotateCalls != 1 ||
		client.request.Username != repository.credential.Username ||
		!bytes.Equal(client.password, password) ||
		len(reporter.stages) != 3 || reporter.stages[0] != "loading" ||
		reporter.stages[1] != "rotating" || reporter.stages[2] != "recording" {
		t.Fatalf("result=%#v repository=%#v client=%#v reporter=%#v", result, repository, client, reporter)
	}
}

type databaseLifecycleTestRepository struct {
	rotation      core.ManagedDatabaseCredentialRotation
	credential    core.DatabaseCredential
	completeCalls int
}

func (*databaseLifecycleTestRepository) LoadDatabaseProvisioning(context.Context, core.ID, core.ID) (core.ManagedDatabaseProvisioning, core.DatabaseCredential, error) {
	return core.ManagedDatabaseProvisioning{}, core.DatabaseCredential{}, nil
}
func (*databaseLifecycleTestRepository) CompleteDatabaseProvisioning(context.Context, core.CompleteDatabaseProvisioningParams) (core.ManagedDatabaseProvisioning, error) {
	return core.ManagedDatabaseProvisioning{}, nil
}
func (*databaseLifecycleTestRepository) LoadDatabaseDeletion(context.Context, core.ID, core.ID) (core.ManagedDatabaseDeletion, error) {
	return core.ManagedDatabaseDeletion{}, nil
}
func (*databaseLifecycleTestRepository) CompleteDatabaseDeletion(context.Context, core.CompleteDatabaseDeletionParams) (core.ManagedDatabaseDeletion, error) {
	return core.ManagedDatabaseDeletion{}, nil
}
func (repository *databaseLifecycleTestRepository) LoadDatabaseCredentialRotation(context.Context, core.ID, core.ID) (core.ManagedDatabaseCredentialRotation, core.DatabaseCredential, error) {
	return repository.rotation, repository.credential, nil
}
func (repository *databaseLifecycleTestRepository) CompleteDatabaseCredentialRotation(_ context.Context, _ core.CompleteDatabaseCredentialRotationParams) (core.ManagedDatabaseCredentialRotation, error) {
	repository.completeCalls++
	now := time.Now().UTC()
	repository.rotation.AppliedAt = &now
	return repository.rotation, nil
}

type databaseLifecycleTestClient struct {
	rotateCalls int
	request     agentprotocol.DatabasePasswordRotateRequest
	password    []byte
}

func (*databaseLifecycleTestClient) ProvisionDatabase(context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.DatabaseProvisionRequest) (agentprotocol.DatabaseProvisionResponse, error) {
	return agentprotocol.DatabaseProvisionResponse{}, nil
}
func (*databaseLifecycleTestClient) DropDatabase(context.Context, string, agentprotocol.AuditCorrelation, agentprotocol.DatabaseDropRequest) (agentprotocol.DatabaseDropResponse, error) {
	return agentprotocol.DatabaseDropResponse{}, nil
}
func (client *databaseLifecycleTestClient) RotateDatabasePassword(_ context.Context, _ string, _ agentprotocol.AuditCorrelation, request agentprotocol.DatabasePasswordRotateRequest) (agentprotocol.DatabasePasswordRotateResponse, error) {
	client.rotateCalls++
	client.request = request
	client.password = append([]byte(nil), request.Password...)
	return agentprotocol.DatabasePasswordRotateResponse{
		Username: request.Username, Host: request.Host, Changed: true, Active: true,
	}, nil
}

type databaseLifecycleTestReporter struct{ stages []string }

func (reporter *databaseLifecycleTestReporter) Checkpoint(
	_ context.Context,
	stage string,
	_ int64,
	_ string,
	_ map[string]any,
) error {
	reporter.stages = append(reporter.stages, stage)
	return nil
}
