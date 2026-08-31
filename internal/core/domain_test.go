// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestDomainAndPathNormalization(t *testing.T) {
	t.Parallel()

	name, err := NormalizeDomainName(" WWW.BÜCHER.Example。 ")
	if err != nil {
		t.Fatalf("NormalizeDomainName: %v", err)
	}
	if name.Display != "www.bücher.example" || name.ASCII != "www.xn--bcher-kva.example" {
		t.Fatalf("normalized name = %#v", name)
	}
	base, err := normalizeDomainBase("WWW.BÜCHER.Example.")
	if err != nil {
		t.Fatalf("normalizeDomainBase: %v", err)
	}
	if base.Display != "bücher.example" || base.ASCII != "xn--bcher-kva.example" {
		t.Fatalf("normalized base = %#v", base)
	}

	invalidDomains := []string{
		"*.example.com",
		"example",
		"127.0.0.1",
		"example..com",
		"exa_mple.com",
		"https://example.com",
		"example.com:443",
	}
	for _, value := range invalidDomains {
		if _, err := NormalizeDomainName(value); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("NormalizeDomainName(%q) error = %v, want ErrInvalidInput", value, err)
		}
	}

	validRoots := []string{"public_html", "sites/example.com", "release-2026.08"}
	for _, value := range validRoots {
		if normalized, err := normalizeDocumentRoot(value); err != nil || normalized != value {
			t.Errorf("normalizeDocumentRoot(%q) = %q, %v", value, normalized, err)
		}
	}
	invalidRoots := []string{"/srv/site", "../other", "sites/../../other", "sites\\other", " sites", "sites//other", ".hidden"}
	for _, value := range invalidRoots {
		if _, err := normalizeDocumentRoot(value); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("normalizeDocumentRoot(%q) error = %v, want ErrInvalidInput", value, err)
		}
	}

	redirectURL, redirectHost, redirectPort, err := normalizeRedirectURL("HTTPS://ZIEL.BÜCHER.Example:443/start?q=1")
	if err != nil {
		t.Fatalf("normalizeRedirectURL: %v", err)
	}
	if redirectURL != "https://ziel.xn--bcher-kva.example/start?q=1" || redirectHost != "ziel.xn--bcher-kva.example" || redirectPort != "" {
		t.Fatalf("normalized redirect = %q, %q, %q", redirectURL, redirectHost, redirectPort)
	}
	for _, value := range []string{
		"http://example.com",
		"/relative",
		"https://user:pass@example.com",
		"https://example.com/path#fragment",
		"https://example.com/%0dheader",
		"https://example.com:0",
		"https://example.com:65536",
	} {
		if _, _, _, err := normalizeRedirectURL(value); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("normalizeRedirectURL(%q) error = %v, want ErrInvalidInput", value, err)
		}
	}
}

func TestDomainLifecyclePreservesHistoryAndTenantBoundaries(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "domains@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Domain package", Slug: "domain-package", Limits: testLimits(20), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Domains", "domains")
	otherAccount := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Other", "other")

	primary, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID,
		Name:      "WWW.BÜCHER.Example.",
		Target:    DomainTargetSpec{Type: DomainTargetStatic},
		ActorID:   &owner.ID,
		RequestID: "domain-primary",
	})
	if err != nil {
		t.Fatalf("CreateDomain primary: %v", err)
	}
	if primary.Name.ASCII != "xn--bcher-kva.example" || primary.Name.Display != "bücher.example" {
		t.Fatalf("primary name = %#v", primary.Name)
	}
	if primary.Status != DomainPending || primary.CanonicalMode != CanonicalPreferApex {
		t.Fatalf("primary status/canonical = %q/%q", primary.Status, primary.CanonicalMode)
	}
	if primary.Target.DocumentRoot == nil || primary.Target.DocumentRoot.RelativePath != "public_html" {
		t.Fatalf("primary target = %#v", primary.Target)
	}
	if !primary.TLS.Enabled || primary.TLS.Mode != TLSModeACME || primary.TLS.ChallengeType != TLSChallengeHTTP01 {
		t.Fatalf("primary TLS = %#v", primary.TLS)
	}
	if !slicesEqual(primary.TLS.Names, []string{"xn--bcher-kva.example", "www.xn--bcher-kva.example"}) {
		t.Fatalf("primary TLS names = %#v", primary.TLS.Names)
	}

	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: otherAccount.ID,
		Name:      "www.bücher.example",
		Target:    DomainTargetSpec{Type: DomainTargetStatic},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("global host collision error = %v, want ErrConflict", err)
	}
	if _, err := repository.GetDomain(ctx, otherAccount.ID, primary.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account GetDomain error = %v, want ErrNotFound", err)
	}
	if err := repository.RemoveDomain(ctx, RemoveDomainParams{
		AccountID: otherAccount.ID, DomainID: primary.ID, ActorID: &owner.ID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account RemoveDomain error = %v, want ErrNotFound", err)
	}
	unchangedPrimary, err := repository.GetDomain(ctx, account.ID, primary.ID)
	if err != nil || unchangedPrimary.Status != DomainPending {
		t.Fatalf("primary after cross-account remove = %#v, %v", unchangedPrimary, err)
	}

	phpDomain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID,
		Name:      "Shop.BÜCHER.Example",
		Target: DomainTargetSpec{
			Type:       DomainTargetPHP,
			PHPVersion: "8.4",
		},
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDomain PHP: %v", err)
	}
	if phpDomain.Target.DocumentRoot == nil || phpDomain.Target.DocumentRoot.RelativePath != "shop.xn--bcher-kva.example" {
		t.Fatalf("PHP default root = %#v", phpDomain.Target.DocumentRoot)
	}
	if phpDomain.Target.PHPVersion != "8.4" {
		t.Fatalf("PHP version = %q", phpDomain.Target.PHPVersion)
	}

	sharedDomain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID,
		Name:      "cdn.bücher.example",
		Target: DomainTargetSpec{
			Type:               DomainTargetStatic,
			RootMode:           DocumentRootShared,
			SharedWithDomainID: &primary.ID,
		},
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDomain shared root: %v", err)
	}
	if sharedDomain.Target.DocumentRoot == nil || sharedDomain.Target.DocumentRoot.ID != primary.Target.DocumentRoot.ID {
		t.Fatalf("shared root = %#v, primary root = %#v", sharedDomain.Target.DocumentRoot, primary.Target.DocumentRoot)
	}
	primary, err = repository.GetDomain(ctx, account.ID, primary.ID)
	if err != nil {
		t.Fatalf("reload primary: %v", err)
	}
	if primary.Target.DocumentRoot.ReferenceCount != 2 {
		t.Fatalf("shared root reference count = %d, want 2", primary.Target.DocumentRoot.ReferenceCount)
	}

	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID,
		Name:      "unsafe.example",
		Target: DomainTargetSpec{
			Type:         DomainTargetStatic,
			RootMode:     DocumentRootCustom,
			DocumentRoot: "../other-account",
		},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("path traversal error = %v, want ErrInvalidInput", err)
	}

	redirectDomain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID,
		Name:      "redirect.example",
		Target: DomainTargetSpec{
			Type: DomainTargetRedirect,
			Redirect: &RedirectSpec{
				StatusCode:         RedirectPermanent,
				TargetURL:          "https://Destination.Example/start",
				PreservePath:       true,
				PreserveQuery:      true,
				WildcardSubdomains: true,
			},
		},
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDomain wildcard redirect: %v", err)
	}
	if redirectDomain.Target.Redirect == nil || redirectDomain.Target.Redirect.TargetURL != "https://destination.example/start" ||
		redirectDomain.Target.Redirect.HostMode != RedirectHostBoth {
		t.Fatalf("redirect target = %#v", redirectDomain.Target.Redirect)
	}
	if redirectDomain.TLS.ChallengeType != TLSChallengeDNS01 || !slicesEqual(redirectDomain.TLS.Names, []string{
		"redirect.example", "www.redirect.example", "*.redirect.example",
	}) {
		t.Fatalf("wildcard TLS state = %#v", redirectDomain.TLS)
	}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: otherAccount.ID,
		Name:      "child.redirect.example",
		Target:    DomainTargetSpec{Type: DomainTargetStatic},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("wildcard overlap error = %v, want ErrConflict", err)
	}
	redirectDomain, err = repository.ReplaceDomainTarget(ctx, ReplaceDomainTargetParams{
		AccountID: account.ID,
		DomainID:  redirectDomain.ID,
		Target: DomainTargetSpec{
			Type:         DomainTargetStatic,
			RootMode:     DocumentRootCustom,
			DocumentRoot: "sites/redirect",
		},
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("replace wildcard redirect: %v", err)
	}
	if redirectDomain.TLS.ChallengeType != TLSChallengeHTTP01 || !slicesEqual(redirectDomain.TLS.Names, []string{
		"redirect.example", "www.redirect.example",
	}) {
		t.Fatalf("revised TLS intent = %#v", redirectDomain.TLS)
	}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: otherAccount.ID,
		Name:      "child.redirect.example",
		Target:    DomainTargetSpec{Type: DomainTargetStatic},
	}); err != nil {
		t.Fatalf("released wildcard route still conflicts: %v", err)
	}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID,
		Name:      "loop.example",
		Target: DomainTargetSpec{
			Type: DomainTargetRedirect,
			Redirect: &RedirectSpec{
				StatusCode: RedirectTemporary,
				TargetURL:  "https://www.loop.example/again",
			},
		},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("redirect loop error = %v, want ErrInvalidInput", err)
	}

	oldTargetID := phpDomain.Target.ID
	phpDomain, err = repository.ReplaceDomainTarget(ctx, ReplaceDomainTargetParams{
		AccountID: account.ID,
		DomainID:  phpDomain.ID,
		Target: DomainTargetSpec{
			Type:         DomainTargetPHP,
			RootMode:     DocumentRootCustom,
			DocumentRoot: "sites/shop",
			PHPVersion:   "8.5",
		},
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("ReplaceDomainTarget: %v", err)
	}
	if phpDomain.Target.ID == oldTargetID || phpDomain.Target.DocumentRoot.RelativePath != "sites/shop" || phpDomain.Target.PHPVersion != "8.5" {
		t.Fatalf("replacement target = %#v", phpDomain.Target)
	}
	var targetHistory, supersededTargets int
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(superseded_at)
			FROM domain_targets
			WHERE account_id = ? AND domain_id = ?`, string(account.ID), string(phpDomain.ID)).Scan(&targetHistory, &supersededTargets)
	}); err != nil {
		t.Fatalf("read target history: %v", err)
	}
	if targetHistory != 2 || supersededTargets != 1 {
		t.Fatalf("target history = %d/%d, want 2/1", targetHistory, supersededTargets)
	}

	if err := repository.RemoveDomain(ctx, RemoveDomainParams{
		AccountID: account.ID, DomainID: primary.ID, ActorID: &owner.ID,
	}); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	removed, err := repository.GetDomain(ctx, account.ID, primary.ID)
	if err != nil {
		t.Fatalf("GetDomain removed: %v", err)
	}
	if removed.Status != DomainRemoved || removed.RemovedAt == nil || removed.Target.DocumentRoot == nil {
		t.Fatalf("removed domain = %#v", removed)
	}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: otherAccount.ID,
		Name:      "bücher.example",
		Target:    DomainTargetSpec{Type: DomainTargetStatic},
	}); err != nil {
		t.Fatalf("reuse released host: %v", err)
	}
	visible, err := repository.ListDomains(ctx, account.ID, false)
	if err != nil {
		t.Fatalf("ListDomains visible: %v", err)
	}
	all, err := repository.ListDomains(ctx, account.ID, true)
	if err != nil {
		t.Fatalf("ListDomains all: %v", err)
	}
	if len(all) != len(visible)+1 {
		t.Fatalf("domain list sizes visible/all = %d/%d", len(visible), len(all))
	}

	if err := state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, "DELETE FROM domains WHERE id = ?", string(primary.ID))
		return err
	}); err == nil {
		t.Fatal("database allowed hard deletion of a domain")
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func TestDomainPackageLimitAndAppliedStateHistory(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "limits@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "One domain", Slug: "one-domain", Limits: testLimits(1), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Limited", "limited")
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID, Name: "one.example", Target: DomainTargetSpec{Type: DomainTargetStatic},
	}); err != nil {
		t.Fatalf("CreateDomain first: %v", err)
	}
	if _, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID, Name: "two.example", Target: DomainTargetSpec{Type: DomainTargetStatic},
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("domain package limit error = %v, want ErrConflict", err)
	}

	desiredOne, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: account.ID, Document: map[string]any{"version": 1}, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDesiredStateRevision 1: %v", err)
	}
	desiredTwo, err := repository.CreateDesiredStateRevision(ctx, CreateDesiredStateRevisionParams{
		AccountID: account.ID, Document: map[string]any{"version": 2}, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDesiredStateRevision 2: %v", err)
	}
	operation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "config.apply", RetryClass: RetrySafe, RequestID: "apply-1",
	})
	if err != nil {
		t.Fatalf("CreateOperation: %v", err)
	}
	digestOne := sha256.Sum256([]byte("config-one"))
	first, err := repository.RecordAppliedStateRevision(ctx, RecordAppliedStateRevisionParams{
		AccountID: account.ID, DesiredStateRevisionID: desiredOne.ID, OperationID: &operation.ID,
		ConfigDigest: digestOne[:], ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("RecordAppliedStateRevision 1: %v", err)
	}
	replayed, err := repository.RecordAppliedStateRevision(ctx, RecordAppliedStateRevisionParams{
		AccountID: account.ID, DesiredStateRevisionID: desiredOne.ID, OperationID: &operation.ID,
		ConfigDigest: digestOne[:], ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("replay RecordAppliedStateRevision: %v", err)
	}
	if replayed.ID != first.ID {
		t.Fatalf("replayed applied revision ID = %s, want %s", replayed.ID, first.ID)
	}
	changedDigest := sha256.Sum256([]byte("changed-on-replay"))
	if _, err := repository.RecordAppliedStateRevision(ctx, RecordAppliedStateRevisionParams{
		AccountID: account.ID, DesiredStateRevisionID: desiredOne.ID, OperationID: &operation.ID,
		ConfigDigest: changedDigest[:], ActorID: &owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay error = %v, want ErrConflict", err)
	}
	digestTwo := sha256.Sum256([]byte("config-two"))
	second, err := repository.RecordAppliedStateRevision(ctx, RecordAppliedStateRevisionParams{
		AccountID: account.ID, DesiredStateRevisionID: desiredTwo.ID, ConfigDigest: digestTwo[:], ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("RecordAppliedStateRevision 2: %v", err)
	}
	current, err := repository.CurrentAppliedStateRevision(ctx, account.ID)
	if err != nil {
		t.Fatalf("CurrentAppliedStateRevision: %v", err)
	}
	if current.ID != second.ID || !bytes.Equal(current.ConfigDigest, digestTwo[:]) {
		t.Fatalf("current applied revision = %#v", current)
	}
	var firstStatus string
	var firstSuperseded bool
	var appliedRevisionCount int
	if err := state.Read(ctx, func(reader store.Reader) error {
		if err := reader.QueryRowContext(ctx, `
			SELECT status, superseded_at IS NOT NULL
			FROM applied_state_revisions WHERE id = ?`, string(first.ID)).Scan(&firstStatus, &firstSuperseded); err != nil {
			return err
		}
		return reader.QueryRowContext(ctx, `SELECT count(*) FROM applied_state_revisions`).Scan(&appliedRevisionCount)
	}); err != nil {
		t.Fatalf("read first applied revision: %v", err)
	}
	if firstStatus != string(AppliedStateSuperseded) || !firstSuperseded {
		t.Fatalf("first applied status = %q/%v", firstStatus, firstSuperseded)
	}
	if appliedRevisionCount != 2 {
		t.Fatalf("applied revision count = %d, want 2", appliedRevisionCount)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
}

func createTestAccount(t *testing.T, repository *Repository, ownerID, packageID ID, name, slug string) HostingAccount {
	t.Helper()
	account, err := repository.CreateHostingAccount(context.Background(), CreateHostingAccountParams{
		Name: name, Slug: slug, OwnerIdentityID: ownerID, PackageID: packageID, ActorID: &ownerID,
	})
	if err != nil {
		t.Fatalf("CreateHostingAccount(%q): %v", slug, err)
	}
	return account
}
