// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSelfServiceContextReturnsOnlyOwnMembershipsAndCurrentUsage(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "owner-self-service@example.test")
	other := createTestIdentity(t, repository, "other-self-service@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Owner package", Slug: "owner-package", Limits: testLimits(3), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Owner account", "owner-account")
	_ = createTestAccount(t, repository, other.ID, packageRecord.ID, "Other account", "other-account")
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID, Name: "owner.example", Target: DomainTargetSpec{Type: DomainTargetStatic},
		ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	subject := createAuthorizationSubject(t, repository, owner)

	result, err := repository.GetSelfServiceContext(ctx, GetSelfServiceContextParams{Subject: subject})
	if err != nil || result.PlatformAdministrator || len(result.Accounts) != 1 {
		t.Fatalf("GetSelfServiceContext = %#v, %v", result, err)
	}
	got := result.Accounts[0]
	if got.ID != account.ID || got.MembershipRole != MembershipOwner || got.PackageID != packageRecord.ID ||
		got.PackageName != packageRecord.Name || got.PackageRevision != 1 ||
		got.EffectiveLimits.MaxDomains != 3 || got.DomainCount != 1 || got.HostReady {
		t.Fatalf("self-service account = %#v", got)
	}

	if err := repository.GrantPlatformRole(ctx, GrantPlatformRoleParams{
		IdentityID: owner.ID, Role: PlatformAdministrator, ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("GrantPlatformRole: %v", err)
	}
	result, err = repository.GetSelfServiceContext(ctx, GetSelfServiceContextParams{Subject: subject})
	if err != nil || !result.PlatformAdministrator || len(result.Accounts) != 1 {
		t.Fatalf("administrator self-service context = %#v, %v", result, err)
	}
}

func TestUpdateOwnProfileRequiresFreshSessionAndKeepsTargetBoundToSubject(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	current := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return current }
	owner := createTestIdentity(t, repository, "profile-owner@example.test")
	other := createTestIdentity(t, repository, "profile-other@example.test")
	subject := createAuthorizationSubject(t, repository, owner)

	updated, err := repository.UpdateOwnProfile(ctx, UpdateOwnProfileParams{
		Subject: subject, Email: "new-owner@example.test", DisplayName: "Updated Owner",
		Locale: LocaleGerman, RequestID: "profile-update", SourceAddress: "192.0.2.10",
	})
	if err != nil || updated.ID != owner.ID || updated.Email != "new-owner@example.test" ||
		updated.DisplayName != "Updated Owner" || updated.Locale != LocaleGerman {
		t.Fatalf("UpdateOwnProfile = %#v, %v", updated, err)
	}
	unchanged, err := repository.GetIdentity(ctx, other.ID)
	if err != nil || unchanged.Email != "profile-other@example.test" {
		t.Fatalf("other identity changed = %#v, %v", unchanged, err)
	}

	current = current.Add(recentAuthenticationTTL + time.Second)
	_, err = repository.UpdateOwnProfile(ctx, UpdateOwnProfileParams{
		Subject: subject, Email: updated.Email, DisplayName: "Too late", Locale: updated.Locale,
	})
	if !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale profile update error = %v, want ErrRecentAuthenticationRequired", err)
	}
}
