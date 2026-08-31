// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestPHPMyAdminHandoffIsSingleUseAudienceBoundAndDigestOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, state := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "phpmyadmin-owner@example.test")
	account := createManagedDatabaseTestAccount(t, repository, owner, "phpmyadmin-account")
	provisioning, err := repository.PrepareDatabaseWizard(ctx, PrepareDatabaseWizardParams{
		AccountID: account.ID, DatabaseAlias: "application", NewUserAlias: "application",
		Preset: DatabaseGrantReadWrite, ActorID: owner.ID,
		RequestID: "phpmyadmin-setup", IdempotencyKey: "phpmyadmin-setup",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, expected, err := repository.LoadDatabaseProvisioning(ctx, account.ID, provisioning.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(expected.Password)
	if _, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: provisioning.Operation.ID, AccountID: account.ID,
		ActorID: &owner.ID, RequestID: "phpmyadmin-setup-complete",
	}); err != nil {
		t.Fatal(err)
	}
	subject := createAuthorizationSubject(t, repository, owner)
	handoff, err := repository.IssuePHPMyAdminHandoff(ctx, IssuePHPMyAdminHandoffParams{
		Subject: subject, AccountID: account.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "phpmyadmin-issue",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(handoff.Token) != 43 || !handoff.ExpiresAt.After(repository.timestamp()) {
		t.Fatalf("handoff metadata = %#v", handoff)
	}
	var storedHash []byte
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `SELECT token_hash FROM phpmyadmin_handoffs`).Scan(&storedHash)
	}); err != nil {
		t.Fatal(err)
	}
	wantedHash := sha256.Sum256([]byte(handoff.Token))
	if !bytes.Equal(storedHash, wantedHash[:]) || bytes.Contains(storedHash, []byte(handoff.Token)) {
		t.Fatal("phpMyAdmin handoff was not stored as an exact digest")
	}
	if _, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: handoff.Token, Audience: "foreign-audience",
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("foreign audience error = %v", err)
	}
	credential, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: handoff.Token, Audience: PHPMyAdminHandoffAudience,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(credential.Password)
	if credential.Username != provisioning.DatabaseUser.PhysicalName || credential.Host != "localhost" ||
		!bytes.Equal(credential.Password, expected.Password) {
		t.Fatalf("redeemed credential metadata = %#v", credential)
	}
	if _, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: handoff.Token, Audience: PHPMyAdminHandoffAudience,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replay error = %v, want ErrNotFound", err)
	}
}

func TestPHPMyAdminHandoffReplacementExpiryAndTenantBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "phpmyadmin-boundary@example.test")
	first := createManagedDatabaseTestAccount(t, repository, owner, "phpmyadmin-first")
	second := createManagedDatabaseTestAccount(t, repository, owner, "phpmyadmin-second")
	provisioning, err := repository.PrepareDatabaseWizard(ctx, PrepareDatabaseWizardParams{
		AccountID: first.ID, DatabaseAlias: "first", NewUserAlias: "first",
		Preset: DatabaseGrantReadOnly, ActorID: owner.ID,
		RequestID: "phpmyadmin-boundary-setup", IdempotencyKey: "phpmyadmin-boundary-setup",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteDatabaseProvisioning(ctx, CompleteDatabaseProvisioningParams{
		OperationID: provisioning.Operation.ID, AccountID: first.ID,
		ActorID: &owner.ID, RequestID: "phpmyadmin-boundary-complete",
	}); err != nil {
		t.Fatal(err)
	}
	subject := createAuthorizationSubject(t, repository, owner)
	if _, err := repository.IssuePHPMyAdminHandoff(ctx, IssuePHPMyAdminHandoffParams{
		Subject: subject, AccountID: second.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "phpmyadmin-cross-account",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account issue error = %v, want ErrNotFound", err)
	}
	firstHandoff, err := repository.IssuePHPMyAdminHandoff(ctx, IssuePHPMyAdminHandoffParams{
		Subject: subject, AccountID: first.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "phpmyadmin-first-issue",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondHandoff, err := repository.IssuePHPMyAdminHandoff(ctx, IssuePHPMyAdminHandoffParams{
		Subject: subject, AccountID: first.ID, DatabaseUserID: provisioning.DatabaseUser.ID,
		RequestID: "phpmyadmin-second-issue",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: firstHandoff.Token, Audience: PHPMyAdminHandoffAudience,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced handoff error = %v, want ErrNotFound", err)
	}
	repository.now = func() time.Time { return secondHandoff.ExpiresAt.Add(time.Nanosecond) }
	if _, err := repository.RedeemPHPMyAdminHandoff(ctx, RedeemPHPMyAdminHandoffParams{
		Token: secondHandoff.Token, Audience: PHPMyAdminHandoffAudience,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired handoff error = %v, want ErrNotFound", err)
	}
}
