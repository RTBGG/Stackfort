// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"errors"
	"testing"
)

func TestHostingResourceIntentTracksPackageRevisionAndAppliedTruth(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	owner := createTestIdentity(t, repository, "resource-state@example.test")
	limits := testLimits(5)
	cpuQuota, cpuWeight := int64(250), int64(800)
	memory, swap, processes := int64(512<<20), int64(0), int64(64)
	limits.CPUQuotaPercent, limits.CPUWeight = &cpuQuota, &cpuWeight
	limits.MemoryBytes, limits.SwapBytes, limits.ProcessLimit = &memory, &swap, &processes
	packageOne, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Resources One", Slug: "resources-one", Limits: limits, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage one: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageOne.ID, "Resources", "resources")
	state, err := repository.GetHostingResourceState(t.Context(), account.ID)
	if err != nil {
		t.Fatalf("GetHostingResourceState: %v", err)
	}
	if state.Revision != 1 || state.Status != HostingResourcePending ||
		state.CapabilityStatus != HostingResourceCapabilityPending ||
		state.DesiredCPUQuotaPercent == nil || *state.DesiredCPUQuotaPercent != cpuQuota ||
		state.DesiredCPUWeight == nil || *state.DesiredCPUWeight != cpuWeight ||
		state.DesiredMemoryBytes == nil || *state.DesiredMemoryBytes != memory ||
		state.DesiredSwapBytes == nil || *state.DesiredSwapBytes != 0 ||
		state.DesiredProcessLimit == nil || *state.DesiredProcessLimit != processes {
		t.Fatalf("initial resource state = %#v", state)
	}
	operationOne, err := repository.CreateOperation(t.Context(), CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "hosting.resources.reconcile",
		RetryClass: RetrySafe, RequestID: "resource-operation-1", IdempotencyKey: "resource-op-1",
		Payload: map[string]any{"revision": float64(1)},
	})
	if err != nil {
		t.Fatalf("CreateOperation one: %v", err)
	}
	applied, err := repository.ConfirmHostingResourcesApplied(t.Context(), ConfirmHostingResourcesAppliedParams{
		AccountID: account.ID, ExpectedRevision: 1, OperationID: operationOne.ID,
		ActorID: &owner.ID, RequestID: "resources-applied-1",
	})
	if err != nil {
		t.Fatalf("ConfirmHostingResourcesApplied: %v", err)
	}
	if applied.Status != HostingResourceApplied || applied.AppliedAt == nil ||
		applied.LastOperationID == nil || *applied.LastOperationID != operationOne.ID ||
		applied.AppliedSwapBytes == nil || *applied.AppliedSwapBytes != 0 ||
		applied.AppliedMemoryBytes == nil || *applied.AppliedMemoryBytes != memory {
		t.Fatalf("applied resource state = %#v", applied)
	}

	limitsTwo := limits
	cpuTwo, memoryTwo, processesTwo := int64(400), int64(1<<30), int64(128)
	limitsTwo.CPUQuotaPercent, limitsTwo.MemoryBytes, limitsTwo.ProcessLimit = &cpuTwo, &memoryTwo, &processesTwo
	limitsTwo.SwapBytes = nil
	packageTwo, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Resources Two", Slug: "resources-two", Limits: limitsTwo, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage two: %v", err)
	}
	if _, err := repository.AssignPackage(t.Context(), AssignPackageParams{
		AccountID: account.ID, PackageID: packageTwo.ID, ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("AssignPackage: %v", err)
	}
	pending, err := repository.GetHostingResourceState(t.Context(), account.ID)
	if err != nil {
		t.Fatalf("Get pending resource state: %v", err)
	}
	if pending.Revision != 2 || pending.Status != HostingResourcePending ||
		pending.DesiredCPUQuotaPercent == nil || *pending.DesiredCPUQuotaPercent != cpuTwo ||
		pending.DesiredMemoryBytes == nil || *pending.DesiredMemoryBytes != memoryTwo ||
		pending.DesiredSwapBytes != nil || pending.AppliedSwapBytes == nil || *pending.AppliedSwapBytes != 0 {
		t.Fatalf("pending revised resource state = %#v", pending)
	}
	if _, err := repository.ConfirmHostingResourcesApplied(t.Context(), ConfirmHostingResourcesAppliedParams{
		AccountID: account.ID, ExpectedRevision: 1, OperationID: operationOne.ID, ActorID: &owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale confirmation error = %v", err)
	}
}

func TestHostingResourceBlockedStateRequiresTypedCapability(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	owner := createTestIdentity(t, repository, "resource-blocked@example.test")
	packageOne, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Blocked Resources", Slug: "blocked-resources", Limits: testLimits(1), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageOne.ID, "Blocked", "blocked")
	operation, err := repository.CreateOperation(t.Context(), CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "hosting.resources.reconcile",
		RetryClass: RetrySafe, RequestID: "resource-blocked-request",
		IdempotencyKey: "resource-blocked-op", Payload: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	blocked, err := repository.ConfirmHostingResourcesBlocked(t.Context(), ConfirmHostingResourcesBlockedParams{
		AccountID: account.ID, ExpectedRevision: 1, OperationID: operation.ID,
		CapabilityStatus: HostingResourceCapabilityUnavailable,
		ReasonCode:       "cgroup-controller-memory-missing", ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("ConfirmHostingResourcesBlocked: %v", err)
	}
	if blocked.Status != HostingResourceBlocked ||
		blocked.CapabilityStatus != HostingResourceCapabilityUnavailable ||
		blocked.ReasonCode != "cgroup-controller-memory-missing" {
		t.Fatalf("blocked state = %#v", blocked)
	}
}
