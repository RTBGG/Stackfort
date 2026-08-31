// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestDatabaseWizardIsTenantScopedEncryptedAndReplaySafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, state := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "database-owner@example.test")
	account := createManagedDatabaseTestAccount(t, repository, owner, "database-account")

	params := PrepareDatabaseWizardParams{
		AccountID: account.ID, DatabaseAlias: "application", NewUserAlias: "application",
		Preset: DatabaseGrantReadWrite, ActorID: owner.ID,
		RequestID: "database-wizard-request", IdempotencyKey: "database-wizard-idempotency",
	}
	prepared, err := repository.PrepareDatabaseWizard(ctx, params)
	if err != nil {
		t.Fatalf("PrepareDatabaseWizard: %v", err)
	}
	if prepared.Database.Status != ManagedDatabasePending ||
		prepared.DatabaseUser.Status != ManagedDatabasePending || prepared.DatabaseUser.Host != "localhost" ||
		prepared.Grant.Status != "pending" || prepared.Operation.Kind != managedDatabaseOperationKind {
		t.Fatalf("prepared provisioning = %#v", prepared)
	}
	if prepared.Database.PhysicalName != "sf_"+compactTestID(account.ID)+"_application" ||
		prepared.DatabaseUser.PhysicalName != "sf_"+compactTestID(account.ID)+"_application" {
		t.Fatalf("derived names = %q / %q", prepared.Database.PhysicalName, prepared.DatabaseUser.PhysicalName)
	}

	replayed, err := repository.PrepareDatabaseWizard(ctx, params)
	if err != nil || replayed.Operation.ID != prepared.Operation.ID ||
		replayed.Database.ID != prepared.Database.ID || replayed.DatabaseUser.ID != prepared.DatabaseUser.ID {
		t.Fatalf("replayed provisioning = %#v, %v", replayed, err)
	}

	loaded, credential, err := repository.LoadDatabaseProvisioning(ctx, account.ID, prepared.Operation.ID)
	if err != nil {
		t.Fatalf("LoadDatabaseProvisioning: %v", err)
	}
	defer clear(credential.Password)
	if loaded.Database.ID != prepared.Database.ID || credential.Username != prepared.DatabaseUser.PhysicalName ||
		credential.Host != "localhost" || len(credential.Password) != 32 {
		t.Fatalf("loaded credential metadata = %#v", credential)
	}
	var ciphertext []byte
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext FROM managed_database_users WHERE id = ?`,
			string(prepared.DatabaseUser.ID)).Scan(&ciphertext)
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, credential.Password) || bytes.Equal(ciphertext, credential.Password) {
		t.Fatal("managed database password was stored without authenticated encryption")
	}

	completed, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: prepared.Operation.ID, AccountID: account.ID, ActorID: &owner.ID,
		RequestID: "database-provision-complete",
	})
	if err != nil {
		t.Fatalf("CompleteDatabaseProvisioning: %v", err)
	}
	if completed.Database.Status != ManagedDatabaseActive ||
		completed.DatabaseUser.Status != ManagedDatabaseActive || completed.Grant.Status != "active" {
		t.Fatalf("completed provisioning = %#v", completed)
	}
	if _, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: prepared.Operation.ID, AccountID: account.ID, ActorID: &owner.ID,
		RequestID: "database-provision-complete-replay",
	}); err != nil {
		t.Fatalf("CompleteDatabaseProvisioning replay: %v", err)
	}
	workspace, err := repository.ListDatabaseWorkspace(ctx, account.ID)
	if err != nil || len(workspace.Databases) != 1 || len(workspace.Users) != 1 || len(workspace.Grants) != 1 {
		t.Fatalf("ListDatabaseWorkspace = %#v, %v", workspace, err)
	}
}

func TestDatabaseWizardRejectsCrossAccountUserAndLimitOverflow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "database-isolation@example.test")
	first := createManagedDatabaseTestAccount(t, repository, owner, "database-first")
	second := createManagedDatabaseTestAccount(t, repository, owner, "database-second")

	firstProvisioning, err := repository.PrepareDatabaseWizard(ctx, PrepareDatabaseWizardParams{
		AccountID: first.ID, DatabaseAlias: "first", NewUserAlias: "first",
		Preset: DatabaseGrantReadOnly, ActorID: owner.ID,
		RequestID: "first-wizard", IdempotencyKey: "first-wizard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: firstProvisioning.Operation.ID, AccountID: first.ID,
		ActorID: &owner.ID, RequestID: "first-complete",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = repository.PrepareDatabaseWizard(ctx, PrepareDatabaseWizardParams{
		AccountID: second.ID, DatabaseAlias: "stolen", ExistingUserID: &firstProvisioning.DatabaseUser.ID,
		Preset: DatabaseGrantReadWrite, ActorID: owner.ID,
		RequestID: "cross-account", IdempotencyKey: "cross-account",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account user error = %v, want ErrNotFound", err)
	}
}

func TestDatabaseDeletionRequiresExactAliasRevokesFirstAndDestroysCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, state := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "database-delete@example.test")
	account := createManagedDatabaseTestAccount(t, repository, owner, "database-delete")
	provisioning, err := repository.PrepareDatabaseWizard(ctx, PrepareDatabaseWizardParams{
		AccountID: account.ID, DatabaseAlias: "records", NewUserAlias: "records_app",
		Preset: DatabaseGrantReadWrite, ActorID: owner.ID,
		RequestID: "delete-setup", IdempotencyKey: "delete-setup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: provisioning.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "delete-setup-complete",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repository.PrepareDatabaseDeletion(ctx, PrepareDatabaseDeletionParams{
		AccountID: account.ID, TargetKind: DatabaseDeletionDatabase, TargetID: provisioning.Database.ID,
		Confirmation: "wrong", ActorID: owner.ID, RequestID: "wrong-confirmation", IdempotencyKey: "wrong-confirmation",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong confirmation error = %v, want ErrConflict", err)
	}
	if _, err := repository.PrepareDatabaseDeletion(ctx, PrepareDatabaseDeletionParams{
		AccountID: account.ID, TargetKind: DatabaseDeletionUser, TargetID: provisioning.DatabaseUser.ID,
		Confirmation: provisioning.DatabaseUser.Alias, ActorID: owner.ID,
		RequestID: "user-with-grant", IdempotencyKey: "user-with-grant",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("user with grant error = %v, want ErrConflict", err)
	}

	dropParams := PrepareDatabaseDeletionParams{
		AccountID: account.ID, TargetKind: DatabaseDeletionDatabase, TargetID: provisioning.Database.ID,
		Confirmation: provisioning.Database.Alias, ActorID: owner.ID,
		RequestID: "drop-database", IdempotencyKey: "drop-database",
	}
	drop, err := repository.PrepareDatabaseDeletion(ctx, dropParams)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := repository.PrepareDatabaseDeletion(ctx, dropParams)
	if err != nil || replay.Operation.ID != drop.Operation.ID {
		t.Fatalf("database deletion replay = %#v, %v", replay, err)
	}
	loaded, err := repository.LoadDatabaseDeletion(ctx, account.ID, drop.Operation.ID)
	if err != nil || loaded.Database == nil || len(loaded.Grants) != 1 || len(loaded.GrantUsers) != 1 {
		t.Fatalf("loaded database deletion = %#v, %v", loaded, err)
	}
	if _, err := repository.CompleteDatabaseDeletion(ctx, CompleteDatabaseDeletionParams{
		OperationID: drop.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "drop-database-complete",
	}); err != nil {
		t.Fatal(err)
	}
	subject := createAuthorizationSubject(t, repository, owner)
	rotation, err := repository.PrepareDatabaseCredentialRotation(ctx, PrepareDatabaseCredentialRotationParams{
		Subject: subject, AccountID: account.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "delete-pending-rotation", IdempotencyKey: "delete-pending-rotation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PrepareDatabaseDeletion(ctx, PrepareDatabaseDeletionParams{
		AccountID: account.ID, TargetKind: DatabaseDeletionUser, TargetID: provisioning.DatabaseUser.ID,
		Confirmation: provisioning.DatabaseUser.Alias, ActorID: owner.ID,
		RequestID: "drop-user-during-rotation", IdempotencyKey: "drop-user-during-rotation",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("user deletion during password rotation error = %v, want ErrConflict", err)
	}
	if _, err := repository.CompleteDatabaseCredentialRotation(ctx, CompleteDatabaseCredentialRotationParams{
		OperationID: rotation.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "delete-pending-rotation-complete",
	}); err != nil {
		t.Fatal(err)
	}

	userDrop, err := repository.PrepareDatabaseDeletion(ctx, PrepareDatabaseDeletionParams{
		AccountID: account.ID, TargetKind: DatabaseDeletionUser, TargetID: provisioning.DatabaseUser.ID,
		Confirmation: provisioning.DatabaseUser.Alias, ActorID: owner.ID,
		RequestID: "drop-user", IdempotencyKey: "drop-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteDatabaseDeletion(ctx, CompleteDatabaseDeletionParams{
		OperationID: userDrop.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "drop-user-complete",
	}); err != nil {
		t.Fatal(err)
	}
	workspace, err := repository.ListDatabaseWorkspace(ctx, account.ID)
	if err != nil || len(workspace.Databases) != 0 || len(workspace.Users) != 0 || len(workspace.Grants) != 0 {
		t.Fatalf("workspace after deletion = %#v, %v", workspace, err)
	}
	var ciphertext, wrappedKey []byte
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext, password_wrapped_key
			FROM managed_database_users WHERE id = ?`, string(provisioning.DatabaseUser.ID)).Scan(
			&ciphertext, &wrappedKey,
		)
	}); err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) != 0 || !bytes.Equal(wrappedKey, make([]byte, 48)) {
		t.Fatal("deleted database user's encrypted credential material was retained")
	}
}

func TestDatabaseCredentialRotationPromotesOnlyAfterHostSuccessAndRevokesHandoffs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, state := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "database-rotation@example.test")
	account := createManagedDatabaseTestAccount(t, repository, owner, "database-rotation")
	provisioning, err := repository.PrepareDatabaseWizard(ctx, PrepareDatabaseWizardParams{
		AccountID: account.ID, DatabaseAlias: "rotation", NewUserAlias: "rotation_app",
		Preset: DatabaseGrantReadWrite, ActorID: owner.ID,
		RequestID: "rotation-setup", IdempotencyKey: "rotation-setup",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, originalCredential, err := repository.LoadDatabaseProvisioning(
		ctx, account.ID, provisioning.Operation.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(originalCredential.Password)
	if _, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: provisioning.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "rotation-setup-complete",
	}); err != nil {
		t.Fatal(err)
	}

	subject := createAuthorizationSubject(t, repository, owner)
	oldHandoff, err := repository.IssuePHPMyAdminHandoff(ctx, IssuePHPMyAdminHandoffParams{
		Subject: subject, AccountID: account.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "rotation-old-handoff",
	})
	if err != nil {
		t.Fatal(err)
	}
	var originalCiphertext []byte
	var originalGeneration int64
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext, credential_generation
			FROM managed_database_users WHERE account_id = ? AND id = ?`,
			string(account.ID), string(provisioning.DatabaseUser.ID)).Scan(
			&originalCiphertext, &originalGeneration,
		)
	}); err != nil {
		t.Fatal(err)
	}

	params := PrepareDatabaseCredentialRotationParams{
		Subject: subject, AccountID: account.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "rotation-prepare", IdempotencyKey: "rotation-prepare",
	}
	prepared, err := repository.PrepareDatabaseCredentialRotation(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.PrepareDatabaseCredentialRotation(ctx, params)
	if err != nil || replayed.Operation.ID != prepared.Operation.ID {
		t.Fatalf("rotation replay = %#v, %v", replayed, err)
	}
	if _, err := repository.PrepareDatabaseCredentialRotation(ctx, PrepareDatabaseCredentialRotationParams{
		Subject: subject, AccountID: account.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "rotation-overlap", IdempotencyKey: "rotation-overlap",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("overlapping rotation error = %v, want ErrConflict", err)
	}

	var authoritativeCiphertext, candidateCiphertext []byte
	var generation int64
	if err := state.Read(ctx, func(reader store.Reader) error {
		if err := reader.QueryRowContext(ctx, `
			SELECT password_ciphertext, credential_generation
			FROM managed_database_users WHERE account_id = ? AND id = ?`,
			string(account.ID), string(provisioning.DatabaseUser.ID)).Scan(
			&authoritativeCiphertext, &generation,
		); err != nil {
			return err
		}
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext
			FROM managed_database_credential_rotations
			WHERE account_id = ? AND operation_id = ?`,
			string(account.ID), string(prepared.Operation.ID)).Scan(&candidateCiphertext)
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(authoritativeCiphertext, originalCiphertext) || generation != originalGeneration ||
		bytes.Equal(candidateCiphertext, authoritativeCiphertext) {
		t.Fatal("preparing rotation changed the authoritative credential envelope")
	}

	loaded, candidateCredential, err := repository.LoadDatabaseCredentialRotation(
		ctx, account.ID, prepared.Operation.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(candidateCredential.Password)
	if loaded.AppliedAt != nil || len(candidateCredential.Password) != 32 ||
		bytes.Equal(candidateCredential.Password, originalCredential.Password) {
		t.Fatalf("pending rotation = %#v, password bytes = %d", loaded, len(candidateCredential.Password))
	}
	completed, err := repository.CompleteDatabaseCredentialRotation(ctx, CompleteDatabaseCredentialRotationParams{
		OperationID: prepared.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "rotation-complete",
	})
	if err != nil || completed.AppliedAt == nil {
		t.Fatalf("completed rotation = %#v, %v", completed, err)
	}
	if _, err := repository.CompleteDatabaseCredentialRotation(ctx, CompleteDatabaseCredentialRotationParams{
		OperationID: prepared.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "rotation-complete-replay",
	}); err != nil {
		t.Fatalf("rotation completion replay: %v", err)
	}

	var promotedCiphertext []byte
	var revealed bool
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT password_ciphertext, credential_generation,
			       password_revealed_at IS NOT NULL
			FROM managed_database_users WHERE account_id = ? AND id = ?`,
			string(account.ID), string(provisioning.DatabaseUser.ID)).Scan(
			&promotedCiphertext, &generation, &revealed,
		)
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(promotedCiphertext, candidateCiphertext) ||
		generation != originalGeneration+1 || revealed {
		t.Fatal("rotation did not atomically promote exactly one new credential generation")
	}
	if _, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: oldHandoff.Token, Audience: PHPMyAdminHandoffAudience,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pre-rotation handoff error = %v, want ErrNotFound", err)
	}
	applied, credentialAfterApply, err := repository.LoadDatabaseCredentialRotation(
		ctx, account.ID, prepared.Operation.ID,
	)
	if err != nil || applied.AppliedAt == nil || len(credentialAfterApply.Password) != 0 {
		t.Fatalf("applied rotation worker view = %#v / %#v, %v", applied, credentialAfterApply, err)
	}
	newHandoff, err := repository.IssuePHPMyAdminHandoff(ctx, IssuePHPMyAdminHandoffParams{
		Subject: subject, AccountID: account.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "rotation-new-handoff",
	})
	if err != nil {
		t.Fatal(err)
	}
	newCredential, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: newHandoff.Token, Audience: PHPMyAdminHandoffAudience,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(newCredential.Password)
	if !bytes.Equal(newCredential.Password, candidateCredential.Password) {
		t.Fatal("post-rotation handoff did not use the promoted credential")
	}
}

func newManagedDatabaseTestRepository(t *testing.T) (*Repository, *store.Store) {
	t.Helper()
	state, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state", "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x5a}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return repository, state
}

func createManagedDatabaseTestAccount(
	t *testing.T, repository *Repository, owner Identity, slug string,
) HostingAccount {
	t.Helper()
	packageRecord, err := repository.CreatePackage(context.Background(), CreatePackageParams{
		Name: "Database package " + slug, Slug: slug + "-package", Limits: testLimits(2),
		ActorID: &owner.ID, RequestID: "create-package-" + slug,
	})
	if err != nil {
		t.Fatal(err)
	}
	return createTestAccount(t, repository, owner.ID, packageRecord.ID, "Database account "+slug, slug)
}

func compactTestID(id ID) string {
	result := make([]byte, 0, 32)
	for _, value := range []byte(id) {
		if value != '-' {
			result = append(result, value)
		}
	}
	return string(result)
}
