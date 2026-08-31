// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"errors"
	"testing"
)

func TestHostingFilesystemIntentTracksPackageRevisionAndAppliedTruth(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	owner := createTestIdentity(t, repository, "filesystem-state@example.test")
	limits := testLimits(5)
	inodes := int64(100000)
	limits.StorageInodes = &inodes
	packageOne, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Storage One", Slug: "storage-one", Limits: limits, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage one: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageOne.ID, "Storage", "storage")
	state, err := repository.GetHostingFilesystemState(t.Context(), account.ID)
	if err != nil {
		t.Fatalf("GetHostingFilesystemState: %v", err)
	}
	if state.ProjectID != account.UnixIdentity.UID || state.Revision != 1 ||
		state.Status != HostingFilesystemPending || state.CapabilityStatus != HostingFilesystemCapabilityPending ||
		state.DesiredStorageBytes == nil || *state.DesiredStorageBytes != *limits.StorageBytes ||
		state.DesiredStorageInodes == nil || *state.DesiredStorageInodes != inodes {
		t.Fatalf("initial filesystem state = %#v", state)
	}
	operationOne, err := repository.CreateOperation(t.Context(), CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "hosting.filesystem.reconcile",
		RetryClass: RetrySafe, RequestID: "filesystem-operation-1", IdempotencyKey: "filesystem-op-1",
		Payload: map[string]any{"revision": float64(1)},
	})
	if err != nil {
		t.Fatalf("CreateOperation one: %v", err)
	}
	applied, err := repository.ConfirmHostingFilesystemApplied(t.Context(), ConfirmHostingFilesystemAppliedParams{
		AccountID: account.ID, ExpectedRevision: 1, OperationID: operationOne.ID,
		ActorID: &owner.ID, RequestID: "filesystem-applied-1",
	})
	if err != nil {
		t.Fatalf("ConfirmHostingFilesystemApplied: %v", err)
	}
	if applied.Status != HostingFilesystemApplied || applied.AppliedAt == nil ||
		applied.LastOperationID == nil || *applied.LastOperationID != operationOne.ID ||
		applied.AppliedStorageBytes == nil || *applied.AppliedStorageBytes != *limits.StorageBytes {
		t.Fatalf("applied state = %#v", applied)
	}

	limitsTwo := limits
	storageTwo := int64(20 << 30)
	inodesTwo := int64(200000)
	limitsTwo.StorageBytes, limitsTwo.StorageInodes = &storageTwo, &inodesTwo
	packageTwo, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Storage Two", Slug: "storage-two", Limits: limitsTwo, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage two: %v", err)
	}
	if _, err := repository.AssignPackage(t.Context(), AssignPackageParams{
		AccountID: account.ID, PackageID: packageTwo.ID, ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("AssignPackage: %v", err)
	}
	pending, err := repository.GetHostingFilesystemState(t.Context(), account.ID)
	if err != nil {
		t.Fatalf("Get pending filesystem state: %v", err)
	}
	if pending.Revision != 2 || pending.Status != HostingFilesystemPending ||
		pending.DesiredStorageBytes == nil || *pending.DesiredStorageBytes != storageTwo ||
		pending.AppliedStorageBytes == nil || *pending.AppliedStorageBytes != *limits.StorageBytes {
		t.Fatalf("pending revised state = %#v", pending)
	}
	if _, err := repository.ConfirmHostingFilesystemApplied(t.Context(), ConfirmHostingFilesystemAppliedParams{
		AccountID: account.ID, ExpectedRevision: 1, OperationID: operationOne.ID, ActorID: &owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale confirmation error = %v", err)
	}
}

func TestPackageRejectsUnrepresentableStorageQuota(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	owner := createTestIdentity(t, repository, "quota-alignment@example.test")
	limits := testLimits(1)
	unaligned := int64(1025)
	limits.StorageBytes = &unaligned
	if _, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Unaligned", Slug: "unaligned", Limits: limits, ActorID: &owner.ID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unaligned storage error = %v", err)
	}
}
