// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDomainWAFExceptionIsPackageBoundReplaySafeAndSoftRemoved(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "waf-exception@example.test")
	limits := testLimits(2)
	limits.Features.WAFExceptions = true
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "WAF exceptions", Slug: "waf-exceptions", Limits: limits, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "WAF", "waf-exceptions")
	domain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID, Name: "waf-exception.example",
		Target: DomainTargetSpec{Type: DomainTargetStatic}, WAFMode: WAFModeBlockingPL1,
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	createOperation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "domain.lifecycle.apply",
		RetryClass: RetrySafe, RequestID: "waf-exception-create", IdempotencyKey: "waf-exception-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	params := CreateDomainWAFExceptionParams{
		AccountID: account.ID, DomainID: domain.ID, ExceptionID: createOperation.ID,
		RuleID: 941100, RequestPath: "/search", Parameter: "q", ExpiresAt: expiresAt,
		OperationID: createOperation.ID, ActorID: &owner.ID, RequestID: "waf-exception-create",
	}
	created, err := repository.CreateDomainWAFException(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.CreateDomainWAFException(ctx, params)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("replay = %#v / %v", replayed, err)
	}
	reloaded, err := repository.GetDomain(ctx, account.ID, domain.ID)
	if err != nil || len(reloaded.WAF.Exceptions) != 1 || reloaded.WAF.Exceptions[0].Parameter != "q" {
		t.Fatalf("GetDomain exceptions = %#v / %v", reloaded.WAF.Exceptions, err)
	}
	removeOperation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID: &account.ID, ActorID: &owner.ID, Kind: "domain.lifecycle.apply",
		RetryClass: RetrySafe, RequestID: "waf-exception-remove", IdempotencyKey: "waf-exception-remove",
	})
	if err != nil {
		t.Fatal(err)
	}
	remove := RemoveDomainWAFExceptionParams{
		AccountID: account.ID, DomainID: domain.ID, ExceptionID: created.ID,
		OperationID: removeOperation.ID, ActorID: &owner.ID, RequestID: "waf-exception-remove",
	}
	if err := repository.RemoveDomainWAFException(ctx, remove); err != nil {
		t.Fatal(err)
	}
	if err := repository.RemoveDomainWAFException(ctx, remove); err != nil {
		t.Fatalf("remove replay: %v", err)
	}
	listed, err := repository.ListDomainWAFExceptions(ctx, account.ID, domain.ID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("active exceptions after remove = %#v / %v", listed, err)
	}
	if err := repository.VerifyAuditChain(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDomainWAFExceptionRejectsFreeFormAndDisabledFeature(t *testing.T) {
	t.Parallel()
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "waf-disabled@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "No exceptions", Slug: "no-waf-exceptions", Limits: testLimits(1), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "No WAF", "no-waf")
	domain, err := repository.CreateDomain(ctx, CreateDomainParams{
		AccountID: account.ID, Name: "no-waf.example", Target: DomainTargetSpec{Type: DomainTargetStatic},
		WAFMode: WAFModeDetectionOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID: &account.ID, Kind: "domain.lifecycle.apply", RetryClass: RetrySafe,
		RequestID: "disabled", IdempotencyKey: "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateDomainWAFException(ctx, CreateDomainWAFExceptionParams{
		AccountID: account.ID, DomainID: domain.ID, ExceptionID: operation.ID,
		RuleID: 941100, RequestPath: "/.*", ExpiresAt: time.Now().UTC().Add(time.Hour),
		OperationID: operation.ID,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("free-form scope error = %v", err)
	}
	_, err = repository.CreateDomainWAFException(ctx, CreateDomainWAFExceptionParams{
		AccountID: account.ID, DomainID: domain.ID, ExceptionID: operation.ID,
		RuleID: 941100, RequestPath: "/search", ExpiresAt: time.Now().UTC().Add(time.Hour),
		OperationID: operation.ID,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled feature error = %v", err)
	}
}
