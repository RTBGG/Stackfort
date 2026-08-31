// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"testing"
)

func TestHostingAccountReadinessRequiresEveryAppliedHostBoundary(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "host-ready@example.test")
	packageRecord := createLifecyclePackage(t, repository, owner.ID)
	account := createLifecycleAccount(t, repository, owner.ID, packageRecord.ID, "Host ready", "host-ready")

	ready, err := repository.HostingAccountHostReady(ctx, account.ID)
	if err != nil || ready {
		t.Fatalf("initial HostingAccountHostReady = %v, %v", ready, err)
	}
	pending, err := repository.ListHostingAccountsNeedingHostReconcile(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0] != account.ID {
		t.Fatalf("initial pending accounts = %#v, %v", pending, err)
	}
	operation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "hosting.account.test",
		RetryClass: RetrySafe, RequestID: "host-ready-test", Payload: map[string]any{}, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	filesystem, _ := repository.GetHostingFilesystemState(ctx, account.ID)
	if _, err := repository.ConfirmHostingFilesystemApplied(ctx, ConfirmHostingFilesystemAppliedParams{
		AccountID: account.ID, ExpectedRevision: filesystem.Revision, OperationID: operation.ID,
		ActorID: &owner.ID, RequestID: "host-ready-filesystem",
	}); err != nil {
		t.Fatalf("ConfirmHostingFilesystemApplied: %v", err)
	}
	resources, _ := repository.GetHostingResourceState(ctx, account.ID)
	if _, err := repository.ConfirmHostingResourcesApplied(ctx, ConfirmHostingResourcesAppliedParams{
		AccountID: account.ID, ExpectedRevision: resources.Revision, OperationID: operation.ID,
		ActorID: &owner.ID, RequestID: "host-ready-resources",
	}); err != nil {
		t.Fatalf("ConfirmHostingResourcesApplied: %v", err)
	}
	if _, err := repository.MarkHostingUnixIdentityReconciled(ctx, HostingAccountLifecycleParams{
		AccountID: account.ID, ActorID: &owner.ID, OperationID: &operation.ID, RequestID: "host-ready-identity",
	}); err != nil {
		t.Fatalf("MarkHostingUnixIdentityReconciled: %v", err)
	}
	ready, err = repository.HostingAccountHostReady(ctx, account.ID)
	if err != nil || !ready {
		t.Fatalf("final HostingAccountHostReady = %v, %v", ready, err)
	}
	pending, err = repository.ListHostingAccountsNeedingHostReconcile(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("final pending accounts = %#v, %v", pending, err)
	}
}
