// SPDX-License-Identifier: AGPL-3.0-or-later

package accountprovisioning

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/operations"
)

func TestCreatePersistsAccountAndQueuesImmutableHostSnapshot(t *testing.T) {
	t.Parallel()

	repository := provisioningRepositoryStub(t)
	service, err := New(repository)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := service.Create(context.Background(), CreateCommand{
		Name: "Customer", Slug: "customer", OwnerIdentityID: testProvisioningID("2"),
		PackageID: testProvisioningID("3"), RequestID: "create-customer",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Account.ID != repository.account.ID || result.Operation.ID != repository.operation.ID ||
		repository.authorizedAction != core.AuthorizationAccountsCreate || repository.created == nil {
		t.Fatalf("result/repository = %#v / %#v", result, repository)
	}
	created := *repository.operationParams
	if created.Kind != operations.HostingAccountReconcileKind || created.RetryClass != core.RetrySafe ||
		created.MaxAttempts != 3 || created.AccountID == nil || *created.AccountID != repository.account.ID ||
		created.IdempotencyKey != "account-host-"+string(repository.account.ID)+"-1-1" {
		t.Fatalf("operation params = %#v", created)
	}
	encoded, err := json.Marshal(created.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload operations.HostingAccountReconcilePayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Identity.AccountID != string(repository.account.ID) ||
		payload.Storage.ProjectID != repository.filesystem.ProjectID ||
		payload.Storage.ByteLimit != 20<<30 || payload.Resources.MemoryBytes.Value != 2<<30 {
		t.Fatalf("queued payload = %#v", payload)
	}
}

func TestQueuePendingRepairsAccountOperationGap(t *testing.T) {
	t.Parallel()

	repository := provisioningRepositoryStub(t)
	repository.pending = []core.ID{repository.account.ID}
	service, _ := New(repository)
	queued, err := service.QueuePending(context.Background(), 10)
	if err != nil || queued != 1 || repository.operationParams == nil ||
		repository.operationParams.RequestID != "system-account-reconcile" || repository.operationParams.ActorID != nil {
		t.Fatalf("QueuePending = %d, %v; operation = %#v", queued, err, repository.operationParams)
	}
}

func TestNegativeStorageIntentIsRejected(t *testing.T) {
	t.Parallel()
	value := int64(-1)
	if _, err := optionalLimit(&value); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("optionalLimit error = %v, want ErrConflict", err)
	}
}

type provisioningRepository struct {
	account          core.HostingAccount
	filesystem       core.HostingFilesystemState
	resources        core.HostingResourceState
	operation        core.Operation
	pending          []core.ID
	authorizedAction core.AuthorizationAction
	created          *core.CreateHostingAccountParams
	operationParams  *core.CreateOperationParams
}

func provisioningRepositoryStub(t *testing.T) *provisioningRepository {
	t.Helper()
	accountID := testProvisioningID("1")
	username, err := hostingidentity.UsernameForAccount(string(accountID))
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(string(accountID))
	if err != nil {
		t.Fatal(err)
	}
	storageBytes := int64(20 << 30)
	storageInodes := int64(250_000)
	memoryBytes := int64(2 << 30)
	return &provisioningRepository{
		account: core.HostingAccount{ID: accountID, UnixIdentity: core.HostingUnixIdentity{
			AccountID: accountID, Username: username, UID: hostingidentity.MinimumID,
			GID: hostingidentity.MinimumID, HomeDirectory: home,
		}},
		filesystem: core.HostingFilesystemState{
			AccountID: accountID, ProjectID: hostingidentity.MinimumID, Revision: 1,
			DesiredStorageBytes: &storageBytes, DesiredStorageInodes: &storageInodes,
		},
		resources: core.HostingResourceState{
			AccountID: accountID, Revision: 1, DesiredMemoryBytes: &memoryBytes,
		},
		operation: core.Operation{ID: testProvisioningID("4"), Status: core.OperationPending},
	}
}

func (repository *provisioningRepository) Authorize(_ context.Context, params core.AuthorizeParams) (core.AuthorizationDecision, error) {
	repository.authorizedAction = params.Action
	return core.AuthorizationDecision{}, nil
}

func (repository *provisioningRepository) CreateHostingAccount(_ context.Context, params core.CreateHostingAccountParams) (core.HostingAccount, error) {
	repository.created = &params
	return repository.account, nil
}

func (repository *provisioningRepository) GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error) {
	return repository.account, nil
}

func (repository *provisioningRepository) GetHostingFilesystemState(context.Context, core.ID) (core.HostingFilesystemState, error) {
	return repository.filesystem, nil
}

func (repository *provisioningRepository) GetHostingResourceState(context.Context, core.ID) (core.HostingResourceState, error) {
	return repository.resources, nil
}

func (repository *provisioningRepository) CreateOperation(_ context.Context, params core.CreateOperationParams) (core.Operation, error) {
	repository.operationParams = &params
	return repository.operation, nil
}

func (repository *provisioningRepository) ListHostingAccountsNeedingHostReconcile(context.Context, int) ([]core.ID, error) {
	return repository.pending, nil
}

func testProvisioningID(suffix string) core.ID {
	return core.ID("019cff00-0000-7000-8000-00000000000" + suffix)
}
