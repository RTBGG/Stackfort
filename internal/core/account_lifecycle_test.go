// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/store"
)

func TestHostingAccountsReceiveStableOpaqueUnixIdentities(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "mutable-address@example.test")
	packageRecord := createLifecyclePackage(t, repository, owner.ID)
	first := createLifecycleAccount(t, repository, owner.ID, packageRecord.ID, "Customer Display Name", "customer-slug")
	second := createLifecycleAccount(t, repository, owner.ID, packageRecord.ID, "Another Name", "another-slug")

	if !strings.HasPrefix(first.UnixIdentity.Username, "sf_") ||
		strings.Contains(first.UnixIdentity.Username, "customer") ||
		first.UnixIdentity.UID != hostingidentity.MinimumID ||
		first.UnixIdentity.GID != first.UnixIdentity.UID ||
		second.UnixIdentity.UID != first.UnixIdentity.UID+1 {
		t.Fatalf("first=%#v second=%#v", first.UnixIdentity, second.UnixIdentity)
	}
	wantHome, err := hostingidentity.HomeDirectoryForAccount(string(first.ID))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetHostingAccount(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UnixIdentity.Username != first.UnixIdentity.Username ||
		loaded.UnixIdentity.HomeDirectory != wantHome || loaded.UnixIdentity.State != HostingUnixIdentityAllocated {
		t.Fatalf("loaded identity = %#v", loaded.UnixIdentity)
	}
	if spec, err := loaded.UnixIdentity.HostSpec(); err != nil || spec.AccountID != string(first.ID) {
		t.Fatalf("HostSpec=%#v err=%v", spec, err)
	}
}

func TestHostingAccountRemovalRequiresEveryArchiveAndDeletionStage(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "lifecycle@example.test")
	packageRecord := createLifecyclePackage(t, repository, owner.ID)
	account := createLifecycleAccount(t, repository, owner.ID, packageRecord.ID, "Lifecycle", "lifecycle")
	base := HostingAccountLifecycleParams{AccountID: account.ID, ActorID: &owner.ID, RequestID: "lifecycle-step"}

	if _, err := repository.RequestHostingAccountDeletion(ctx, base); err == nil {
		t.Fatal("deletion was requested before archive")
	}
	reconciled, err := repository.MarkHostingUnixIdentityReconciled(ctx, base)
	if err != nil || reconciled.UnixIdentity.State != HostingUnixIdentityReconciled ||
		reconciled.UnixIdentity.ReconciledAt == nil {
		t.Fatalf("reconciled=%#v err=%v", reconciled, err)
	}
	requested, err := repository.RequestHostingAccountArchive(ctx, base)
	if err != nil || requested.Status != AccountArchived ||
		requested.UnixIdentity.State != HostingUnixIdentityArchiveRequested ||
		requested.UnixIdentity.ArchiveRequestedAt == nil {
		t.Fatalf("requested=%#v err=%v", requested, err)
	}
	if _, err := repository.RequestHostingAccountDeletion(ctx, base); err == nil {
		t.Fatal("deletion was requested before archive confirmation")
	}
	if _, err := repository.ConfirmHostingAccountArchive(ctx, ConfirmHostingAccountArchiveParams{
		AccountID: account.ID, ActorID: &owner.ID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty archive reference error = %v", err)
	}
	archived, err := repository.ConfirmHostingAccountArchive(ctx, ConfirmHostingAccountArchiveParams{
		AccountID: account.ID, ArchiveReference: "backup-manifest:019c1234", ActorID: &owner.ID,
		RequestID: "archive-confirmed",
	})
	if err != nil || archived.UnixIdentity.State != HostingUnixIdentityArchived ||
		archived.UnixIdentity.ArchiveReference == "" || archived.UnixIdentity.ArchivedAt == nil {
		t.Fatalf("archived=%#v err=%v", archived, err)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, updateErr := executor.ExecContext(ctx, `
			UPDATE hosting_account_unix_identities SET archive_reference = 'replacement'
			WHERE account_id = ?`, string(account.ID))
		return updateErr
	}); err == nil {
		t.Fatal("archive evidence was rewritten")
	}
	deletionRequested, err := repository.RequestHostingAccountDeletion(ctx, base)
	if err != nil || deletionRequested.UnixIdentity.State != HostingUnixIdentityDeletionRequested ||
		deletionRequested.UnixIdentity.DeletionRequestedAt == nil {
		t.Fatalf("deletion requested=%#v err=%v", deletionRequested, err)
	}
	deleted, err := repository.ConfirmHostingAccountDeleted(ctx, base)
	if err != nil || deleted.UnixIdentity.State != HostingUnixIdentityDeleted ||
		deleted.UnixIdentity.DeletedAt == nil {
		t.Fatalf("deleted=%#v err=%v", deleted, err)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx,
			"DELETE FROM hosting_account_unix_identities WHERE account_id = ?", string(account.ID))
		return err
	}); err == nil {
		t.Fatal("Unix identity tombstone was physically deleted")
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestHostingUnixIdentityKeyFieldsCannotBeChanged(t *testing.T) {
	t.Parallel()
	repository, state := newTestRepository(t)
	owner := createTestIdentity(t, repository, "immutable@example.test")
	packageRecord := createLifecyclePackage(t, repository, owner.ID)
	account := createLifecycleAccount(t, repository, owner.ID, packageRecord.ID, "Immutable", "immutable")
	err := state.Write(t.Context(), func(executor store.Executor) error {
		_, updateErr := executor.ExecContext(t.Context(), `
			UPDATE hosting_account_unix_identities SET uid = uid + 1, gid = gid + 1
			WHERE account_id = ?`, string(account.ID))
		return updateErr
	})
	if err == nil {
		t.Fatal("immutable numeric identity was changed")
	}
	loaded, loadErr := repository.GetHostingAccount(t.Context(), account.ID)
	if loadErr != nil || loaded.UnixIdentity.UID != account.UnixIdentity.UID {
		t.Fatalf("loaded=%#v err=%v", loaded.UnixIdentity, loadErr)
	}
}

func createLifecyclePackage(t *testing.T, repository *Repository, actorID ID) Package {
	t.Helper()
	value, err := repository.CreatePackage(t.Context(), CreatePackageParams{
		Name: "Lifecycle", Slug: "lifecycle-package-" + strings.ToLower(string(actorID)[:8]),
		Limits: testLimits(5), ActorID: &actorID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	return value
}

func createLifecycleAccount(
	t *testing.T,
	repository *Repository,
	ownerID ID,
	packageID ID,
	name string,
	slug string,
) HostingAccount {
	t.Helper()
	value, err := repository.CreateHostingAccount(t.Context(), CreateHostingAccountParams{
		Name: name, Slug: slug, OwnerIdentityID: ownerID, PackageID: packageID, ActorID: &ownerID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount: %v", err)
	}
	return value
}
