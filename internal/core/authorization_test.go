// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestAuthorizationMatrixDeniesByDefault(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }

	admin := createTestIdentity(t, repository, "matrix-admin@example.test")
	owner := createTestIdentity(t, repository, "matrix-owner@example.test")
	member := createTestIdentity(t, repository, "matrix-member@example.test")
	auditor := createTestIdentity(t, repository, "matrix-auditor@example.test")
	outsider := createTestIdentity(t, repository, "matrix-outsider@example.test")
	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{IdentityID: admin.ID, Role: PlatformAdministrator}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Authorization package", Slug: "authorization-package", Limits: testLimits(20), ActorID: &admin.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Authorization", "authorization")
	if _, err := repository.AddMembership(ctx, AddMembershipParams{
		AccountID: account.ID, IdentityID: member.ID, Role: MembershipMember, ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("AddMembership member: %v", err)
	}
	if _, err := repository.AddMembership(ctx, AddMembershipParams{
		AccountID: account.ID, IdentityID: auditor.ID, Role: MembershipAuditor, ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("AddMembership auditor: %v", err)
	}

	principals := map[string]AuthorizationSubject{
		"administrator": createAuthorizationSubject(t, repository, admin),
		"owner":         createAuthorizationSubject(t, repository, owner),
		"member":        createAuthorizationSubject(t, repository, member),
		"auditor":       createAuthorizationSubject(t, repository, auditor),
		"outsider":      createAuthorizationSubject(t, repository, outsider),
	}
	accountID := account.ID
	tests := []struct {
		action  AuthorizationAction
		account *ID
		allowed map[string]bool
	}{
		{AuthorizationPlatformView, nil, allow("administrator")},
		{AuthorizationPlatformManage, nil, allow("administrator")},
		{AuthorizationPackagesView, nil, allow("administrator")},
		{AuthorizationPackagesManage, nil, allow("administrator")},
		{AuthorizationAccountsCreate, nil, allow("administrator")},
		{AuthorizationAccountView, &accountID, allow("administrator", "owner", "member", "auditor")},
		{AuthorizationAccountManage, &accountID, allow("administrator", "owner")},
		{AuthorizationAccountMembershipsView, &accountID, allow("administrator", "owner", "auditor")},
		{AuthorizationAccountMembershipsManage, &accountID, allow("administrator", "owner")},
		{AuthorizationAccountPackageView, &accountID, allow("administrator", "owner", "member", "auditor")},
		{AuthorizationAccountPackageManage, &accountID, allow("administrator")},
		{AuthorizationAccountResourcesView, &accountID, allow("administrator", "owner", "member", "auditor")},
		{AuthorizationAccountResourcesManage, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationAccountFilesView, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationAccountFilesManage, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationAccountBackupsView, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationAccountBackupsManage, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationAccountBackupsRestore, &accountID, allow("administrator", "owner")},
		{AuthorizationAccountBackupsDelete, &accountID, allow("administrator", "owner")},
		{AuthorizationAccountLogsView, &accountID, allow("administrator", "owner", "member", "auditor")},
		{AuthorizationAccountJobsView, &accountID, allow("administrator", "owner", "member", "auditor")},
		{AuthorizationAccountJobsManage, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationAccountAuditView, &accountID, allow("administrator", "owner", "auditor")},
		{AuthorizationAccountDestructive, &accountID, allow("administrator", "owner")},
		{AuthorizationAccountCredentialsManage, &accountID, allow("administrator", "owner", "member")},
		{AuthorizationIdentityProfileView, nil, allow("administrator", "owner", "member", "auditor", "outsider")},
		{AuthorizationIdentityProfileManage, nil, allow("administrator", "owner", "member", "auditor", "outsider")},
		{AuthorizationIdentityFactorsView, nil, allow("administrator", "owner", "member", "auditor", "outsider")},
		{AuthorizationIdentityFactorsManage, nil, allow("administrator", "owner", "member", "auditor", "outsider")},
		{AuthorizationIdentitySessionsView, nil, allow("administrator", "owner", "member", "auditor", "outsider")},
		{AuthorizationIdentitySessionsManage, nil, allow("administrator", "owner", "member", "auditor", "outsider")},
	}

	for _, test := range tests {
		for principal, subject := range principals {
			decision, err := repository.Authorize(ctx, AuthorizeParams{
				Subject: subject, Action: test.action, AccountID: test.account,
			})
			if test.allowed[principal] {
				if err != nil {
					t.Errorf("%s %s: %v", principal, test.action, err)
					continue
				}
				if decision.Action != test.action || (test.account != nil && (decision.AccountID == nil || *decision.AccountID != account.ID)) {
					t.Errorf("%s %s decision = %#v", principal, test.action, decision)
				}
				continue
			}
			if !errors.Is(err, ErrAuthorizationDenied) {
				t.Errorf("%s %s error = %v, want ErrAuthorizationDenied", principal, test.action, err)
			}
		}
	}

	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: principals["administrator"], Action: AuthorizationAction("future.unregistered"),
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("unknown action error = %v, want ErrAuthorizationDenied", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: principals["owner"], Action: AuthorizationAccountView,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("missing account error = %v, want ErrAuthorizationDenied", err)
	}
}

func TestAuthorizationUsesCurrentPrivilegeAndAccountState(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	current := time.Date(2026, time.August, 16, 13, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	admin := createTestIdentity(t, repository, "current-admin@example.test")
	owner := createTestIdentity(t, repository, "current-owner@example.test")
	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{IdentityID: admin.ID, Role: PlatformAdministrator}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Current policy", Slug: "current-policy", Limits: testLimits(10), ActorID: &admin.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Current", "current")
	adminSubject := createAuthorizationSubject(t, repository, admin)
	ownerSubject := createAuthorizationSubject(t, repository, owner)

	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationAccountManage, AccountID: &account.ID,
	}); err != nil {
		t.Fatalf("fresh owner sensitive authorization: %v", err)
	}
	current = current.Add(recentAuthenticationTTL + time.Second)
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationAccountManage, AccountID: &account.ID,
	}); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale sensitive authorization error = %v, want ErrRecentAuthenticationRequired", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationAccountResourcesManage, AccountID: &account.ID,
	}); err != nil {
		t.Fatalf("ordinary resource mutation unexpectedly required recent authentication: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationIdentitySessionsView,
	}); err != nil {
		t.Fatalf("identity session view unexpectedly required recent authentication: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationIdentitySessionsManage,
	}); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale identity session mutation error = %v, want ErrRecentAuthenticationRequired", err)
	}

	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `UPDATE hosting_accounts SET status = 'suspended', updated_at = ? WHERE id = ?`,
			formatTime(current), string(account.ID))
		return err
	}); err != nil {
		t.Fatalf("suspend account: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationAccountResourcesManage, AccountID: &account.ID,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("suspended owner mutation error = %v, want ErrAuthorizationDenied", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationAccountView, AccountID: &account.ID,
	}); err != nil {
		t.Fatalf("suspended owner read: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: adminSubject, Action: AuthorizationAccountResourcesManage, AccountID: &account.ID,
	}); err != nil {
		t.Fatalf("administrator suspended-account mutation: %v", err)
	}

	if err := state.Write(ctx, func(executor store.Executor) error {
		if _, err := executor.ExecContext(ctx, `UPDATE hosting_accounts SET status = 'active', updated_at = ? WHERE id = ?`,
			formatTime(current), string(account.ID)); err != nil {
			return err
		}
		_, err := executor.ExecContext(ctx, `UPDATE account_memberships SET revoked_at = ?
			WHERE account_id = ? AND identity_id = ? AND revoked_at IS NULL`,
			formatTime(current), string(account.ID), string(owner.ID))
		return err
	}); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: ownerSubject, Action: AuthorizationAccountView, AccountID: &account.ID,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("revoked membership error = %v, want ErrAuthorizationDenied", err)
	}

	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `DELETE FROM platform_role_assignments WHERE identity_id = ? AND role = 'platform_admin'`, string(admin.ID))
		return err
	}); err != nil {
		t.Fatalf("remove platform role: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: adminSubject, Action: AuthorizationPlatformView,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("removed platform role error = %v, want ErrAuthorizationDenied", err)
	}
}

func TestAuthorizedAccountLookupAndObjectScopesDoNotCrossTenants(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 14, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }
	ownerOne := createTestIdentity(t, repository, "tenant-one@example.test")
	ownerTwo := createTestIdentity(t, repository, "tenant-two@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Tenant policy", Slug: "tenant-policy", Limits: testLimits(20), ActorID: &ownerOne.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	accountOne := createTestAccount(t, repository, ownerOne.ID, packageRecord.ID, "Tenant One", "tenant-one")
	accountTwo := createTestAccount(t, repository, ownerTwo.ID, packageRecord.ID, "Tenant Two", "tenant-two")
	subject := createAuthorizationSubject(t, repository, ownerOne)

	result, err := repository.GetAuthorizedHostingAccount(ctx, GetAuthorizedHostingAccountParams{
		Subject: subject, AccountID: accountOne.ID,
	})
	if err != nil || result.Account.ID != accountOne.ID || result.Authorization.MembershipRole == nil ||
		*result.Authorization.MembershipRole != MembershipOwner {
		t.Fatalf("own authorized account = %#v, %v", result, err)
	}
	if _, err := repository.GetAuthorizedHostingAccount(ctx, GetAuthorizedHostingAccountParams{
		Subject: subject, AccountID: accountTwo.ID,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("other account error = %v, want ErrAuthorizationDenied", err)
	}
	missingID, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if _, err := repository.GetAuthorizedHostingAccount(ctx, GetAuthorizedHostingAccountParams{
		Subject: subject, AccountID: missingID,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("missing account error = %v, want same ErrAuthorizationDenied", err)
	}

	domainTwo, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: accountTwo.ID, Name: "tenant-two.example", Target: DomainTargetSpec{Type: DomainTargetStatic}, ActorID: &ownerTwo.ID,
	})
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if _, err := repository.GetDomain(ctx, accountOne.ID, domainTwo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign domain under authorized account error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: subject, Action: AuthorizationAccountResourcesView, AccountID: &accountTwo.ID,
	}); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("foreign account for real domain error = %v, want ErrAuthorizationDenied", err)
	}
}

func TestAuthorizationRejectsRevokedSession(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 15, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return now }
	admin := createTestIdentity(t, repository, "revoked-admin@example.test")
	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{IdentityID: admin.ID, Role: PlatformAdministrator}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	subject := createAuthorizationSubject(t, repository, admin)
	if err := repository.RevokeSession(ctx, RevokeSessionParams{
		IdentityID: admin.ID, SessionID: subject.SessionID(), Reason: "test_revocation",
	}); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := repository.Authorize(ctx, AuthorizeParams{
		Subject: subject, Action: AuthorizationPlatformView,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session error = %v, want ErrSessionInvalid", err)
	}
}

func TestAuthorizationRejectsForgedServerContext(t *testing.T) {
	t.Parallel()

	repository, _ := newTestRepository(t)
	admin := createTestIdentity(t, repository, "forged-admin@example.test")
	if err := repository.GrantPlatformRole(context.Background(), GrantPlatformRoleParams{
		IdentityID: admin.ID, Role: PlatformAdministrator,
	}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	valid := createAuthorizationSubject(t, repository, admin)
	forged := AuthenticatedSession{
		Identity: admin,
		Session:  Session{ID: valid.SessionID(), IdentityID: admin.ID},
	}.AuthorizationSubject()
	if _, err := repository.Authorize(context.Background(), AuthorizeParams{
		Subject: forged, Action: AuthorizationPlatformView,
	}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("forged context error = %v, want ErrSessionInvalid", err)
	}
}

func createAuthorizationSubject(t *testing.T, repository *Repository, identity Identity) AuthorizationSubject {
	t.Helper()
	tokenHash := sha256.Sum256([]byte("authorization-session:" + string(identity.ID)))
	csrfHash := sha256.Sum256([]byte("authorization-csrf:" + string(identity.ID)))
	session, err := repository.CreateSession(context.Background(), CreateSessionParams{
		IdentityID: identity.ID, TokenHash: tokenHash[:], CSRFSecretHash: csrfHash[:],
		ExpiresAt: repository.timestamp().Add(passwordSessionAbsoluteTTL),
	})
	if err != nil {
		t.Fatalf("CreateSession(%s): %v", identity.ID, err)
	}
	return repository.newAuthorizationSubject(identity.ID, session.ID)
}

func allow(principals ...string) map[string]bool {
	allowed := make(map[string]bool, len(principals))
	for _, principal := range principals {
		allowed[principal] = true
	}
	return allowed
}
