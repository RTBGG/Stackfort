// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"testing"
)

func TestAdminQueriesReturnBoundedCurrentInventory(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	administrator := createTestIdentity(t, repository, "admin-query@example.test")

	second, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Zulu", Slug: "zulu", Limits: testLimits(2), ActorID: &administrator.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage Zulu: %v", err)
	}
	first, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Alpha", Slug: "alpha", Limits: testLimits(7), ActorID: &administrator.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage Alpha: %v", err)
	}
	account, err := repository.CreateHostingAccount(ctx, CreateHostingAccountParams{
		Name: "Primary", Slug: "primary", OwnerIdentityID: administrator.ID,
		PackageID: first.ID, ActorID: &administrator.ID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	operation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID: &account.ID, ActorID: &administrator.ID, Kind: "admin.query.test",
		RetryClass: RetryNone, RequestID: "admin-query-operation", Payload: map[string]any{}, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}

	packages, err := repository.ListPackages(ctx)
	if err != nil || len(packages) != 2 || packages[0].ID != first.ID || packages[1].ID != second.ID ||
		packages[0].Limits.MaxDomains != 7 {
		t.Fatalf("ListPackages = %#v, %v", packages, err)
	}
	accounts, err := repository.ListHostingAccountSummaries(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != account.ID ||
		accounts[0].PackageID != first.ID || accounts[0].PackageRevision != 1 ||
		accounts[0].PackageName != first.Name || accounts[0].HostReady {
		t.Fatalf("ListHostingAccountSummaries = %#v, %v", accounts, err)
	}
	operations, err := repository.ListRecentOperations(ctx, 10)
	if err != nil || len(operations) != 1 || operations[0].ID != operation.ID {
		t.Fatalf("ListRecentOperations = %#v, %v", operations, err)
	}
	events, err := repository.ListRecentAuditEvents(ctx, 20)
	if err != nil || len(events) < 4 || events[0].Sequence <= events[len(events)-1].Sequence {
		t.Fatalf("ListRecentAuditEvents = %#v, %v", events, err)
	}
	if _, err := repository.ListRecentOperations(ctx, maximumAdminListLimit+1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized operation list error = %v, want ErrInvalidInput", err)
	}
}
