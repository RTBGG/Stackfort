// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/store"
)

func TestOCIApplicationDraftLifecycleIsRevisionFencedAndTenantScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, state := newTestRepository(t)
	owner := createTestIdentity(t, repository, "oci-owner@example.test")
	packageRecord := createOCIApplicationTestPackage(t, repository, owner, "oci-lifecycle", 2, true)
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI lifecycle", "oci-lifecycle")
	other := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI other", "oci-other")

	created, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: account.ID, Name: "Web application", Slug: "web",
		Spec: digestOCIApplicationSpec("a"), ActorID: owner.ID, RequestID: "oci-create",
	})
	if err != nil {
		t.Fatalf("CreateOCIApplication: %v", err)
	}
	if created.Status != OCIApplicationDraft || created.Revision != 1 || created.AppliedRevision != nil {
		t.Fatalf("created application = %#v", created)
	}
	if _, err := repository.GetOCIApplication(ctx, other.ID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account read error = %v", err)
	}

	updatedSpec := ociapps.Spec{
		Source: ociapps.Source{
			Kind: ociapps.SourceContainerfile, BuildContext: "apps/web", ContainerfilePath: "Containerfile",
		},
		InternalPort: 9000,
		Health: ociapps.HealthCheck{
			Kind: ociapps.HealthTCP, IntervalSeconds: 10, TimeoutSeconds: 2, Retries: 2,
		},
	}
	updated, err := repository.UpdateOCIApplicationDraft(ctx, UpdateOCIApplicationDraftParams{
		AccountID: account.ID, ApplicationID: created.ID, ExpectedRevision: 1,
		Name: "Worker application", Slug: "worker", Spec: updatedSpec,
		ActorID: owner.ID, RequestID: "oci-update",
	})
	if err != nil {
		t.Fatalf("UpdateOCIApplicationDraft: %v", err)
	}
	if updated.Revision != 2 || updated.Name != "Worker application" || updated.Slug != "worker" || updated.Spec != updatedSpec {
		t.Fatalf("updated application = %#v", updated)
	}
	if _, err := repository.UpdateOCIApplicationDraft(ctx, UpdateOCIApplicationDraftParams{
		AccountID: account.ID, ApplicationID: created.ID, ExpectedRevision: 1,
		Name: "Stale", Slug: "stale", Spec: updatedSpec, ActorID: owner.ID, RequestID: "oci-stale",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	applications, err := repository.ListOCIApplications(ctx, account.ID)
	if err != nil || len(applications) != 1 || applications[0].ID != created.ID {
		t.Fatalf("ListOCIApplications = %#v, %v", applications, err)
	}
	if err := repository.RemoveOCIApplicationDraft(ctx, RemoveOCIApplicationDraftParams{
		AccountID: account.ID, ApplicationID: created.ID, ExpectedRevision: 2,
		ActorID: owner.ID, RequestID: "oci-remove",
	}); err != nil {
		t.Fatalf("RemoveOCIApplicationDraft: %v", err)
	}
	applications, err = repository.ListOCIApplications(ctx, account.ID)
	if err != nil || len(applications) != 0 {
		t.Fatalf("applications after removal = %#v, %v", applications, err)
	}
	var retainedStatus string
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `SELECT status FROM oci_applications WHERE id = ?`, string(created.ID)).Scan(&retainedStatus)
	}); err != nil || retainedStatus != string(OCIApplicationDeleted) {
		t.Fatalf("retained status = %q, %v", retainedStatus, err)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestOCIApplicationsEnforceClosedSpecFeatureAndPackageLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := newTestRepository(t)
	owner := createTestIdentity(t, repository, "oci-limits@example.test")
	disabledPackage := createOCIApplicationTestPackage(t, repository, owner, "oci-disabled", 1, false)
	disabledAccount := createTestAccount(t, repository, owner.ID, disabledPackage.ID, "OCI disabled", "oci-disabled")
	if _, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: disabledAccount.ID, Name: "Disabled", Slug: "disabled",
		Spec: digestOCIApplicationSpec("b"), ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled feature error = %v", err)
	}

	enabledPackage := createOCIApplicationTestPackage(t, repository, owner, "oci-enabled", 1, true)
	enabledAccount := createTestAccount(t, repository, owner.ID, enabledPackage.ID, "OCI enabled", "oci-enabled")
	first, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: enabledAccount.ID, Name: "First", Slug: "first",
		Spec: digestOCIApplicationSpec("c"), ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: enabledAccount.ID, Name: "Overflow", Slug: "overflow",
		Spec: digestOCIApplicationSpec("d"), ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("package limit error = %v", err)
	}
	if err := repository.RemoveOCIApplicationDraft(ctx, RemoveOCIApplicationDraftParams{
		AccountID: enabledAccount.ID, ApplicationID: first.ID, ExpectedRevision: 1, ActorID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	invalid := digestOCIApplicationSpec("e")
	invalid.Source.ImageReference = "registry.example/app:latest"
	if _, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: enabledAccount.ID, Name: "Tagged", Slug: "tagged", Spec: invalid, ActorID: owner.ID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("tagged image error = %v", err)
	}

	uniquePackage := createOCIApplicationTestPackage(t, repository, owner, "oci-unique", 2, true)
	uniqueAccount := createTestAccount(t, repository, owner.ID, uniquePackage.ID, "OCI unique", "oci-unique")
	if _, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: uniqueAccount.ID, Name: "First slug", Slug: "same-slug",
		Spec: digestOCIApplicationSpec("1"), ActorID: owner.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: uniqueAccount.ID, Name: "Duplicate slug", Slug: "same-slug",
		Spec: digestOCIApplicationSpec("2"), ActorID: owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug error = %v", err)
	}

	limits := testLimits(1)
	limits.MaxOCIApplications = ociapps.MaximumApplicationsPerAccount + 1
	if _, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Excessive OCI", Slug: "excessive-oci", Limits: limits, ActorID: &owner.ID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("excessive package limit error = %v", err)
	}
}

func TestOCIDomainTargetRequiresActiveAccountOwnedApplication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, state := newTestRepository(t)
	owner := createTestIdentity(t, repository, "oci-domain@example.test")
	packageRecord := createOCIApplicationTestPackage(t, repository, owner, "oci-domain", 2, true)
	first := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI first", "oci-domain-first")
	second := createTestAccount(t, repository, owner.ID, packageRecord.ID, "OCI second", "oci-domain-second")
	application, err := repository.CreateOCIApplication(ctx, CreateOCIApplicationParams{
		AccountID: first.ID, Name: "Web", Slug: "web", Spec: digestOCIApplicationSpec("f"), ActorID: owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := DomainTargetSpec{Type: DomainTargetOCIApplication, ApplicationID: &application.ID}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: first.ID, Name: "draft.example.test", Target: target, ActorID: &owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("draft application target error = %v", err)
	}
	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			UPDATE oci_applications SET status = 'active', applied_revision = revision WHERE id = ?`, string(application.ID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	domain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: first.ID, Name: "app.example.test", Target: target, ActorID: &owner.ID,
	})
	if err != nil || domain.Target.ApplicationID == nil || *domain.Target.ApplicationID != application.ID {
		t.Fatalf("active application domain = %#v, %v", domain, err)
	}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: second.ID, Name: "cross-account.example.test", Target: target, ActorID: &owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-account target error = %v", err)
	}

	staticDomain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: second.ID, Name: "database-trigger.example.test",
		Target: DomainTargetSpec{Type: DomainTargetStatic}, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	unsafeTargetID, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(repository.timestamp())
	err = state.Write(ctx, func(executor store.Executor) error {
		if _, err := executor.ExecContext(ctx, `
			UPDATE domain_targets SET superseded_at = ?
			WHERE account_id = ? AND domain_id = ? AND superseded_at IS NULL`,
			now, string(second.ID), string(staticDomain.ID)); err != nil {
			return err
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO domain_targets (
				id, account_id, domain_id, target_type, application_id,
				created_at, created_by_identity_id
			) VALUES (?, ?, ?, 'oci_application', ?, ?, ?)`,
			string(unsafeTargetID), string(second.ID), string(staticDomain.ID),
			string(application.ID), now, string(owner.ID))
		return err
	})
	if err == nil {
		t.Fatal("database accepted a cross-account OCI domain target")
	}
	var currentTargets int
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM domain_targets
			WHERE account_id = ? AND domain_id = ? AND superseded_at IS NULL`,
			string(second.ID), string(staticDomain.ID)).Scan(&currentTargets)
	}); err != nil || currentTargets != 1 {
		t.Fatalf("rolled-back current target count = %d, %v", currentTargets, err)
	}
}

func createOCIApplicationTestPackage(
	t *testing.T, repository *Repository, owner Identity, slug string, limit int64, enabled bool,
) Package {
	t.Helper()
	limits := testLimits(3)
	limits.MaxOCIApplications = limit
	limits.Features.OCIApplications = enabled
	packageRecord, err := repository.CreatePackage(context.Background(), CreatePackageParams{
		Name: "OCI package " + slug, Slug: slug + "-package", Limits: limits,
		ActorID: &owner.ID, RequestID: "create-" + slug,
	})
	if err != nil {
		t.Fatal(err)
	}
	return packageRecord
}

func digestOCIApplicationSpec(digestCharacter string) ociapps.Spec {
	return ociapps.Spec{
		Source: ociapps.Source{
			Kind:           ociapps.SourceImageDigest,
			ImageReference: "registry.example/stackfort/app@sha256:" + strings.Repeat(digestCharacter, 64),
		},
		InternalPort: 8080,
		Health: ociapps.HealthCheck{
			Kind: ociapps.HealthHTTP, Path: "/health", IntervalSeconds: 30, TimeoutSeconds: 5, Retries: 3,
		},
	}
}
